package game

import (
	"math"
	"sort"
	"strings"

	"sportsim/engine/dev"
	"sportsim/engine/model"
	"sportsim/engine/transfer"
)

// Target is one player as the transfer market lists them.
//
// It is deliberately not a SquadRow: the market is answering a different
// question from the squad screen — not "how is my player doing" but "should I
// buy this one" — so it carries the asking price, whether the club can afford
// it, and what signing the player would actually do to the side.
type Target struct {
	PlayerID uint32
	Name     string

	// Positions is every position the player is natural in, best first, as
	// "CAM/CM". Most players have two or three, and a search for a right back
	// that only looked at the first would miss the majority of them.
	Positions string
	Primary   string

	Age       int
	Rating    int
	Potential int
	Nation    string
	Club      string

	Fee  int64 // the asking price, not the book value
	Wage int64

	Contract  int  // year the current deal expires
	Expiring  bool // the deal runs out at the end of this season
	FreeAgent bool

	// Improves is how many rating points the player would add over the best the
	// managed club already has in their strongest position, or 0 if they would
	// not improve the side at all. It answers the question a list of ratings
	// cannot: good compared with what?
	Improves    int
	ImprovesAt  string // the position that improvement is in
	Affordable  bool   // the fee is within the transfer budget
	Shortlisted bool
}

// Scope narrows the market to a subset worth looking at on its own.
type Scope uint8

const (
	ScopeAll Scope = iota
	ScopeShortlist
	ScopeFreeAgents
	ScopeExpiring
	NumScopes
)

// ScopeNames labels the scopes for a frontend's controls.
var ScopeNames = [NumScopes]string{"all players", "shortlist", "free agents", "expiring deals"}

func (s Scope) String() string {
	if s >= NumScopes {
		return ScopeNames[ScopeAll]
	}
	return ScopeNames[s]
}

// SortBy chooses the order search results come back in.
type SortBy uint8

const (
	SortRating SortBy = iota
	SortImproves
	SortPotential
	SortAge
	SortFee
	SortWage
	NumSorts
)

// SortNames labels the orderings for a frontend's controls.
var SortNames = [NumSorts]string{"rating", "improvement", "potential", "age", "fee", "wage"}

func (s SortBy) String() string {
	if s >= NumSorts {
		return SortNames[SortRating]
	}
	return SortNames[s]
}

// SearchFilter narrows the market. A zero filter matches every player in the
// world who is not already ours, which is a usable starting point rather than an
// empty screen: the manager narrows down from everybody rather than having to
// guess a name before seeing anything at all.
type SearchFilter struct {
	Name      string
	Position  model.Pos // NumPos or NoPos for any position
	MinAge    int
	MaxAge    int
	MinRating int
	MaxFee    int64
	MaxWage   int64
	Scope     Scope
	Sort      SortBy
}

// AnyPosition is the Position value that applies no positional filter.
const AnyPosition = model.NumPos

// Search returns the players matching the filter, in the requested order.
//
// Results are capped at limit, but the count of everything that matched is
// returned too, so a frontend can say how much of the market it is showing.
func (g *Game) Search(f SearchFilter, limit int) (rows []Target, matched int) {
	w := g.World
	prices := transfer.NewPriceList(w)
	ours := g.bestPerPosition()
	shortlisted := g.shortlistSet()
	budget := int64(0)
	if c := g.Club(); c != nil {
		budget = c.TransferBudget
	}
	seasonEnd := w.Date.SeasonYear() + 1

	out := make([]Target, 0, 256)
	for i := range w.Players {
		p := &w.Players[i]
		if p.Potential == 0 || p.ClubID == w.HumanClubID {
			continue
		}
		switch f.Scope {
		case ScopeShortlist:
			if !shortlisted[p.ID] {
				continue
			}
		case ScopeFreeAgents:
			if p.ClubID != 0 {
				continue
			}
		case ScopeExpiring:
			if p.ClubID != 0 && int(p.ContractUntil) > seasonEnd {
				continue
			}
		}
		age := w.Age(p)
		if f.MinAge > 0 && age < f.MinAge {
			continue
		}
		if f.MaxAge > 0 && age > f.MaxAge {
			continue
		}
		if f.Position < model.NumPos && !p.PlaysPos(f.Position) {
			continue
		}
		if f.MinRating > 0 && int(math.Round(p.CurrentAbility())) < f.MinRating {
			continue
		}
		if f.MaxWage > 0 && int64(p.WageEUR) > f.MaxWage {
			continue
		}
		if f.Name != "" && !containsFold(p.Name, f.Name) && !containsFold(p.FullName, f.Name) {
			continue
		}
		fee := prices.Ask(p)
		if f.MaxFee > 0 && fee > f.MaxFee {
			continue
		}

		gain, at := ours.improvementFrom(p)
		out = append(out, Target{
			PlayerID:    p.ID,
			Name:        p.Name,
			Positions:   PositionList(p),
			Primary:     p.Primary().String(),
			Age:         age,
			Rating:      int(math.Round(p.CurrentAbility())),
			Potential:   int(p.Potential),
			Nation:      w.NationName(p.NationID),
			Club:        w.ClubName(p.ClubID),
			Fee:         fee,
			Wage:        int64(p.WageEUR),
			Contract:    int(p.ContractUntil),
			Expiring:    p.ClubID != 0 && int(p.ContractUntil) <= seasonEnd,
			FreeAgent:   p.ClubID == 0,
			Improves:    gain,
			ImprovesAt:  at,
			Affordable:  fee <= budget,
			Shortlisted: shortlisted[p.ID],
		})
	}

	sortTargets(out, f.Sort)
	matched = len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, matched
}

