// Package assets embeds the packed game databases into the binary, so the game
// ships as a single self-contained executable with no data files to install.
//
// One file is one edition of the source dataset — one season's squads, ratings,
// values and wages — named world_YYYY.dat for the season it starts. A career
// begins in whichever edition the manager picks, so the game ships with as many
// as have been imported and discovers them at startup rather than naming any of
// them in code.
package assets

import (
	"bytes"
	"embed"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	"sportsim/data/pack"
)

//go:embed *.dat
var files embed.FS

// editions maps a year to the embedded file holding it, built once on first use.
var (
	editionsOnce sync.Once
	editions     map[int]string
	years        []int
)

func index() {
	editionsOnce.Do(func() {
		editions = make(map[int]string)
		entries, err := files.ReadDir(".")
		if err != nil {
			return
		}
		for _, e := range entries {
			y, ok := yearOf(e.Name())
			if !ok {
				continue
			}
			editions[y] = e.Name()
			years = append(years, y)
		}
		sort.Ints(years)
	})
}

// yearOf reads the season out of a "world_2016.dat" filename.
func yearOf(name string) (int, bool) {
	base := strings.TrimSuffix(path.Base(name), ".dat")
	rest, ok := strings.CutPrefix(base, "world_")
	if !ok {
		return 0, false
	}
	y, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return y, true
}

// Editions returns the seasons available to start a career in, oldest first.
// It reads only the file names, so a year picker costs nothing to draw.
func Editions() []int {
	index()
	return append([]int(nil), years...)
}

// Latest returns the most recent season available, or 0 if none is embedded.
func Latest() int {
	index()
	if len(years) == 0 {
		return 0
	}
	return years[len(years)-1]
}

// Has reports whether the given season is embedded.
func Has(year int) bool {
	index()
	_, ok := editions[year]
	return ok
}

// Load decodes the database for one season. It returns a fresh copy each call.
func Load(year int) (*pack.Data, error) {
	index()
	name, ok := editions[year]
	if !ok {
		return nil, fmt.Errorf("no game database for %d (have %v)", year, years)
	}
	b, err := files.ReadFile(name)
	if err != nil {
		return nil, err
	}
	d, err := pack.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	// The file name and the header have to agree: they are two statements of the
	// same fact, and a mismatch means one of them was edited by hand.
	if d.Year != year {
		return nil, fmt.Errorf("%s holds the %d season, not %d", name, d.Year, year)
	}
	return d, nil
}

// Size reports the compressed size of every embedded database in bytes.
func Size() int {
	index()
	total := 0
	for _, name := range editions {
		if b, err := files.ReadFile(name); err == nil {
			total += len(b)
		}
	}
	return total
}
