package match

import (
	"math"

	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// EventType enumerates everything that can happen in a match. The ordering is
// persisted in saved match reports, so append only.
type EventType uint8

const (
	EvKickoff EventType = iota
	EvGoal
	EvOwnGoal
	EvPenaltyScored
	EvPenaltyMissed
	EvShotSaved
	EvShotOff
	EvWoodwork
	EvYellow
	EvSecondYellow
	EvRed
	EvSub
	EvInjury
	EvHalfTime
	EvFullTime
)

// Event is a single incident. Player and Other are player IDs; Other carries
// the assister for a goal, the player coming on for a substitution, or the
// fouled player for a card.
type Event struct {
	Minute  uint8
	Team    uint8 // 0 home, 1 away, 255 neutral
	Type    EventType
	Player  uint32
	Other   uint32
	Home    uint8 // score after this event
	Away    uint8
	Variant uint8 // selects a commentary phrasing, fixed per event
}

// TeamStats is the box score for one side.
type TeamStats struct {
	Possession uint8
	Shots      uint16
	OnTarget   uint16
	Corners    uint16
	Fouls      uint16
	Offsides   uint16
	Yellows    uint16
	Reds       uint16
	XG         float64
}

// PlayerLine is one player's match record, used to update season tallies and
// to show a post-match player-rating table.
type PlayerLine struct {
	PlayerID uint32
	Team     uint8
	Minutes  uint8
	Goals    uint8
	Assists  uint8
	Yellow   bool
	Red      bool
	Rating   float64
	Injury   uint16 // days out, 0 if uninjured
}

// Result is everything the rest of the game needs to know about a played match.
type Result struct {
	HomeClub, AwayClub uint16
	HomeGoals          int
	AwayGoals          int
	Events             []Event
	Stats              [2]TeamStats
	Lines              []PlayerLine
	Attendance         uint32
}

// Played reports whether this result has been simulated.
func (r *Result) Played() bool { return r != nil && len(r.Events) > 0 }

// tuning constants, calibrated against real top-division rates:
// ~2.7 goals, ~25 shots, ~22 fouls and ~3.8 yellow cards per match, with the
// home side taking roughly 46% of points more than the away side.
const (
	baseXG         = 0.077 // average chance quality
	homeAttackEdge = 1.062
	homeDefEdge    = 1.035
	fatiguePerMin  = 0.34
	regulationMins = 90

	// baseChanceRate is the per-minute probability that a chance falls to one
	// side or the other. It sets the total number of shots in a match and is
	// deliberately independent of how mismatched the teams are.
	baseChanceRate = 0.262

	// threatExponent controls how sharply a strength advantage skews the share
	// of chances. Because it only redistributes chances, raising it makes strong
	// sides dominate without changing league-wide scoring.
	threatExponent = 1.62
)

// Sim plays a match and returns the result. The generator is consumed, so the
// same seed always produces the same match.
//
// The same code path serves both presentation modes: a full event list is
// always produced, and the UI either prints the final score immediately or
// reveals events minute by minute. There is no separate "quick" engine that
// could disagree with the detailed one.
func Sim(r *rng.R, home, away *Side, attendance uint32) *Result {
	res := &Result{
		HomeClub:   home.ClubID,
		AwayClub:   away.ClubID,
		Attendance: attendance,
		Events:     make([]Event, 0, 40),
	}

	sides := [2]*Side{home, away}
	st := newTracker(sides)

	var possMinutes [2]int
	stoppage := r.Range(2, 6)
	total := regulationMins + stoppage

	for minute := 1; minute <= total; minute++ {
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
		possMinutes[att]++

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

	res.Events = append(res.Events, Event{Minute: uint8(regulationMins), Team: 255, Type: EvFullTime,
		Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals)})

	// Possession percentages.
	tot := possMinutes[0] + possMinutes[1]
	if tot > 0 {
		p := int(math.Round(float64(possMinutes[0]) * 100 / float64(tot)))
		res.Stats[0].Possession = uint8(p)
		res.Stats[1].Possession = uint8(100 - p)
	}

	st.finish(r, res, sides)
	return res
}

// gameState returns attack and defence multipliers for a side that is `lead`
// goals ahead (negative when behind) at the given minute. The effect grows as
// the match wears on: a one-goal lead changes little at 20 minutes and a great
// deal at 85.
func gameState(lead, minute int) (atk, def float64) {
	if lead == 0 {
		return 1, 1
	}
	urgency := float64(minute) / regulationMins
	if urgency > 1 {
		urgency = 1
	}
	// Beyond two goals the shape stops changing much; the game is settled.
	mag := math.Min(math.Abs(float64(lead)), 2.5)

	if lead > 0 {
		return 1 - 0.095*urgency*mag, 1 + 0.100*urgency*mag
	}
	return 1 + 0.145*urgency*mag, 1 - 0.105*urgency*mag
}

// tracker holds mutable per-match bookkeeping that does not belong on Side.
type tracker struct {
	minutes  map[uint32]int     // minutes played
	entered  map[uint32]int     // minute the player came on
	pts      map[uint32]float64 // running rating contribution
	yellow   map[uint32]bool
	sentOff  map[uint32]bool
	goals    map[uint32]uint8
	assists  map[uint32]uint8
	injuries map[uint32]uint16
	teamOf   map[uint32]uint8

	// fatigue accumulates each player's exertion in floating point, alongside
	// the fitness they started with. Fitness itself is a uint8, so subtracting
	// a fractional per-minute cost directly from it would truncate every minute
	// and burn a full point each time — a match would cost ninety points of
	// condition instead of thirty.
	fatigue  map[uint32]float64
	startFit map[uint32]float64

	// all indexes every player involved in the match, including those later
	// substituted or sent off, who are no longer reachable via Lineup or Bench.
	all    map[uint32]*model.Player
	lineOf map[uint32]model.Line

	// order lists everyone who took the field, in the order they did so.
	// Ratings draw random noise, so they must be assigned in a fixed sequence:
	// ranging over a map here would make match results depend on Go's
	// randomised map iteration and break save/load reproducibility.
	order []uint32
}

func newTracker(sides [2]*Side) *tracker {
	t := &tracker{
		minutes: map[uint32]int{}, entered: map[uint32]int{}, pts: map[uint32]float64{},
		yellow: map[uint32]bool{}, sentOff: map[uint32]bool{}, goals: map[uint32]uint8{},
		assists: map[uint32]uint8{}, injuries: map[uint32]uint16{}, teamOf: map[uint32]uint8{},
		all: map[uint32]*model.Player{}, lineOf: map[uint32]model.Line{},
		fatigue: map[uint32]float64{}, startFit: map[uint32]float64{},
	}
	for si, s := range sides {
		for i, p := range s.Lineup {
			if p == nil {
				continue
			}
			t.entered[p.ID] = 0
			t.pts[p.ID] = 6.0
			t.teamOf[p.ID] = uint8(si)
			t.all[p.ID] = p
			t.lineOf[p.ID] = s.Slots[i].Line()
			t.order = append(t.order, p.ID)
			t.startFit[p.ID] = float64(p.Fitness)
		}
		for _, p := range s.Bench {
			t.teamOf[p.ID] = uint8(si)
			t.all[p.ID] = p
			t.startFit[p.ID] = float64(p.Fitness)
		}
	}
	return t
}

// foulCheck rolls for a foul by the given team, and any card that follows.
func (t *tracker) foulCheck(r *rng.R, res *Result, sides [2]*Side, team, minute uint8) {
	rate := 0.232 * (0.7 + 0.6*float64(sides[team].Tactics.Tackling)/100)
	if r.Chance(rate) {
		t.foul(r, res, sides, team, minute)
	}
}

// chance resolves one opportunity falling to the attacking side. ratio is that
// side's attacking strength measured against the opposition's defence, and
// shapes how good the opening is rather than whether it happened at all.
func (t *tracker) chance(r *rng.R, res *Result, sides [2]*Side, att, minute uint8, ratio float64) {
	a, d := sides[att], sides[1-att]

	shooter := t.pickShooter(r, a)
	if shooter < 0 {
		return
	}
	sp := a.Lineup[shooter]

	// A small share of chances come from the spot.
	if r.Chance(0.035) {
		t.penalty(r, res, sides, att, minute)
		return
	}

	res.Stats[att].Shots++

	// Both sides of the duel are scaled by condition. Scaling only the keeper
	// would make every goal easier whenever teams are tired, inflating scoring
	// across the whole calendar rather than just tilting the individual duel.
	fin := (float64(sp.A(model.AtkFinishing))*0.5 +
		float64(sp.A(model.MenComposure))*0.25 +
		float64(sp.A(model.MenPositioning))*0.25) / 100 * condition(sp)
	gk := d.Lineup[0]
	gkQ := 0.72
	if gk != nil {
		gkQ = (float64(gk.A(model.GKReflexes))*0.40 +
			float64(gk.A(model.GKDiving))*0.30 +
			float64(gk.A(model.GKPositioning))*0.30) / 100 * condition(gk)
	}

	// The finish is a duel, scored as a ratio rather than as two independent
	// terms, so that tiring both teams equally does not change the outcome.
	duel := math.Pow(fin/math.Max(gkQ, 0.12), 0.90)
	duel = math.Max(0.45, math.Min(duel, 2.10))

	xg := baseXG * math.Pow(ratio, 0.42) * duel
	xg = math.Max(0.012, math.Min(xg, 0.62))
	res.Stats[att].XG += xg

	switch {
	case r.Chance(xg):
		t.scoreGoal(r, res, sides, att, minute, sp, shooter, false)
	case r.Chance(0.26):
		res.Stats[att].OnTarget++
		res.Events = append(res.Events, Event{Minute: minute, Team: att, Type: EvShotSaved,
			Player: sp.ID, Other: gkID(gk), Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals),
			Variant: uint8(r.Intn(4))})
		if gk != nil {
			t.pts[gk.ID] += 0.08
		}
		t.pts[sp.ID] += 0.02
		if r.Chance(0.34) {
			res.Stats[att].Corners++
		}
	case r.Chance(0.07):
		res.Events = append(res.Events, Event{Minute: minute, Team: att, Type: EvWoodwork,
			Player: sp.ID, Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals),
			Variant: uint8(r.Intn(3))})
		t.pts[sp.ID] += 0.05
	default:
		res.Events = append(res.Events, Event{Minute: minute, Team: att, Type: EvShotOff,
			Player: sp.ID, Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals),
			Variant: uint8(r.Intn(4))})
		t.pts[sp.ID] -= 0.06
		if r.Chance(0.22) {
			res.Stats[att].Corners++
		}
	}
}