// sortTargets orders results, always breaking ties on rating so that two players
// of the same age or price come back in a stable, sensible order.
func sortTargets(out []Target, by SortBy) {
	less := func(a, b int) bool { return out[a].Rating > out[b].Rating }
	switch by {
	case SortImproves:
		less = func(a, b int) bool {
			if out[a].Improves != out[b].Improves {
				return out[a].Improves > out[b].Improves
			}
			return out[a].Rating > out[b].Rating
		}
	case SortPotential:
		less = func(a, b int) bool {
			if out[a].Potential != out[b].Potential {
				return out[a].Potential > out[b].Potential
			}
			return out[a].Rating > out[b].Rating
		}
	case SortAge:
		less = func(a, b int) bool {
			if out[a].Age != out[b].Age {
				return out[a].Age < out[b].Age
			}
			return out[a].Rating > out[b].Rating
		}
	case SortFee:
		less = func(a, b int) bool {
			if out[a].Fee != out[b].Fee {
				return out[a].Fee < out[b].Fee
			}
			return out[a].Rating > out[b].Rating
		}
	case SortWage:
		less = func(a, b int) bool {
			if out[a].Wage != out[b].Wage {
				return out[a].Wage < out[b].Wage
			}
			return out[a].Rating > out[b].Rating
		}
	}
	sort.SliceStable(out, less)
}

// PositionList renders every position a player is natural in, as "CAM/CM".
func PositionList(p *model.Player) string {
	nat := p.NaturalPositions()
	parts := make([]string, 0, len(nat))
	for _, pos := range nat {
		parts = append(parts, pos.String())
	}
	return strings.Join(parts, "/")
}

// ---------------------------------------------------------------- squad fit

// squadStrength is the managed club's best rating in each position, so a target
// can be measured against what the manager already has rather than against the
// abstract 1-99 scale.
type squadStrength [model.NumPos]float64

func (g *Game) bestPerPosition() squadStrength {
	var best squadStrength
	for _, p := range g.World.Squad(g.World.HumanClubID) {
		for pos := model.Pos(0); pos < model.NumPos; pos++ {
			if r := p.Rating(pos); r > best[pos] {
				best[pos] = r
			}
		}
	}
	return best
}

// improvementFrom returns how much the player would add, and where. A player is
// judged in the positions they are actually natural in: a centre back's rating
// as an emergency striker is not an argument for signing him.
func (s squadStrength) improvementFrom(p *model.Player) (gain int, at string) {
	for _, pos := range p.NaturalPositions() {
		if d := int(math.Round(p.Rating(pos) - s[pos])); d > gain {
			gain, at = d, pos.String()
		}
	}
	return gain, at
}

// WeakestPosition returns the position the managed club most needs to
// strengthen, judged by the same measure the AI clubs use on themselves.
func (g *Game) WeakestPosition() model.Pos {
	c := g.Club()
	if c == nil {
		return AnyPosition
	}
	worst, weakest := 0.0, AnyPosition
	for _, pos := range c.Tactics.Formation.Slots() {
		if s := transfer.Need(g.World, c.ID, pos); s > worst {
			worst, weakest = s, pos
		}
	}
	return weakest
}

// ---------------------------------------------------------------- shortlist

