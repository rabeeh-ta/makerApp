package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"makerapp/internal/engine"
	"makerapp/internal/fetch"
	"makerapp/internal/library"
	"makerapp/internal/secret"
	"makerapp/internal/store"
	"makerapp/internal/web"
)

// TestScreensRebuildKeepsRows checks the migration that allows the gif layout
// against a database that already has a screen.
func TestScreensRebuildKeepsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	last := slices.IndexFunc(migrations, func(m string) bool { return strings.Contains(m, "screens_new") })
	for _, m := range migrations[:last] {
		if _, err := db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, last)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO screens (id, user_id, app_id, app_pos, name, layout, spec, updated_at, created_at)
		VALUES ('s1', 'u1', 'a1', 3, 'Mine', 'spark', '{"title":"x"}', 'now', 'then')`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var appID, name, layout, spec string
	var pos int
	err = db.QueryRow(`SELECT app_id, app_pos, name, layout, spec FROM screens WHERE id = 's1'`).Scan(&appID, &pos, &name, &layout, &spec)
	if err != nil || appID != "a1" || pos != 3 || name != "Mine" || layout != "spark" || spec != `{"title":"x"}` {
		t.Fatalf("after the rebuild: %v %q %d %q %q %q", err, appID, pos, name, layout, spec)
	}
	if _, err := db.Exec(`UPDATE screens SET layout = 'gif' WHERE id = 's1'`); err != nil {
		t.Fatalf("gif layout refused: %v", err)
	}
	if _, err := db.Exec(`UPDATE screens SET layout = 'nope' WHERE id = 's1'`); err == nil {
		t.Fatal("the layout check is gone")
	}
}

// TestGIFScreen walks a GIF from upload to what a board downloads.
func TestGIFScreen(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := store.New(db)
	box, _ := secret.New("test")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(st, box, fetch.New(true), time.UTC, log)
	srv := httptest.NewServer(web.New(web.Config{AllowSignup: true}, st, eng, library.New(st, eng), log).Handler())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	upload := func(content []byte) (int, string) {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		w, _ := mw.CreateFormFile("file", "a.gif")
		w.Write(content)
		mw.Close()
		resp, err := client.Post(srv.URL+"/api/animations", mw.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var r struct{ Hash string }
		_ = json.NewDecoder(resp.Body).Decode(&r)
		return resp.StatusCode, r.Hash
	}
	fetchFile := func(path string) (int, string, []byte) {
		t.Helper()
		resp, err := http.Get(srv.URL + path) // no session: boards don't have one
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), b
	}

	pal := color.Palette{color.Black, color.White}
	g := &gif.GIF{}
	for n := range 3 {
		img := image.NewPaletted(image.Rect(0, 0, 60, 34), pal)
		img.Pix[n] = 1
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 10)
	}
	var picture bytes.Buffer
	if err := gif.EncodeAll(&picture, g); err != nil {
		t.Fatal(err)
	}

	if code, _ := upload(picture.Bytes()); code != http.StatusUnauthorized {
		t.Fatalf("signed out upload: %d, want 401", code)
	}
	resp, err := client.PostForm(srv.URL+"/api/signup", url.Values{"email": {"me@example.com"}, "password": {"password123"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if code, _ := upload([]byte("not a picture")); code != http.StatusBadRequest {
		t.Fatalf("upload of a non-GIF: %d, want 400", code)
	}
	code, hash := upload(picture.Bytes())
	if code != 200 || hash == "" {
		t.Fatalf("upload: %d %q", code, hash)
	}
	if again, same := upload(picture.Bytes()); again != 200 || same != hash {
		t.Fatalf("same GIF again: %d %q, want %q", again, same, hash)
	}

	resp, err = client.PostForm(srv.URL+"/api/screens/preview", url.Values{"layout": {"gif"}, "gif": {hash}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var preview struct{ Layout, GIF string }
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	if preview.Layout != "gif" || preview.GIF != hash {
		t.Fatalf("preview: %+v", preview)
	}

	// The GIFs app: added to the list, with a time and an order, and on the board's screens.
	addGIF := func(content []byte, filename string) (int, []byte) {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		w, _ := mw.CreateFormFile("file", filename)
		w.Write(content)
		mw.Close()
		resp, err := client.Post(srv.URL+"/api/gifs", mw.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	if code, b := addGIF(picture.Bytes(), "itachi eyes.gif"); code != 200 {
		t.Fatalf("add: %d %s", code, b)
	}
	if code, _ := addGIF(picture.Bytes(), "again.gif"); code != http.StatusBadRequest {
		t.Fatalf("the same GIF twice: %d, want 400", code)
	}
	g.Image[0].Pix[5] = 1
	var other bytes.Buffer
	if err := gif.EncodeAll(&other, g); err != nil {
		t.Fatal(err)
	}
	code, b := addGIF(other.Bytes(), "second.gif")
	if code != 200 {
		t.Fatalf("add second: %d %s", code, b)
	}
	var list struct {
		Items []struct {
			Hash, Name  string
			Hold, Bytes int
		}
	}
	if err := json.Unmarshal(b, &list); err != nil || len(list.Items) != 2 || list.Items[0].Name != "itachi eyes" || list.Items[0].Hold != 10 || list.Items[0].Bytes == 0 {
		t.Fatalf("list: %v %+v", err, list)
	}
	first, second := list.Items[0].Hash, list.Items[1].Hash
	// Reordered, one time out of range, and a hash that was never uploaded: it can't get in.
	order := fmt.Sprintf(`[{"Hash":%q,"Hold":1},{"Hash":"aaaaaaaaaaaa","Hold":5},{"Hash":%q,"Hold":45}]`, second, first)
	resp, err = client.PostForm(srv.URL+"/api/gifs/save", url.Values{"items": {order}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var userID string
	if err := db.QueryRow(`SELECT id FROM users`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	shown := eng.GIFScreens(context.Background(), userID)
	if len(shown) != 2 || shown[0].GIF != second || shown[0].Hold != 2 || shown[1].GIF != first || shown[1].Hold != 45 || shown[0].Layout != "gif" {
		t.Fatalf("on the board: %+v", shown)
	}

	// Saved as a real screen too: the database has to accept the layout.
	resp, err = client.PostForm(srv.URL+"/api/screens", url.Values{"app": {"new"}, "name": {"Clip"}, "layout": {"gif"}, "gif": {hash}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var saved struct{ Redirect, Error string }
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil || saved.Error != "" || saved.Redirect == "" {
		t.Fatalf("saving the screen: %v %+v", err, saved)
	}

	if code, kind, b := fetchFile("/anims/" + hash + ".anm"); code != 200 || kind != "application/octet-stream" || !strings.HasPrefix(string(b), "MKA1") {
		t.Fatalf("board file: %d %s %q", code, kind, b[:min(len(b), 8)])
	}
	if code, kind, b := fetchFile("/anims/" + hash + ".gif"); code != 200 || kind != "image/gif" || !strings.HasPrefix(string(b), "GIF8") {
		t.Fatalf("preview file: %d %s", code, kind)
	}
	for _, bad := range []string{"000000000000.anm", hash + ".exe", "nope.anm"} {
		if code, _, _ := fetchFile("/anims/" + bad); code != 404 {
			t.Errorf("GET /anims/%s: %d, want 404", bad, code)
		}
	}
}