func gkID(p *model.Player) uint32 {
	if p == nil {
		return 0
	}
	return p.ID
}

// scoreGoal records a goal, picks an assister and updates the score.
func (t *tracker) scoreGoal(r *rng.R, res *Result, sides [2]*Side, att, minute uint8, sp *model.Player, shooterSlot int, penalty bool) {
	res.Stats[att].OnTarget++
	if att == 0 {
		res.HomeGoals++
	} else {
		res.AwayGoals++
	}
	t.goals[sp.ID]++
	t.pts[sp.ID] += 1.05

	ev := Event{Minute: minute, Team: att, Type: EvGoal, Player: sp.ID,
		Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals), Variant: uint8(r.Intn(5))}
	if penalty {
		ev.Type = EvPenaltyScored
	} else if a := t.pickAssist(r, sides[att], shooterSlot); a != nil {
		ev.Other = a.ID
		t.assists[a.ID]++
		t.pts[a.ID] += 0.65
	}
	res.Events = append(res.Events, ev)

	// Conceding costs the defence and the keeper a little.
	d := sides[1-att]
	for i, p := range d.Lineup {
		if p == nil {
			continue
		}
		switch d.Slots[i].Line() {
		case model.LineGK:
			t.pts[p.ID] -= 0.28
		case model.LineDef:
			t.pts[p.ID] -= 0.16
		}
	}
}

