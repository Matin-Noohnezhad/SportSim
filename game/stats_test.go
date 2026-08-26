package game_test

import (
	"testing"

	"sportsim/game"
)

// TestSeasonStats plays a stretch of a season and checks the statistics the
// league screen shows against the fixtures they are drawn from.
//
// Season tallies live on the player and the goals live on the fixture, and the
// two are written by different code at different times: the point of this test
// is that they still agree, so a scoring chart can never disagree with the
// results it was compiled from.
func TestSeasonStats(t *testing.T) {
	g, err := game.New("Statistician", 1, game.LatestEdition(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		g.AdvanceDay()
	}

	// What the fixtures say happened.
	var goals, pens, assists, shutouts, played int
	for i := range g.Sched.Fixtures {
		f := &g.Sched.Fixtures[i]
		if !f.Played {
			continue
		}
		played++
		for _, gl := range f.Goals {
			goals++
			if gl.Penalty {
				pens++
			}
			if gl.Assist != 0 {
				assists++
			}
		}
		if f.AwayGoals == 0 {
			shutouts++
		}
		if f.HomeGoals == 0 {
			shutouts++
		}
	}
	if played == 0 {
		t.Fatal("no matches were played")
	}

	// What the players' season tallies say.
	var pGoals, pPens, pAssists, pClean int
	for i := range g.World.Players {
		p := &g.World.Players[i]
		if p.Penalties > p.Goals {
			t.Fatalf("%s has %d penalties from %d goals", p.Name, p.Penalties, p.Goals)
		}
		pGoals += int(p.Goals)
		pPens += int(p.Penalties)
		pAssists += int(p.Assists)
		pClean += int(p.CleanSheets)
	}

	if pGoals != goals {
		t.Errorf("players are credited with %d goals, the fixtures record %d", pGoals, goals)
	}
	if pPens != pens {
		t.Errorf("players are credited with %d penalties, the fixtures record %d", pPens, pens)
	}
	if pAssists != assists {
		t.Errorf("players are credited with %d assists, the fixtures record %d", pAssists, assists)
	}
	// A clean sheet belongs to whoever was in goal, and only if they saw enough
	// of the match out, so there is at most one per side that conceded nothing.
	if pClean > shutouts {
		t.Errorf("%d clean sheets credited across %d goalless defences", pClean, shutouts)
	}
	if pClean < shutouts/2 {
		t.Errorf("only %d clean sheets credited across %d goalless defences — keepers are being missed",
			pClean, shutouts)
	}
	t.Logf("%d matches: %d goals (%d penalties, %d assisted), %d clean sheets of %d goalless defences",
		played, goals, pens, assists, pClean, shutouts)

	// Every chart must be ordered on what it ranks, and lead with a real player.
	st := g.Stats(g.Club().LeagueID, 10)
	if len(st.Scorers) == 0 || len(st.Assists) == 0 || len(st.CleanSheets) == 0 {
		t.Fatalf("charts are empty after %d matches", played)
	}
	descending := func(name string, rows []game.PlayerStat, of func(game.PlayerStat) float64) {
		t.Helper()
		for i := 1; i < len(rows); i++ {
			if of(rows[i]) > of(rows[i-1]) {
				t.Errorf("%s chart is out of order at row %d", name, i+1)
				return
			}
		}
	}
	descending("scorers", st.Scorers, func(s game.PlayerStat) float64 { return float64(s.Goals) })
	descending("assists", st.Assists, func(s game.PlayerStat) float64 { return float64(s.Assists) })
	descending("clean sheets", st.CleanSheets, func(s game.PlayerStat) float64 { return float64(s.CleanSheets) })
	descending("ratings", st.Ratings, func(s game.PlayerStat) float64 { return s.AvgRating })
	descending("yellows", st.Yellows, func(s game.PlayerStat) float64 { return float64(s.Yellows) })

	for _, s := range st.CleanSheets {
		if s.Position != "GK" {
			t.Errorf("%s (%s) is on the clean-sheet chart: it is a goalkeeping record",
				s.Name, s.Position)
		}
	}
	for _, s := range st.Ratings {
		if s.Apps < st.RatingApps {
			t.Errorf("%s qualified for the ratings chart on %d apps, the bar is %d",
				s.Name, s.Apps, st.RatingApps)
		}
	}

	// The summary has to add up to the fixtures it was compiled from.
	sum := st.Summary
	if sum.HomeWins+sum.Draws+sum.AwayWins != sum.Played {
		t.Errorf("%d home wins + %d draws + %d away wins is not %d matches played",
			sum.HomeWins, sum.Draws, sum.AwayWins, sum.Played)
	}
	if sum.Played > sum.Total {
		t.Errorf("%d matches played of a %d-match fixture list", sum.Played, sum.Total)
	}
	if sum.BiggestWin == nil {
		t.Error("no biggest win recorded")
	}
	top := st.Scorers[0]
	t.Logf("leading scorer %s (%s) on %d goals, %d from the spot; %s champion-elect chart bar %d apps",
		top.Name, top.Club, top.Goals, top.Penalties, g.League().Name, st.RatingApps)
}

// TestStatsResetEachSeason checks the new tallies are cleared at rollover along
// with the ones that were always there. A tally that survives the summer would
// show up as a striker on ninety goals two seasons in.
func TestStatsResetEachSeason(t *testing.T) {
	g, err := game.New("Rollover", 1, game.LatestEdition(), 77)
	if err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 400; day++ {
		if g.AdvanceDay().SeasonEnded {
			break
		}
	}
	for i := range g.World.Players {
		p := &g.World.Players[i]
		if p.Goals != 0 || p.Penalties != 0 || p.Assists != 0 || p.CleanSheets != 0 ||
			p.Apps != 0 || p.Yellows != 0 || p.Reds != 0 {
			t.Fatalf("%s carried season tallies into the new campaign: %+v", p.Name, *p)
		}
	}
}
