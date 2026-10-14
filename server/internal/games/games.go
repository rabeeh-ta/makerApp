// Package games holds the downloadable Lua games, one script each; the API
// they can use is described in README.md. Boards fetch a game by ID and content
// hash, so editing a script here ships a new version of the game to every board
// that has it installed, without a firmware update.
package games

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"strings"
)

//go:embed *.lua
var files embed.FS

type script struct {
	src  []byte
	hash string
}

var all = map[string]script{}

func init() {
	entries, _ := files.ReadDir(".")
	for _, e := range entries {
		src, _ := files.ReadFile(e.Name())
		sum := sha256.Sum256(src)
		all[strings.TrimSuffix(e.Name(), ".lua")] = script{src: src, hash: hex.EncodeToString(sum[:])[:12]}
	}
}

// Hash names the current version of a game; "" if there is no such game.
func Hash(id string) string { return all[id].hash }

// Script returns a game's source if hash is its current version.
func Script(id, hash string) ([]byte, bool) {
	s, ok := all[id]
	return s.src, ok && hash != "" && s.hash == hash
}