// penalty resolves a spot kick.
func (t *tracker) penalty(r *rng.R, res *Result, sides [2]*Side, att, minute uint8) {
	a, d := sides[att], sides[1-att]
	taker := t.penaltyTaker(a)
	if taker == nil {
		return
	}
	res.Stats[att].Shots++
	res.Stats[att].XG += 0.78

	skill := float64(taker.A(model.MenPenalties))*0.6 + float64(taker.A(model.MenComposure))*0.4
	gk := d.Lineup[0]
	gkQ := 60.0
	if gk != nil {
		gkQ = float64(gk.A(model.GKReflexes))*0.6 + float64(gk.A(model.GKDiving))*0.4
	}
	p := 0.78 + (skill-70)*0.0022 - (gkQ-70)*0.0016

	if r.Chance(math.Max(0.45, math.Min(p, 0.93))) {
		slot := a.starterIndex(taker.ID)
		t.scoreGoal(r, res, sides, att, minute, taker, slot, true)
		return
	}
	res.Stats[att].OnTarget++
	res.Events = append(res.Events, Event{Minute: minute, Team: att, Type: EvPenaltyMissed,
		Player: taker.ID, Other: gkID(gk), Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals),
		Variant: uint8(r.Intn(3))})
	t.pts[taker.ID] -= 0.75
	if gk != nil {
		t.pts[gk.ID] += 0.55
	}
}

