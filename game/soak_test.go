package game_test

import (
	"os"
	"path/filepath"
	"testing"

	"sportsim/engine/transfer"
	"sportsim/game"
	"sportsim/store"
)

// TestMultiSeason plays three full seasons and checks the world is still
// coherent afterwards. Rollover touches almost everything — promotion,
// contracts, retirement, youth intake — so this is the test most likely to
// catch a career quietly degenerating over time.
func TestMultiSeason(t *testing.T) {
	g, err := game.New("Test Manager", 1, 2026)
	if err != nil {
		t.Fatal(err)
	}

	// Record the league sizes we must preserve.
	want := map[uint16]int{}
	for i := range g.World.Leagues {
		l := &g.World.Leagues[i]
		want[l.ID] = len(l.ClubIDs)
	}
	moneyAtKickoff := worldMoney(g)

	seasons := 0
	for day := 0; day < 3*400 && seasons < 3; day++ {
		rep := g.AdvanceDay()
		if rep.SeasonEnded {
			seasons++
			t.Logf("--- season %d complete, now %s ---", seasons, g.World.Date.Format())
			for _, h := range rep.Outcome.Headlines[:min(3, len(rep.Outcome.Headlines))] {
				t.Logf("    %s", h)
			}
			t.Logf("    %d youth promoted, %d retired", rep.Outcome.YouthCount, len(rep.Outcome.Retired))
		}
	}
	if seasons < 3 {
		t.Fatalf("only completed %d seasons; the calendar is not advancing correctly", seasons)
	}

	w := g.World

	// League sizes must be preserved exactly, or promotion and relegation are
	// leaking clubs.
	for i := range w.Leagues {
		l := &w.Leagues[i]
		if got := len(l.ClubIDs); got != want[l.ID] {
			t.Errorf("%s has %d clubs, started with %d", l.Name, got, want[l.ID])
		}
	}

	// The money has to stay coherent too, and it is the slowest thing in the game
	// to go wrong: the four flows that make up a club's books — the gate and the
	// prize pot in, wages and running costs out — only show they disagree over a
	// span of seasons. Both directions are failures. Runaway growth means every
	// club can afford everybody, which is what happened when gate receipts were
	// paid twice and again when nothing but wages went out; a collapse means the
	// board is bankrupt by the third Christmas.
	if money := worldMoney(g); money < moneyAtKickoff || money > 6*moneyAtKickoff {
		t.Errorf("the world holds %s after three seasons, started with %s",
			transfer.Money(money), transfer.Money(moneyAtKickoff))
	}
	// Some clubs living beyond their means is football; most of them doing it is
	// a broken economy.
	red := 0
	for i := range w.Clubs {
		if w.Clubs[i].Balance < 0 {
			red++
		}
	}
	if red > len(w.Clubs)/5 {
		t.Errorf("%d of %d clubs are in the red", red, len(w.Clubs))
	}

	// Every club must be able to field a team.
	small, big := 0, 0
	for i := range w.Clubs {
		n := w.SquadSize(w.Clubs[i].ID)
		if n < 16 {
			small++
			if small <= 3 {
				t.Errorf("%s has only %d players", w.Clubs[i].Name, n)
			}
		}
		if n > 40 {
			big++
		}
	}
	if big > 0 {
		t.Errorf("%d clubs have bloated squads (>40 players)", big)
	}

	// Ages must stay plausible: nobody should still be playing at 50.
	oldest, active := 0, 0
	for i := range w.Players {
		p := &w.Players[i]
		if p.Potential == 0 || p.ClubID == 0 {
			continue
		}
		active++
		if a := w.Age(p); a > oldest {
			oldest = a
		}
	}
	if oldest > 43 {
		t.Errorf("oldest active player is %d years old", oldest)
	}
	t.Logf("after 3 seasons: %d active players, oldest %d, %d total records", active, oldest, len(w.Players))

	// Quality must not collapse or inflate: the elite should still be elite.
	best := 0.0
	over80 := 0
	for i := range w.Players {
		p := &w.Players[i]
		if p.ClubID == 0 {
			continue
		}
		ca := p.CurrentAbility()
		if ca > best {
			best = ca
		}
		if ca >= 80 {
			over80++
		}
	}
	t.Logf("best player %.1f, %d players rated 80+", best, over80)
	if best < 84 || best > 96 {
		t.Errorf("best player in the world is rated %.1f, expected 84-96", best)
	}
	if over80 < 40 || over80 > 900 {
		t.Errorf("%d players rated 80+, expected roughly 100-500", over80)
	}
}

