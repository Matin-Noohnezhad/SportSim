// Package transfer implements the player market: what clubs are willing to pay,
// what players are willing to accept, and how AI clubs trade among themselves.
package transfer

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"sportsim/engine/dev"
	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// Window reports whether the transfer market is open on a given date. The
// summer window runs June to August, the winter window through January.
func Window(d model.Date) bool {
	switch d.Month() {
	case 6, 7, 8, 1:
		return true
	}
	return false
}

// WindowName describes the open window, for the UI.
func WindowName(d model.Date) string {
	switch d.Month() {
	case 6, 7, 8:
		return "Summer window open"
	case 1:
		return "Winter window open"
	}
	return "Transfer window closed"
}

// AskingPrice is what the selling club wants for a player. It starts from
// market value, discounts a short contract, and adds a premium for a player the
// club actually depends on.
func AskingPrice(w *model.World, p *model.Player) int64 {
	return askingPrice(w, p, squadRank(w, p))
}

// askingPrice is AskingPrice with the player's place in their club's pecking
// order already known, since establishing it is the expensive half of the sum.
func askingPrice(w *model.World, p *model.Player, rank int) int64 {
	age := w.Age(p)
	base := dev.Value(p, age)
	years := int(p.ContractUntil) - w.Date.SeasonYear()
	price := int64(dev.ContractValue(base, years))

	// Clubs charge a premium for players who are central to the first team.
	if c := w.Club(p.ClubID); c != nil {
		if rank >= 0 && rank < 11 {
			price = price * 128 / 100
		} else if rank >= 20 {
			price = price * 82 / 100 // surplus to requirements
		}
		// A club with money has no need to sell.
		if c.Balance > 60_000_000 {
			price = price * 112 / 100
		}
	}
	if price < 25_000 {
		price = 25_000
	}
	return price
}

// squadRank returns a player's place in their club's pecking order by ability,
// or -1 if they have no club.
func squadRank(w *model.World, p *model.Player) int {
	if p.ClubID == 0 {
		return -1
	}
	ca := p.CurrentAbility()
	rank := 0
	for i := range w.Players {
		o := &w.Players[i]
		if o.ClubID == p.ClubID && o.ID != p.ID && o.CurrentAbility() > ca {
			rank++
		}
	}
	return rank
}

// PriceList quotes a whole market at once.
//
// A single AskingPrice has to work out where the player stands in their club's
// pecking order, which costs a pass over the world; asking it player by player
// makes pricing every player in the game quadratic, and slow enough that a
// search cannot be run on every keystroke. Establishing every pecking order in
// one pass up front makes the search linear again.
type PriceList struct {
	w    *model.World
	rank []int32 // by player ID minus one; -1 for a free agent
}

// NewPriceList ranks every squad in the world by ability, ready for pricing.
func NewPriceList(w *model.World) *PriceList {
	pl := &PriceList{w: w, rank: make([]int32, len(w.Players))}

	byClub := make(map[uint16][]uint32, len(w.Clubs))
	ability := make([]float64, len(w.Players))
	for i := range w.Players {
		p := &w.Players[i]
		if p.ClubID == 0 {
			pl.rank[i] = -1
			continue
		}
		ability[i] = p.CurrentAbility()
		byClub[p.ClubID] = append(byClub[p.ClubID], p.ID)
	}
	for _, ids := range byClub {
		sort.SliceStable(ids, func(a, b int) bool {
			return ability[ids[a]-1] > ability[ids[b]-1]
		})
		for rank, id := range ids {
			pl.rank[id-1] = int32(rank)
		}
	}
	return pl
}

// Ask is what the player's club wants for them.
func (pl *PriceList) Ask(p *model.Player) int64 {
	if p == nil || int(p.ID) > len(pl.rank) {
		return 0
	}
	return askingPrice(pl.w, p, int(pl.rank[p.ID-1]))
}

// Offer is a bid for a player.
type Offer struct {
	PlayerID uint32
	From     uint16 // buying club
	Fee      int64
	Wage     uint32 // weekly wage offered to the player
	Years    int    // contract length
}

// Response is the outcome of an offer.
type Response struct {
	ClubAccepted   bool
	PlayerAccepted bool
	Reason         string
	ClubCounter    int64  // what the club would accept, when it rejects
	WageDemand     uint32 // what the player wants, when they reject
}

