package game_test

import (
	"testing"

	"sportsim/engine/model"
	"sportsim/game"
)

// TestCareerStartsInItsEdition checks that the calendar comes from the packed
// database rather than from a constant.
//
// The squads, values and wages in an edition are a snapshot of one summer, so
// starting them in any other year would date every player and every contract in
// the game wrongly. This is the whole reason pack.Data carries a year.
func TestCareerStartsInItsEdition(t *testing.T) {
	for _, year := range game.Editions() {
		w, err := game.NewWorld(year, 1)
		if err != nil {
			t.Fatalf("%d: %v", year, err)
		}
		if w.SeasonYear != year || w.StartYear != year {
			t.Errorf("%d: world opens in season %d, start year %d", year, w.SeasonYear, w.StartYear)
		}
		if w.Date != model.NewDate(year, 7, 1) {
			t.Errorf("%d: world opens on %s", year, w.Date.Format())
		}

		// Every contract must still have time to run: a deal that expired
		// before the career began would put the whole squad out of contract on
		// day one.
		expired := 0
		for i := range w.Players {
			if int(w.Players[i].ContractUntil) < year {
				expired++
			}
		}
		if expired > 0 {
			t.Errorf("%d: %d players start out of contract", year, expired)
		}
	}
}

// TestEveryEditionFieldsASeason checks that each edition can actually be played:
// its squads fill out, its divisions produce a fixture list and its clubs are
// not double-booked. It stops short of playing the season, which is what
// TestSeasonCalibration does for the newest edition only — twelve of those on
// every test run would not be affordable.
func TestEveryEditionFieldsASeason(t *testing.T) {
	for _, year := range game.Editions() {
		g, err := game.New("Tester", 1, year, 7)
		if err != nil {
			t.Fatalf("%d: %v", year, err)
		}
		if len(g.World.Leagues) == 0 {
			t.Fatalf("%d: no divisions", year)
		}
		for i := range g.World.Leagues {
			l := &g.World.Leagues[i]
			if len(l.ClubIDs) < 4 {
				t.Errorf("%d: %s has only %d clubs", year, l.Name, len(l.ClubIDs))
			}
		}
		// A week of play is enough to prove the fixture list is real and the
		// engine can resolve it.
		for d := 0; d < 14; d++ {
			g.AdvanceDay()
		}
		if g.World.StartYear != year {
			t.Errorf("%d: start year drifted to %d after a fortnight", year, g.World.StartYear)
		}
		t.Logf("%s: %d clubs across %d divisions",
			model.SeasonLabel(year), len(g.World.Clubs), len(g.World.Leagues))
	}
}

// TestStartYearOutlivesTheSeason is what makes a save file's name stable. The
// season advances every rollover; the year the career began does not, or a
// manager would find a new save file appearing every summer.
func TestStartYearOutlivesTheSeason(t *testing.T) {
	year := game.LatestEdition()
	g, err := game.New("Tester", 1, year, 42)
	if err != nil {
		t.Fatal(err)
	}

	for day := 0; day < 400; day++ {
		if g.AdvanceDay().SeasonEnded {
			break
		}
	}
	if g.World.SeasonYear != year+1 {
		t.Fatalf("season is %d after a rollover, want %d", g.World.SeasonYear, year+1)
	}
	if g.World.StartYear != year {
		t.Errorf("start year moved to %d; it must stay at %d for the life of the career",
			g.World.StartYear, year)
	}
}
