package game

import (
	"sort"

	"sportsim/engine/match"
	"sportsim/engine/model"
	"sportsim/engine/season"
)

// MomentKind is what sort of incident a moment is. Only the incidents that
// decide a match are moments: goals and dismissals, and nothing else.
type MomentKind uint8

const (
	MomentGoal MomentKind = iota
	MomentPenalty
	MomentRed
	MomentSecondYellow
)

// String names a moment the way a scoreboard would.
func (k MomentKind) String() string {
	switch k {
	case MomentPenalty:
		return "PENALTY"
	case MomentRed:
		return "RED CARD"
	case MomentSecondYellow:
		return "SECOND YELLOW"
	}
	return "GOAL"
}

// Goal reports whether this moment put the ball in the net.
func (k MomentKind) Goal() bool { return k == MomentGoal || k == MomentPenalty }

// Moment is one incident in a match's highlights, with the scoreline as it
// stood once the incident was over.
type Moment struct {
	Minute int
	Away   bool // the incident belongs to the away side
	Kind   MomentKind
	Club   string
	Player string
	Assist string // empty for an unassisted goal or a card

	HomeGoals int
	AwayGoals int
}

// MatchPlayer is one player's line in a match report.
type MatchPlayer struct {
	Name     string
	Position string
	Minutes  int
	Goals    int
	Assists  int
	Yellow   bool
	Red      bool
	Injured  bool
	Rating   float64
}

// MatchReport is one match as the statistics screens draw it: the box score,
// the incidents that decided it and how the manager's own players rated.
//
// A match just finished on the touchline and one looked up from the fixture
// list months later become the same report, so the screen that draws it never
// has to know which of the two it is holding. That is the whole point of the
// type: one match report, shown identically wherever a match is opened.
type MatchReport struct {
	Date       model.Date
	Home       string
	Away       string
	HomeGoals  int
	AwayGoals  int
	Attendance uint32

	// Competition names what the match was: the division, or the European round.
	Competition string

	// Ours is which side of the match the managed club is, 0 at home and 1
	// away, or -1 when they were not involved.
	Ours int

	Stats   [2]match.TeamStats
	Moments []Moment
	Players []MatchPlayer
}

// ResultReport turns a match the manager has just watched into a report. It is
// safe to call while the match is still running: the box score is filled in as
// it goes, and the ratings only exist once the final whistle has gone.
func (g *Game) ResultReport(res *match.Result) *MatchReport {
	if res == nil {
		return nil
	}
	w := g.World
	rep := &MatchReport{
		Date:        w.Date,
		Home:        w.ClubName(res.HomeClub),
		Away:        w.ClubName(res.AwayClub),
		HomeGoals:   res.HomeGoals,
		AwayGoals:   res.AwayGoals,
		Attendance:  res.Attendance,
		Competition: g.CompetitionLabel(g.fixtureFor(res)),
		Ours:        humanSide(w, res),
		Stats:       res.Stats,
	}

	for _, ev := range res.Events {
		away := ev.Team == 1
		switch ev.Type {
		case match.EvGoal, match.EvPenaltyScored:
			kind := MomentGoal
			if ev.Type == match.EvPenaltyScored {
				kind = MomentPenalty
			}
			rep.Moments = append(rep.Moments, Moment{
				Minute: int(ev.Minute), Away: away, Kind: kind,
				Club:   rep.clubOf(away),
				Player: g.playerName(ev.Player),
				Assist: g.playerName(ev.Other),
			})
		case match.EvRed, match.EvSecondYellow:
			kind := MomentRed
			if ev.Type == match.EvSecondYellow {
				kind = MomentSecondYellow
			}
			rep.Moments = append(rep.Moments, Moment{
				Minute: int(ev.Minute), Away: away, Kind: kind,
				Club:   rep.clubOf(away),
				Player: g.playerName(ev.Player),
			})
		}
	}
	rep.Moments = g.orderMoments(rep.Moments)

	if rep.Ours >= 0 {
		rep.Players = g.describeLines(ourLines(res, rep.Ours))
	}
	return rep
}

