// Package store persists and restores a career.
//
// Saves are gob-encoded and gzipped. A world of six thousand players with a
// full fixture list compresses to a few hundred kilobytes, so keeping many
// saves costs almost nothing.
package store

import (
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"sportsim/engine/model"
	"sportsim/engine/season"
	"sportsim/game"
)

// formatVersion is bumped whenever the saved layout changes. Version 2 added
// the penalty and clean-sheet season tallies to a player; version 3 gave a
// played fixture the match report it keeps — box score, sendings-off and, for
// the managed club's own matches, the player ratings; version 4 added the
// manager's transfer shortlist.
//
// Version 5 changes no field, but League.PrizeMoney means something different:
// it is now the division's whole broadcast deal rather than the champion's
// cheque, and season.PrizeShare divides it. A version-4 save carries the old
// figures, which the new split would read as a pot seven times too small — the
// save would load and quietly starve every club in the game, which is worse
// than refusing it.
const formatVersion = 5

// snapshot is the on-disk representation of a career.
type snapshot struct {
	Version  int
	Saved    time.Time
	World    *model.World
	Schedule *season.Schedule
	Inbox    []game.Message
	RNGState [4]uint64
}

// Save writes a career to disk, replacing any existing file at that path.
func Save(g *game.Game, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Write to a temporary file and rename, so an interrupted save cannot
	// destroy the previous one.
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(f)
	err = gob.NewEncoder(gz).Encode(snapshot{
		Version:  formatVersion,
		Saved:    time.Now(),
		World:    g.World,
		Schedule: g.Sched,
		Inbox:    g.Inbox,
		RNGState: g.RNGState(),
	})
	if err == nil {
		err = gz.Close()
	} else {
		gz.Close()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing save: %w", err)
	}
	return os.Rename(tmp, path)
}

// Load restores a career from disk.
func Load(path string) (*game.Game, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("reading save: %w", err)
	}
	defer gz.Close()

	var s snapshot
	if err := gob.NewDecoder(gz).Decode(&s); err != nil {
		return nil, fmt.Errorf("reading save: %w", err)
	}
	if s.Version != formatVersion {
		return nil, fmt.Errorf("save was written by a different version of the game (%d, expected %d)",
			s.Version, formatVersion)
	}
	return game.Restore(s.World, s.Schedule, s.Inbox, s.RNGState), nil
}

// Entry describes one save file, for a load menu.
type Entry struct {
	Path  string
	Name  string
	Saved time.Time
	Size  int64
}

// List returns the saves in a directory, newest first.
func List(dir string) ([]Entry, error) {
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, fe := range files {
		if fe.IsDir() || filepath.Ext(fe.Name()) != ".sav" {
			continue
		}
		info, err := fe.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{
			Path:  filepath.Join(dir, fe.Name()),
			Name:  fe.Name()[:len(fe.Name())-4],
			Saved: info.ModTime(),
			Size:  info.Size(),
		})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Saved.After(out[b].Saved) })
	return out, nil
}

// Dir returns the directory saves live in.
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".sportsim"
	}
	return filepath.Join(home, ".sportsim", "saves")
}