// foul resolves a foul by the given team, and any card that follows.
func (t *tracker) foul(r *rng.R, res *Result, sides [2]*Side, team, minute uint8) {
	s := sides[team]
	res.Stats[team].Fouls++

	// The offender is weighted by aggression and by how much defending they do.
	w := make([]float64, 11)
	for i, p := range s.Lineup {
		if p == nil || t.sentOff[p.ID] {
			continue
		}
		base := 1.0
		switch s.Slots[i].Line() {
		case model.LineGK:
			base = 0.12
		case model.LineDef:
			base = 1.5
		case model.LineMid:
			base = 1.35
		case model.LineAtt:
			base = 0.7
		}
		w[i] = base * (0.5 + float64(p.A(model.MenAggression))/100)
	}
	i := r.Pick(w)
	if i < 0 {
		return
	}
	p := s.Lineup[i]

	cardChance := 0.192 * (0.75 + 0.5*float64(s.Tactics.Tackling)/100)
	if !r.Chance(cardChance) {
		return
	}

	// Straight reds are rare; a second yellow is the usual route to a dismissal.
	if r.Chance(0.014) {
		t.sendOff(res, sides, team, minute, p, EvRed)
		return
	}
	if t.yellow[p.ID] {
		// Already booked: usually the referee shows leniency, or the manager
		// has taken the player off the edge. Only a minority become dismissals.
		if r.Chance(0.22) {
			t.sendOff(res, sides, team, minute, p, EvSecondYellow)
		}
		return
	}
	t.yellow[p.ID] = true
	res.Stats[team].Yellows++
	t.pts[p.ID] -= 0.25
	res.Events = append(res.Events, Event{Minute: minute, Team: team, Type: EvYellow,
		Player: p.ID, Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals), Variant: uint8(r.Intn(3))})
}

// sendOff removes a player and weakens the side for the rest of the match.
func (t *tracker) sendOff(res *Result, sides [2]*Side, team, minute uint8, p *model.Player, kind EventType) {
	s := sides[team]
	t.sentOff[p.ID] = true
	t.pts[p.ID] -= 1.6
	res.Stats[team].Reds++
	if kind == EvSecondYellow {
		res.Stats[team].Yellows++
	}
	res.Events = append(res.Events, Event{Minute: minute, Team: team, Type: kind,
		Player: p.ID, Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals)})

	if i := s.starterIndex(p.ID); i >= 0 {
		t.minutes[p.ID] = int(minute) - t.entered[p.ID]
		s.Lineup[i] = nil
	}
	s.recompute()
	// Ten men defend deeper and create far less.
	s.attack *= 0.74
	s.defence *= 0.90
	s.midfield *= 0.80
}

