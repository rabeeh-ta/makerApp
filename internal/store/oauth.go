package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// MCP sign-in (OAuth 2.1): the AI clients, each user's grants to them, the
// one-time codes and the tokens. Codes and tokens are stored hashed.

// ErrTokenReused means a refresh token was exchanged twice: someone copied it.
// The grant is revoked when that happens.
var ErrTokenReused = errors.New("refresh token reused")

type OAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	FetchedAt    int64 // clients identified by a metadata document: when it was fetched; 0 for registered ones
}

func (s *Store) SaveOAuthClient(ctx context.Context, c *OAuthClient) error {
	uris, _ := json.Marshal(c.RedirectURIs)
	_, err := s.db.ExecContext(ctx, `INSERT INTO oauth_clients (id, name, redirect_uris, created_at, fetched_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, redirect_uris = excluded.redirect_uris, fetched_at = excluded.fetched_at`,
		c.ID, c.Name, string(uris), iso(now()), iso(c.FetchedAt))
	return err
}

func (s *Store) OAuthClient(ctx context.Context, id string) (*OAuthClient, error) {
	c := &OAuthClient{}
	var uris string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, redirect_uris, COALESCE(unixepoch(fetched_at), 0) FROM oauth_clients WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &uris, &c.FetchedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return c, json.Unmarshal([]byte(uris), &c.RedirectURIs)
}

// OAuthGrant is one user's permission for one AI client.
type OAuthGrant struct {
	ID         string
	UserID     string
	ClientID   string
	ClientName string
	CreatedAt  int64
	LastUsedAt int64
}

const grantCols = `id, user_id, client_id, client_name, unixepoch(created_at), COALESCE(unixepoch(last_used_at), 0)`

func scanGrant(row interface{ Scan(...any) error }) (*OAuthGrant, error) {
	g := &OAuthGrant{}
	err := row.Scan(&g.ID, &g.UserID, &g.ClientID, &g.ClientName, &g.CreatedAt, &g.LastUsedAt)
	return g, notFound(err)
}

// Grant returns the user's grant for the client, making it on first approval.
func (s *Store) Grant(ctx context.Context, userID, clientID, clientName string) (*OAuthGrant, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO oauth_grants (id, user_id, client_id, client_name, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, client_id) DO UPDATE SET client_name = excluded.client_name`,
		NewID(), userID, clientID, clientName, iso(now())); err != nil {
		return nil, err
	}
	return scanGrant(s.db.QueryRowContext(ctx, `SELECT `+grantCols+` FROM oauth_grants WHERE user_id = ? AND client_id = ?`, userID, clientID))
}

func (s *Store) UserGrants(ctx context.Context, userID string) ([]*OAuthGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+grantCols+` FROM oauth_grants WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OAuthGrant
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RevokeGrant disconnects the client; its codes and tokens stop working at once.
func (s *Store) RevokeGrant(ctx context.Context, userID, grantID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_grants WHERE id = ? AND user_id = ?`, grantID, userID)
	return err
}

// TouchGrant records use, at most once a minute so tool calls don't each write.
func (s *Store) TouchGrant(ctx context.Context, grantID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE oauth_grants SET last_used_at = ?
		WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)`, iso(now()), grantID, iso(now()-60))
	return err
}

// OAuthCode is what an approval issues: redeemable once, by the same client,
// redirect URI and PKCE verifier.
type OAuthCode struct {
	GrantID     string
	RedirectURI string
	Challenge   string
	Resource    string
	ExpiresAt   int64
}

func (s *Store) CreateOAuthCode(ctx context.Context, c OAuthCode, ttl time.Duration) (string, error) {
	code := RandomToken(32)
	_, err := s.db.ExecContext(ctx, `INSERT INTO oauth_codes (code_hash, grant_id, redirect_uri, challenge, resource, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, HashToken(code), c.GrantID, c.RedirectURI, c.Challenge, c.Resource, iso(time.Now().Add(ttl).Unix()))
	return code, err
}

// UseOAuthCode redeems a code, deleting it so it can't be used again. An
// expired code is ErrNotFound.
func (s *Store) UseOAuthCode(ctx context.Context, code string) (*OAuthCode, *OAuthGrant, error) {
	c := &OAuthCode{}
	err := s.db.QueryRowContext(ctx, `DELETE FROM oauth_codes WHERE code_hash = ?
		RETURNING grant_id, redirect_uri, challenge, resource, unixepoch(expires_at)`, HashToken(code)).
		Scan(&c.GrantID, &c.RedirectURI, &c.Challenge, &c.Resource, &c.ExpiresAt)
	if err != nil {
		return nil, nil, notFound(err)
	}
	if c.ExpiresAt < now() {
		return nil, nil, ErrNotFound
	}
	g, err := scanGrant(s.db.QueryRowContext(ctx, `SELECT `+grantCols+` FROM oauth_grants WHERE id = ?`, c.GrantID))
	return c, g, err
}

// IssueOAuthTokens makes an access and a refresh token for the grant, and
// clears the grant's expired ones.
func (s *Store) IssueOAuthTokens(ctx context.Context, grantID string, accessTTL, refreshTTL time.Duration) (access, refresh string, err error) {
	access, refresh = RandomToken(32), RandomToken(32)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	for _, q := range []struct {
		token, kind string
		ttl         time.Duration
	}{{access, "access", accessTTL}, {refresh, "refresh", refreshTTL}} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_tokens (token_hash, grant_id, kind, expires_at) VALUES (?, ?, ?, ?)`,
			HashToken(q.token), grantID, q.kind, iso(time.Now().Add(q.ttl).Unix())); err != nil {
			return "", "", err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE grant_id = ? AND expires_at < ?`, grantID, iso(now())); err != nil {
		return "", "", err
	}
	return access, refresh, tx.Commit()
}

// OAuthAccess returns the grant behind a valid access token, with the token's expiry.
func (s *Store) OAuthAccess(ctx context.Context, token string) (*OAuthGrant, int64, error) {
	var exp int64
	g := &OAuthGrant{}
	err := s.db.QueryRowContext(ctx, `SELECT g.id, g.user_id, g.client_id, g.client_name, unixepoch(g.created_at),
		COALESCE(unixepoch(g.last_used_at), 0), unixepoch(t.expires_at)
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
		WHERE t.token_hash = ? AND t.kind = 'access' AND t.expires_at > ?`, HashToken(token), iso(now())).
		Scan(&g.ID, &g.UserID, &g.ClientID, &g.ClientName, &g.CreatedAt, &g.LastUsedAt, &exp)
	return g, exp, notFound(err)
}

// UseRefreshToken exchanges a refresh token once. A second exchange of the
// same token revokes the grant and returns ErrTokenReused.
func (s *Store) UseRefreshToken(ctx context.Context, token string) (*OAuthGrant, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var grantID string
	var used sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT grant_id, used_at FROM oauth_tokens
		WHERE token_hash = ? AND kind = 'refresh' AND expires_at > ?`, HashToken(token), iso(now())).Scan(&grantID, &used)
	if err != nil {
		return nil, notFound(err)
	}
	if used.Valid {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE id = ?`, grantID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, ErrTokenReused
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_tokens SET used_at = ? WHERE token_hash = ?`, iso(now()), HashToken(token)); err != nil {
		return nil, err
	}
	g, err := scanGrant(tx.QueryRowContext(ctx, `SELECT `+grantCols+` FROM oauth_grants WHERE id = ?`, grantID))
	if err != nil {
		return nil, err
	}
	return g, tx.Commit()
}
