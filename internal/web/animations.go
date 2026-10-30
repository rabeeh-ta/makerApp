package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"makerapp/internal/anim"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

// maxAnimationsPerUser keeps uploads from filling the database: each one stays for good.
const maxAnimationsPerUser = 50

// convertUpload turns the uploaded GIF into a stored animation. On failure it
// has answered the request, and the hash is "".
func (s *Server) convertUpload(w http.ResponseWriter, r *http.Request) (hash, filename string) {
	u := currentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, anim.MaxUpload+1024)
	f, head, err := r.FormFile("file")
	if err != nil {
		fail(w, http.StatusBadRequest, "Pick a GIF under 8 MB.")
		return "", ""
	}
	defer f.Close()
	if n, err := s.st.UserAnimationCount(r.Context(), u.ID); err != nil {
		failed(w, err)
		return "", ""
	} else if n >= maxAnimationsPerUser {
		fail(w, http.StatusBadRequest, "You've reached the limit of "+strconv.Itoa(maxAnimationsPerUser)+" GIFs.")
		return "", ""
	}
	data, err := anim.Encode(f)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return "", ""
	}
	hash = anim.Hash(data)
	if failed(w, s.st.SaveAnimation(r.Context(), hash, u.ID, data)) {
		return "", ""
	}
	return hash, head.Filename
}

// uploadAnimation converts a GIF for the board and answers with its name. The
// original isn't kept: the converted file is all a board or the preview needs.
func (s *Server) uploadAnimation(w http.ResponseWriter, r *http.Request) {
	hash, _ := s.convertUpload(w, r)
	if hash == "" {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": hash, "bytes": s.st.AnimationSize(r.Context(), hash)})
}

// The GIFs app. Boards keep GIFs in 128 KB of storage, shared with games, and
// need some of it free to work, so the page stops at gifRoom.
const (
	gifRoom     = 112 * 1024
	maxGIFs     = 10
	defaultHold = 10
	minHold     = 2
	maxHold     = 300
)

func (s *Server) writeGIFs(w http.ResponseWriter, r *http.Request, items []store.GIFItem) {
	type row struct {
		store.GIFItem
		Bytes int
	}
	rows, used := []row{}, 0
	for _, it := range items {
		n := s.st.AnimationSize(r.Context(), it.Hash)
		rows = append(rows, row{it, n})
		used += n
	}
	writeJSON(w, http.StatusOK, map[string]any{"Items": rows, "Used": used, "Room": gifRoom, "Max": maxGIFs})
}

func (s *Server) gifs(w http.ResponseWriter, r *http.Request) {
	items, err := s.st.GIFPlaylist(r.Context(), currentUser(r).ID)
	if failed(w, err) {
		return
	}
	s.writeGIFs(w, r, items)
}

// addGIF converts an uploaded GIF and adds it to the end of the person's GIFs app.
func (s *Server) addGIF(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	items, err := s.st.GIFPlaylist(r.Context(), u.ID)
	if failed(w, err) {
		return
	}
	if len(items) >= maxGIFs {
		fail(w, http.StatusBadRequest, "That's "+strconv.Itoa(maxGIFs)+" GIFs, the most it holds. Remove one first.")
		return
	}
	hash, filename := s.convertUpload(w, r)
	if hash == "" {
		return
	}
	used := s.st.AnimationSize(r.Context(), hash)
	for _, it := range items {
		if it.Hash == hash {
			fail(w, http.StatusBadRequest, "That GIF is already in the list.")
			return
		}
		used += s.st.AnimationSize(r.Context(), it.Hash)
	}
	if used > gifRoom {
		fail(w, http.StatusBadRequest, "No room for that one on the board. Remove a GIF first.")
		return
	}
	name := render.ASCII(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if len(name) > 40 {
		name = name[:40]
	}
	if name == "" {
		name = "GIF"
	}
	items = append(items, store.GIFItem{Hash: hash, Name: name, Hold: defaultHold})
	if failed(w, s.st.SaveGIFPlaylist(r.Context(), u.ID, items)) {
		return
	}
	s.eng.PushGIFs(r.Context(), u.ID)
	s.writeGIFs(w, r, items)
}

// saveGIFs takes the list back in its new order, with new times and without
// what was removed. It can't add: a GIF only gets in by being uploaded.
func (s *Server) saveGIFs(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in []struct {
		Hash string
		Hold int
	}
	if json.Unmarshal([]byte(r.FormValue("items")), &in) != nil {
		fail(w, http.StatusBadRequest, "bad list")
		return
	}
	have, err := s.st.GIFPlaylist(r.Context(), u.ID)
	if failed(w, err) {
		return
	}
	items := []store.GIFItem{}
	for _, it := range in {
		i := slices.IndexFunc(have, func(g store.GIFItem) bool { return g.Hash == it.Hash })
		if i < 0 || slices.ContainsFunc(items, func(g store.GIFItem) bool { return g.Hash == it.Hash }) {
			continue
		}
		g := have[i]
		g.Hold = clamp(it.Hold, minHold, maxHold)
		items = append(items, g)
	}
	if failed(w, s.st.SaveGIFPlaylist(r.Context(), u.ID, items)) {
		return
	}
	s.eng.PushGIFs(r.Context(), u.ID)
	s.writeGIFs(w, r, items)
}

// animFile serves an animation: <hash>.anm to boards and <hash>.gif to the
// website's preview. Boards have no session, so the URL is public, like /games/.
// The name is the content's hash, so it can be cached for good.
func (s *Server) animFile(w http.ResponseWriter, r *http.Request) {
	name, kind, _ := strings.Cut(r.PathValue("file"), ".")
	if !anim.ValidHash(name) || (kind != "anm" && kind != "gif") {
		http.NotFound(w, r)
		return
	}
	data, err := s.st.Animation(r.Context(), name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := "application/octet-stream"
	if kind == "gif" {
		contentType = "image/gif"
		if data, err = anim.Preview(data); err != nil {
			failed(w, errors.New("can't read the animation: "+err.Error()))
			return
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
}
