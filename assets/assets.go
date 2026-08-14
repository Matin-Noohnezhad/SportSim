// Package assets embeds the packed game database into the binary, so the game
// ships as a single self-contained executable with no data files to install.
package assets

import (
	"bytes"
	_ "embed"

	"sportsim/data/pack"
)

//go:embed world.dat
var packed []byte

// Load decodes the embedded database. It returns a fresh copy each call.
func Load() (*pack.Data, error) { return pack.Decode(bytes.NewReader(packed)) }

// Size reports the compressed size of the embedded database in bytes.
func Size() int { return len(packed) }
