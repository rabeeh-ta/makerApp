package web

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"esphog/internal/engine"
	"esphog/internal/fw"
	"esphog/internal/store"
)

var tokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ---- browser flashing ----

func (s *Server) flashPage(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	board, ok := fw.BoardByID(r.URL.Query().Get("board"))
	if !ok {
		// Step 0: pick the board model.
		s.render(w, r, "flash", map[string]any{"Boards": fw.Boards})
		return
	}
	claim, err := s.st.CreateClaim(r.Context(), u.ID, 24*time.Hour)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	base := s.publicBase(r)
	s.render(w, r, "flash", map[string]any{
		"Board":        board,
		"Claim":        claim,
		"ManifestURL":  "/flash/manifest.json?claim=" + claim,
		"ServerURL":    wsURL(base),
		"Insecure":     strings.HasPrefix(base, "http://") && !strings.Contains(r.Host, "localhost") && !strings.HasPrefix(r.Host, "127."),
		"FWVersion":    fw.Version(),
		"LocalNetwork": isLocalHost(r.Host),
		"Available":    fw.Available(),
	})
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
	claim := r.URL.Query().Get("claim")
	deviceID, _ := s.st.ClaimDevice(r.Context(), claim, u.ID)
	if deviceID == "" {
		s.partial(w, "flash_waiting", map[string]any{"Claim": claim})
		return
	}
	d, err := s.st.UserDevice(r.Context(), u.ID, deviceID)
	if err != nil {
		s.partial(w, "flash_waiting", map[string]any{"Claim": claim})
		return
	}
	s.partial(w, "flash_done", d)
}

func isLocalHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
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

func (s *Server) pairPage(w http.ResponseWriter, r *http.Request) {
	id := strings.ToUpper(r.URL.Query().Get("device"))
	code := r.URL.Query().Get("code")
	data := map[string]any{"Device": id, "Code": code}
	if id != "" && code != "" {
		d, err := s.tryPair(r, id, code)
		switch {
		case err == nil:
			http.Redirect(w, r, "/devices/"+d.ID+"?paired=1", http.StatusSeeOther)
			return
		case errors.Is(err, errPairWaiting):
			data["Waiting"] = true
		default:
			data["Error"] = err.Error()
		}
	}
	s.render(w, r, "pair", data)
}

func (s *Server) pairStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.ToUpper(r.URL.Query().Get("device"))
	code := r.URL.Query().Get("code")
	d, err := s.tryPair(r, id, code)
	if err == nil {
		w.Header().Set("HX-Redirect", "/devices/"+d.ID+"?paired=1")
		return
	}
	data := map[string]any{"Device": id, "Code": code}
	if errors.Is(err, errPairWaiting) {
		data["Waiting"] = true
	} else {
		data["Error"] = err.Error()
	}
	s.partial(w, "pair_waiting", data)
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	code := strings.ReplaceAll(strings.TrimSpace(r.FormValue("code")), " ", "")
	fail := func(msg string) {
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, r, "pair", map[string]any{"Error": msg, "Code": code})
	}
	if !s.allow("pair:"+u.ID, 10, time.Minute) {
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
	http.Redirect(w, r, "/devices/"+d.ID+"?paired=1", http.StatusSeeOther)
}