// ToggleShortlist adds a player to the manager's shortlist, or removes one who
// is already on it, and reports which it did.
func (g *Game) ToggleShortlist(playerID uint32) (listed bool) {
	w := g.World
	if w.Player(playerID) == nil {
		return false
	}
	for i, id := range w.Shortlist {
		if id == playerID {
			w.Shortlist = append(w.Shortlist[:i], w.Shortlist[i+1:]...)
			return false
		}
	}
	w.Shortlist = append(w.Shortlist, playerID)
	return true
}

// ShortlistSize is how many players the manager is watching.
func (g *Game) ShortlistSize() int { return len(g.World.Shortlist) }

func (g *Game) shortlistSet() map[uint32]bool {
	set := make(map[uint32]bool, len(g.World.Shortlist))
	for _, id := range g.World.Shortlist {
		set[id] = true
	}
	return set
}

// ---------------------------------------------------------------- quotes

// Quote is what it would take to sign a player, worked out without committing
// to anything. Asking for one costs nothing and moves nothing: it draws no
// randomness and changes no state, so a manager may price up the whole market
// before deciding. Fee and Wage are an offer that would be accepted today,
// which a frontend can put in front of the manager as a starting point.
type Quote struct {
	PlayerID  uint32
	Name      string
	Positions string
	Club      string
	Age       int
	Rating    int
	Potential int

	AskingPrice int64 // what the selling club is holding out for
	WageDemand  int64 // what the player wants per week

	Fee   int64 // a fee that would be accepted
	Wage  uint32
	Years int

	Budget     int64 // the managed club's transfer budget
	WageRoom   int64 // weekly wage still available under the budget
	WindowOpen bool
	Blocked    string // why a bid cannot be made at all, empty if it can
}

// Quote prices a signing without making an offer.
func (g *Game) Quote(playerID uint32) *Quote {
	w := g.World
	p := w.Player(playerID)
	c := g.Club()
	if p == nil || c == nil {
		return nil
	}

	demand, _ := transfer.WageDemand(w, p, c)
	ask := transfer.AskingPrice(w, p)

	q := &Quote{
		PlayerID:    p.ID,
		Name:        p.Name,
		Positions:   PositionList(p),
		Club:        w.ClubName(p.ClubID),
		Age:         w.Age(p),
		Rating:      int(math.Round(p.CurrentAbility())),
		Potential:   int(p.Potential),
		AskingPrice: ask,
		WageDemand:  int64(demand),
		Fee:         ask,
		Wage:        demand,
		Years:       4,
		Budget:      c.TransferBudget,
		WageRoom:    c.WageBudget - w.WageBill(c.ID),
		WindowOpen:  transfer.Window(w.Date),
	}
	// A free agent costs nothing but the wages.
	if p.ClubID == 0 {
		q.Fee = 0
	}

	switch {
	case p.ClubID == w.HumanClubID:
		q.Blocked = "That player already plays for you."
	case !q.WindowOpen:
		q.Blocked = transfer.WindowName(w.Date) + "."
	case w.SquadSize(c.ID) >= 32:
		q.Blocked = "Your squad is already full (32 players)."
	}
	return q
}

// Renewal prices keeping a player the club already has, so the squad screen can
// offer terms the same way the market screen bids for a signing.
func (g *Game) Renewal(playerID uint32) *Quote {
	w := g.World
	p := w.Player(playerID)
	c := g.Club()
	if p == nil || c == nil || p.ClubID != w.HumanClubID {
		return nil
	}
	want := dev.WageAsk(p, w.Age(p), c.Reputation)
	return &Quote{
		PlayerID:   p.ID,
		Name:       p.Name,
		Positions:  PositionList(p),
		Club:       c.Name,
		Age:        w.Age(p),
		Rating:     int(math.Round(p.CurrentAbility())),
		Potential:  int(p.Potential),
		WageDemand: int64(want),
		Wage:       want,
		Years:      4,
		Budget:     c.TransferBudget,
		WageRoom:   c.WageBudget - w.WageBill(c.ID) + int64(p.WageEUR),
	}
}

// ---------------------------------------------------------------- text search

func containsFold(hay, needle string) bool {
	h, n := []rune(lower(hay)), []rune(lower(needle))
	if len(n) == 0 || len(n) > len(h) {
		return len(n) == 0
	}
	for i := 0; i+len(n) <= len(h); i++ {
		ok := true
		for j := range n {
			if h[i+j] != n[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func lower(s string) string {
	r := []rune(s)
	for i, c := range r {
		if c >= 'A' && c <= 'Z' {
			r[i] = c + 32
		}
	}
	return string(r)
}
