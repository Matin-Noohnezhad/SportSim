package season

import (
	"testing"

	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// TestPrizeMoneyPaidOnce pins what the rollover settles: the division's
// broadcast pot, split across the table, and nothing else.
//
// The pot is the whole deal rather than the champion's cheque, so the shares
// must come to exactly it — pay out more and every club in the game is quietly
// subsidised, pay out less and the money simply vanishes. The gate belongs to
// the matches it was taken at and is banked there; adding a season's worth again
// here, as this once did, doubled every club's matchday income.
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

	var paid int64
	for i := range w.Clubs {
		paid += w.Clubs[i].Balance
	}
	// Integer truncation costs a euro per club at most.
	if diff := 100_000_000 - paid; diff < 0 || diff > int64(len(w.Clubs)) {
		t.Errorf("paid out %d of a %d pot", paid, 100_000_000)
	}

	// Every club is level on reputation here, so only the finishing order can
	// separate them, and it must: a division that paid the same either way would
	// make the table worth nothing.
	rows := Table(w, s, 1)
	first, last := w.Club(rows[0].ClubID), w.Club(rows[len(rows)-1].ClubID)
	if first.Balance <= last.Balance {
		t.Errorf("champion took %d, bottom club %d", first.Balance, last.Balance)
	}
}

// TestPrizeMoneyFollowsTheAudience checks the quarter of the pot that goes on
// how big a club is rather than where it finished.
//
// Real domestic deals are weighted this way, and the game needs it badly: while
// the pot was shared out flat, a mid-table Getafe drew the same television money
// as Real Madrid, which left the small club in a rich division earning several
// times what it does in life and the giant earning a fraction of it.
func TestPrizeMoneyFollowsTheAudience(t *testing.T) {
	w := leagueOf(4)
	w.Leagues[0].PrizeMoney = 100_000_000
	reps := []uint8{95, 70, 55, 40}
	for i := range w.Clubs {
		w.Clubs[i].Reputation = reps[i]
	}

	// Same finishing place for each, so only market size can differ.
	var shares []float64
	for i := range w.Clubs {
		shares = append(shares, PrizeShare(w, &w.Clubs[i], 1, 4))
	}
	for i := 1; i < len(shares); i++ {
		if shares[i] >= shares[i-1] {
			t.Errorf("club of reputation %d takes %.4f, the bigger one on %d takes %.4f",
				reps[i], shares[i], reps[i-1], shares[i-1])
		}
	}

	// Finishing above a bigger club must still be worth something.
	if PrizeShare(w, &w.Clubs[1], 0, 4) <= PrizeShare(w, &w.Clubs[1], 3, 4) {
		t.Error("winning the division pays no more than finishing bottom of it")
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

// TestCommercialCarriesTheGiants is the guard on the stream that was missing
// altogether, and the reason the biggest clubs could not pay their wages.
//
// Real Madrid earn €594m of their €1,161m from sponsorship, merchandise and
// touring — more than the gate and television put together. Modelling only the
// other two left them with a revenue smaller than their wage bill, losing money
// every season whatever the costs were set to, because no share of a number too
// small can cover a number bigger than it.
func TestCommercialCarriesTheGiants(t *testing.T) {
	giant, ordinary, small := Commercial(95), Commercial(75), Commercial(45)
	if !(giant > ordinary && ordinary > small) {
		t.Fatalf("commercial income does not rise with standing: %d, %d, %d", giant, ordinary, small)
	}
	// It has to be steep, not merely increasing: a global name earns an order of
	// magnitude more from it than a solid top-flight club, which is what lets the
	// same curve serve both ends of the game.
	if giant < 10*ordinary {
		t.Errorf("a club of reputation 95 earns %d commercially against %d for one on 75", giant, ordinary)
	}
	// And everyone has some, or the smallest clubs cannot cover their overheads.
	if Commercial(20) <= 0 {
		t.Error("the smallest clubs have no commercial income at all")
	}
}

// TestTransferBudgetComesFromRevenue pins the link that was broken on purpose.
//
// Budgets used to be half of whatever had piled up in the bank, so any drift in
// the books eventually handed every club unlimited buying power. What a club can
// spend still has to follow what it earns: savings add to it, but only up to a
// bound set by the club's own revenue, so no amount of hoarding turns a small
// club into a giant.
func TestTransferBudgetComesFromRevenue(t *testing.T) {
	w := leagueOf(2)
	w.Leagues[0].PrizeMoney = 50_000_000
	c := &w.Clubs[0]
	c.Reputation, c.StadiumCap = 80, 50_000
	c.TicketPrice = model.DefaultTicketPrice(c.Reputation)

	// Flush with cash: the budget is bounded by revenue, not by the balance.
	c.Balance = 10_000_000_000
	rich := TransferBudget(w, c)
	if rich >= c.Balance/4 {
		t.Errorf("a club with %d in the bank may spend %d", c.Balance, rich)
	}
	if ceiling := int64(float64(Revenue(w, c)) * (transferBudgetShare + warChestCap)); rich > ceiling {
		t.Errorf("budget %d exceeds %d, everything a season's revenue can justify", rich, ceiling)
	}

	// Short of cash: the balance is the ceiling.
	c.Balance = 1_000_000
	if got := TransferBudget(w, c); got != 1_000_000 {
		t.Errorf("a club with 1M in the bank may spend %d", got)
	}

	// Overdrawn: nothing at all.
	c.Balance = -5_000_000
	if got := TransferBudget(w, c); got != 0 {
		t.Errorf("an overdrawn club may spend %d", got)
	}
}

// TestSavingsAreSpendable is the other half of that bargain, and the reason the
// war chest exists at all: a club that banks a season's profit must be able to
// spend it the following summer.
//
// While the budget was a flat share of revenue, a career of careful trading
// changed nothing — the money accumulated in the bank and the board offered the
// same purse every August, which reads to a manager as their thrift being
// ignored.
func TestSavingsAreSpendable(t *testing.T) {
	w := leagueOf(2)
	w.Leagues[0].PrizeMoney = 50_000_000
	c := &w.Clubs[0]
	c.Reputation, c.StadiumCap = 80, 50_000
	c.TicketPrice = model.DefaultTicketPrice(c.Reputation)
	revenue := Revenue(w, c)

	// Living hand to mouth: the working capital a club runs on is not a war chest.
	c.Balance = int64(float64(revenue) * workingCapitalShare)
	lean := TransferBudget(w, c)
	if want := int64(float64(revenue) * transferBudgetShare); lean != want {
		t.Errorf("a club with no savings may spend %d, want its revenue share of %d", lean, want)
	}

	// A season of it put by, and the budget has to move with it.
	c.Balance += revenue
	saved := TransferBudget(w, c)
	if saved <= lean {
		t.Errorf("banking a season's revenue moved the budget from %d to %d", lean, saved)
	}

	// Twice as much saved buys more again, until the cap bites.
	c.Balance += revenue
	if more := TransferBudget(w, c); more <= saved {
		t.Errorf("banking a second season moved the budget from %d to %d", saved, more)
	}
}
