package season

import (
	"math"

	"sportsim/engine/model"
)

// A club's books, and the constants that have to balance against each other.
//
// Money comes in three ways and goes out two. In: the gate, banked match by
// match in game.playFixture; commercial income, credited weekly; and the
// division's broadcast money, settled once a year in payPrizeMoney. Out: wages
// and running costs, both weekly. Move any one of these and the rest decide
// whether clubs slowly go broke or slowly become untouchably rich — the money
// checks in TestMultiSeason are what hold them together.
const (
	// homeGamesPerSeason is a club's share of a two-round league.
	homeGamesPerSeason = 19

	// typicalFill is the share of a stadium that turns up on an ordinary
	// afternoon, averaged over a season. See attendance in game/game.go: a big
	// club sells out, a small one is around four fifths full.
	typicalFill = 0.90

	// How a division's broadcast pot is split, after the pattern the real
	// domestic deals use: half of it equally between the clubs, a quarter on
	// where they finished, and a quarter on how much of the audience they bring.
	//
	// The last quarter is what makes the distribution realistic. Paying it out
	// flat gave a mid-table Getafe the same television money as Real Madrid, so
	// the small club in a rich division earned three times what it does in life
	// while the giant earned a fraction — which is most of why the giants could
	// not pay their wage bills.
	prizeEqualShare  = 0.50
	prizeMeritShare  = 0.25
	prizeMarketShare = 0.25

	// marketWeightExponent decides how steeply the audience quarter tilts towards
	// the biggest clubs. Reputation runs 1-100 and the curve is deliberately
	// sharp: the gap between a good side and a global one is not linear in
	// anything, least of all in what a broadcaster will pay for them.
	marketWeightExponent = 6.0

	// Commercial income: sponsorship, shirt deals, merchandise, tours. It is the
	// largest single stream in real football and the game had none of it — Real
	// Madrid earn €594m of their €1,161m that way, more than gate and television
	// together, which is exactly why they could not be made solvent by tuning
	// anything else. It is steeper in reputation than any other stream, because
	// it is the one that scales with global following rather than with a stadium
	// or a league's collective deal.
	commercialBase     = 4_000_000.0
	commercialPeak     = 1_150_000_000.0
	commercialExponent = 11.0

	// runningCostShare is the fraction of revenue a club spends on everything
	// that is not a player's wage: maintaining the stadium and staffing it on a
	// matchday, the academy, the training ground, coaching and medical staff,
	// travel, and the people who run the place.
	//
	// It exists because wages were once the only outgoing, and without it every
	// club banked the difference: the world's money grew by €10bn a season and
	// after six the median club sat on €186m and could buy anybody.
	runningCostShare = 0.62

	// transferBudgetShare is how much of a season's revenue a club will commit to
	// fees. Budgets are set from revenue rather than from the bank balance so
	// that money piling up over a long career cannot quietly turn into unlimited
	// buying power — a club spends against what it earns. See TransferBudget.
	transferBudgetShare = 0.45

	// What a club will do with its savings, on top of that share.
	//
	// Revenue alone is not the whole answer: a club banks what it does not spend,
	// and with the budget deaf to the balance that money did nothing at all. Six
	// seasons in, Barcelona sit on €1.5bn and are still told they have €324m to
	// spend, which is the complaint every long career eventually produces — and
	// it is wrong in football's own terms too, since a war chest is exactly what
	// a season of thrift buys you.
	//
	// So a club first holds workingCapitalShare of a season's revenue back as the
	// money it runs on, and puts warChestShare of what is left over into the
	// budget. The addition is capped at warChestCap of revenue, for the reason
	// budgets were cut loose from the balance in the first place: the books drift
	// upwards across a long career, and an unbounded slice of the bank would
	// eventually hand every club in the world the same bottomless purse and
	// flatten the market. Sizing the cap against the club's own revenue keeps the
	// hierarchy intact — savings let a mid-table club buy well, never buy anybody.
	workingCapitalShare = 0.50
	warChestShare       = 0.35
	warChestCap         = 1.00
)

// What a continental campaign is worth, following the shape of UEFA's own
// distribution: a fee for turning up at all, a cheque per point won in the
// group, and a larger one for every knockout round reached.
//
// The Champions League is worth an order of magnitude more than the Conference
// League, and that gap is doing real work in the game: it is why finishing
// fourth rather than seventh is worth spending a transfer budget on, and why a
// mid-table club in a strong league can out-earn the champion of a weak one.
// The figures are the real competitions' scaled to this world's smaller field.
var europeanPurse = [model.NumCompetitions]struct {
	Participation int64
	PerPoint      int64
	Round16       int64
	Quarter       int64
	Semi          int64
	Final         int64
	Winner        int64
}{
	model.ChampionsLeague:  {18_000_000, 1_000_000, 11_000_000, 12_500_000, 15_000_000, 18_500_000, 6_000_000},
	model.EuropaLeague:     {4_200_000, 400_000, 0, 1_800_000, 2_800_000, 4_600_000, 4_000_000},
	model.ConferenceLeague: {3_000_000, 200_000, 0, 1_300_000, 2_000_000, 3_000_000, 2_000_000},
}

// EuroParticipation is what a club banks simply for reaching the group stage.
func EuroParticipation(c model.Competition) int64 {
	if c >= model.NumCompetitions {
		return 0
	}
	return europeanPurse[c].Participation
}

