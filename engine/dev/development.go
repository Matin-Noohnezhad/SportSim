// Package dev models how players change over a career: growth toward their
// ceiling, physical decline, fitness recovery, injury healing, form and morale.
package dev

import (
	"math"

	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// Attribute groups. They age on very different curves: a winger loses their
// legs years before they lose their reading of the game.
const (
	groupPhysical  = iota // pace, agility, stamina — peaks early, fades first
	groupTechnical        // ball skills — long plateau
	groupMental           // vision, composure, positioning — improves into the 30s
	groupPower            // strength, jumping — peaks late-20s
)

var attrGroup = func() [model.NumAttr]int {
	var g [model.NumAttr]int
	for i := range g {
		g[i] = groupTechnical
	}
	for _, a := range []int{model.MovAcceleration, model.MovSprintSpeed, model.MovAgility,
		model.MovBalance, model.PowStamina} {
		g[a] = groupPhysical
	}
	for _, a := range []int{model.PowStrength, model.PowJumping, model.PowShotPower} {
		g[a] = groupPower
	}
	for _, a := range []int{model.MenVision, model.MenComposure, model.MenPositioning,
		model.MenInterceptions, model.MovReactions, model.DefMarking, model.MenAggression,
		model.GKPositioning} {
		g[a] = groupMental
	}
	return g
}()

// ageCurve returns the yearly drift for an attribute group at a given age,
// in attribute points. Positive means natural improvement is still available,
// negative means the body is going the other way regardless of training.
func ageCurve(age, group int) float64 {
	a := float64(age)
	switch group {
	case groupPhysical:
		switch {
		case age <= 21:
			return 1.1
		case age <= 26:
			return 0.15
		case age <= 29:
			return -0.55
		default:
			return -0.95 - (a-29)*0.28
		}
	case groupPower:
		switch {
		case age <= 23:
			return 1.3
		case age <= 30:
			return 0.10
		default:
			return -0.55 - (a-30)*0.16
		}
	case groupMental:
		switch {
		case age <= 24:
			return 1.2
		case age <= 31:
			return 0.45
		case age <= 34:
			return 0.05
		default:
			return -0.35
		}
	default: // technical
		switch {
		case age <= 23:
			return 1.5
		case age <= 28:
			return 0.35
		case age <= 32:
			return -0.10
		default:
			return -0.60 - (a-32)*0.15
		}
	}
}

// Context supplies the club-level inputs that shape a player's development.
type Context struct {
	TrainingFacilities uint8   // 1-100
	MinutesShare       float64 // 0-1, share of available league minutes played
}

// Monthly advances one player by a month of training and aging. Development is
// applied in small monthly steps rather than one annual jump, so a wonderkid
// visibly improves across a season instead of transforming overnight.
func Monthly(r *rng.R, p *model.Player, age int, ctx Context) {
	ca := p.CurrentAbility()
	room := float64(p.Potential) - ca

	// Players close to their ceiling improve slowly; those far below it, fast.
	roomFactor := math.Max(0, math.Min(room/18, 1.4))

	training := 0.55 + 0.9*float64(ctx.TrainingFacilities)/100
	// Game time is the strongest developer for the young, and matters less once
	// a player is established.
	playing := 0.55 + 0.85*math.Min(ctx.MinutesShare*1.6, 1)
	if age > 26 {
		playing = 0.80 + 0.35*math.Min(ctx.MinutesShare*1.6, 1)
	}

	// Which attributes this player's training is invested in.
	weights := model.PositionWeights(p.Primary())
	var wsum float64
	for _, w := range weights {
		wsum += w.W
	}

	for i := 0; i < model.NumAttr; i++ {
		g := attrGroup[i]
		drift := ageCurve(age, g) / 12 // per month

		if drift > 0 {
			// Growth is earned: it needs headroom, facilities and minutes, and
			// is concentrated in the attributes the player's role demands.
			focus := 0.35
			for _, w := range weights {
				if w.Attr == i {
					focus = 0.35 + 1.65*(w.W/wsum)*float64(len(weights))/3
					break
				}
			}
			drift *= roomFactor * training * playing * focus
		} else {
			// Decline is softened a little by good facilities, never stopped.
			drift *= 1.12 - 0.28*float64(ctx.TrainingFacilities)/100
		}

		// Month-to-month noise, so two identical players diverge over a career.
		drift += r.Norm(0, 0.10)

		v := float64(p.Attr[i]) + drift
		// Attributes hold their value probabilistically rather than needing a
		// fractional store: a drift of +0.3 is a 30% chance of +1 this month.
		lo := math.Floor(v)
		if r.Float() < v-lo {
			lo++
		}
		p.Attr[i] = clampAttr(lo)
	}

	// A player who overshoots their assessed ceiling has that ceiling revised.
	if newCA := p.CurrentAbility(); newCA > float64(p.Potential) {
		p.Potential = clampAttr(newCA)
	}
}

func clampAttr(v float64) uint8 {
	if v < 1 {
		return 1
	}
	if v > 99 {
		return 99
	}
	return uint8(math.Round(v))
}

// Daily applies the between-match housekeeping: injuries heal, fitness
// recovers, and sharpness decays for players who are not getting games.
func Daily(r *rng.R, p *model.Player, played bool) {
	if p.InjuryDays > 0 {
		p.InjuryDays--
		// An injured player loses condition fast.
		if p.Fitness > 40 {
			p.Fitness -= 1
		}
		if p.Sharpness > 0 {
			p.Sharpness--
		}
		if p.InjuryDays == 0 {
			p.Morale = up(p.Morale, 8)
		}
		return
	}

	if !played {
		// Recovery is quicker for the fit and the young.
		rate := 4 + int(p.Attr[model.PowStamina])/22
		p.Fitness = up(p.Fitness, uint8(rate))
	}

	// Morale drifts back toward a contented baseline. Like form, it must revert
	// or it pins to 0 or 100 and stops meaning anything.
	const baseMorale = 70
	if r.Chance(0.25) {
		switch {
		case p.Morale > baseMorale:
			p.Morale--
		case p.Morale < baseMorale:
			p.Morale++
		}
	}
	// Match sharpness fades without competitive minutes.
	if !played && p.Sharpness > 0 && r.Chance(0.30) {
		p.Sharpness--
	}
}

// Performance is what one player did in one match. It mirrors the match
// engine's player line without this package having to know about the engine,
// which is the caller's job to translate.
type Performance struct {
	Minutes    int
	Goals      uint8
	Penalties  uint8 // goals from the spot, counted within Goals
	Assists    uint8
	CleanSheet bool
	Rating     float64
	Yellow     bool
	Red        bool
	Injury     uint16 // days out, 0 if uninjured
}

// AfterMatch folds a match performance into a player's season tallies, form and
// morale.
func AfterMatch(p *model.Player, perf Performance) {
	if perf.Minutes > 0 {
		p.Apps++
		p.MinutesSum += uint32(perf.Minutes)
		p.RatingSum += uint32(math.Round(perf.Rating * 100))
	}
	p.Goals += uint16(perf.Goals)
	p.Penalties += uint16(perf.Penalties)
	p.Assists += uint16(perf.Assists)
	if perf.CleanSheet {
		p.CleanSheets++
	}
	if perf.Yellow {
		p.Yellows++
	}
	if perf.Red {
		p.Reds++
		p.Suspension += 2
	}
	// Every fifth booking in a season carries a one-match ban.
	if perf.Yellow && p.Yellows%5 == 0 {
		p.Suspension++
	}
	if perf.Injury > 0 {
		p.InjuryDays = perf.Injury
		p.Morale = down(p.Morale, 12)
	}

	// Form tracks recent ratings around a 6.7 baseline. It is mean-reverting:
	// each match discounts what came before, so form behaves like a rolling
	// average of recent displays rather than a running total. Without the decay
	// it saturates at its bounds within a dozen games and becomes a permanent
	// bonus for good teams and a permanent penalty for bad ones, which pulls
	// league tables and goal counts far apart.
	if perf.Minutes >= 20 {
		delta := (perf.Rating - 6.7) * 1.9
		f := float64(p.Form)*0.72 + delta
		if f > 10 {
			f = 10
		}
		if f < -10 {
			f = -10
		}
		p.Form = int8(math.Round(f))

		switch {
		case perf.Rating >= 8:
			p.Morale = up(p.Morale, 6)
		case perf.Rating >= 7:
			p.Morale = up(p.Morale, 2)
		case perf.Rating < 5.5:
			p.Morale = down(p.Morale, 4)
		}
	}
}

// MoraleForResult nudges a whole squad's morale after a result.
func MoraleForResult(p *model.Player, won, drew bool) {
	switch {
	case won:
		p.Morale = up(p.Morale, 4)
	case drew:
		p.Morale = up(p.Morale, 1)
	default:
		p.Morale = down(p.Morale, 3)
	}
}

// BenchMorale reflects a player's unhappiness at not being picked. Better
// players expect to play, and resent being left out more.
func BenchMorale(p *model.Player, expected bool) {
	if expected {
		p.Morale = down(p.Morale, 2)
	}
}

func up(v, n uint8) uint8 {
	if int(v)+int(n) > 100 {
		return 100
	}
	return v + n
}

func down(v, n uint8) uint8 {
	if int(v)-int(n) < 0 {
		return 0
	}
	return v - n
}

// Value recomputes a player's market value from ability, ceiling, age and
// contract length. It is the anchor every transfer negotiation starts from.
func Value(p *model.Player, age int) uint32 {
	ca := p.CurrentAbility()

	// Value is strongly convex in ability: the last few points of quality cost
	// far more than the first, which is why elite players are so expensive.
	v := math.Pow(math.Max(ca-38, 1), 3.15) * 850

	// Young players with room to grow carry a premium.
	room := float64(p.Potential) - ca
	switch {
	case age <= 20:
		v *= 1 + room*0.085
	case age <= 23:
		v *= 1 + room*0.060
	case age <= 26:
		v *= 1 + room*0.025
	}

	// Age discount, sharp once the decline sets in.
	switch {
	case age <= 24:
		v *= 1.06
	case age <= 28:
		v *= 1.0
	case age <= 31:
		v *= 0.80
	case age <= 33:
		v *= 0.52
	default:
		v *= math.Max(0.10, 0.34-float64(age-34)*0.07)
	}

	// Form moves the market a little. Contract length is applied separately by
	// ContractValue, since it discounts a fee without changing what the player
	// is actually worth.
	v *= 1 + float64(p.Form)*0.012

	if v < 10_000 {
		v = 10_000
	}
	if v > 300_000_000 {
		v = 300_000_000
	}
	return uint32(v)
}

// ContractValue discounts a valuation by how long the deal has left to run: a
// player entering the final year of a contract can be bought cheaply, and one
// out of contract costs nothing but wages.
func ContractValue(base uint32, yearsLeft int) uint32 {
	switch {
	case yearsLeft <= 0:
		return 0
	case yearsLeft == 1:
		return uint32(float64(base) * 0.45)
	case yearsLeft == 2:
		return uint32(float64(base) * 0.78)
	default:
		return base
	}
}

// WageFor returns the weekly wage a player of this standing expects at a club
// of the given reputation.
func WageFor(p *model.Player, age int, clubRep uint8) uint32 {
	ca := p.CurrentAbility()
	base := math.Pow(math.Max(ca-40, 1), 2.55) * 1.55
	// A bigger club pays more for the same player, and is expected to.
	base *= 0.55 + 0.9*float64(clubRep)/100
	if age <= 21 {
		base *= 0.55
	} else if age <= 24 {
		base *= 0.82
	}
	if base < 500 {
		base = 500
	}
	return uint32(base)
}