// Consider evaluates an offer from both the selling club's and the player's
// point of view.
func Consider(w *model.World, r *rng.R, o Offer) Response {
	p := w.Player(o.PlayerID)
	if p == nil {
		return Response{Reason: "No such player."}
	}
	buyer := w.Club(o.From)
	if buyer == nil {
		return Response{Reason: "No such club."}
	}

	var res Response

	// ---- the selling club ----
	if p.ClubID == 0 {
		res.ClubAccepted = true // free agent, nobody to negotiate with
	} else {
		ask := AskingPrice(w, p)
		if o.Fee >= HaggleFloor(ask) {
			res.ClubAccepted = true
		} else {
			res.ClubCounter = ask
			res.Reason = "The club has rejected your offer."
		}
	}

	// ---- the player ----
	demand, prestige := WageDemand(w, p, buyer)
	res.WageDemand = demand

	switch {
	case o.Wage >= demand:
		res.PlayerAccepted = true
	case prestige > 22 && float64(o.Wage) >= float64(demand)*0.88:
		res.PlayerAccepted = true // tempted by the step up
	default:
		if res.Reason == "" {
			res.Reason = "The player is not satisfied with the terms."
		}
	}
	return res
}

// WageDemand is the weekly wage a player would want to join a club, along with
// how much of a step up the move is. It draws no randomness, so a manager may
// ask what a signing would cost as often as they like without moving the market.
func WageDemand(w *model.World, p *model.Player, buyer *model.Club) (wage uint32, prestige float64) {
	if p == nil || buyer == nil {
		return 0, 0
	}
	want := dev.WageAsk(p, w.Age(p), buyer.Reputation)

	// Players weigh the move itself, not only the money: dropping down the
	// pyramid needs compensating, moving up is attractive on its own.
	if current := w.Club(p.ClubID); current != nil {
		prestige = float64(buyer.Reputation) - float64(current.Reputation)
	} else {
		prestige = 10 // any club beats being unemployed
	}
	// A first-team place matters. A star will not join to sit on a bench.
	demand := float64(want) * (1 - prestige*0.006)
	if demand < float64(want)*0.6 {
		demand = float64(want) * 0.6
	}
	// The step up is worth taking less to get, but not less than the player is
	// already on: a discount off an anchored ask must not reintroduce the pay
	// cut the anchor exists to prevent.
	if p.ClubID != 0 && demand < float64(p.WageEUR) {
		demand = float64(p.WageEUR)
	}
	return uint32(demand), prestige
}

// HaggleFloor is the least a selling club will take for a player: they will
// come down a little from the asking price, but not far. A manager who knows
// the floor can save the difference, which is what makes bidding a decision.
func HaggleFloor(ask int64) int64 { return ask * 92 / 100 }

// CanAfford reports whether a club can fund a transfer without breaking its
// budget, and explains why not when it cannot.
func CanAfford(w *model.World, clubID uint16, fee int64, wage uint32) (bool, string) {
	c := w.Club(clubID)
	if c == nil {
		return false, "Unknown club."
	}
	if fee > c.TransferBudget {
		return false, "That fee is beyond your transfer budget."
	}
	if w.WageBill(clubID)+int64(wage) > c.WageBudget {
		return false, "That wage would take you over your wage budget."
	}
	if w.SquadSize(clubID) >= 32 {
		return false, "Your squad is already full (32 players)."
	}
	return true, ""
}

// Complete executes an agreed transfer, moving the player and the money.
func Complete(w *model.World, o Offer) {
	p := w.Player(o.PlayerID)
	if p == nil {
		return
	}
	if seller := w.Club(p.ClubID); seller != nil {
		seller.Balance += o.Fee
		seller.TransferBudget += o.Fee
	}
	if buyer := w.Club(o.From); buyer != nil {
		buyer.Balance -= o.Fee
		buyer.TransferBudget -= o.Fee
	}
	p.ClubID = o.From
	p.WageEUR = o.Wage
	p.ContractUntil = uint16(w.Date.SeasonYear() + o.Years)
	p.Morale = 78 // a fresh start
	p.Form = 0
}

// Need scores how badly a club wants reinforcement in a position, from the
// depth and quality it already has there. The AI market uses it to decide what
// to buy; a human manager is shown the same judgement rather than a second one
// invented for the interface.
func Need(w *model.World, clubID uint16, pos model.Pos) float64 {
	var best, second float64
	count := 0
	for i := range w.Players {
		p := &w.Players[i]
		if p.ClubID != clubID {
			continue
		}
		r := p.Rating(pos)
		if p.PlaysPos(pos) {
			count++
		}
		if r > best {
			second, best = best, r
		} else if r > second {
			second = r
		}
	}
	// A club is short in a position if its best there is weak, or if it has
	// nobody to cover.
	score := math.Max(0, 78-best) * 1.5
	if count < 2 {
		score += 18
	}
	score += math.Max(0, 70-second) * 0.4
	return score
}

