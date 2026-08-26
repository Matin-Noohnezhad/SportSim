package store_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"sportsim/game"
	"sportsim/store"
)

// TestPathNamesTheCareer checks that a save is identified by club and starting
// season together.
//
// The club alone is not enough: a manager may well run Real Madrid from 2016 and
// again from 2020, and those are two careers. The season being played is not the
// right half either — it changes every summer, which would leave a trail of dead
// files behind one live career.
func TestPathNamesTheCareer(t *testing.T) {
	year := game.LatestEdition()
	g, err := game.New("Tester", 1, year, 5)
	if err != nil {
		t.Fatal(err)
	}

	path := store.Path(g)
	name := filepath.Base(path)

	club := strings.ReplaceAll(g.World.ClubName(g.World.HumanClubID), " ", "_")
	if !strings.HasPrefix(name, club+"_") {
		t.Errorf("save %q does not name the club %q", name, club)
	}
	if !strings.Contains(name, "_"+strconv.Itoa(year)+".sav") {
		t.Errorf("save %q does not name the starting season %d", name, year)
	}

	// Playing on must not move the file the career saves to.
	for day := 0; day < 400; day++ {
		if g.AdvanceDay().SeasonEnded {
			break
		}
	}
	if g.World.SeasonYear == year {
		t.Fatal("the season did not roll over, so this proves nothing")
	}
	if got := store.Path(g); got != path {
		t.Errorf("save path moved to %q after a rollover; it was %q", got, path)
	}
}

// TestRoundTripKeepsTheStartYear guards the field the save name is built from.
// Losing it on the way through gob would rename the file at the next save.
func TestRoundTripKeepsTheStartYear(t *testing.T) {
	year := game.LatestEdition()
	g, err := game.New("Tester", 1, year, 9)
	if err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 30; day++ {
		g.AdvanceDay()
	}

	path := filepath.Join(t.TempDir(), "career.sav")
	if err := store.Save(g, path); err != nil {
		t.Fatal(err)
	}
	back, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.World.StartYear != year {
		t.Errorf("start year came back as %d, want %d", back.World.StartYear, year)
	}
	if back.World.SeasonYear != g.World.SeasonYear {
		t.Errorf("season came back as %d, want %d", back.World.SeasonYear, g.World.SeasonYear)
	}
}
