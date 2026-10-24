package web

import (
	"bytes"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"makerapp/internal/engine"
	"makerapp/internal/fw"
	"makerapp/internal/store"
)

var tokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ---- browser flashing ----

// flashInfo is the flash page: the boards it can set up, and where boards
// installed from it will connect.
func (s *Server) flashInfo(w http.ResponseWriter, r *http.Request) {
	base := s.publicBase(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"Boards":    fw.Boards,
		"ServerURL": wsURL(base),
		"Insecure":  strings.HasPrefix(base, "http://") && !strings.Contains(r.Host, "localhost") && !strings.HasPrefix(r.Host, "127."),
		"FWVersion": fw.Version(),
		"Available": fw.Available(),
	})
}

// newClaim mints the one-time token written into the board being flashed, so
// it joins this account when it first connects.
func (s *Server) newClaim(w http.ResponseWriter, r *http.Request) {
	claim, err := s.st.CreateClaim(r.Context(), currentUser(r).ID, 24*time.Hour)
	if err != nil {
		fail(w, http.StatusInternalServerError, "server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"Claim": claim, "ManifestURL": "/flash/manifest.json?claim=" + claim})
}

func (s *Server) flashManifest(w http.ResponseWriter, r *http.Request) {
	claim := r.URL.Query().Get("claim")
	if !tokenRe.MatchString(claim) {
		http.Error(w, "missing claim", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, private")
	w.Write(fw.Manifest("/flash/config.bin?claim=" + claim))
}

func (s *Server) flashConfig(w http.ResponseWriter, r *http.Request) {
	claim := r.URL.Query().Get("claim")
	if !tokenRe.MatchString(claim) {
		http.Error(w, "missing claim", http.StatusBadRequest)
		return
	}
	img := fw.ConfigImage(wsURL(s.publicBase(r)), claim)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(img)
}

func (s *Server) flashPart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("part")
	if !fw.Parts[name] {
		http.NotFound(w, r)
		return
	}
	s.serveFirmwareFile(w, r, name)
}

func (s *Server) otaFirmware(w http.ResponseWriter, r *http.Request) {
	s.serveFirmwareFile(w, r, "firmware.bin")
}

func (s *Server) serveFirmwareFile(w http.ResponseWriter, r *http.Request, name string) {
	b, err := fw.File(name)
	if err != nil {
		http.Error(w, "firmware not built into this server (run make)", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if h := r.PathValue("hash"); h != "" && h == fw.Hash() {
		// Content-addressed URL: safe to cache forever.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// Unversioned or stale URL: never let a CDN keep it.
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}

// flashStatus is polled by the flash page until the new device checks in.
func (s *Server) flashStatus(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var device any
	if id, _ := s.st.ClaimDevice(r.Context(), r.URL.Query().Get("claim"), u.ID); id != "" {
		if d, err := s.st.UserDevice(r.Context(), u.ID, id); err == nil {
			device = map[string]string{"ID": d.ID, "Name": d.Name}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"Device": device})
}

// ---- pairing with the code shown on the device ----

var errPairWaiting = errors.New("waiting for device")

// tryPair claims device id for the user when the code matches what the device reported.
func (s *Server) tryPair(r *http.Request, id, code string) (*store.Device, error) {
	u := currentUser(r)
	d, err := s.st.Device(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errPairWaiting
	}
	if err != nil {
		return nil, err
	}
	if d.UserID == u.ID {
		return d, nil
	}
	if d.UserID != "" {
		return nil, errors.New("This device belongs to another account.")
	}
	if d.PairCode != code {
		if !s.eng.Online(id) {
			return nil, errPairWaiting
		}
		return nil, errors.New("That code doesn't match the one on the device screen.")
	}
	if err := s.claim(r, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Server) claim(r *http.Request, d *store.Device) error {
	u := currentUser(r)
	if err := s.st.ClaimDeviceFor(r.Context(), d.ID, u.ID, engine.DefaultName(d.ID)); err != nil {
		return err
	}
	s.eng.PushDevice(r.Context(), d.ID)
	return nil
}

// pairTry pairs the device from the link printed on its screen
// (/pair?device=&code=). The pair page polls it while the device connects.
func (s *Server) pairTry(w http.ResponseWriter, r *http.Request) {
	id := strings.ToUpper(r.FormValue("device"))
	d, err := s.tryPair(r, id, r.FormValue("code"))
	switch {
	case err == nil:
		redirect(w, "/devices/"+d.ID+"?paired=1")
	case errors.Is(err, errPairWaiting):
		writeJSON(w, http.StatusOK, map[string]any{"Waiting": true})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"Error": err.Error()})
	}
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	code := strings.ReplaceAll(strings.TrimSpace(r.FormValue("code")), " ", "")
	fail := func(msg string) { writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg}) }
	if !s.lim.Allow("pair:"+u.ID, 10, time.Minute) {
		fail("Too many attempts. Wait a minute.")
		return
	}
	if len(code) != 6 {
		fail("Enter the 6-digit code from the device screen.")
		return
	}
	d, err := s.st.UnclaimedDeviceByCode(r.Context(), code, time.Now().Add(-15*time.Minute))
	if err != nil || !s.eng.Online(d.ID) {
		fail("No device is showing that code right now. Check the screen and that it's online.")
		return
	}
	if err := s.claim(r, d); err != nil {
		fail("Could not pair the device. Try again.")
		return
	}
	redirect(w, "/devices/"+d.ID+"?paired=1")
}
