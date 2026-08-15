package game

import (
	"fmt"

	"sportsim/engine/match"
	"sportsim/engine/model"
	"sportsim/engine/rng"
	"sportsim/engine/season"
)

// LiveMatch is the managed club's fixture while the manager is working the
// touchline: it advances a minute at a time and accepts substitutions and
// tactical changes in between.
//
// It is the same match that AdvanceDay would otherwise have played out in one
// go. Taking charge of a match changes what the manager can do to it, never how
// it is simulated, and a match left half-watched is finished by the engine when
// the day is advanced.
//
// Everything decided from the touchline lasts ninety minutes and no longer: the
// shape, the instructions and the substitutions all live on the match.Side and
// are thrown away with it at full time. The club's own selection and tactics are
// what the manager set before kickoff, and only the tactics screen changes them
// — see the note on SetTactics.
type LiveMatch struct {
	g *Game
	f *season.Fixture
	l *match.Live

	// us is which side of the match the managed club is, 0 at home.
	us uint8
}

// LivePlayer is one of the manager's players during a match, on the pitch or on
// the bench.
type LivePlayer struct {
	PlayerID uint32
	Name     string
	Slot     int       // lineup place, -1 on the bench
	Pos      model.Pos // the shirt being filled, or the player's best position on the bench
	Fitness  uint8
	Goals    int
	Booked   bool
	Injured  bool
	Keeper   bool
}

// begin sets a fixture up at kickoff without playing any of it.
func (g *Game) begin(f *season.Fixture) *LiveMatch {
	w := g.World
	// Attendance draws from its own stream rather than the master generator, so
	// the gate is the same whether the manager watched the match or skipped it —
	// and drawing it cannot shift the match itself.
	att := attendance(w, w.Club(f.Home), w.Club(f.Away), rng.Derive(w.Seed, fixtureKey(f)^0xA77))

	us := uint8(0)
	if f.Away == w.HumanClubID {
		us = 1
	}
	return &LiveMatch{
		g:  g,
		f:  f,
		l:  match.Begin(rng.Derive(w.Seed, fixtureKey(f)), g.buildSide(f.Home), g.buildSide(f.Away), att),
		us: us,
	}
}

// KickOff hands the manager control of their club's fixture for today, or nil
// when there is no match to take charge of. The day does not move on until
// AdvanceDay is called, which finishes the match if it is still running.
func (g *Game) KickOff() *LiveMatch {
	if g.live != nil && !g.live.f.Played {
		return g.live
	}
	for _, i := range g.Sched.On(g.World.Date) {
		f := &g.Sched.Fixtures[i]
		if f.Played || (f.Home != g.World.HumanClubID && f.Away != g.World.HumanClubID) {
			continue
		}
		g.live = g.begin(f)
		g.live.l.TakeCharge(g.live.us)
		return g.live
	}
	return nil
}

// MatchInProgress reports whether a match is part-played and waiting on the
// manager. A career must not be saved in that state, because the players on the
// pitch have already been run down by the minutes played so far.
func (g *Game) MatchInProgress() bool {
	return g.live != nil && !g.live.l.Done()
}

// ----- watching -----

// Minute is how far the match has been played.
func (lm *LiveMatch) Minute() int { return lm.l.Minute() }

// Done reports whether the final whistle has gone.
func (lm *LiveMatch) Done() bool { return lm.l.Done() }

// Result is the match report, complete once Done reports true.
func (lm *LiveMatch) Result() *match.Result { return lm.l.Result() }

// Step plays the next minute and reports whether the match is still running.
func (lm *LiveMatch) Step() bool { return lm.l.Step() }

// PlayOut abandons the touchline and lets the engine finish the match.
func (lm *LiveMatch) PlayOut() { lm.l.PlayOut() }

// Score is the current scoreline, home first.
func (lm *LiveMatch) Score() (int, int) {
	res := lm.l.Result()
	return res.HomeGoals, res.AwayGoals
}

// Home reports whether the managed club is the home side.
func (lm *LiveMatch) Home() bool { return lm.us == 0 }

