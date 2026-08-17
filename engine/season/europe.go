package season

import (
	"fmt"
	"sort"

	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// The continental competitions.
//
// Three tournaments run alongside the league season, and everything about them
// lives here: who qualifies, how the groups are drawn, when the nights are
// played and how a two-legged tie is settled. Only the money is elsewhere, in
// finance.go, with the rest of the club economy — see invariant 14.
//
// The one structural difference from a league is that a continental season
// cannot be written out in August. A knockout bracket is drawn from the clubs
// that came through the round before it, so the fixtures for it do not exist
// until they do: Generate lays out the group stage, and EuroAdvance appends
// each knockout round on the day the previous one finishes.

// Stage is how far a continental competition has got. It is persisted on a
// fixture, so values may only be appended.
type Stage uint8

const (
	StageGroup Stage = iota
	StageRound16
	StageQuarter
	StageSemi
	StageFinal
	StageDone
)

// String names the stage the way a fixture list would.
func (s Stage) String() string {
	switch s {
	case StageRound16:
		return "Round of 16"
	case StageQuarter:
		return "Quarter-final"
	case StageSemi:
		return "Semi-final"
	case StageFinal:
		return "Final"
	case StageDone:
		return "Complete"
	}
	return "Group stage"
}

// TwoLegged reports whether a round of this stage is played over two matches.
// Only the final is settled in one, on neutral ground.
func (s Stage) TwoLegged() bool { return s != StageFinal && s != StageDone }

// Tie is one knockout pairing, with the aggregate score once it has been
// played out. Home is the club that hosts the first leg.
type Tie struct {
	Home, Away uint16

	HomeGoals, AwayGoals uint8 // aggregate over both legs
	ShootoutHome         uint8
	ShootoutAway         uint8
	Winner               uint16
}

// Decided reports whether the tie has a winner.
func (t Tie) Decided() bool { return t.Winner != 0 }

// KnockoutRound is one round of a bracket. Rounds are kept once they have been
// played rather than replaced, so a competition can be shown as the run it
// actually was — the tie a club came through in February is as much a part of
// winning a trophy as the final.
type KnockoutRound struct {
	Stage Stage
	Ties  []Tie
}

// CompSeason is one competition's campaign: the groups it was drawn into, the
// round it has reached, and every round it has played.
type CompSeason struct {
	Comp   model.Competition
	Groups [][]uint16
	Stage  Stage
	Rounds []KnockoutRound

	Winner   uint16
	RunnerUp uint16
}

// Round returns the ties of one stage, or nil if it has not been drawn.
func (cs *CompSeason) Round(st Stage) *KnockoutRound {
	if cs == nil {
		return nil
	}
	for i := range cs.Rounds {
		if cs.Rounds[i].Stage == st {
			return &cs.Rounds[i]
		}
	}
	return nil
}

// current is the round most recently drawn, which is the one being played.
func (cs *CompSeason) current() *KnockoutRound {
	if len(cs.Rounds) == 0 {
		return nil
	}
	return &cs.Rounds[len(cs.Rounds)-1]
}

// Euro is the continental season across all three competitions.
type Euro struct {
	Comps []CompSeason
}

// Comp returns one competition's campaign, or nil if it was not played.
func (e *Euro) Comp(c model.Competition) *CompSeason {
	if e == nil {
		return nil
	}
	for i := range e.Comps {
		if e.Comps[i].Comp == c {
			return &e.Comps[i]
		}
	}
	return nil
}

// groupSize is how many clubs are drawn into each group. Every competition uses
// four, and the top two go through, so the size of the knockout bracket follows
// from the number of groups alone.
const groupSize = 4

// ----- qualification -----

// AssignEuropeanPlaces settles which clubs play in Europe next season, from the
// final tables of the top divisions. It is called at the rollover, and again for
// a world that has never played a season, where a club's reputation stands in
// for a finishing position it does not yet have.
//
// Places are shared between the top divisions in proportion to the square of
// their reputation, which is the game's stand-in for UEFA's coefficient list: a
// steep enough curve that the Premier League sends twice what the Süper Lig
// does, flat enough that every country in the game has a route in.
func AssignEuropeanPlaces(w *model.World, s *Schedule) {
	for i := range w.Clubs {
		w.Clubs[i].Competition = model.NoComp
	}

	leagues := topFlights(w)
	// A world too small to fill the competitions plays no European football at
	// all. Half-filling a group stage would be worse than leaving it out, and
	// the fixture-list tests build worlds of a single small league.
	total := 0
	for _, c := range model.AllCompetitions() {
		total += c.Entrants()
	}
	clubs := 0
	for _, l := range leagues {
		clubs += len(l.ClubIDs)
	}
	if clubs < total {
		return
	}

	weights := make([]float64, len(leagues))
	for i, l := range leagues {
		rep := float64(l.Reputation)
		weights[i] = rep * rep
	}

	// Every competition's allocation is worked out before any of it is handed
	// out, because a league fills its places from the top of one table: the
	// clubs below its Champions League places take the Europa League ones.
	places := make(map[model.Competition][]int, model.NumCompetitions)
	for _, c := range model.AllCompetitions() {
		places[c] = allocatePlaces(weights, c.Entrants(), c.MaxPerLeague())
	}

	for i, l := range leagues {
		order := finalOrder(w, s, l)
		next := 0
		for _, c := range model.AllCompetitions() {
			for k := 0; k < places[c][i] && next < len(order); k++ {
				if club := w.Club(order[next]); club != nil {
					club.Competition = c
				}
				next++
			}
		}
	}
}

// topFlights lists the first-tier divisions, strongest first. Only they send
// clubs to Europe: a second division's champion goes up, not abroad.
func topFlights(w *model.World) []*model.League {
	out := make([]*model.League, 0, len(w.Leagues))
	for i := range w.Leagues {
		if w.Leagues[i].Tier == 1 && len(w.Leagues[i].ClubIDs) > 0 {
			out = append(out, &w.Leagues[i])
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Reputation != out[b].Reputation {
			return out[a].Reputation > out[b].Reputation
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// finalOrder is the league's clubs in the order they finished. Before a ball
// has been kicked there is no table to read, so reputation stands in for one.
func finalOrder(w *model.World, s *Schedule, l *model.League) []uint16 {
	if s != nil {
		if rows := Table(w, s, l.ID); len(rows) > 0 && rows[0].Played > 0 {
			out := make([]uint16, 0, len(rows))
			for _, r := range rows {
				out = append(out, r.ClubID)
			}
			return out
		}
	}
	out := append([]uint16(nil), l.ClubIDs...)
	sort.SliceStable(out, func(a, b int) bool {
		ca, cb := w.Club(out[a]), w.Club(out[b])
		if ca == nil || cb == nil {
			return ca != nil
		}
		if ca.Reputation != cb.Reputation {
			return ca.Reputation > cb.Reputation
		}
		return ca.Name < cb.Name
	})
	return out
}

// allocatePlaces splits total places between leagues in proportion to weight,
// by largest remainder, and never gives one league more than cap.
//
// The cap is what keeps a competition continental. Without it the two richest
// divisions take a third of the Champions League between them, and the clubs
// that would make the tournament worth winning are all playing each other in
// the group stage.
func allocatePlaces(weights []float64, total, cap int) []int {
	out := make([]int, len(weights))
	if len(weights) == 0 || total <= 0 {
		return out
	}
	var sum float64
	for _, x := range weights {
		sum += x
	}
	if sum <= 0 {
		return out
	}

	type share struct {
		league int
		frac   float64
	}
	fracs := make([]share, 0, len(weights))
	given := 0
	for i, x := range weights {
		exact := x / sum * float64(total)
		whole := int(exact)
		if whole > cap {
			whole = cap
		}
		out[i] = whole
		given += whole
		fracs = append(fracs, share{i, exact - float64(int(exact))})
	}
	sort.SliceStable(fracs, func(a, b int) bool { return fracs[a].frac > fracs[b].frac })

	// Rounding always leaves places over. They go to the largest remainders
	// first, and the loop goes round again if a cap sent one past a league that
	// had already taken its fill.
	for given < total {
		handed := false
		for _, f := range fracs {
			if given >= total {
				break
			}
			if out[f.league] >= cap {
				continue
			}
			out[f.league]++
			given++
			handed = true
		}
		if !handed {
			break
		}
	}
	return out
}

// ----- the draw -----

// drawEurope draws the group stage of every competition and puts it in the
// calendar. A competition the world cannot fill is simply not played.
func drawEurope(w *model.World, s *Schedule, r *rng.R) {
	if !anyQualified(w) {
		AssignEuropeanPlaces(w, nil)
	}

	e := &Euro{}
	cal := newCalendar(s)
	for _, c := range model.AllCompetitions() {
		entrants := entrantsFor(w, c)
		if len(entrants) != c.Entrants() {
			// Whoever was given a place in a competition that is not being
			// played is not in Europe at all, or their books would be credited
			// with money from a tournament that does not exist.
			for _, id := range entrants {
				w.Club(id).Competition = model.NoComp
			}
			continue
		}
		cs := CompSeason{Comp: c, Groups: drawGroups(w, entrants, c.Groups(), r), Stage: StageGroup}
		scheduleGroupStage(s, cal, &cs)
		payEuro(w, entrants, EuroParticipation(c))
		e.Comps = append(e.Comps, cs)
	}
	s.Euro = e
}

func anyQualified(w *model.World) bool {
	for i := range w.Clubs {
		if w.Clubs[i].Competition != model.NoComp {
			return true
		}
	}
	return false
}

// entrantsFor lists a competition's clubs, strongest first, which is the order
// the seeding pots are cut from.
func entrantsFor(w *model.World, c model.Competition) []uint16 {
	out := make([]uint16, 0, c.Entrants())
	for i := range w.Clubs {
		if w.Clubs[i].Competition == c {
			out = append(out, w.Clubs[i].ID)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		ca, cb := w.Club(out[a]), w.Club(out[b])
		if ca.Reputation != cb.Reputation {
			return ca.Reputation > cb.Reputation
		}
		return ca.Name < cb.Name
	})
	return out
}

// drawGroups seeds the entrants into pots and draws one club from each pot into
// each group, keeping two clubs of the same country apart wherever the pot
// still allows it.
func drawGroups(w *model.World, entrants []uint16, groups int, r *rng.R) [][]uint16 {
	out := make([][]uint16, groups)
	for i := range out {
		out[i] = make([]uint16, 0, groupSize)
	}

	for pot := 0; pot < groupSize; pot++ {
		drum := append([]uint16(nil), entrants[pot*groups:(pot+1)*groups]...)
		r.Shuffle(len(drum), func(i, j int) { drum[i], drum[j] = drum[j], drum[i] })

		for _, id := range drum {
			target := -1
			for gi := range out {
				if len(out[gi]) > pot {
					continue // already took a club from this pot
				}
				if target < 0 {
					target = gi
				}
				if !sameCountry(w, out[gi], id) {
					target = gi
					break
				}
			}
			out[target] = append(out[target], id)
		}
	}
	return out
}

func sameCountry(w *model.World, group []uint16, id uint16) bool {
	c := w.Club(id)
	if c == nil {
		return false
	}
	for _, other := range group {
		if o := w.Club(other); o != nil && o.NationID == c.NationID {
			return true
		}
	}
	return false
}

// ----- the calendar -----

// groupNights are the six European weeks the group stage is played in. They sit
// midweek and clear of the domestic weekends either side of them.
var groupNights = [6][2]int{{9, 16}, {10, 7}, {10, 28}, {11, 11}, {12, 2}, {12, 16}}

// knockoutNights gives a round its two legs, as month and day in the calendar
// year the season finishes in. The three competitions are staggered rather than
// stacked: a club in the Conference League should not be asked to play its
// quarter-final in the same week the Champions League plays one, because the
// domestic weekend either side of it is the same weekend.
//
// The finals fall after the last league fixture, which is what carries the
// season past the final day and into June.
func knockoutNights(c model.Competition, st Stage) [2][2]int {
	// A competition with four groups sends eight clubs through and so starts at
	// the quarter-finals; only the Champions League plays a round of sixteen.
	// Each competition's rounds are spread across the same February-to-May span
	// whatever its first round is, so no club sits four months between matches.
	switch c {
	case model.EuropaLeague:
		switch st {
		case StageQuarter:
			return [2][2]int{{2, 19}, {3, 12}}
		case StageSemi:
			return [2][2]int{{4, 9}, {4, 16}}
		}
		return [2][2]int{{5, 20}, {5, 20}}
	case model.ConferenceLeague:
		switch st {
		case StageQuarter:
			return [2][2]int{{2, 26}, {3, 19}}
		case StageSemi:
			return [2][2]int{{4, 23}, {4, 30}}
		}
		return [2][2]int{{5, 27}, {5, 27}}
	}
	switch st {
	case StageRound16:
		return [2][2]int{{2, 17}, {3, 10}}
	case StageQuarter:
		return [2][2]int{{4, 7}, {4, 14}}
	case StageSemi:
		return [2][2]int{{4, 28}, {5, 5}}
	}
	return [2][2]int{{5, 30}, {5, 30}}
}

// calendar tracks the days each club is already committed to, so that a
// European night can be moved off a domestic fixture rather than doubling a
// club up. Two matches in one day would run the same players down twice and
// leave the manager watching only one of them.
type calendar map[int64]bool

func newCalendar(s *Schedule) calendar {
	c := make(calendar, len(s.Fixtures)*2)
	for i := range s.Fixtures {
		f := &s.Fixtures[i]
		c.claim(f.Date, f.Home, f.Away)
	}
	return c
}

func dayKey(d model.Date, club uint16) int64 { return int64(d)<<16 | int64(club) }

func (c calendar) busy(d model.Date, clubs ...uint16) bool {
	for _, id := range clubs {
		if c[dayKey(d, id)] {
			return true
		}
	}
	return false
}

func (c calendar) claim(d model.Date, clubs ...uint16) {
	for _, id := range clubs {
		c[dayKey(d, id)] = true
	}
}

// night finds a day for a European tie at or near the target, on which neither
// club is already playing, preferring to push a clash later rather than earlier.
//
// A fortnight either side is far more room than a midweek slot beside a league
// weekend needs; falling through it would mean a club committed to fifteen days
// running, which no fixture list in the game produces.
func (c calendar) night(target model.Date, home, away uint16) model.Date {
	for off := 0; off <= 7; off++ {
		for _, d := range []model.Date{target + model.Date(off), target - model.Date(off)} {
			if !c.busy(d, home, away) {
				c.claim(d, home, away)
				return d
			}
			if off == 0 {
				break
			}
		}
	}
	c.claim(target, home, away)
	return target
}

// scheduleGroupStage lays out six matchdays of a double round-robin in every
// group, using the same circle method the league fixture list is built with.
func scheduleGroupStage(s *Schedule, cal calendar, cs *CompSeason) {
	year := s.Year
	for gi, clubs := range cs.Groups {
		rounds := roundRobin(clubs)
		for half := 0; half < 2; half++ {
			for ri, pairs := range rounds {
				idx := half*len(rounds) + ri
				if idx >= len(groupNights) {
					continue
				}
				target := model.NewDate(year, groupNights[idx][0], groupNights[idx][1])
				for _, p := range pairs {
					home, away := p[0], p[1]
					if half == 1 {
						home, away = away, home
					}
					s.Fixtures = append(s.Fixtures, Fixture{
						Comp:  cs.Comp,
						Stage: StageGroup,
						Group: uint8(gi + 1),
						Round: uint16(idx + 1),
						Date:  cal.night(target, home, away),
						Home:  home,
						Away:  away,
					})
				}
			}
		}
	}
}

// scheduleRound puts a knockout round's ties in the calendar. Two legs, except
// for a final, which is one match on neutral ground.
func scheduleRound(s *Schedule, cal calendar, cs *CompSeason) {
	round := cs.current()
	if round == nil {
		return
	}
	nights := knockoutNights(cs.Comp, round.Stage)
	legs := 2
	if !round.Stage.TwoLegged() {
		legs = 1
	}
	for _, t := range round.Ties {
		for leg := 0; leg < legs; leg++ {
			home, away := t.Home, t.Away
			if leg == 1 {
				home, away = t.Away, t.Home
			}
			target := model.NewDate(s.Year+1, nights[leg][0], nights[leg][1])
			s.Fixtures = append(s.Fixtures, Fixture{
				Comp:  cs.Comp,
				Stage: round.Stage,
				Leg:   uint8(leg + 1),
				Date:  cal.night(target, home, away),
				Home:  home,
				Away:  away,
			})
		}
	}
}

// ----- running the competitions -----

// EuroAdvance moves the continental competitions on: it settles any knockout
// round whose matches have all been played, draws the next one, and pays the
// money each round is worth. It returns whatever is worth telling the manager.
//
// It is called once a day, after the day's fixtures. Nothing happens on most of
// them, which is the point: a bracket only exists once the clubs in it are known.
func EuroAdvance(w *model.World, s *Schedule, r *rng.R) []string {
	if s.Euro == nil {
		return nil
	}
	var news []string
	// The calendar is only worth building on a day a round is actually drawn,
	// which is a dozen days a season out of three hundred and sixty-five.
	var cal calendar
	for i := range s.Euro.Comps {
		cs := &s.Euro.Comps[i]
		if cs.Stage == StageDone || !roundPlayed(s, cs.Comp, cs.Stage) {
			continue
		}
		if cal == nil {
			cal = newCalendar(s)
		}
		if cs.Stage == StageGroup {
			news = append(news, openKnockout(w, s, cs, cal, r)...)
		} else {
			news = append(news, settleRound(w, s, cs, cal, r)...)
		}
	}
	return news
}

// roundPlayed reports whether every fixture of a stage has been played.
func roundPlayed(s *Schedule, c model.Competition, st Stage) bool {
	found := false
	for i := range s.Fixtures {
		f := &s.Fixtures[i]
		if f.Comp != c || f.Stage != st {
			continue
		}
		found = true
		if !f.Played {
			return false
		}
	}
	return found
}

// openKnockout takes the top two of every group through, pays the group stage's
// merit money and draws the first knockout round.
func openKnockout(w *model.World, s *Schedule, cs *CompSeason, cal calendar, r *rng.R) []string {
	var winners, runnersUp []uint16
	for gi := range cs.Groups {
		rows := GroupTable(w, s, cs.Comp, gi+1)
		for i, row := range rows {
			// The group is worth a point-by-point cheque as well as a place in
			// the draw, so a club knocked out in December still leaves with
			// something for the nights it won.
			payEuro(w, []uint16{row.ClubID}, int64(row.Points)*EuroPointBonus(cs.Comp))
			switch i {
			case 0:
				winners = append(winners, row.ClubID)
			case 1:
				runnersUp = append(runnersUp, row.ClubID)
			}
		}
	}

	news := []string{fmt.Sprintf("The %s group stage is over.", cs.Comp)}
	if h := w.HumanClubID; h != 0 {
		switch {
		case contains(winners, h):
			news = append(news, fmt.Sprintf("%s win their %s group and go through.",
				w.ClubName(h), cs.Comp))
		case contains(runnersUp, h):
			news = append(news, fmt.Sprintf("%s go through from their %s group in second.",
				w.ClubName(h), cs.Comp))
		case w.Club(h).Competition == cs.Comp:
			news = append(news, fmt.Sprintf("%s are out of the %s at the group stage.",
				w.ClubName(h), cs.Comp))
		}
	}

	cs.Stage = stageFor(len(winners) + len(runnersUp))
	cs.Rounds = append(cs.Rounds, KnockoutRound{Stage: cs.Stage, Ties: drawFirstKnockout(winners, runnersUp, r)})
	scheduleRound(s, cal, cs)
	return append(news, payRound(w, cs)...)
}

// settleRound works out who came through each tie of the round just played,
// then either draws the next round or crowns the winner.
func settleRound(w *model.World, s *Schedule, cs *CompSeason, cal calendar, r *rng.R) []string {
	round := cs.current()
	if round == nil {
		return nil
	}
	var news []string
	through := make([]uint16, 0, len(round.Ties))
	for i := range round.Ties {
		t := &round.Ties[i]
		if !t.Decided() {
			decide(s, cs.Comp, round.Stage, t, r)
		}
		through = append(through, t.Winner)
		if n := tieNews(w, cs, *t); n != "" {
			news = append(news, n)
		}
	}

	if cs.Stage == StageFinal {
		cs.Stage = StageDone
		if len(round.Ties) == 1 {
			t := round.Ties[0]
			cs.Winner = t.Winner
			cs.RunnerUp = t.Home
			if cs.RunnerUp == cs.Winner {
				cs.RunnerUp = t.Away
			}
			payEuro(w, []uint16{cs.Winner}, EuroWinnerBonus(cs.Comp))
			news = append(news, fmt.Sprintf("%s win the %s, beating %s %s.",
				w.ClubName(cs.Winner), cs.Comp, w.ClubName(cs.RunnerUp), aggregate(t)))
		}
		return news
	}

	cs.Stage = stageFor(len(through))
	cs.Rounds = append(cs.Rounds, KnockoutRound{Stage: cs.Stage, Ties: drawOpenRound(through, r)})
	scheduleRound(s, cal, cs)
	return append(news, payRound(w, cs)...)
}

// decide reads a tie's legs back and settles it: the aggregate score, and a
// shootout when the two clubs cannot be separated by it.
//
// Away goals are deliberately not a tie-break. UEFA abolished the rule, and a
// shootout is both simpler to explain to a manager and easier to show.
func decide(s *Schedule, c model.Competition, st Stage, t *Tie, r *rng.R) {
	var last *Fixture
	for i := range s.Fixtures {
		f := &s.Fixtures[i]
		if f.Comp != c || f.Stage != st || !f.Played {
			continue
		}
		switch {
		case f.Home == t.Home && f.Away == t.Away:
			t.HomeGoals += f.HomeGoals
			t.AwayGoals += f.AwayGoals
		case f.Home == t.Away && f.Away == t.Home:
			t.HomeGoals += f.AwayGoals
			t.AwayGoals += f.HomeGoals
		default:
			continue
		}
		if last == nil || f.Date > last.Date {
			last = f
		}
	}

	switch {
	case t.HomeGoals > t.AwayGoals:
		t.Winner = t.Home
	case t.AwayGoals > t.HomeGoals:
		t.Winner = t.Away
	default:
		t.ShootoutHome, t.ShootoutAway = shootout(r)
		t.Winner = t.Home
		if t.ShootoutAway > t.ShootoutHome {
			t.Winner = t.Away
		}
		if last != nil {
			// The shootout belongs to the match that went to one, so a fixture
			// looked up months later still says how the tie was settled.
			if last.Home == t.Home {
				last.ShootoutHome, last.ShootoutAway = t.ShootoutHome, t.ShootoutAway
			} else {
				last.ShootoutHome, last.ShootoutAway = t.ShootoutAway, t.ShootoutHome
			}
		}
	}
}

// shootout settles a level tie from twelve yards.
//
// It draws no strength into it on purpose. A penalty is the one moment in
// football where the gap between a great side and an ordinary one all but
// disappears, and letting the favourite win the shootout as well as most of the
// football would take the only thing a small club has over them.
func shootout(r *rng.R) (uint8, uint8) {
	const conversion = 0.76
	var home, away uint8
	for kick := 0; kick < 5; kick++ {
		if r.Chance(conversion) {
			home++
		}
		if r.Chance(conversion) {
			away++
		}
	}
	for home == away {
		if r.Chance(conversion) {
			home++
		}
		if r.Chance(conversion) {
			away++
		}
	}
	return home, away
}

// stageFor names the round a given number of clubs plays.
func stageFor(clubs int) Stage {
	switch clubs {
	case 16:
		return StageRound16
	case 8:
		return StageQuarter
	case 4:
		return StageSemi
	case 2:
		return StageFinal
	}
	return StageDone
}

// drawFirstKnockout pairs every group winner with a runner-up from another
// group, and gives the winner the home leg second. Coming first in the group is
// otherwise worth nothing at all, and the second leg at home is what the real
// draw rewards it with.
func drawFirstKnockout(winners, runnersUp []uint16, r *rng.R) []Tie {
	seeds := append([]uint16(nil), winners...)
	rest := append([]uint16(nil), runnersUp...)
	r.Shuffle(len(seeds), func(i, j int) { seeds[i], seeds[j] = seeds[j], seeds[i] })
	r.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })

	ties := make([]Tie, 0, len(seeds))
	for i, seed := range seeds {
		if i >= len(rest) {
			break
		}
		// The unseeded club is at home first, so the group winner finishes the
		// tie in front of its own crowd.
		ties = append(ties, Tie{Home: rest[i], Away: seed})
	}
	return ties
}

// drawOpenRound is the free draw every round after the first: anyone can meet
// anyone, and the club drawn first is at home in the first leg.
func drawOpenRound(clubs []uint16, r *rng.R) []Tie {
	drum := append([]uint16(nil), clubs...)
	r.Shuffle(len(drum), func(i, j int) { drum[i], drum[j] = drum[j], drum[i] })

	ties := make([]Tie, 0, len(drum)/2)
	for i := 0; i+1 < len(drum); i += 2 {
		ties = append(ties, Tie{Home: drum[i], Away: drum[i+1]})
	}
	return ties
}

// tieNews reports the managed club's own knockout ties, and nobody else's: a
// full draw is sixteen lines the manager did not ask for.
func tieNews(w *model.World, cs *CompSeason, t Tie) string {
	h := w.HumanClubID
	if h == 0 || (t.Home != h && t.Away != h) {
		return ""
	}
	other := t.Home
	if other == h {
		other = t.Away
	}
	if t.Winner == h {
		return fmt.Sprintf("%s knock %s out of the %s, %s.",
			w.ClubName(h), w.ClubName(other), cs.Comp, aggregate(t))
	}
	return fmt.Sprintf("%s are out of the %s, beaten by %s %s.",
		w.ClubName(h), cs.Comp, w.ClubName(other), aggregate(t))
}

// aggregate renders a settled tie's score from the winner's point of view.
func aggregate(t Tie) string {
	hi, lo := t.HomeGoals, t.AwayGoals
	sh, sa := t.ShootoutHome, t.ShootoutAway
	if t.Winner == t.Away {
		hi, lo = lo, hi
		sh, sa = sa, sh
	}
	if sh != 0 || sa != 0 {
		return fmt.Sprintf("%d-%d on aggregate and %d-%d on penalties", hi, lo, sh, sa)
	}
	return fmt.Sprintf("%d-%d on aggregate", hi, lo)
}

// payRound hands every club still in the competition what reaching this round
// is worth, and tells the manager if they are one of them.
func payRound(w *model.World, cs *CompSeason) []string {
	round := cs.current()
	if round == nil {
		return nil
	}
	fee := EuroRoundBonus(cs.Comp, round.Stage)
	for _, t := range round.Ties {
		payEuro(w, []uint16{t.Home, t.Away}, fee)
	}
	for _, t := range round.Ties {
		if h := w.HumanClubID; h != 0 && (t.Home == h || t.Away == h) {
			other := t.Home
			if other == h {
				other = t.Away
			}
			return []string{fmt.Sprintf("%s draw %s in the %s %s.",
				w.ClubName(h), w.ClubName(other), cs.Comp, round.Stage)}
		}
	}
	return nil
}

func payEuro(w *model.World, clubs []uint16, amount int64) {
	if amount == 0 {
		return
	}
	for _, id := range clubs {
		if c := w.Club(id); c != nil {
			c.Balance += amount
		}
	}
}

func contains(ids []uint16, id uint16) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// ----- queries -----

// GroupTable computes the standings of one group, 1-based, of a competition.
func GroupTable(w *model.World, s *Schedule, c model.Competition, group int) []Row {
	cs := s.Euro.Comp(c)
	if cs == nil || group < 1 || group > len(cs.Groups) {
		return nil
	}
	return standings(w, s, cs.Groups[group-1], func(f *Fixture) bool {
		return f.Comp == c && f.Stage == StageGroup && int(f.Group) == group
	})
}

// GroupOf reports which group a club was drawn into, or 0.
func (cs *CompSeason) GroupOf(clubID uint16) int {
	if cs == nil {
		return 0
	}
	for gi, g := range cs.Groups {
		if contains(g, clubID) {
			return gi + 1
		}
	}
	return 0
}

// GroupLabel names a group the way the draw does: A, B, C.
func GroupLabel(group int) string {
	if group < 1 || group > 26 {
		return "?"
	}
	return string(rune('A' + group - 1))
}
