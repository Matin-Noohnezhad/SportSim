package season

import "sportsim/engine/model"

// A club's books, and the constants that have to balance against each other.
//
// Money comes in two ways and goes out two ways. In: the gate, banked match by
// match in game.playFixture, and the division's prize money, settled once a year
// in payPrizeMoney. Out: wages, paid weekly, and running costs, paid alongside
// them. Change any one of these four and the other three decide whether clubs
// slowly go broke or slowly become untouchably rich — TestClubFinances is what
// holds the four together.
const (
	// homeGamesPerSeason is a club's share of a two-round league.
	homeGamesPerSeason = 19

	// typicalFill is the share of a stadium that turns up on an ordinary
	// afternoon, averaged over a season. See attendance in game/game.go: a big
	// club sells out, a small one is around four fifths full.
	typicalFill = 0.90

	// averagePrizeShare is what a club expects from its division's pot before it
	// knows where it will finish. payPrizeMoney pays the champion the full pot
	// and the bottom club a third of it, so the table averages about two thirds.
	averagePrizeShare = 0.67

	// runningCostShare is the fraction of its revenue a club spends on
	// everything that is not a player's wage: maintaining the stadium and
	// staffing it on a matchday, the academy, the training ground, coaching and
	// medical staff, travel, and the people who run the place.
	//
	// It is by far the largest number here, and it exists because wages were the
	// only outgoing the simulation had. A real club spends most of what is left
	// after wages on all of that, which is why one with revenue of €150m and a
	// €60m wage bill does not bank €90m a year. Without it every club in the game
	// did, and the world's money grew by €10bn a season — after six the median
	// club sat on €186m and could buy anybody.
	//
	// The value is set so the world's money is flat in the first season and
	// drifts up only slowly after it: wages fall as imported contracts are
	// replaced by ones the wage curve prices (see invariant 14), so a share that
	// balanced the books exactly at kickoff would have clubs hoarding by the
	// fourth season and one that balanced them in the fourth would strangle
	// everybody in the first.
	runningCostShare = 0.60
)

// SeasonGate is the money a club can expect to take on the gate across a
// campaign. The receipts themselves are banked match by match as they are taken,
// against the crowd that actually turned up; this is the figure a club budgets
// with before a ball is kicked.
func SeasonGate(c *model.Club) int64 {
	if c == nil {
		return 0
	}
	return int64(float64(c.StadiumCap) * float64(c.TicketPrice) * homeGamesPerSeason * typicalFill)
}

// Revenue is what a club expects to earn across a season: the gate, plus the
// television money its division pays.
func Revenue(w *model.World, c *model.Club) int64 {
	rev := SeasonGate(c)
	if l := w.League(c.LeagueID); l != nil {
		rev += int64(float64(l.PrizeMoney) * averagePrizeShare)
	}
	return rev
}

// RunningCosts is what a club spends in a week on everything that is not a
// player's wage.
//
// It is sized against the club's own revenue rather than its wage bill, and that
// is the point of it: a club's overheads do not fall because it sold its best
// players. They are what a badly run club still has to pay, so a wage bill that
// looked affordable against a big stadium is not affordable after relegation.
func RunningCosts(w *model.World, c *model.Club) int64 {
	if c == nil {
		return 0
	}
	return int64(float64(Revenue(w, c)) * runningCostShare / 52)
}