// TestSaveRoundTrip verifies a career survives being written to disk and read
// back, and that it resumes on the same random stream.
func TestSaveRoundTrip(t *testing.T) {
	g, err := game.New("Round Trip", 1, 99)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		g.AdvanceDay()
	}

	path := filepath.Join(t.TempDir(), "test.sav")
	if err := store.Save(g, path); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	t.Logf("save file is %.0f KB", float64(info.Size())/1024)

	loaded, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.World.Date != g.World.Date {
		t.Errorf("date differs after load: %v vs %v", loaded.World.Date, g.World.Date)
	}
	if len(loaded.World.Players) != len(g.World.Players) {
		t.Errorf("player count differs after load")
	}
	if loaded.World.ManagerName != "Round Trip" {
		t.Errorf("manager name lost: %q", loaded.World.ManagerName)
	}

	// Both copies must now simulate identically.
	for i := 0; i < 20; i++ {
		g.AdvanceDay()
		loaded.AdvanceDay()
	}
	ta := g.Table(1)
	tb := loaded.Table(1)
	for i := range ta {
		if ta[i].ClubID != tb[i].ClubID || ta[i].Points != tb[i].Points {
			t.Fatalf("diverged at table row %d: %+v vs %+v", i, ta[i], tb[i])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestSeasonCalibration plays one complete season across all thirteen leagues
// and checks the output against the rates real top-division football produces.
//
// This, rather than the engine-level sweep, is the binding calibration target:
// it measures the game as a player actually experiences it, with fatigue,
// injuries, suspensions, form, transfers and squad rotation all in play.
func TestSeasonCalibration(t *testing.T) {
	g, err := game.New("Calibration", 1, 2026)
	if err != nil {
		t.Fatal(err)
	}

	var goals, matches, homeWins, draws, penalties, assisted int
	for g.Sched.Remaining() > 0 {
		for _, f := range g.AdvanceDay().Results {
			matches++
			goals += int(f.HomeGoals) + int(f.AwayGoals)
			for _, gl := range f.Goals {
				if gl.Penalty {
					penalties++
				}
				if gl.Assist != 0 {
					assisted++
				}
			}
			switch {
			case f.HomeGoals > f.AwayGoals:
				homeWins++
			case f.HomeGoals == f.AwayGoals:
				draws++
			}
		}
	}
	n := float64(matches)
	t.Logf("%d matches: %.2f goals, %.1f%% home wins, %.1f%% draws",
		matches, float64(goals)/n, 100*float64(homeWins)/n, 100*float64(draws)/n)
	t.Logf("  %.2f penalties per match (%.1f%% of goals), %.1f%% of goals assisted",
		float64(penalties)/n, 100*float64(penalties)/float64(goals),
		100*float64(assisted)/float64(goals))

	// A penalty is worth nearly ten open chances, so getting their frequency
	// wrong is not a rounding error: it moves both the scoring rate and who
	// scores, since spot kicks go to one nominated taker.
	if ps := 100 * float64(penalties) / float64(goals); ps < 6 || ps > 14 {
		t.Errorf("penalties are %.1f%% of goals, want 6-14%% (real ~9%%)", ps)
	}

	if gpm := float64(goals) / n; gpm < 2.50 || gpm > 3.00 {
		t.Errorf("goals/match = %.2f, want 2.50-3.00 (real ~2.75)", gpm)
	}
	if hw := 100 * float64(homeWins) / n; hw < 39 || hw > 49 {
		t.Errorf("home win rate = %.1f%%, want 39-49%% (real ~44%%)", hw)
	}
	if dr := 100 * float64(draws) / n; dr < 20 || dr > 30 {
		t.Errorf("draw rate = %.1f%%, want 20-30%% (real ~26%%)", dr)
	}

	// League tables must show a believable spread. A title race decided at 60
	// points, or a bottom club on 2, would mean the engine has lost its grip on
	// how much better good teams really are.
	for li := range g.World.Leagues {
		l := &g.World.Leagues[li]
		rows := g.Table(l.ID)
		if len(rows) < 2 {
			continue
		}
		played := rows[0].Played
		top, bottom := rows[0].Points, rows[len(rows)-1].Points
		// Normalise to a 38-game season so leagues of different sizes compare.
		scale := 38.0 / float64(played)
		nt, nb := float64(top)*scale, float64(bottom)*scale
		t.Logf("  %-16s %3d..%3d pts over %d games — champion %s",
			l.Name, top, bottom, played, g.World.ClubName(rows[0].ClubID))
		// The upper bound is generous because normalising a short season
		// exaggerates a dominant club: the Süper Lig's 34 games scale up by a
		// ninth, and Galatasaray are far stronger than anyone they face.
		if nt < 68 || nt > 110 {
			t.Errorf("%s champion on %.0f pts (38-game equivalent), want 68-110", l.Name, nt)
		}
		if nb < 8 || nb > 45 {
			t.Errorf("%s bottom club on %.0f pts (38-game equivalent), want 8-45", l.Name, nb)
		}
	}
}

// worldMoney totals what every club has in the bank.
func worldMoney(g *game.Game) int64 {
	var total int64
	for i := range g.World.Clubs {
		total += g.World.Clubs[i].Balance
	}
	return total
}