// drain applies per-minute fatigue and rolls for injuries.
func (t *tracker) drain(sides [2]*Side, r *rng.R) {
	for _, s := range sides {
		for _, p := range s.Lineup {
			if p == nil {
				continue
			}
			// Better stamina burns less; heavy pressing burns more.
			cost := fatiguePerMin * (1.55 - float64(p.A(model.PowStamina))/100) *
				(0.85 + 0.30*float64(s.Tactics.Pressing)/100)
			t.fatigue[p.ID] += cost
			t.applyFitness(p)
			// Tired players get hurt more often.
			risk := 0.00019 * (1.9 - float64(p.Fitness)/100)
			if r.Chance(risk) && t.injuries[p.ID] == 0 {
				t.injuries[p.ID] = uint16(r.Range(4, 70))
			}
		}
	}
}

// applyFitness writes accumulated fatigue back to the player's uint8 fitness.
func (t *tracker) applyFitness(p *model.Player) {
	f := t.startFit[p.ID] - t.fatigue[p.ID]
	if f < 0 {
		f = 0
	}
	if f > 100 {
		f = 100
	}
	p.Fitness = uint8(f)
}

func (t *tracker) recover(sides [2]*Side, amount float64) {
	for _, s := range sides {
		for _, p := range s.Lineup {
			if p == nil {
				continue
			}
			if t.fatigue[p.ID] -= amount; t.fatigue[p.ID] < 0 {
				t.fatigue[p.ID] = 0
			}
			t.applyFitness(p)
		}
	}
}

// maybeSub lets the AI manager replace a tired, injured or ineffective player.
func (t *tracker) maybeSub(r *rng.R, res *Result, sides [2]*Side, team, minute uint8) {
	s := sides[team]
	if s.subsUsed >= 5 || len(s.Bench) == 0 {
		return
	}

	// Find the starter most in need of coming off.
	worst, worstNeed := -1, 0.0
	for i, p := range s.Lineup {
		if p == nil {
			continue
		}
		need := 0.0
		if t.injuries[p.ID] > 0 {
			need = 100
		} else if p.Fitness < 62 {
			need = float64(62-p.Fitness) / 10
		}
		if need > worstNeed {
			worst, worstNeed = i, need
		}
	}
	if worst < 0 {
		return
	}
	// Injuries force a change; fatigue only prompts one.
	if worstNeed < 90 && !r.Chance(0.22) {
		return
	}

	slot := s.Slots[worst]
	off := s.Lineup[worst]

	best, bestScore := -1, -1.0
	for i, b := range s.Bench {
		if b.IsGK() != (slot == model.GK) {
			continue
		}
		if sc := effective(b, slot); sc > bestScore {
			best, bestScore = i, sc
		}
	}
	if best < 0 {
		return
	}
	on := s.Bench[best]
	s.Bench = append(s.Bench[:best], s.Bench[best+1:]...)

	t.minutes[off.ID] = int(minute) - t.entered[off.ID]
	s.Lineup[worst] = on
	t.entered[on.ID] = int(minute)
	t.pts[on.ID] = 6.0
	t.lineOf[on.ID] = slot.Line()
	t.order = append(t.order, on.ID)
	t.startFit[on.ID] = float64(on.Fitness)
	s.subsUsed++
	s.recompute()

	res.Events = append(res.Events, Event{Minute: minute, Team: team, Type: EvSub,
		Player: off.ID, Other: on.ID, Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals)})
	if t.injuries[off.ID] > 0 {
		res.Events = append(res.Events, Event{Minute: minute, Team: team, Type: EvInjury,
			Player: off.ID, Home: uint8(res.HomeGoals), Away: uint8(res.AwayGoals)})
	}
}

// pickShooter chooses who takes the shot, weighted by role and finishing.
func (t *tracker) pickShooter(r *rng.R, s *Side) int {
	w := make([]float64, 11)
	for i, p := range s.Lineup {
		if p == nil {
			continue
		}
		var base float64
		switch s.Slots[i] {
		case model.ST, model.CF:
			base = 4.3
		case model.LW, model.RW:
			base = 3.4
		case model.CAM:
			base = 2.9
		case model.CM, model.LM, model.RM:
			base = 1.5
		case model.CDM:
			base = 0.65
		case model.CB:
			base = 0.42
		case model.GK:
			base = 0.004
		default:
			base = 0.38
		}
		quality := (float64(p.A(model.AtkFinishing)) + float64(p.A(model.MenPositioning))) / 140
		w[i] = base * quality
	}
	return r.Pick(w)
}