// Opponent names the other club.
func (lm *LiveMatch) Opponent() string {
	if lm.us == 0 {
		return lm.g.World.ClubName(lm.f.Away)
	}
	return lm.g.World.ClubName(lm.f.Home)
}

// ----- managing -----

// SubsLeft is how many changes the manager may still make.
func (lm *LiveMatch) SubsLeft() int { return lm.l.SubsLeft(lm.us) }

// Tactics is the club's current instructions, as they stand on the pitch.
func (lm *LiveMatch) Tactics() model.Tactics { return lm.l.Side(lm.us).Tactics }

// OnPitch lists the managed club's eleven in shirt order. A slot left empty by a
// sending-off is omitted.
func (lm *LiveMatch) OnPitch() []LivePlayer {
	s := lm.l.Side(lm.us)
	out := make([]LivePlayer, 0, 11)
	for i, p := range s.Lineup {
		if p == nil {
			continue
		}
		out = append(out, lm.describe(p, i, s.Slots[i]))
	}
	return out
}

// Bench lists the substitutes still available.
func (lm *LiveMatch) Bench() []LivePlayer {
	s := lm.l.Side(lm.us)
	out := make([]LivePlayer, 0, len(s.Bench))
	for _, p := range s.Bench {
		out = append(out, lm.describe(p, -1, p.Primary()))
	}
	return out
}

// describe fills in a player's live match state from the report so far.
func (lm *LiveMatch) describe(p *model.Player, slot int, pos model.Pos) LivePlayer {
	lp := LivePlayer{
		PlayerID: p.ID,
		Name:     p.Name,
		Slot:     slot,
		Pos:      pos,
		Fitness:  p.Fitness,
		Keeper:   p.IsGK(),
		// A knock is only reported in the event stream once the player actually
		// comes off, which is exactly the decision the manager is here to make.
		Injured: lm.l.Injured(p.ID),
	}
	for _, e := range lm.l.Result().Events {
		if e.Team != lm.us || e.Player != p.ID {
			continue
		}
		switch e.Type {
		case match.EvGoal, match.EvPenaltyScored:
			lp.Goals++
		case match.EvYellow:
			lp.Booked = true
		}
	}
	return lp
}

// Substitute brings a bench player on for one of the eleven, both given as
// indices into the Bench and OnPitch lists. It reports whether the change was
// made, with a line of text for the manager either way.
func (lm *LiveMatch) Substitute(pitchIdx, benchIdx int) (bool, string) {
	on := lm.Bench()
	pitch := lm.OnPitch()
	if pitchIdx < 0 || pitchIdx >= len(pitch) || benchIdx < 0 || benchIdx >= len(on) {
		return false, "Nobody selected."
	}
	off := pitch[pitchIdx]
	if err := lm.l.Substitute(lm.us, off.Slot, benchIdx); err != nil {
		return false, capitalise(err.Error()) + "."
	}
	return true, fmt.Sprintf("%s on for %s.", on[benchIdx].Name, off.Name)
}

// SetFormation changes the club's shape for the rest of this match without
// changing who is on the pitch. The club reverts to the shape set on the tactics
// screen for the next fixture.
func (lm *LiveMatch) SetFormation(f model.Formation) (bool, string) {
	t := lm.Tactics()
	if t.Formation == f {
		return false, "Already playing that shape."
	}
	t.Formation = f
	if err := lm.l.SetTactics(lm.us, t); err != nil {
		return false, capitalise(err.Error()) + "."
	}
	return true, fmt.Sprintf("Switched to %s.", f)
}

// SetTactics applies new instructions to the side already on the pitch, for this
// match only.
//
// It deliberately does not write back to the club. A touchline change is a
// response to how one game is going — going three at the back to see out a lead,
// pushing the line up when chasing — and carrying it into the next fixture would
// silently rewrite a selection the manager made in cold blood. The tactics screen
// is where a lasting change is made; here the whistle takes it all back.
func (lm *LiveMatch) SetTactics(t model.Tactics) (bool, string) {
	if err := lm.l.SetTactics(lm.us, t); err != nil {
		return false, capitalise(err.Error()) + "."
	}
	return true, "Instructions passed on."
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}
