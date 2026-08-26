package game

import (
	"testing"
	"time"

	"sportsim/engine/model"
)

// TestMarketSearch checks the filters actually narrow the market, that a player
// is found by any of their positions rather than only their best one, and that
// an unfiltered search of the whole world is fast enough to run on every
// keystroke — the screen re-searches as the manager types.
func TestMarketSearch(t *testing.T) {
	g, err := New("Tester", 1, LatestEdition(), 5)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	all, matched := g.Search(SearchFilter{Position: AnyPosition}, searchCap)
	elapsed := time.Since(start)
	if matched < 5000 {
		t.Fatalf("an empty filter should match nearly the whole world, got %d", matched)
	}
	if elapsed > 250*time.Millisecond {
		t.Errorf("searching the whole market took %s; it runs on every keystroke", elapsed)
	}
	if len(all) != searchCap {
		t.Fatalf("expected the results to be capped at %d, got %d", searchCap, len(all))
	}
	if all[0].Rating < all[len(all)-1].Rating {
		t.Error("default order should be by rating, best first")
	}

	// A player with more than one position must be findable by every one of them,
	// which is the whole point of keeping the dataset's position list.
	var multi *model.Player
	for i := range g.World.Players {
		p := &g.World.Players[i]
		if p.NumPositions > 1 && p.ClubID != 0 && p.ClubID != g.World.HumanClubID {
			multi = p
			break
		}
	}
	if multi == nil {
		t.Fatal("no multi-position player in the world")
	}
	for _, pos := range multi.NaturalPositions() {
		rows, _ := g.Search(SearchFilter{Name: multi.FullName, Position: pos}, 50)
		found := false
		for _, r := range rows {
			if r.PlayerID == multi.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("%s (%s) not found when filtering on %s",
				multi.Name, PositionList(multi), pos)
		}
	}

	// Each filter has to bite.
	young, _ := g.Search(SearchFilter{Position: AnyPosition, MaxAge: 21}, searchCap)
	for _, r := range young {
		if r.Age > 21 {
			t.Fatalf("%s is %d, past the age ceiling", r.Name, r.Age)
		}
	}
	strong, _ := g.Search(SearchFilter{Position: AnyPosition, MinRating: 80}, searchCap)
	for _, r := range strong {
		if r.Rating < 80 {
			t.Fatalf("%s is rated %d, under the floor", r.Name, r.Rating)
		}
	}
	cheap, _ := g.Search(SearchFilter{Position: AnyPosition, MaxFee: 1_000_000}, searchCap)
	for _, r := range cheap {
		if r.Fee > 1_000_000 {
			t.Fatalf("%s costs %d, over the ceiling", r.Name, r.Fee)
		}
	}
	if len(young) == 0 || len(strong) == 0 || len(cheap) == 0 {
		t.Error("a filter matched nobody at all")
	}

	// Ordering.
	byAge, _ := g.Search(SearchFilter{Position: AnyPosition, Sort: SortAge}, 100)
	for i := 1; i < len(byAge); i++ {
		if byAge[i].Age < byAge[i-1].Age {
			t.Fatal("age order is not ascending")
		}
	}
	byFee, _ := g.Search(SearchFilter{Position: AnyPosition, Sort: SortFee}, 100)
	for i := 1; i < len(byFee); i++ {
		if byFee[i].Fee < byFee[i-1].Fee {
			t.Fatal("fee order is not ascending")
		}
	}
}

// searchCap is the limit the tests search with, large enough to cover the world.
const searchCap = 300

// TestQuoteIsFree asserts that pricing a signing neither commits to it nor
// disturbs the world: a manager must be able to price up the whole market
// without the act of looking changing what happens next.
func TestQuoteIsFree(t *testing.T) {
	g, _ := New("Tester", 1, LatestEdition(), 9)
	rows, _ := g.Search(SearchFilter{Position: AnyPosition}, 20)
	target := rows[0]

	before := g.RNGState()
	beforeClub := g.World.Player(target.PlayerID).ClubID
	beforeBudget := g.Club().TransferBudget

	var q *Quote
	for i := 0; i < 5; i++ {
		q = g.Quote(target.PlayerID)
	}
	if q == nil {
		t.Fatal("no quote for a player the search just returned")
	}
	if g.RNGState() != before {
		t.Error("asking for a quote consumed randomness")
	}
	if g.World.Player(target.PlayerID).ClubID != beforeClub {
		t.Error("asking for a quote moved the player")
	}
	if g.Club().TransferBudget != beforeBudget {
		t.Error("asking for a quote spent money")
	}
	if q.AskingPrice != target.Fee {
		t.Errorf("quote asks %d but the list showed %d", q.AskingPrice, target.Fee)
	}
	if q.Fee < q.AskingPrice || q.Wage < uint32(q.WageDemand) {
		t.Error("the pre-filled offer should be one that would be accepted")
	}
}

// TestShortlist checks the shortlist round-trips through the search scope, since
// that is the only way the manager ever sees it.
func TestShortlist(t *testing.T) {
	g, _ := New("Tester", 1, LatestEdition(), 13)
	rows, _ := g.Search(SearchFilter{Position: AnyPosition}, 10)

	if listed := g.ToggleShortlist(rows[0].PlayerID); !listed {
		t.Fatal("first toggle should add")
	}
	if listed := g.ToggleShortlist(rows[1].PlayerID); !listed {
		t.Fatal("first toggle should add")
	}
	if g.ShortlistSize() != 2 {
		t.Fatalf("expected 2 shortlisted, got %d", g.ShortlistSize())
	}

	only, matched := g.Search(SearchFilter{Position: AnyPosition, Scope: ScopeShortlist}, 50)
	if matched != 2 || len(only) != 2 {
		t.Fatalf("shortlist scope returned %d players, expected 2", matched)
	}
	for _, r := range only {
		if !r.Shortlisted {
			t.Errorf("%s came back from the shortlist scope unflagged", r.Name)
		}
	}

	if listed := g.ToggleShortlist(rows[0].PlayerID); listed {
		t.Fatal("second toggle should remove")
	}
	if g.ShortlistSize() != 1 {
		t.Fatalf("expected 1 shortlisted after removal, got %d", g.ShortlistSize())
	}
}

// TestImprovementIsAgainstOurSquad checks the fit column measures a target
// against what the manager already has, in a position the player actually
// plays, rather than against the abstract rating scale.
func TestImprovementIsAgainstOurSquad(t *testing.T) {
	g, _ := New("Tester", 1, LatestEdition(), 17)
	best := g.bestPerPosition()

	rows, _ := g.Search(SearchFilter{Position: AnyPosition, Sort: SortImproves}, 50)
	if len(rows) == 0 {
		t.Fatal("no results")
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Improves > rows[i-1].Improves {
			t.Fatal("improvement order is not descending")
		}
	}
	top := rows[0]
	if top.Improves <= 0 {
		t.Skip("this club already has the best player available everywhere")
	}
	p := g.World.Player(top.PlayerID)
	natural := false
	for _, pos := range p.NaturalPositions() {
		if pos.String() == top.ImprovesAt {
			natural = true
		}
	}
	if !natural {
		t.Errorf("%s is credited with improving us at %s, which is not one of their positions (%s)",
			top.Name, top.ImprovesAt, PositionList(p))
	}
	pos, _ := model.ParsePos(top.ImprovesAt)
	gain := p.Rating(pos) - best[pos]
	if gain <= 0 {
		t.Errorf("%s is listed as +%d at %s but is not actually better than what we have",
			top.Name, top.Improves, top.ImprovesAt)
	}
}