// pickAssist chooses the assister, or nil for an unassisted goal.
func (t *tracker) pickAssist(r *rng.R, s *Side, shooterSlot int) *model.Player {
	if r.Chance(0.22) {
		return nil // solo effort, rebound, deflection
	}
	w := make([]float64, 11)
	for i, p := range s.Lineup {
		if p == nil || i == shooterSlot {
			continue
		}
		var base float64
		switch s.Slots[i] {
		case model.CAM:
			base = 3.2
		case model.LW, model.RW, model.LM, model.RM:
			base = 2.9
		case model.CM:
			base = 2.2
		case model.ST, model.CF:
			base = 1.7
		case model.LB, model.RB, model.LWB, model.RWB:
			base = 1.5
		case model.CDM:
			base = 0.9
		case model.CB:
			base = 0.35
		case model.GK:
			base = 0.02
		}
		quality := (float64(p.A(model.MenVision)) + float64(p.A(model.AtkCrossing)) +
			float64(p.A(model.AtkShortPass))) / 210
		w[i] = base * quality
	}
	i := r.Pick(w)
	if i < 0 {
		return nil
	}
	return s.Lineup[i]
}

// penaltyTaker returns the designated taker, or the best available substitute
// for that duty if the nominated player is not on the pitch.
func (t *tracker) penaltyTaker(s *Side) *model.Player {
	if s.Tactics.PenaltyTaker != 0 {
		if i := s.starterIndex(s.Tactics.PenaltyTaker); i >= 0 {
			return s.Lineup[i]
		}
	}
	var best *model.Player
	bestScore := -1.0
	for i, p := range s.Lineup {
		if p == nil || s.Slots[i] == model.GK {
			continue
		}
		sc := float64(p.A(model.MenPenalties))*0.7 + float64(p.A(model.MenComposure))*0.3
		if sc > bestScore {
			best, bestScore = p, sc
		}
	}
	return best
}

// finish converts running tallies into per-player match records.
func (t *tracker) finish(r *rng.R, res *Result, sides [2]*Side) {
	final := [2]int{res.HomeGoals, res.AwayGoals}

	for _, id := range t.order {
		entered := t.entered[id]
		mins, ok := t.minutes[id]
		if !ok {
			mins = regulationMins - entered
		}
		if mins < 0 {
			mins = 0
		}
		team := t.teamOf[id]

		p := t.all[id]
		if p == nil {
			continue
		}

		rating := t.pts[id]

		// Result and clean-sheet adjustments, scaled by time on the pitch.
		share := float64(mins) / regulationMins
		gf, ga := final[team], final[1-team]
		switch {
		case gf > ga:
			rating += 0.35 * share
		case gf < ga:
			rating -= 0.28 * share
		}
		if line, ok := t.lineOf[id]; ok {
			if ga == 0 && (line == model.LineGK || line == model.LineDef) && mins > 60 {
				rating += 0.55
			}
			if line == model.LineGK {
				// Keepers are judged mostly on goals conceded.
				rating += 0.30 * float64(4-ga)
			}
		}
		// A little noise so identical performances are not identically rated.
		rating += r.Norm(0, 0.28)
		rating = math.Max(1.0, math.Min(rating, 10.0))
		rating = math.Round(rating*10) / 10

		res.Lines = append(res.Lines, PlayerLine{
			PlayerID: id,
			Team:     team,
			Minutes:  uint8(mins),
			Goals:    t.goals[id],
			Assists:  t.assists[id],
			Yellow:   t.yellow[id],
			Red:      t.sentOff[id],
			Rating:   rating,
			Injury:   t.injuries[id],
		})

		// Match sharpness climbs with game time and decays without it.
		sh := int(p.Sharpness) + mins/9 - 1
		if sh > 100 {
			sh = 100
		}
		if sh < 0 {
			sh = 0
		}
		p.Sharpness = uint8(sh)
	}
}
