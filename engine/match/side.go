package match

import (
	"math"
	"sort"

	"sportsim/engine/model"
)

// Side is one team as it takes the field: eleven players bound to the eleven
// slots of a formation, a bench, and the instructions they are playing under.
type Side struct {
	ClubID  uint16
	Name    string
	Tactics model.Tactics

	Slots      [11]model.Pos
	Lineup     [11]*model.Player
	Bench      []*model.Player
	Reputation uint8

	// Derived at kickoff and refreshed after substitutions.
	attack   float64
	midfield float64
	defence  float64

	subsUsed int
}

// condition scales a player's effective rating by how ready they are to play:
// fitness, match sharpness, morale and current form all pull on it.
func condition(p *model.Player) float64 {
	fit := 0.60 + 0.40*float64(p.Fitness)/100
	sharp := 0.88 + 0.12*float64(p.Sharpness)/100
	mor := 0.955 + 0.09*float64(p.Morale)/100
	frm := 1 + float64(p.Form)*0.0045
	return fit * sharp * mor * frm
}

// effective is a player's rating in a slot, after condition.
func effective(p *model.Player, pos model.Pos) float64 {
	if p == nil {
		return 20
	}
	return p.Rating(pos) * condition(p)
}

// AutoPick fills a side's lineup and bench with the strongest legal selection
// available from the squad, respecting the club's chosen formation.
//
// It uses greedy maximum-weight matching: repeatedly take the single best
// remaining (slot, player) pairing. That is not guaranteed optimal like the
// Hungarian algorithm would be, but it is within a rating point in practice and
// runs fast enough to reselect every club every match day.
func AutoPick(squad []*model.Player, t model.Tactics) (lineup [11]*model.Player, bench []*model.Player) {
	slots := t.Formation.Slots()

	avail := make([]*model.Player, 0, len(squad))
	for _, p := range squad {
		if p.Available() {
			avail = append(avail, p)
		}
	}
	// Considering the whole squad for every slot is wasteful; the best 26 by
	// raw ability always contain the best eleven.
	sort.SliceStable(avail, func(a, b int) bool {
		return effective(avail[a], avail[a].Primary()) > effective(avail[b], avail[b].Primary())
	})
	if len(avail) > 26 {
		avail = avail[:26]
	}

	taken := make([]bool, len(avail))
	filled := [11]bool{}

	for n := 0; n < 11; n++ {
		bestSlot, bestPlayer, bestScore := -1, -1, -1.0
		for s := 0; s < 11; s++ {
			if filled[s] {
				continue
			}
			for i, p := range avail {
				if taken[i] {
					continue
				}
				sc := effective(p, slots[s])
				if sc > bestScore {
					bestScore, bestSlot, bestPlayer = sc, s, i
				}
			}
		}
		if bestSlot < 0 || bestPlayer < 0 {
			break // squad too small to field eleven
		}
		lineup[bestSlot] = avail[bestPlayer]
		filled[bestSlot] = true
		taken[bestPlayer] = true
	}

	// Bench: the best remaining, biased to cover a keeper and each outfield unit.
	rest := make([]*model.Player, 0, len(avail))
	for i, p := range avail {
		if !taken[i] {
			rest = append(rest, p)
		}
	}
	sort.SliceStable(rest, func(a, b int) bool {
		return effective(rest[a], rest[a].Primary()) > effective(rest[b], rest[b].Primary())
	})
	bench = make([]*model.Player, 0, 9)
	// Always carry a reserve keeper if one exists.
	for i, p := range rest {
		if p.IsGK() {
			bench = append(bench, p)
			rest = append(rest[:i], rest[i+1:]...)
			break
		}
	}
	for _, p := range rest {
		if len(bench) >= 9 {
			break
		}
		if p.IsGK() {
			continue // one spare keeper is enough
		}
		bench = append(bench, p)
	}
	return lineup, bench
}

// NewSide assembles a Side and computes its opening strength.
func NewSide(clubID uint16, name string, rep uint8, t model.Tactics, lineup [11]*model.Player, bench []*model.Player) *Side {
	s := &Side{
		ClubID:     clubID,
		Name:       name,
		Tactics:    t,
		Slots:      t.Formation.Slots(),
		Lineup:     lineup,
		Bench:      bench,
		Reputation: rep,
	}
	s.recompute()
	return s
}

// recompute derives the three unit strengths from who is currently on the pitch.
//
// Each player contributes to every phase, weighted by the unit they occupy: a
// midfielder helps defend and attack, a centre-back contributes little going
// forward. Summing contributions rather than averaging them means formation
// choice matters — five defenders really are harder to break down.
func (s *Side) recompute() {
	// Player ratings across Europe's top two tiers occupy a narrow band, but the
	// footballing gap between a 60 and an 85 is enormous. Contributions are
	// therefore raised to a power before being summed, so a 1.35x rating
	// advantage becomes roughly a 1.7x quality advantage and strong sides
	// dominate the way they do in reality.
	//
	// The exponent is a balance: too low and title races are decided by coin
	// flips, too high and the weakest club in a division becomes hopeless rather
	// than merely bad, which is neither realistic nor much fun to manage.
	const ref = 70.0
	quality := func(r float64) float64 { return math.Pow(r/ref, 1.85) }

	var gk, defSum, midSum, attSum float64

	for i, p := range s.Lineup {
		if p == nil {
			continue
		}
		q := quality(effective(p, s.Slots[i]))
		switch s.Slots[i].Line() {
		case model.LineGK:
			gk = q
		case model.LineDef:
			defSum += q
		case model.LineMid:
			midSum += q
		case model.LineAtt:
			attSum += q
		}
	}
	if gk == 0 {
		gk = quality(25) // playing without a keeper
	}

	t := s.Tactics
	mentality := float64(t.Mentality) / 100 // 0 defensive .. 1 attacking

	// Defensive mass: keeper, the back line, and midfield screening.
	dm := gk*1.15 + defSum*1.00 + midSum*0.34 + attSum*0.07
	// Attacking mass: forwards, midfield support, overlapping defenders.
	am := attSum*1.00 + midSum*0.46 + defSum*0.11
	// Control of midfield decides who has the ball.
	mm := midSum*1.00 + defSum*0.20 + attSum*0.26

	// Divisors are the masses a 4-3-3 of rating-70 players produces, so an
	// average side scores 1.0 in every phase and the ratios stay near unity.
	s.defence = dm / 6.38
	s.attack = am / 4.82
	s.midfield = mm / 4.58

	// A high line and heavy pressing win the ball higher up but leave gaps.
	press := float64(t.Pressing) / 100
	line := float64(t.LineHeight) / 100
	s.attack *= 0.86 + 0.28*mentality + 0.10*press
	s.defence *= 1.14 - 0.24*mentality - 0.10*(line-0.5)
	s.midfield *= 0.94 + 0.14*press

}

// starterIndex returns the lineup slot a player occupies, or -1.
func (s *Side) starterIndex(id uint32) int {
	for i, p := range s.Lineup {
		if p != nil && p.ID == id {
			return i
		}
	}
	return -1
}
