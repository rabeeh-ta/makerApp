// Package fw serves the firmware baked into this binary: the ESP Web Tools
// manifest for browser flashing, the per-flash config image, and OTA updates.
package fw

import (
	"crypto/sha256"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"strings"
)

//go:embed all:bin
var files embed.FS

// Flash layout; must match firmware/partitions.csv.
const (
	OffsetBootloader = 0x0
	OffsetPartitions = 0x8000
	OffsetBootApp0   = 0xE000
	OffsetApp        = 0x10000
	OffsetConfig     = 0x3D0000
	configSize       = 4096
)

// Board is a device model the site can flash. Add entries here as more boards
// get firmware builds.
type Board struct {
	ID    string
	Name  string
	Maker string
	Specs string
}

var Boards = []Board{
	{ID: "feather-s3-rtft", Name: "ESP32-S3 Rev TFT Feather", Maker: "Adafruit",
		Specs: "240×135 color screen · buttons D0, D1, D2 · USB-C"},
}

func BoardByID(id string) (Board, bool) {
	for _, b := range Boards {
		if b.ID == id {
			return b, true
		}
	}
	return Board{}, false
}

var Parts = map[string]bool{"bootloader.bin": true, "partitions.bin": true, "boot_app0.bin": true, "firmware.bin": true}

func File(name string) ([]byte, error) { return fs.ReadFile(files, "bin/"+name) }

func Version() string {
	b, err := File("version.txt")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

// Hash identifies this firmware build. It goes into download URLs so caches
// (proxies and CDNs may cache .bin files for hours) can never serve an older build.
func Hash() string {
	b, err := File("firmware.bin")
	if err != nil {
		return "none"
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:6])
}

func Available() bool {
	_, err := File("firmware.bin")
	return err == nil
}

type manifestPart struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
}

type manifestBuild struct {
	ChipFamily string         `json:"chipFamily"`
	Parts      []manifestPart `json:"parts"`
}

// Manifest builds the ESP Web Tools manifest. configPath points at the
// device-specific config image so each flash carries its own claim token.
func Manifest(configPath string) []byte {
	m := map[string]any{
		"name":                         "makerDevice",
		"version":                      Version(),
		"new_install_prompt_erase":     true,
		"new_install_improv_wait_time": 20,
		"builds": []manifestBuild{{
			ChipFamily: "ESP32-S3",
			Parts: []manifestPart{
				{Path: "/flash/" + Hash() + "/bootloader.bin", Offset: OffsetBootloader},
				{Path: "/flash/" + Hash() + "/partitions.bin", Offset: OffsetPartitions},
				{Path: "/flash/" + Hash() + "/boot_app0.bin", Offset: OffsetBootApp0},
				{Path: "/flash/" + Hash() + "/firmware.bin", Offset: OffsetApp},
				{Path: configPath, Offset: OffsetConfig},
			},
		}},
	}
	b, _ := json.Marshal(m)
	return b
}

// ConfigImage is the "hogcfg" partition: "HOGCFG1\0", uint16 LE length, JSON,
// padded with 0xFF (erased flash).
func ConfigImage(server, claim string) []byte {
	payload, _ := json.Marshal(map[string]string{"server": server, "claim": claim})
	img := make([]byte, configSize)
	for i := range img {
		img[i] = 0xFF
	}
	copy(img, "HOGCFG1\x00")
	binary.LittleEndian.PutUint16(img[8:], uint16(len(payload)))
	copy(img[10:], payload)
	return img
}
