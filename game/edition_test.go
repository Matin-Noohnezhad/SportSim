package game_test

import (
	"testing"

	"sportsim/engine/model"
	"sportsim/engine/season"
	"sportsim/engine/transfer"
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

// TestOlderEditionsStaySolvent is what settles the revenue index in the
// importer.
//
// Wages and player values come out of the CSV and are already correct for their
// season; the broadcast pots in the importer's `wanted` table are today's, so an
// old edition is scaled down by a per-year factor. That factor is an estimate,
// and the way to tell whether it is a good one is to play a season and look at
// the books: set too low, the clubs of that era cannot cover a wage bill they
// really did pay; too high, and everybody is rich.
//
// It runs on the oldest edition embedded, and skips when there is only one —
// the newest is already covered by TestMultiSeason.
func TestOlderEditionsStaySolvent(t *testing.T) {
	years := game.Editions()
	if len(years) < 2 {
		t.Skip("only one edition is embedded; nothing to compare an era against")
	}
	year := years[0]

	g, err := game.New("Treasurer", 1, year, 2016)
	if err != nil {
		t.Fatal(err)
	}
	opening := 0
	atKickoff := make(map[uint16]int64, len(g.World.Clubs))
	for i := range g.World.Clubs {
		c := &g.World.Clubs[i]
		atKickoff[c.ID] = c.Balance
		if c.Balance < 0 {
			opening++
		}
	}

	for day := 0; day < 400; day++ {
		if g.AdvanceDay().SeasonEnded {
			break
		}
	}

	w := g.World
	red, poorest := 0, int64(0)
	for i := range w.Clubs {
		if b := w.Clubs[i].Balance; b < 0 {
			red++
			if b < poorest {
				poorest = b
			}
		}
	}
	t.Logf("%s: %d of %d clubs in the red after a season (%d at kickoff), worst %s",
		model.SeasonLabel(year), red, len(w.Clubs), opening, transfer.Money(poorest))

	// Some clubs living beyond their means is football; most of them doing it
	// means the era's broadcast money is set wrongly.
	if red > len(w.Clubs)/5 {
		t.Errorf("%d of %d clubs are in the red: revenueIndex[%d] is too low",
			red, len(w.Clubs), year)
	}

	// The other direction, measured against each club's own income rather than
	// a flat figure, so the bar means the same thing in every era: a club that
	// clears more profit in a season than it earned all year is being paid out
	// of a television deal that did not exist yet.
	for i := range w.Clubs {
		c := &w.Clubs[i]
		if c.Reputation < 90 {
			continue
		}
		profit := c.Balance - atKickoff[c.ID]
		if rev := season.Revenue(w, c); profit > rev {
			t.Errorf("%s profited %s on revenue of %s: revenueIndex[%d] is too high",
				c.Name, transfer.Money(profit), transfer.Money(rev), year)
			break
		}
	}
}
