package season

import (
	"testing"

	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// TestPrizeMoneyPaidOnce pins what the rollover settles: the division's pot,
// shared down the table, and nothing else.
//
// The gate belongs to the matches it was taken at and is banked there. Paying a
// season's worth again here — as this once did — quietly doubled every club's
// matchday income, which is the sort of mistake that only shows up as clubs
// being inexplicably rich several seasons later.
func TestPrizeMoneyPaidOnce(t *testing.T) {
	w := leagueOf(4)
	w.Leagues[0].PrizeMoney = 100_000_000
	for i := range w.Clubs {
		c := &w.Clubs[i]
		c.Reputation = 60
		c.StadiumCap = 40_000
		c.TicketPrice = model.DefaultTicketPrice(c.Reputation)
		c.Balance = 0
	}
	s := Generate(w, 2026, rng.New(1))

	out := &Outcome{Champions: map[uint16]uint16{}}
	payPrizeMoney(w, s, out)

	// With no matches played the table is level, so the order is arbitrary but
	// the shares are not: the top club takes the pot and the bottom about a third.
	var got []int64
	for i := range w.Clubs {
		got = append(got, w.Clubs[i].Balance)
	}
	want := []int64{100_000_000, 78_000_000, 56_000_000, 34_000_000}
	sortDesc(got)
	for i, v := range want {
		if got[i] != v {
			t.Errorf("place %d paid %d, want %d", i+1, got[i], v)
		}
	}
}

func sortDesc(x []int64) {
	for i := range x {
		for j := i + 1; j < len(x); j++ {
			if x[j] > x[i] {
				x[i], x[j] = x[j], x[i]
			}
		}
	}
}

// TestClubHasGateIncome is the guard on the bug that made this file necessary:
// every club must charge something at the turnstile.
//
// TicketPrice was computed by the importer and then never written to the packed
// database, so every club in the game loaded charging nothing. Both formulas
// that turn a crowd into money multiplied by it, so a club's only income was the
// prize cheque at the end of the season while wages went out every Monday, and
// balances fell in a straight line all year.
func TestClubHasGateIncome(t *testing.T) {
	for rep := 1; rep <= 100; rep++ {
		if p := model.DefaultTicketPrice(uint8(rep)); p < 12 {
			t.Fatalf("a club of reputation %d charges %d on the gate", rep, p)
		}
	}

	c := &model.Club{Reputation: 80, StadiumCap: 50_000, TicketPrice: model.DefaultTicketPrice(80)}
	gate := SeasonGate(c)
	if gate <= 0 {
		t.Fatalf("season gate is %d", gate)
	}
	// Nineteen home games at nine tenths of a 50,000 capacity, at €52 a seat.
	if want := int64(50_000 * 52 * 19 * 0.90); gate != want {
		t.Errorf("season gate %d, want %d", gate, want)
	}
}

// TestRunningCostsScaleWithRevenue checks the outgoing that balances the books.
//
// A club's overheads have to follow what it earns rather than what it pays its
// players: they are what it still owes after selling everybody, which is what
// stops a relegated club simply shedding wages until it is comfortable again.
func TestRunningCostsScaleWithRevenue(t *testing.T) {
	w := leagueOf(2)
	w.Leagues[0].PrizeMoney = 50_000_000
	big := &w.Clubs[0]
	big.Reputation, big.StadiumCap = 90, 60_000
	big.TicketPrice = model.DefaultTicketPrice(big.Reputation)
	small := &w.Clubs[1]
	small.Reputation, small.StadiumCap = 40, 12_000
	small.TicketPrice = model.DefaultTicketPrice(small.Reputation)

	for _, c := range []*model.Club{big, small} {
		rev, run := Revenue(w, c), RunningCosts(w, c)
		if run <= 0 {
			t.Fatalf("%s spends %d a week running itself", c.Name, run)
		}
		// The weekly charge must come to the intended share of a year's revenue.
		if got, want := float64(run*52)/float64(rev), runningCostShare; got < want-0.01 || got > want+0.01 {
			t.Errorf("%s spends %.3f of revenue, want %.2f", c.Name, got, want)
		}
	}
	if RunningCosts(w, big) <= RunningCosts(w, small) {
		t.Error("the bigger club is not the more expensive to run")
	}
	if RunningCosts(w, nil) != 0 {
		t.Error("a club that does not exist costs something to run")
	}
}
