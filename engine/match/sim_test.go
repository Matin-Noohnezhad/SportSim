package match_test

import (
	"math"
	"testing"

	"sportsim/assets"
	"sportsim/engine/match"
	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// TestEngineSanity plays every fixture in every league with all squads in
// neutral condition and checks the engine stays inside plausible bounds.
//
// These are deliberately looser than real-league figures, because a world of
// fully fit, injury-free, form-neutral teams is not a real season: it scores a
// little less and draws a little more. The binding calibration target is
// TestSeasonCalibration in the game package, which measures an actual season.
func TestEngineSanity(t *testing.T) {
	w := loadWorld(t)
	r := rng.New(42)

	var (
		games, hw, dr                      int
		goals, shots, sot, corners         int
		fouls, yellows, reds, homeG, awayG int
		xg                                 float64
	)

	for li := range w.Leagues {
		for _, a := range w.Leagues[li].ClubIDs {
			for _, b := range w.Leagues[li].ClubIDs {
				if a == b {
					continue
				}
				res := match.Sim(r, side(w, a), side(w, b), 30000)
				games++
				goals += res.HomeGoals + res.AwayGoals
				homeG += res.HomeGoals
				awayG += res.AwayGoals
				for i := 0; i < 2; i++ {
					s := res.Stats[i]
					shots += int(s.Shots)
					sot += int(s.OnTarget)
					corners += int(s.Corners)
					fouls += int(s.Fouls)
					yellows += int(s.Yellows)
					reds += int(s.Reds)
					xg += s.XG
				}
				switch {
				case res.HomeGoals > res.AwayGoals:
					hw++
				case res.HomeGoals == res.AwayGoals:
					dr++
				}
				// Reset wear so the sweep measures the engine, not fatigue drift.
				for _, ln := range res.Lines {
					if p := w.Player(ln.PlayerID); p != nil {
						p.Fitness, p.InjuryDays = 95, 0
					}
				}
			}
		}
	}

	n := float64(games)
	check := func(name string, got, want, tol float64) {
		t.Helper()
		if math.Abs(got-want) > tol {
			t.Errorf("%s = %.3f, want %.3f ±%.3f", name, got, want, tol)
		} else {
			t.Logf("%-18s %7.3f  (real ~%.2f)", name, got, want)
		}
	}

	check("goals/match", float64(goals)/n, 2.45, 0.30)
	check("home goals", float64(homeG)/n, 1.33, 0.20)
	check("away goals", float64(awayG)/n, 1.12, 0.20)
	check("shots/match", float64(shots)/n, 25.0, 3.0)
	check("on target", float64(sot)/n, 8.5, 1.5)
	check("corners", float64(corners)/n, 10.5, 2.0)
	check("fouls", float64(fouls)/n, 22.0, 3.0)
	check("yellows", float64(yellows)/n, 3.9, 0.8)
	check("reds", float64(reds)/n, 0.11, 0.08)
	check("xG/match", xg/n, 2.45, 0.40)
	check("home win %", 100*float64(hw)/n, 42.0, 4.5)
	check("draw %", 100*float64(dr)/n, 26.0, 4.0)
}

// TestDeterminism verifies that the same seed reproduces a match exactly, which
// is what makes a save file replayable.
func TestDeterminism(t *testing.T) {
	w := loadWorld(t)
	a, b := w.Leagues[0].ClubIDs[0], w.Leagues[0].ClubIDs[1]

	first := match.Sim(rng.New(7), side(w, a), side(w, b), 40000)
	for i := range w.Players {
		w.Players[i].Fitness = 95
	}
	second := match.Sim(rng.New(7), side(w, a), side(w, b), 40000)

	if first.HomeGoals != second.HomeGoals || first.AwayGoals != second.AwayGoals {
		t.Fatalf("same seed gave %d-%d then %d-%d",
			first.HomeGoals, first.AwayGoals, second.HomeGoals, second.AwayGoals)
	}
	if len(first.Events) != len(second.Events) {
		t.Fatalf("event counts differ: %d vs %d", len(first.Events), len(second.Events))
	}
	for i := range first.Events {
		if first.Events[i] != second.Events[i] {
			t.Fatalf("event %d differs: %+v vs %+v", i, first.Events[i], second.Events[i])
		}
	}
}

func loadWorld(t *testing.T) *model.World {
	t.Helper()
	d, err := assets.Load()
	if err != nil {
		t.Fatalf("loading embedded database: %v", err)
	}
	w := &model.World{Players: d.Players, Clubs: d.Clubs, Leagues: d.Leagues, Nations: d.Nations,
		Date: model.NewDate(2026, 8, 8), SeasonYear: 2026}
	for i := range w.Players {
		p := &w.Players[i]
		p.Fitness, p.Sharpness, p.Morale, p.Form = 95, 80, 70, 0
	}
	return w
}

func side(w *model.World, id uint16) *match.Side {
	c := w.Club(id)
	lineup, bench := match.AutoPick(w.Squad(id), c.Tactics)
	return match.NewSide(id, c.Name, c.Reputation, c.Tactics, lineup, bench)
}
