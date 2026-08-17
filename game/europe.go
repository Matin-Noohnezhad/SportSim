package game

import (
	"fmt"

	"sportsim/engine/model"
	"sportsim/engine/season"
)

// The continental competitions as a frontend sees them.
//
// Nothing here decides anything: the draw, the calendar and the money all live
// in engine/season, and these are the plain structs a screen draws them from.
// A knockout tie is flattened out into its legs here rather than in a view,
// because working out an aggregate score from a fixture list is exactly the
// business rule a view is not allowed to hold.

// EuroGroup is one group of a group stage.
type EuroGroup struct {
	Label string // "A", "B", "C"
	Rows  []season.Row
}

// EuroLeg is one match of a knockout tie.
type EuroLeg struct {
	Date       model.Date
	Home, Away string
	HomeGoals  int
	AwayGoals  int
	Played     bool
}

// EuroTie is one knockout pairing, with the aggregate as it stands. Home is the
// club that hosted the first leg.
type EuroTie struct {
	Home, Away           string
	HomeGoals, AwayGoals int // aggregate over the legs played so far
	ShootoutHome         int
	ShootoutAway         int
	Winner               string // empty while the tie is still alive
	Ours                 bool   // the managed club is in it
	Legs                 []EuroLeg
}

// EuroRound is one round of a knockout bracket.
type EuroRound struct {
	Stage season.Stage
	Name  string
	Ties  []EuroTie
}

// EuroView is one competition's campaign as the European screen draws it.
type EuroView struct {
	Comp      model.Competition
	Name      string
	Stage     season.Stage
	StageName string

	Groups []EuroGroup
	Rounds []EuroRound // every knockout round drawn so far, earliest first

	Winner string // the champion, once the final has been played

	// Ours is how the managed club stands in this competition: the group they
	// were drawn into, and whether they are still in it at all.
	OurGroup int
	Entered  bool
	StillIn  bool
}

// Europe returns every continental competition being played this season.
func (g *Game) Europe() []EuroView {
	if g.Sched == nil || g.Sched.Euro == nil {
		return nil
	}
	out := make([]EuroView, 0, len(g.Sched.Euro.Comps))
	for i := range g.Sched.Euro.Comps {
		out = append(out, g.euroView(&g.Sched.Euro.Comps[i]))
	}
	return out
}

func (g *Game) euroView(cs *season.CompSeason) EuroView {
	w := g.World
	us := w.HumanClubID

	v := EuroView{
		Comp:      cs.Comp,
		Name:      cs.Comp.String(),
		Stage:     cs.Stage,
		StageName: cs.Stage.String(),
		OurGroup:  cs.GroupOf(us),
		Winner:    "",
	}
	if c := w.Club(us); c != nil {
		v.Entered = c.Competition == cs.Comp
	}
	if cs.Winner != 0 {
		v.Winner = w.ClubName(cs.Winner)
	}

	for gi := range cs.Groups {
		v.Groups = append(v.Groups, EuroGroup{
			Label: season.GroupLabel(gi + 1),
			Rows:  season.GroupTable(w, g.Sched, cs.Comp, gi+1),
		})
	}

	for ri := range cs.Rounds {
		rd := &cs.Rounds[ri]
		out := EuroRound{Stage: rd.Stage, Name: rd.Stage.String()}
		for _, t := range rd.Ties {
			tie := EuroTie{
				Home:         w.ClubName(t.Home),
				Away:         w.ClubName(t.Away),
				ShootoutHome: int(t.ShootoutHome),
				ShootoutAway: int(t.ShootoutAway),
				Ours:         us != 0 && (t.Home == us || t.Away == us),
			}
			if t.Winner != 0 {
				tie.Winner = w.ClubName(t.Winner)
			}
			for _, f := range g.tieLegs(cs.Comp, rd.Stage, t.Home, t.Away) {
				tie.Legs = append(tie.Legs, EuroLeg{
					Date:      f.Date,
					Home:      w.ClubName(f.Home),
					Away:      w.ClubName(f.Away),
					HomeGoals: int(f.HomeGoals),
					AwayGoals: int(f.AwayGoals),
					Played:    f.Played,
				})
				if !f.Played {
					continue
				}
				// The aggregate is read off the legs rather than off the tie, so
				// it is right after the first leg as well as after the second —
				// the tie itself only carries a total once it has been settled.
				if f.Home == t.Home {
					tie.HomeGoals += int(f.HomeGoals)
					tie.AwayGoals += int(f.AwayGoals)
				} else {
					tie.HomeGoals += int(f.AwayGoals)
					tie.AwayGoals += int(f.HomeGoals)
				}
			}
			if tie.Ours {
				v.StillIn = tie.Winner == "" || tie.Winner == w.ClubName(us)
			}
			out.Ties = append(out.Ties, tie)
		}
		v.Rounds = append(v.Rounds, out)
	}
	if cs.Stage == season.StageGroup && v.Entered {
		v.StillIn = true
	}
	return v
}

// tieLegs finds the fixtures a knockout tie is played over, in date order.
func (g *Game) tieLegs(c model.Competition, st season.Stage, home, away uint16) []*season.Fixture {
	var out []*season.Fixture
	for i := range g.Sched.Fixtures {
		f := &g.Sched.Fixtures[i]
		if f.Comp != c || f.Stage != st {
			continue
		}
		if (f.Home == home && f.Away == away) || (f.Home == away && f.Away == home) {
			out = append(out, f)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Date < out[j-1].Date; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// OurCompetition is the continental tournament the managed club is in this
// season, or NoComp.
func (g *Game) OurCompetition() model.Competition {
	if c := g.Club(); c != nil {
		return c.Competition
	}
	return model.NoComp
}

// CompetitionLabel names the competition a fixture belongs to, at the length a
// fixture list wants it: the division's name for a league match, and the round
// for a European one.
func (g *Game) CompetitionLabel(f *season.Fixture) string {
	if f == nil {
		return ""
	}
	if !f.Continental() {
		if l := g.World.League(f.LeagueID); l != nil {
			return l.Name
		}
		return "League"
	}
	switch {
	case f.Stage == season.StageGroup:
		return fmt.Sprintf("%s Group %s", f.Comp.Short(), season.GroupLabel(int(f.Group)))
	case f.Leg == 0:
		return fmt.Sprintf("%s %s", f.Comp.Short(), f.Stage)
	default:
		return fmt.Sprintf("%s %s, %s leg", f.Comp.Short(), f.Stage, legName(f.Leg))
	}
}

func legName(leg uint8) string {
	if leg == 2 {
		return "2nd"
	}
	return "1st"
}

// ShootoutLine renders how a fixture was settled from the spot, or nothing at
// all for the great majority of matches that were not.
func ShootoutLine(f *season.Fixture) string {
	if f == nil || (f.ShootoutHome == 0 && f.ShootoutAway == 0) {
		return ""
	}
	return fmt.Sprintf("%d-%d on penalties", f.ShootoutHome, f.ShootoutAway)
}
