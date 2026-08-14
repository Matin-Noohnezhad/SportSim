package season

import (
	"fmt"
	"sort"

	"sportsim/engine/dev"
	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// Outcome summarises what a completed season did to the game world, so the UI
// can present an end-of-season report.
type Outcome struct {
	Champions  map[uint16]uint16 // league ID -> winning club
	Promoted   []uint16
	Relegated  []uint16
	Retired    []string
	YouthCount int
	Headlines  []string
}

// Rollover closes a season: it pays out prize money, moves clubs between
// divisions, ages and develops every player, expires contracts, brings youth
// through and resets the season's tallies.
func Rollover(w *model.World, s *Schedule, r *rng.R) *Outcome {
	out := &Outcome{Champions: map[uint16]uint16{}}

	payPrizeMoney(w, s, out)
	applyPromotionRelegation(w, s, out)
	expireContracts(w, r, out)
	retireAndAge(w, r, out)
	youthIntake(w, r, out)
	fillSquads(w, r, out)
	resetSeasonStats(w)
	rebalanceBudgets(w)

	return out
}

// payPrizeMoney distributes each league's pot down the table, and records the
// champions.
func payPrizeMoney(w *model.World, s *Schedule, out *Outcome) {
	for li := range w.Leagues {
		l := &w.Leagues[li]
		rows := Table(w, s, l.ID)
		if len(rows) == 0 {
			continue
		}
		out.Champions[l.ID] = rows[0].ClubID
		out.Headlines = append(out.Headlines,
			fmt.Sprintf("%s win the %s.", w.ClubName(rows[0].ClubID), l.Name))

		n := len(rows)
		for i, row := range rows {
			c := w.Club(row.ClubID)
			if c == nil {
				continue
			}
			// The champion takes the full pot; last place takes about a third.
			//
			// Prize money is all that is settled here. Gate receipts are banked
			// match by match as they are taken, in game.playFixture, and adding a
			// season's worth again at the rollover paid every club twice for the
			// same nineteen home games.
			share := 1.0 - 0.66*float64(i)/float64(n-1)
			c.Balance += int64(float64(l.PrizeMoney) * share)
		}
	}
}

// applyPromotionRelegation swaps clubs between the divisions of each country.
func applyPromotionRelegation(w *model.World, s *Schedule, out *Outcome) {
	// Group leagues by country so tiers can be matched up.
	byCountry := map[string][]*model.League{}
	for i := range w.Leagues {
		l := &w.Leagues[i]
		byCountry[l.Country] = append(byCountry[l.Country], l)
	}

	for _, ls := range byCountry {
		sort.SliceStable(ls, func(a, b int) bool { return ls[a].Tier < ls[b].Tier })

		for i := 0; i+1 < len(ls); i++ {
			upper, lower := ls[i], ls[i+1]
			down := int(upper.Relegated)
			up := int(lower.Promoted)
			// The two divisions must exchange the same number of clubs, or the
			// league sizes would drift apart season on season.
			if down > up {
				down = up
			}
			up = down
			if down == 0 {
				continue
			}

			upperRows := Table(w, s, upper.ID)
			lowerRows := Table(w, s, lower.ID)
			if len(upperRows) < down || len(lowerRows) < up {
				continue
			}

			relegated := make([]uint16, 0, down)
			for _, row := range upperRows[len(upperRows)-down:] {
				relegated = append(relegated, row.ClubID)
			}
			promoted := make([]uint16, 0, up)
			for _, row := range lowerRows[:up] {
				promoted = append(promoted, row.ClubID)
			}

			for _, id := range relegated {
				moveClub(w, upper, lower, id)
				out.Relegated = append(out.Relegated, id)
				out.Headlines = append(out.Headlines,
					fmt.Sprintf("%s are relegated to the %s.", w.ClubName(id), lower.Name))
			}
			for _, id := range promoted {
				moveClub(w, lower, upper, id)
				out.Promoted = append(out.Promoted, id)
				out.Headlines = append(out.Headlines,
					fmt.Sprintf("%s are promoted to the %s.", w.ClubName(id), upper.Name))
			}
		}
	}
}

// moveClub transfers a club's registration from one division to another.
func moveClub(w *model.World, from, to *model.League, id uint16) {
	for i, c := range from.ClubIDs {
		if c == id {
			from.ClubIDs = append(from.ClubIDs[:i], from.ClubIDs[i+1:]...)
			break
		}
	}
	to.ClubIDs = append(to.ClubIDs, id)
	if c := w.Club(id); c != nil {
		c.LeagueID = to.ID
	}
}

// expireContracts releases players whose deals have run out, and lets clubs
// renew the ones they want to keep.
func expireContracts(w *model.World, r *rng.R, out *Outcome) {
	year := w.Date.SeasonYear() + 1
	for i := range w.Players {
		p := &w.Players[i]
		if int(p.ContractUntil) > year || p.ClubID == 0 {
			continue
		}
		c := w.Club(p.ClubID)
		if c == nil {
			continue
		}
		age := w.Age(p)
		rank := 0
		for j := range w.Players {
			o := &w.Players[j]
			if o.ClubID == p.ClubID && o.CurrentAbility() > p.CurrentAbility() {
				rank++
			}
		}

		// Clubs renew players they rely on, and let the rest go.
		keep := rank < 18 && age < 35
		if keep && r.Chance(0.88) {
			years := 3
			if age > 30 {
				years = 2
			}
			p.ContractUntil = uint16(year + years)
			p.WageEUR = dev.WageAsk(p, age, c.Reputation)
			continue
		}
		p.ClubID = 0 // released, now a free agent
		p.WageEUR = 0
	}
}

// retireAndAge develops every player through the summer and retires those who
// have reached the end.
func retireAndAge(w *model.World, r *rng.R, out *Outcome) {
	for i := range w.Players {
		p := &w.Players[i]
		if p.ClubID == 0 && p.CurrentAbility() < 55 {
			continue // unattached and not good enough; leave them dormant
		}
		age := w.Age(p)

		// Retirement: increasingly likely past 33, certain by 41.
		if age >= 33 {
			chance := 0.06 + float64(age-33)*0.14
			if p.CurrentAbility() < 65 {
				chance += 0.18
			}
			if age >= 41 || r.Chance(chance) {
				out.Retired = append(out.Retired, fmt.Sprintf("%s (%d) retires", p.Name, age))
				p.ClubID = 0
				p.Potential = 0 // marks the player as no longer active
				continue
			}
		}
	}
}

// youthIntake promotes academy graduates into senior squads each summer. Their
// quality follows the club's youth facilities and recruitment.
func youthIntake(w *model.World, r *rng.R, out *Outcome) {
	// Reuse retired players' slots where possible to keep the slice from
	// growing without bound over a long career.
	free := make([]int, 0, 64)
	for i := range w.Players {
		if w.Players[i].Potential == 0 {
			free = append(free, i)
		}
	}

	for ci := range w.Clubs {
		c := &w.Clubs[ci]
		if w.SquadSize(c.ID) >= 28 {
			continue
		}
		n := 1
		if r.Chance(float64(c.YouthRecruitment) / 140) {
			n = 2
		}
		for k := 0; k < n; k++ {
			p := generateYouth(w, r, c)
			if len(free) > 0 {
				idx := free[len(free)-1]
				free = free[:len(free)-1]
				p.ID = w.Players[idx].ID
				w.Players[idx] = p
			} else {
				p.ID = uint32(len(w.Players) + 1)
				w.Players = append(w.Players, p)
			}
			out.YouthCount++
		}
	}
}

// youthNames supplies plausible names for academy graduates. Real players come
// from the dataset; only players born inside the simulation need generating.
var youthFirst = []string{
	"Luca", "Mateo", "Noah", "Leo", "Enzo", "Hugo", "Jules", "Liam", "Milan", "Adam",
	"Youssef", "Rafael", "Tomás", "Kaan", "Sven", "Finn", "Oscar", "Elias", "Nico", "Diogo",
	"Arda", "Bram", "Kai", "Théo", "Marco", "Iker", "Pau", "Lorenzo", "Jonas", "Emil",
}
var youthLast = []string{
	"Ferreira", "Silva", "Moreau", "Bakker", "Rossi", "Weber", "Costa", "Martín", "Yılmaz",
	"Nowak", "Ricci", "Lambert", "Visser", "Hoffmann", "Brandão", "Serrano", "Demir",
	"Vermeulen", "Colombo", "Fischer", "Marchand", "Aguilar", "Persson", "Kovač", "Blom",
}

// generateYouth creates one academy graduate for a club.
func generateYouth(w *model.World, r *rng.R, c *model.Club) model.Player {
	var p model.Player
	p.Name = youthFirst[r.Intn(len(youthFirst))] + " " + youthLast[r.Intn(len(youthLast))]
	p.FullName = p.Name
	p.ClubID = c.ID
	p.NationID = c.NationID

	age := r.Range(16, 18)
	p.BirthYear = uint16(w.Date.Year() - age)
	p.BirthMonth = uint8(r.Range(1, 12))
	p.BirthDay = uint8(r.Range(1, 28))
	p.HeightCM = uint8(r.Range(170, 195))
	p.WeightKG = uint8(r.Range(64, 86))

	// Position: mostly outfield, with the occasional keeper.
	pos := model.Pos(r.Range(1, int(model.NumPos)-1))
	if r.Chance(0.09) {
		pos = model.GK
	}
	p.Positions = [3]model.Pos{pos, model.NoPos, model.NoPos}
	p.NumPositions = 1

	// Facilities set the standard of graduate a club produces.
	base := 34 + float64(c.YouthFacilities)*0.20 + r.Norm(0, 4)
	for i := 0; i < model.NumAttr; i++ {
		v := base + r.Norm(0, 7)
		p.Attr[i] = uint8(clampInt(int(v), 5, 70))
	}
	// Make them credible in their position rather than uniformly mediocre.
	for _, aw := range model.PositionWeights(pos) {
		v := int(p.Attr[aw.Attr]) + r.Range(4, 14)
		p.Attr[aw.Attr] = uint8(clampInt(v, 5, 75))
	}

	ca := p.CurrentAbility()
	// The ceiling is what makes a prospect interesting, and it is uncertain.
	spread := r.Norm(float64(c.YouthRecruitment)*0.16, 9)
	p.Potential = uint8(clampInt(int(ca+8+spread), int(ca)+1, 92))

	p.Foot = model.FootRight
	if r.Chance(0.24) {
		p.Foot = model.FootLeft
	}
	p.WeakFoot = uint8(r.Range(2, 4))
	p.SkillMoves = uint8(r.Range(1, 3))
	p.IntRep = 1
	p.WorkRateAtk = uint8(r.Range(0, 2))
	p.WorkRateDef = uint8(r.Range(0, 2))
	p.ContractUntil = uint16(w.Date.SeasonYear() + r.Range(2, 4))
	p.WageEUR = dev.WageFor(&p, age, c.Reputation)
	p.ValueEUR = dev.Value(&p, age)
	p.Fitness, p.Sharpness, p.Morale = 100, 55, 75
	p.Jersey = uint8(r.Range(30, 60))
	return p
}

// minSquad is the smallest squad a club is allowed to start a season with. It
// leaves room for injuries and suspensions without forcing forfeits.
const minSquad = 20

// EnsureSquads makes sure every club can field a side, topping up any that are
// short. It runs both at world creation and at every season rollover.
//
// It is needed at creation because the source dataset ships a handful of clubs
// with very small squads, and needed at rollover because contract expiry and
// retirement together release more players than the academies replace. Without
// it, a club can end up unable to put eleven fit players on the pitch, which
// shows up as an absurd league table rather than as an obvious error.
func EnsureSquads(w *model.World, r *rng.R) int {
	var out Outcome
	fillSquads(w, r, &out)
	return out.YouthCount
}

// fillSquads tops up every squad below minSquad. Clubs prefer a proven free
// agent to an untested teenager, and fall back to promoting extra youth when
// the free-agent pool runs dry.
func fillSquads(w *model.World, r *rng.R, out *Outcome) {
	// Collect the free agents once, best first, rather than rescanning per club.
	free := make([]*model.Player, 0, 256)
	for i := range w.Players {
		p := &w.Players[i]
		if p.ClubID == 0 && p.Potential > 0 {
			free = append(free, p)
		}
	}
	sort.SliceStable(free, func(a, b int) bool {
		return free[a].CurrentAbility() > free[b].CurrentAbility()
	})

	for ci := range w.Clubs {
		c := &w.Clubs[ci]
		for w.SquadSize(c.ID) < minSquad {
			// A club will only sign a free agent of roughly its own standard;
			// otherwise the best free agents would all end up in the lower tiers.
			ceiling := float64(c.Reputation)*0.42 + 52
			signed := false
			for _, p := range free {
				if p.ClubID != 0 {
					continue // already picked up
				}
				if p.CurrentAbility() > ceiling {
					continue
				}
				p.ClubID = c.ID
				p.WageEUR = dev.WageFor(p, w.Age(p), c.Reputation)
				p.ContractUntil = uint16(w.Date.SeasonYear() + 1 + r.Range(1, 3))
				p.Morale = 72
				signed = true
				break
			}
			if signed {
				continue
			}
			// Nobody suitable is available: promote another academy player.
			p := generateYouth(w, r, c)
			p.ID = uint32(len(w.Players) + 1)
			w.Players = append(w.Players, p)
			out.YouthCount++
		}
	}
}

// resetSeasonStats clears per-season tallies and refreshes valuations.
func resetSeasonStats(w *model.World) {
	for i := range w.Players {
		p := &w.Players[i]
		p.Apps, p.Goals, p.Assists, p.MinutesSum = 0, 0, 0, 0
		p.Penalties, p.CleanSheets = 0, 0
		p.Yellows, p.Reds, p.RatingSum = 0, 0, 0
		p.Suspension = 0
		p.Fitness, p.Sharpness = 100, 45
		p.Form = 0
		if p.Potential > 0 {
			p.ValueEUR = dev.Value(p, w.Age(p))
		}
	}
}

// rebalanceBudgets resets each club's transfer and wage allowances for the new
// campaign, based on the money it actually has.
func rebalanceBudgets(w *model.World) {
	for i := range w.Clubs {
		c := &w.Clubs[i]
		l := w.League(c.LeagueID)
		rep := 60
		if l != nil {
			rep = int(l.Reputation)
		}
		// Roughly half of free cash goes to the transfer kitty.
		budget := c.Balance / 2
		if budget < 0 {
			budget = 0
		}
		c.TransferBudget = budget
		wages := w.WageBill(c.ID)
		c.WageBudget = wages*112/100 + int64(rep)*4_000
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
