package match

import (
	"errors"
	"math"

	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// Errors returned when a touchline instruction cannot be carried out.
var (
	ErrMatchOver   = errors.New("the match has finished")
	ErrNoSubsLeft  = errors.New("no substitutions left")
	ErrNotOnPitch  = errors.New("that player is not on the pitch")
	ErrNotOnBench  = errors.New("that player is not on the bench")
	ErrKeeperOnly  = errors.New("only a goalkeeper can go in goal")
	ErrNoFormation = errors.New("no such formation")
)

// Live is a match in progress, played out a minute at a time so a manager can
// watch it unfold and intervene from the touchline.
//
// This is the whole match engine: Sim is Begin plus PlayOut, so a match resolved
// instantly and one managed minute by minute cannot diverge. Pausing consumes no
// randomness and neither does any touchline instruction, which is what lets a
// match played in fragments come out exactly like one played straight through —
// TestLiveEqualsSim holds that line.
type Live struct {
	r     *rng.R
	res   *Result
	sides [2]*Side
	st    *tracker

	possMinutes [2]int
	minute      int // minutes completed so far
	total       int // regulation plus stoppage, fixed at kickoff
	done        bool
}

// Begin sets a match up and leaves it at kickoff, with nothing yet played.
func Begin(r *rng.R, home, away *Side, attendance uint32) *Live {
	l := &Live{
		r: r,
		res: &Result{
			HomeClub:   home.ClubID,
			AwayClub:   away.ClubID,
			Attendance: attendance,
			Events:     make([]Event, 0, 40),
		},
		sides: [2]*Side{home, away},
	}
	l.st = newTracker(l.sides)
	// Stoppage is drawn at kickoff so the length of the match is settled before
	// anyone can influence it.
	l.total = regulationMins + r.Range(2, 6)
	return l
}

// Minute is how far the match has been played, in minutes.
func (l *Live) Minute() int { return l.minute }

// Done reports whether the final whistle has gone.
func (l *Live) Done() bool { return l.done }

// Side returns one of the two teams as it currently stands, 0 for the home side.
func (l *Live) Side(team uint8) *Side { return l.sides[team&1] }

// Result is the match report. It is filled in as the match runs and is only
// complete — ratings, possession, minutes played — once Done reports true.
func (l *Live) Result() *Result { return l.res }

// SubsLeft is how many changes a side may still make.
func (l *Live) SubsLeft(team uint8) int { return maxSubs - l.sides[team&1].subsUsed }

// TakeCharge hands a side's changes to a manager working the touchline, so the
// engine stops making discretionary substitutions for them. It still forces one
// when a player is injured and the manager has not acted.
//
// This is what makes a watched match differ from a skipped one, and rightly so:
// the simulation is identical either way, but somebody else is picking the subs.
func (l *Live) TakeCharge(team uint8) { l.sides[team&1].managed = true }

// Injured reports whether a player has picked up a knock and is still on the
// pitch, which is not in the event stream until they actually come off.
func (l *Live) Injured(playerID uint32) bool { return l.st.injuries[playerID] > 0 }

// Step plays the next minute and reports whether the match is still running.
func (l *Live) Step() bool {
	if l.done {
		return false
	}
	l.minute++
	l.playMinute(l.minute)
	if l.minute >= l.total {
		l.complete()
		return false
	}
	return true
}

// PlayOut runs the match to full time. It does nothing to a finished match, so
// it is always safe to call before reading the result.
func (l *Live) PlayOut() {
	for l.Step() {
	}
}

// Substitute brings the bench player at benchIdx on for the starter in slot.
func (l *Live) Substitute(team uint8, slot, benchIdx int) error {
	if l.done {
		return ErrMatchOver
	}
	s := l.sides[team&1]
	if s.subsUsed >= maxSubs {
		return ErrNoSubsLeft
	}
	if slot < 0 || slot > 10 || s.Lineup[slot] == nil {
		return ErrNotOnPitch
	}
	if benchIdx < 0 || benchIdx >= len(s.Bench) {
		return ErrNotOnBench
	}
	if s.Bench[benchIdx].IsGK() != (s.Slots[slot] == model.GK) {
		return ErrKeeperOnly
	}
	l.st.applySub(l.res, s, team&1, slot, benchIdx, uint8(l.minute))
	return nil
}

// SetTactics puts a side on new instructions mid-match. A change of formation
// reshapes the eleven already on the pitch rather than reselecting them, so the
// manager keeps the players they chose and only the shape moves.
func (l *Live) SetTactics(team uint8, t model.Tactics) error {
	if l.done {
		return ErrMatchOver
	}
	if t.Formation >= model.NumFormations {
		return ErrNoFormation
	}
	s := l.sides[team&1]
	shape := t.Formation != s.Tactics.Formation
	s.Tactics = t
	if shape {
		s.reshape(t.Formation)
	} else {
		s.recompute()
	}
	l.res.Events = append(l.res.Events, Event{Minute: uint8(l.minute), Team: team & 1, Type: EvShape,
		Home: uint8(l.res.HomeGoals), Away: uint8(l.res.AwayGoals)})
	return nil
}

// playMinute is one minute of football.
func (l *Live) playMinute(minute int) {
	r, res, sides, st := l.r, l.res, l.sides, l.st
	home, away := sides[0], sides[1]

	if minute == 46 {
		res.Events = append(res.Events, Event{Minute: 45, Team: 255, Type: EvHalfTime,
			Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals)})
		// Half-time recovery: a short breather restores a little fitness.
		st.recover(sides, 4)
	}

	// Who has the ball this minute.
	mh := math.Pow(home.midfield*1.04, 1.35) // slight home edge on control
	ma := math.Pow(away.midfield, 1.35)
	att := 1
	if r.Float() < mh/(mh+ma) {
		att = 0
	}
	l.possMinutes[att]++

	// Fouls belong to the minute's defending side.
	st.foulCheck(r, res, sides, uint8(1-att), uint8(minute))

	// Game state. A side protecting a lead drops deeper as the clock runs
	// down; the team chasing the game commits more players forward and
	// accepts more risk. This produces late comebacks and the draw rate
	// real leagues show, which a purely strength-based model never reaches.
	lead := res.HomeGoals - res.AwayGoals
	hAtk, hDef := gameState(lead, minute)
	aAtk, aDef := gameState(-lead, minute)

	hA := home.attack * hAtk * homeAttackEdge
	hD := home.defence * hDef * homeDefEdge
	aA := away.attack * aAtk
	aD := away.defence * aDef

	// Each side's threat is what its attack does to the other's defence.
	//
	// Crucially, the two threats decide only how chances are *shared*, not
	// how many there are. A mismatch in real football produces a lopsided
	// split of roughly the usual number of chances — a 20-6 shot count, not
	// forty shots — so total chances are held near constant. Letting the
	// count itself grow with the mismatch is what inflates league-wide
	// scoring whenever squads are unevenly worn down.
	rH := hA / math.Max(aD, 0.15)
	rA := aA / math.Max(hD, 0.15)
	thH := math.Pow(rH, threatExponent)
	thA := math.Pow(rA, threatExponent)

	tempo := 0.88 + 0.24*float64(int(home.Tactics.Tempo)+int(away.Tactics.Tempo))/200
	if r.Chance(baseChanceRate * tempo) {
		ct, ratio := uint8(1), rA
		if r.Float() < thH/(thH+thA) {
			ct, ratio = 0, rH
		}
		st.chance(r, res, sides, ct, uint8(minute), ratio)
	} else if r.Chance(0.100) {
		// A move that breaks down can still win a corner or run offside.
		if r.Chance(0.72) {
			res.Stats[att].Corners++
		} else {
			res.Stats[att].Offsides++
		}
	}

	// Fatigue and substitutions.
	st.drain(sides, r)
	if minute >= 55 && minute <= 85 {
		for i := 0; i < 2; i++ {
			st.maybeSub(r, res, sides, uint8(i), uint8(minute))
		}
	}
}

// complete blows the final whistle and fills in everything that can only be
// known once the match is over.
func (l *Live) complete() {
	l.done = true
	l.res.Events = append(l.res.Events, Event{Minute: uint8(regulationMins), Team: 255, Type: EvFullTime,
		Home: uint8(l.res.HomeGoals), Away: uint8(l.res.AwayGoals)})

	// Possession percentages.
	if tot := l.possMinutes[0] + l.possMinutes[1]; tot > 0 {
		p := int(math.Round(float64(l.possMinutes[0]) * 100 / float64(tot)))
		l.res.Stats[0].Possession = uint8(p)
		l.res.Stats[1].Possession = uint8(100 - p)
	}

	l.st.finish(l.r, l.res, l.sides)
}