// EuroPointBonus is what one point in the group stage is worth.
func EuroPointBonus(c model.Competition) int64 {
	if c >= model.NumCompetitions {
		return 0
	}
	return europeanPurse[c].PerPoint
}

// EuroRoundBonus is what reaching a knockout round pays, to both clubs in every
// tie of it.
func EuroRoundBonus(c model.Competition, st Stage) int64 {
	if c >= model.NumCompetitions {
		return 0
	}
	p := europeanPurse[c]
	switch st {
	case StageRound16:
		return p.Round16
	case StageQuarter:
		return p.Quarter
	case StageSemi:
		return p.Semi
	case StageFinal:
		return p.Final
	}
	return 0
}

// EuroWinnerBonus is what lifting the trophy is worth, on top of reaching the
// final.
func EuroWinnerBonus(c model.Competition) int64 {
	if c >= model.NumCompetitions {
		return 0
	}
	return europeanPurse[c].Winner
}

// ExpectedEuro is the continental money a club can plan a season around before
// the draw is even made: the participation fee, a middling group campaign, and
// an even chance of coming through it.
//
// It has to be part of Revenue rather than a windfall on top of it, because
// revenue is what sizes both running costs and the transfer budget. A club that
// banked Champions League money without it ever reaching Revenue would spend
// none of it and simply grow richer every year — the same drift that made every
// club able to buy anybody when budgets came from the balance.
func ExpectedEuro(c *model.Club) int64 {
	if c == nil || c.Competition == model.NoComp {
		return 0
	}
	p := europeanPurse[c.Competition]
	const typicalGroupPoints = 8
	first := p.Round16
	if first == 0 {
		first = p.Quarter
	}
	return p.Participation + typicalGroupPoints*p.PerPoint + first/2
}

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

// Commercial is a club's sponsorship, merchandising and touring income across a
// season, which follows how big a name it is and nothing else.
func Commercial(rep uint8) int64 {
	return int64(commercialBase + commercialPeak*math.Pow(float64(rep)/100, commercialExponent))
}

// marketWeight is a club's pull on a broadcast audience.
func marketWeight(rep uint8) float64 {
	return math.Pow(float64(rep)/100, marketWeightExponent)
}

// PrizeShare is the fraction of its division's pot a club takes, finishing in
// the given place of n. Shares across a division sum to one, so the pot is the
// whole broadcast deal rather than the champion's cheque.
func PrizeShare(w *model.World, c *model.Club, place, n int) float64 {
	if c == nil || n <= 0 {
		return 0
	}
	share := prizeEqualShare / float64(n)

	if n > 1 {
		// Merit runs from the whole of its quarter for the champion down to none
		// for the bottom club, so the average across the table is half of it.
		merit := 1 - float64(place)/float64(n-1)
		share += prizeMeritShare * merit * 2 / float64(n)
	} else {
		share += prizeMeritShare
	}

	if l := w.League(c.LeagueID); l != nil {
		var total float64
		for _, id := range l.ClubIDs {
			if other := w.Club(id); other != nil {
				total += marketWeight(other.Reputation)
			}
		}
		if total > 0 {
			share += prizeMarketShare * marketWeight(c.Reputation) / total
		}
	}
	return share
}

// ExpectedPrize is the broadcast money a club can plan around before a ball is
// kicked, which is its share from a mid-table finish.
func ExpectedPrize(w *model.World, c *model.Club) int64 {
	l := w.League(c.LeagueID)
	if l == nil {
		return 0
	}
	n := len(l.ClubIDs)
	if n == 0 {
		return 0
	}
	return int64(float64(l.PrizeMoney) * PrizeShare(w, c, n/2, n))
}

// Revenue is what a club expects to earn across a season: the gate, the
// television money its division pays, its commercial income, and whatever
// European football it has qualified for.
func Revenue(w *model.World, c *model.Club) int64 {
	if c == nil {
		return 0
	}
	return SeasonGate(c) + ExpectedPrize(w, c) + Commercial(c.Reputation) + ExpectedEuro(c)
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

// CommercialIncome is the week's share of a club's commercial deals.
func CommercialIncome(c *model.Club) int64 {
	if c == nil {
		return 0
	}
	return Commercial(c.Reputation) / 52
}

// TransferBudget is what a club will commit to fees over a season: a share of
// what it earns, plus a bounded slice of the savings it has beyond the money it
// runs on, and never more than it actually has in the bank.
//
// The revenue share is the source and the war chest is the supplement, not the
// other way round. Budgets used to be half of whatever had accumulated, so a
// long career with any drift at all in the books ended with every club able to
// buy anybody; a budget deaf to the balance was the cure and went too far the
// other way, leaving a decade of thrift worth nothing. Capping the war chest
// against the club's own revenue is what holds both ends: a season's saving is
// spendable, an era of it still cannot buy a club a squad it could never earn.
func TransferBudget(w *model.World, c *model.Club) int64 {
	if c == nil {
		return 0
	}
	revenue := float64(Revenue(w, c))
	budget := int64(revenue * transferBudgetShare)
	if spare := float64(c.Balance) - revenue*workingCapitalShare; spare > 0 {
		budget += int64(math.Min(spare*warChestShare, revenue*warChestCap))
	}
	if c.Balance < budget {
		budget = c.Balance
	}
	if budget < 0 {
		budget = 0
	}
	return budget
}
