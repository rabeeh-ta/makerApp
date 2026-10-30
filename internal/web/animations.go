package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"makerapp/internal/anim"
)

// maxAnimationsPerUser keeps uploads from filling the database: each one stays for good.
const maxAnimationsPerUser = 50

// uploadAnimation converts a GIF for the board and answers with its name. The
// original isn't kept: the converted file is all a board or the preview needs.
func (s *Server) uploadAnimation(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, anim.MaxUpload+1024)
	f, _, err := r.FormFile("file")
	if err != nil {
		fail(w, http.StatusBadRequest, "Pick a GIF under 8 MB.")
		return
	}
	defer f.Close()
	if n, err := s.st.UserAnimationCount(r.Context(), u.ID); err != nil {
		failed(w, err)
		return
	} else if n >= maxAnimationsPerUser {
		fail(w, http.StatusBadRequest, "You've reached the limit of "+strconv.Itoa(maxAnimationsPerUser)+" GIFs.")
		return
	}
	data, err := anim.Encode(f)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	hash := anim.Hash(data)
	if failed(w, s.st.SaveAnimation(r.Context(), hash, u.ID, data)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": hash, "bytes": len(data)})
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
