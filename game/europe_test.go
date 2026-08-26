package game_test

import (
	"testing"

	"sportsim/engine/model"
	"sportsim/engine/season"
	"sportsim/game"
)

// TestEuropeanSeason plays a whole campaign and checks the three continental
// competitions from the draw to the trophy.
//
// A European season is the one part of the calendar that is not written out in
// August: each knockout round is drawn from the clubs that came through the one
// before it, so the bracket is built while the season runs. That makes it the
// thing most likely to stall halfway — a round that is never drawn leaves the
// schedule permanently incomplete and the career never rolls over.
func TestEuropeanSeason(t *testing.T) {
	g, err := game.New("Continental", 1, game.LatestEdition(), 2026)
	if err != nil {
		t.Fatal(err)
	}

	// Every competition must be filled exactly, and no club may be in two.
	entrants := map[model.Competition]int{}
	for i := range g.World.Clubs {
		c := &g.World.Clubs[i]
		if c.Competition == model.NoComp {
			continue
		}
		if l := g.World.League(c.LeagueID); l == nil || l.Tier != 1 {
			t.Errorf("%s qualified for Europe from outside a top division", c.Name)
		}
		entrants[c.Competition]++
	}
	for _, c := range model.AllCompetitions() {
		if entrants[c] != c.Entrants() {
			t.Errorf("%s drew %d entrants, wants %d", c, entrants[c], c.Entrants())
		}
	}

	for day := 0; day < 420; day++ {
		if g.AdvanceDay().SeasonEnded {
			t.Fatal("the season rolled over before the European season could be checked")
		}
		if g.Sched.Complete() {
			break
		}
	}
	if !g.Sched.Complete() {
		t.Fatal("fixtures are still unplayed after a full season; a European round was never drawn")
	}

	// No club may be asked to play twice on the same day: a European night that
	// landed on a league fixture would run the same players down twice over.
	busy := map[[2]int]int{}
	euro := 0
	for i := range g.Sched.Fixtures {
		f := &g.Sched.Fixtures[i]
		if f.Continental() {
			euro++
			if f.LeagueID != 0 {
				t.Errorf("a %s fixture is registered to league %d", f.Comp, f.LeagueID)
			}
		}
		for _, club := range []uint16{f.Home, f.Away} {
			k := [2]int{int(f.Date), int(club)}
			if busy[k]++; busy[k] > 1 {
				t.Errorf("%s play twice on %s", g.World.ClubName(club), f.Date.Format())
			}
		}
	}
	t.Logf("%d European fixtures alongside the leagues", euro)

	for _, v := range g.Europe() {
		if v.Stage != season.StageDone {
			t.Errorf("the %s finished the season at the %s", v.Name, v.StageName)
		}
		if v.Winner == "" {
			t.Errorf("the %s produced no winner", v.Name)
		}
		// Every round must halve the field, and every tie must have a winner.
		want := len(v.Groups) * 2
		for _, rd := range v.Rounds {
			if len(rd.Ties) != want/2 {
				t.Errorf("%s %s has %d ties, wants %d", v.Name, rd.Name, len(rd.Ties), want/2)
			}
			want /= 2
			for _, tie := range rd.Ties {
				if tie.Winner == "" {
					t.Errorf("%s %s: %s v %s was never settled",
						v.Name, rd.Name, tie.Home, tie.Away)
				}
				if tie.HomeGoals == tie.AwayGoals && tie.ShootoutHome == tie.ShootoutAway {
					t.Errorf("%s %s: %s v %s finished level with no shootout",
						v.Name, rd.Name, tie.Home, tie.Away)
				}
			}
		}
		if want != 1 {
			t.Errorf("the %s bracket ran out at %d clubs rather than a final", v.Name, want)
		}
		t.Logf("%s won by %s", v.Name, v.Winner)
	}

	// A group table is a table like any other: everyone plays six, and the
	// points have to agree with the results.
	for _, v := range g.Europe() {
		for _, gr := range v.Groups {
			points := 0
			for _, r := range gr.Rows {
				if r.Played != 6 {
					t.Errorf("%s group %s: %s played %d, wants 6",
						v.Name, gr.Label, g.World.ClubName(r.ClubID), r.Played)
				}
				points += r.Points
			}
			// Four clubs playing each other twice is twelve matches, and each
			// shares out three points or two.
			const matches = 12
			if points < matches*2 || points > matches*3 {
				t.Errorf("%s group %s shared out %d points across %d matches",
					v.Name, gr.Label, points, matches)
			}
		}
	}
}

// TestEuropeanMoneyIsEarned checks that a continental campaign pays, that it
// pays through the club's books rather than beside them, and that the Champions
// League is worth more than the Conference League by a distance a manager would
// notice.
func TestEuropeanMoneyIsEarned(t *testing.T) {
	g, err := game.New("Treasurer", 1, game.LatestEdition(), 31)
	if err != nil {
		t.Fatal(err)
	}
	w := g.World

	// A place in Europe has to show up in expected revenue, because revenue is
	// what sizes the transfer budget and the running costs. Money that arrived
	// without passing through it would simply pile up in the bank.
	var byComp [model.NumCompetitions]int64
	var counted [model.NumCompetitions]int
	for i := range w.Clubs {
		c := &w.Clubs[i]
		byComp[c.Competition] += season.ExpectedEuro(c)
		counted[c.Competition]++
	}
	for _, c := range model.AllCompetitions() {
		if counted[c] == 0 {
			t.Fatalf("nobody is in the %s", c)
		}
		byComp[c] /= int64(counted[c])
	}
	if byComp[model.NoComp] != 0 {
		t.Errorf("clubs outside Europe expect %d of European money", byComp[model.NoComp])
	}
	if byComp[model.ChampionsLeague] <= 3*byComp[model.ConferenceLeague] {
		t.Errorf("a Champions League place is worth %d against the Conference League's %d; "+
			"the gap is what makes fourth place worth chasing",
			byComp[model.ChampionsLeague], byComp[model.ConferenceLeague])
	}

	// The participation money is paid at the draw and nowhere else, so the world
	// should hold exactly that much more than it did before the draw was made.
	fresh, err := game.NewWorld(game.LatestEdition(), 31)
	if err != nil {
		t.Fatal(err)
	}
	var before int64
	for i := range fresh.Clubs {
		before += fresh.Clubs[i].Balance
	}
	var after, want int64
	for i := range w.Clubs {
		after += w.Clubs[i].Balance
		want += season.EuroParticipation(w.Clubs[i].Competition)
	}
	if after-before != want {
		t.Errorf("the draw put %d into the game's books, the participation fees come to %d",
			after-before, want)
	}
}