// FixtureReport rebuilds the report of a match played earlier in the season
// from what the fixture kept of it. An unplayed fixture has no report.
func (g *Game) FixtureReport(f *season.Fixture) *MatchReport {
	if f == nil || !f.Played {
		return nil
	}
	w := g.World
	rep := &MatchReport{
		Date:        f.Date,
		Home:        w.ClubName(f.Home),
		Away:        w.ClubName(f.Away),
		HomeGoals:   int(f.HomeGoals),
		AwayGoals:   int(f.AwayGoals),
		Attendance:  f.Attendance,
		Competition: g.CompetitionLabel(f),
		Ours:        -1,
		Stats:       f.Stats,
		Players:     g.describeLines(f.Lines),
	}
	switch w.HumanClubID {
	case f.Home:
		rep.Ours = 0
	case f.Away:
		rep.Ours = 1
	}

	for _, gl := range f.Goals {
		kind := MomentGoal
		if gl.Penalty {
			kind = MomentPenalty
		}
		rep.Moments = append(rep.Moments, Moment{
			Minute: int(gl.Minute), Away: gl.Away, Kind: kind,
			Club:   rep.clubOf(gl.Away),
			Player: g.playerName(gl.Scorer),
			Assist: g.playerName(gl.Assist),
		})
	}
	for _, d := range f.Reds {
		kind := MomentRed
		if d.Second {
			kind = MomentSecondYellow
		}
		rep.Moments = append(rep.Moments, Moment{
			Minute: int(d.Minute), Away: d.Away, Kind: kind,
			Club:   rep.clubOf(d.Away),
			Player: g.playerName(d.Player),
		})
	}
	rep.Moments = g.orderMoments(rep.Moments)
	return rep
}

// OurClub names the side of the report the manager was in charge of.
func (r *MatchReport) OurClub() string {
	switch r.Ours {
	case 0:
		return r.Home
	case 1:
		return r.Away
	}
	return ""
}

func (r *MatchReport) clubOf(away bool) string {
	if away {
		return r.Away
	}
	return r.Home
}

// orderMoments puts the highlights in the order they happened and runs the
// scoreline through them, so every moment carries the score as it stood once
// that incident was over. A goal and a card in the same minute keep the order
// they were recorded in, which is the order the referee dealt with them.
func (g *Game) orderMoments(ms []Moment) []Moment {
	sort.SliceStable(ms, func(a, b int) bool { return ms[a].Minute < ms[b].Minute })
	h, a := 0, 0
	for i := range ms {
		if ms[i].Kind.Goal() {
			if ms[i].Away {
				a++
			} else {
				h++
			}
		}
		ms[i].HomeGoals, ms[i].AwayGoals = h, a
	}
	return ms
}

// describeLines names the players in a set of match lines.
func (g *Game) describeLines(lines []match.PlayerLine) []MatchPlayer {
	out := make([]MatchPlayer, 0, len(lines))
	for _, ln := range lines {
		p := g.World.Player(ln.PlayerID)
		if p == nil {
			continue
		}
		out = append(out, MatchPlayer{
			Name:     p.Name,
			Position: p.Primary().String(),
			Minutes:  int(ln.Minutes),
			Goals:    int(ln.Goals),
			Assists:  int(ln.Assists),
			Yellow:   ln.Yellow,
			Red:      ln.Red,
			Injured:  ln.Injury > 0,
			Rating:   ln.Rating,
		})
	}
	return out
}

func (g *Game) playerName(id uint32) string {
	if p := g.World.Player(id); p != nil {
		return p.Name
	}
	return ""
}

// fixtureFor finds the schedule entry a result belongs to, which is where the
// competition it was played in is recorded.
//
// A match being watched from the touchline is the one in hand and is not marked
// played yet, so it is looked for first. Otherwise the result has just come off
// the pitch, and the latest played match between those two clubs at that ground
// is it — the same two clubs can meet twice at the same ground in a season only
// by meeting in Europe as well as in their division.
func (g *Game) fixtureFor(res *match.Result) *season.Fixture {
	if g.live != nil && g.live.f.Home == res.HomeClub && g.live.f.Away == res.AwayClub {
		return g.live.f
	}
	var best *season.Fixture
	for i := range g.Sched.Fixtures {
		f := &g.Sched.Fixtures[i]
		if !f.Played || f.Home != res.HomeClub || f.Away != res.AwayClub {
			continue
		}
		if best == nil || f.Date > best.Date {
			best = f
		}
	}
	return best
}

// humanSide reports which side of a match the managed club is, 0 at home and 1
// away, or -1 when they did not play in it.
func humanSide(w *model.World, res *match.Result) int {
	switch w.HumanClubID {
	case res.HomeClub:
		return 0
	case res.AwayClub:
		return 1
	}
	return -1
}

// ourLines picks out the managed club's players, in the order they took the
// field. Anyone who stayed on the bench all match is left out.
func ourLines(res *match.Result, ours int) []match.PlayerLine {
	out := make([]match.PlayerLine, 0, 16)
	for _, ln := range res.Lines {
		if int(ln.Team) == ours && ln.Minutes > 0 {
			out = append(out, ln)
		}
	}
	return out
}
