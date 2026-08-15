package game_test

import (
	"testing"

	"sportsim/engine/match"
	"sportsim/engine/season"
	"sportsim/game"
)

// TestMatchReport checks the two ways a match report comes about against each
// other: the one built from the result the manager has just watched, and the
// one rebuilt from what the fixture kept of it.
//
// They are written by different code at different times — one from the live
// event stream, one from the handful of incidents a played fixture stores — and
// the whole point of the type is that a match opened from the fixture list
// months later reads exactly as it did at full time.
func TestMatchReport(t *testing.T) {
	g, err := game.New("Reporter", 1, 909)
	if err != nil {
		t.Fatal(err)
	}
	g.AutoSelect()

	var res *match.Result
	for i := 0; i < 60 && res == nil; i++ {
		res = g.AdvanceDay().HumanResult
	}
	if res == nil {
		t.Fatal("the managed club never played")
	}
	live := g.ResultReport(res)

	var f *season.Fixture
	for i := range g.Sched.Fixtures {
		fx := &g.Sched.Fixtures[i]
		if fx.Played && (fx.Home == g.World.HumanClubID || fx.Away == g.World.HumanClubID) {
			f = fx
		}
	}
	stored := g.FixtureReport(f)
	if stored == nil {
		t.Fatal("the played fixture kept no report")
	}

	if stored.HomeGoals != live.HomeGoals || stored.AwayGoals != live.AwayGoals {
		t.Errorf("scoreline %d-%d, watched %d-%d",
			stored.HomeGoals, stored.AwayGoals, live.HomeGoals, live.AwayGoals)
	}
	if stored.Stats != live.Stats {
		t.Errorf("box score %+v, watched %+v", stored.Stats, live.Stats)
	}
	if len(stored.Players) != len(live.Players) {
		t.Fatalf("%d player ratings kept, %d at full time", len(stored.Players), len(live.Players))
	}
	for i, p := range stored.Players {
		if p != live.Players[i] {
			t.Errorf("%s rated %+v, at full time %+v", p.Name, p, live.Players[i])
		}
	}

	// The highlights are the goals and the sendings-off, in that order through
	// the match, and nothing else at all.
	if len(stored.Moments) != len(live.Moments) {
		t.Fatalf("%d moments kept, %d at full time", len(stored.Moments), len(live.Moments))
	}
	h, a, minute := 0, 0, 0
	for i, mo := range stored.Moments {
		if mo != live.Moments[i] {
			t.Errorf("moment %d is %+v, at full time %+v", i, mo, live.Moments[i])
		}
		if mo.Minute < minute {
			t.Errorf("moment %d is at %d', after one at %d'", i, mo.Minute, minute)
		}
		minute = mo.Minute
		if mo.Player == "" {
			t.Errorf("moment %d has nobody to name for it: %+v", i, mo)
		}
		if !mo.Kind.Goal() && mo.Assist != "" {
			t.Errorf("a %s was credited an assist: %+v", mo.Kind, mo)
		}
		if mo.Kind.Goal() {
			if mo.Away {
				a++
			} else {
				h++
			}
		}
		if mo.HomeGoals != h || mo.AwayGoals != a {
			t.Errorf("moment %d shows %d-%d, should be %d-%d", i, mo.HomeGoals, mo.AwayGoals, h, a)
		}
	}
	if h != stored.HomeGoals || a != stored.AwayGoals {
		t.Errorf("the highlights add up to %d-%d, the result was %d-%d",
			h, a, stored.HomeGoals, stored.AwayGoals)
	}
	if reds := int(stored.Stats[0].Reds + stored.Stats[1].Reds); reds != len(stored.Moments)-h-a {
		t.Errorf("%d sendings-off in the box score, %d in the highlights",
			reds, len(stored.Moments)-h-a)
	}

	// A fixture nobody has played yet has no report to show.
	if next := g.NextFixture(); next != nil && g.FixtureReport(next) != nil {
		t.Error("an unplayed fixture produced a match report")
	}
}