// RunAI moves the market on for one day: AI clubs look for signings that
// improve them and that they can afford, and sell fringe players when short of
// money. It is intentionally modest per day, so the window fills gradually
// rather than resolving in a single burst.
func RunAI(w *model.World, r *rng.R, deals int) []string {
	if !Window(w.Date) {
		return nil
	}
	var news []string

	for n := 0; n < deals; n++ {
		// Pick a buying club, weighted toward those with money to spend.
		buyer := pickBuyer(w, r)
		if buyer == nil {
			continue
		}
		if w.SquadSize(buyer.ID) >= 30 {
			continue
		}

		// What does it need most?
		var wantPos model.Pos
		bestNeed := 0.0
		for _, pos := range buyer.Tactics.Formation.Slots() {
			if s := Need(w, buyer.ID, pos); s > bestNeed {
				bestNeed, wantPos = s, pos
			}
		}
		if bestNeed < 12 {
			continue // squad is fine
		}

		target := findTarget(w, r, buyer, wantPos)
		if target == nil {
			continue
		}

		fee := AskingPrice(w, target)
		wage := dev.WageAsk(target, w.Age(target), buyer.Reputation)
		if ok, _ := CanAfford(w, buyer.ID, fee, wage); !ok {
			continue
		}

		o := Offer{PlayerID: target.ID, From: buyer.ID, Fee: fee, Wage: wage, Years: r.Range(3, 5)}
		resp := Consider(w, r, o)
		if !resp.ClubAccepted || !resp.PlayerAccepted {
			continue
		}
		from := w.ClubName(target.ClubID)
		Complete(w, o)
		news = append(news, formatDeal(target.Name, from, buyer.Name, fee))
	}
	return news
}

// pickBuyer selects an AI club to act, favouring the wealthy and ambitious.
func pickBuyer(w *model.World, r *rng.R) *model.Club {
	for tries := 0; tries < 12; tries++ {
		c := &w.Clubs[r.Intn(len(w.Clubs))]
		if c.IsHuman || c.TransferBudget < 500_000 {
			continue
		}
		// Bigger clubs act more often.
		if r.Chance(0.25 + float64(c.Reputation)/160) {
			return c
		}
	}
	return nil
}

// findTarget looks for an affordable player who would improve the buying club
// in the position it is short of.
func findTarget(w *model.World, r *rng.R, buyer *model.Club, pos model.Pos) *model.Player {
	// The bar the signing has to clear: better than what the club already has.
	var incumbent float64
	for i := range w.Players {
		p := &w.Players[i]
		if p.ClubID == buyer.ID {
			if v := p.Rating(pos); v > incumbent {
				incumbent = v
			}
		}
	}

	type cand struct {
		p     *model.Player
		score float64
	}
	var best []cand

	// Sampling rather than scanning every player keeps a day of AI trading
	// cheap even with thousands of players on the books.
	for tries := 0; tries < 260; tries++ {
		p := &w.Players[r.Intn(len(w.Players))]
		if p.ClubID == buyer.ID || p.InjuryDays > 40 {
			continue
		}
		if !p.PlaysPos(pos) {
			continue
		}
		rating := p.Rating(pos)
		if rating <= incumbent+1.5 {
			continue
		}
		// Would the player even consider the move?
		if cur := w.Club(p.ClubID); cur != nil && int(cur.Reputation)-int(buyer.Reputation) > 14 {
			continue
		}
		fee := AskingPrice(w, p)
		if fee > buyer.TransferBudget {
			continue
		}
		// Value for money: improvement per euro, with youth preferred.
		score := (rating - incumbent) / (1 + float64(fee)/8_000_000)
		if w.Age(p) <= 23 {
			score *= 1.25
		}
		best = append(best, cand{p, score})
	}
	if len(best) == 0 {
		return nil
	}
	sort.SliceStable(best, func(a, b int) bool { return best[a].score > best[b].score })
	// Pick from the top few, so the AI is not perfectly predictable.
	n := len(best)
	if n > 4 {
		n = 4
	}
	return best[r.Intn(n)].p
}

func formatDeal(player, from, to string, fee int64) string {
	return fmt.Sprintf("%s joins %s from %s for %s.", player, to, from, Money(fee))
}

// Money formats a euro amount compactly for headlines and tables.
func Money(v int64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("%s\u20ac%s", sign, strconv.FormatFloat(math.Round(float64(v)/100_000)/10, 'f', -1, 64)+"M")
	case v >= 1_000:
		return fmt.Sprintf("%s\u20ac%s", sign, strconv.FormatFloat(math.Round(float64(v)/100)/10, 'f', -1, 64)+"k")
	default:
		return fmt.Sprintf("%s\u20ac%d", sign, v)
	}
}
