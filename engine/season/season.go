// Package season owns the competition calendar: fixture lists, league tables
// and the promotion and relegation that closes out a campaign.
//
// It reaches into engine/match for the box score and player lines a played
// fixture keeps, so that a match report has one definition rather than a copy
// here that would drift out of step with the engine that fills it in.
package season

import (
	"sort"

	"sportsim/engine/match"
	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// Goal records a single goal for a played fixture, so any past match can be
// shown with its scorers without keeping the full event stream in memory.
type Goal struct {
	Minute  uint8
	Away    bool // scored by the away side
	Penalty bool
	Scorer  uint32
	Assist  uint32
}

// Dismissal records a sending-off. Goals and dismissals are the two incidents
// that decide a match, which is why they are the two a fixture keeps: the rest
// of the commentary is colour, and storing every event of 4,676 fixtures to
// replay it costs far more than reading it back is worth.
type Dismissal struct {
	Minute uint8
	Away   bool // shown to a player of the away side
	Player uint32
	Second bool // a second booking rather than a straight red
}

// Fixture is one scheduled match.
//
// A played one keeps enough of its match report to be looked over again months
// later: the box score for both sides and the incidents that decided it. Player
// ratings are the exception — they are kept only for matches the managed club
// played, since those are the only ratings any screen shows and a whole
// division's would be a squad's worth of lines per fixture.
type Fixture struct {
	LeagueID uint16
	Round    uint16
	Date     model.Date
	Home     uint16
	Away     uint16

	// ---- continental fixtures ----
	//
	// A league match leaves all of these zero, which costs a save file nothing:
	// gob writes no bytes for a zero field. Comp is what tells the two apart —
	// a continental fixture belongs to no division, so its LeagueID is zero and
	// it can never reach a league table.
	Comp  model.Competition
	Stage Stage
	Group uint8 // 1-based group in the group stage, 0 in a knockout round
	Leg   uint8 // 1 or 2 in a two-legged tie, 0 otherwise

	Played     bool
	HomeGoals  uint8
	AwayGoals  uint8
	Attendance uint32
	Goals      []Goal
	Reds       []Dismissal
	Stats      [2]match.TeamStats
	Lines      []match.PlayerLine

	// A shootout is kept on the leg that went to one, so a fixture that decided
	// a tie can be read back knowing how it was actually settled.
	ShootoutHome uint8
	ShootoutAway uint8
}

// Continental reports whether the fixture is a European tie rather than a
// league match.
func (f *Fixture) Continental() bool { return f.Comp != model.NoComp }

// Schedule is every fixture in the game world for one season, domestic and
// continental alike.
//
// The continental knockout rounds are not in it at kickoff: a bracket is drawn
// only once the round before it has been played, so Euro carries how far each
// tournament has got and EuroAdvance appends the next round's fixtures as they
// are drawn.
type Schedule struct {
	Year     int
	Fixtures []Fixture
	Euro     *Euro
}

// Row is one club's line in a league table.
type Row struct {
	ClubID                 uint16
	Played                 int
	Won, Drawn, Lost       int
	GoalsFor, GoalsAgainst int
	Points                 int
	// Form holds the club's most recent results, oldest first: 'W', 'D', 'L'.
	Form []byte
}

// GoalDiff returns goals scored minus goals conceded.
func (r Row) GoalDiff() int { return r.GoalsFor - r.GoalsAgainst }

// Generate builds a double round-robin fixture list for every league and
// spreads it across the season, from the opening weekend in August to the final
// day in late May.
func Generate(w *model.World, year int, r *rng.R) *Schedule {
	s := &Schedule{Year: year}
	start := model.NewDate(year, 8, 8)
	end := model.NewDate(year+1, 5, 24)

	for li := range w.Leagues {
		l := &w.Leagues[li]
		clubs := append([]uint16(nil), l.ClubIDs...)
		if len(clubs) < 2 {
			continue
		}
		// Shuffling means a club does not meet opponents in the same order
		// every season.
		r.Shuffle(len(clubs), func(i, j int) { clubs[i], clubs[j] = clubs[j], clubs[i] })

		rounds := roundRobin(clubs)
		dates := matchdays(start, end, len(rounds)*2)

		for half := 0; half < 2; half++ {
			for ri := range rounds {
				idx := half*len(rounds) + ri
				// The second half replays every round with the venues reversed,
				// shifted on by one round. Replaying them in the same order would
				// leave most clubs with the same venue either side of the halfway
				// point, and that break next to the one break each club already
				// carries is what produces a run of three; the shift moves the
				// two apart. See TestVenueAlternation.
				pairs := rounds[ri]
				if half == 1 {
					pairs = rounds[(ri+1)%len(rounds)]
				}
				for _, p := range pairs {
					home, away := p[0], p[1]
					if half == 1 {
						home, away = away, home
					}
					s.Fixtures = append(s.Fixtures, Fixture{
						LeagueID: l.ID,
						Round:    uint16(idx + 1),
						Date:     dates[idx],
						Home:     home,
						Away:     away,
					})
				}
			}
		}
	}

	sort.SliceStable(s.Fixtures, func(a, b int) bool {
		return s.Fixtures[a].Date < s.Fixtures[b].Date
	})

	// Europe is drawn last, so that a continental night can be moved off a day a
	// club is already playing on. Only the group stage is scheduled now — the
	// knockout draws are made as the rounds are reached, in EuroAdvance.
	drawEurope(w, s, r)
	return s
}

// roundRobin pairs every club with every other exactly once, using the circle
// method: the last club stands still while the rest rotate around it, so in
// round rd the stationary club meets list[rd] and the others pair off either
// side of it.
//
// Venues come from the canonical assignment rather than from anything about the
// clubs themselves, because it is the only one that keeps every club close to
// strict home-away alternation. A club's opponent distance from the pivot,
// (club - rd) mod m, falls by one every round, so alternating on the parity of
// that distance alternates the venue too. The parity can only fail to alternate
// on the round in which the club meets the stationary one, which gives each club
// exactly one back-to-back pair per single round-robin and none at all in an
// odd-sized league, where that round is its bye.
func roundRobin(clubs []uint16) [][][2]uint16 {
	list := append([]uint16(nil), clubs...)
	// A bye (club 0) lets the circle method handle odd-sized leagues. Standing
	// it still means the bye is what absorbs each club's one venue repeat.
	if len(list)%2 == 1 {
		list = append(list, 0)
	}
	n := len(list)
	m := n - 1 // clubs that rotate; list[m] stands still

	rounds := make([][][2]uint16, 0, m)
	for rd := 0; rd < m; rd++ {
		pairs := make([][2]uint16, 0, n/2)
		add := func(home, away uint16) {
			if home == 0 || away == 0 {
				return // the club drawn against the bye does not play
			}
			pairs = append(pairs, [2]uint16{home, away})
		}

		// The stationary club has no distance to alternate on, so it simply
		// swaps venue every round and never repeats one.
		if rd%2 == 0 {
			add(list[m], list[rd])
		} else {
			add(list[rd], list[m])
		}
		for j := 1; j <= (m-1)/2; j++ {
			near, far := list[(rd+j)%m], list[(rd-j+m)%m]
			// m is odd, so exactly one of j and m-j is odd and the two clubs
			// always agree on which of them is at home.
			if j%2 == 1 {
				add(near, far)
			} else {
				add(far, near)
			}
		}
		rounds = append(rounds, pairs)
	}
	return rounds
}

// matchdays spreads n rounds across the season window, preferring Saturdays and
// falling back to midweek when a league has more rounds than there are
// weekends, which is how real congested calendars behave.
func matchdays(start, end model.Date, n int) []model.Date {
	out := make([]model.Date, n)
	if n == 0 {
		return out
	}
	window := int(end - start)
	prev := model.Date(0)

	for i := 0; i < n; i++ {
		d := start + model.Date(i*window/n)
		// Nudge onto a Saturday when one is close by.
		switch wd := d.Weekday(); {
		case wd == 6: // already Saturday
		case wd == 0: // Sunday, keep it
		default:
			shift := (6 - int(wd)) % 7
			if shift <= 3 {
				d += model.Date(shift)
			} else {
				// Too far from the weekend: play midweek instead.
				d += model.Date((3 - int(wd) + 7) % 7)
			}
		}
		// Never let two rounds land on the same day or run backwards.
		if i > 0 && d <= prev+2 {
			d = prev + 3
		}
		out[i] = d
		prev = d
	}
	return out
}

// Table computes the current standings for a league.
func Table(w *model.World, s *Schedule, leagueID uint16) []Row {
	l := w.League(leagueID)
	if l == nil {
		return nil
	}
	return standings(w, s, l.ClubIDs, func(f *Fixture) bool {
		return !f.Continental() && f.LeagueID == leagueID
	})
}

// standings is the one definition of a table, whether it is a division's or a
// European group's: the same three points for a win, the same tie-breaks and
// the same run of recent form. A second copy of this for the group stage would
// be a second set of rules to keep in step.
func standings(w *model.World, s *Schedule, clubs []uint16, include func(*Fixture) bool) []Row {
	idx := make(map[uint16]int, len(clubs))
	rows := make([]Row, 0, len(clubs))
	for _, c := range clubs {
		idx[c] = len(rows)
		rows = append(rows, Row{ClubID: c, Form: make([]byte, 0, 6)})
	}

	for i := range s.Fixtures {
		f := &s.Fixtures[i]
		if !f.Played || !include(f) {
			continue
		}
		hi, ok1 := idx[f.Home]
		ai, ok2 := idx[f.Away]
		if !ok1 || !ok2 {
			continue // a club that has since moved divisions
		}
		h, a := &rows[hi], &rows[ai]
		hg, ag := int(f.HomeGoals), int(f.AwayGoals)

		h.Played++
		a.Played++
		h.GoalsFor += hg
		h.GoalsAgainst += ag
		a.GoalsFor += ag
		a.GoalsAgainst += hg

		switch {
		case hg > ag:
			h.Won++
			a.Lost++
			h.Points += 3
			h.Form = append(h.Form, 'W')
			a.Form = append(a.Form, 'L')
		case hg < ag:
			a.Won++
			h.Lost++
			a.Points += 3
			h.Form = append(h.Form, 'L')
			a.Form = append(a.Form, 'W')
		default:
			h.Drawn++
			a.Drawn++
			h.Points++
			a.Points++
			h.Form = append(h.Form, 'D')
			a.Form = append(a.Form, 'D')
		}
	}

	for i := range rows {
		if n := len(rows[i].Form); n > 6 {
			rows[i].Form = rows[i].Form[n-6:]
		}
	}

	sort.SliceStable(rows, func(a, b int) bool {
		x, y := rows[a], rows[b]
		if x.Points != y.Points {
			return x.Points > y.Points
		}
		if x.GoalDiff() != y.GoalDiff() {
			return x.GoalDiff() > y.GoalDiff()
		}
		if x.GoalsFor != y.GoalsFor {
			return x.GoalsFor > y.GoalsFor
		}
		return w.ClubName(x.ClubID) < w.ClubName(y.ClubID)
	})
	return rows
}

// Position returns a club's current place in its league, 1-based, or 0.
func Position(rows []Row, clubID uint16) int {
	for i, r := range rows {
		if r.ClubID == clubID {
			return i + 1
		}
	}
	return 0
}

// On returns the indices of fixtures scheduled for a given day.
func (s *Schedule) On(d model.Date) []int {
	var out []int
	for i := range s.Fixtures {
		if s.Fixtures[i].Date == d {
			out = append(out, i)
		}
	}
	return out
}

// NextFor returns the club's next unplayed fixture, or nil.
func (s *Schedule) NextFor(clubID uint16) *Fixture {
	var best *Fixture
	for i := range s.Fixtures {
		f := &s.Fixtures[i]
		if f.Played || (f.Home != clubID && f.Away != clubID) {
			continue
		}
		if best == nil || f.Date < best.Date {
			best = f
		}
	}
	return best
}

// RecentFor returns a club's most recent played fixtures, newest first.
func (s *Schedule) RecentFor(clubID uint16, limit int) []*Fixture {
	var out []*Fixture
	for i := range s.Fixtures {
		f := &s.Fixtures[i]
		if f.Played && (f.Home == clubID || f.Away == clubID) {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Date > out[b].Date })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Complete reports whether every fixture has been played.
func (s *Schedule) Complete() bool {
	for i := range s.Fixtures {
		if !s.Fixtures[i].Played {
			return false
		}
	}
	return true
}

// LastDate returns the date of the final fixture of the season.
func (s *Schedule) LastDate() model.Date {
	var last model.Date
	for i := range s.Fixtures {
		if s.Fixtures[i].Date > last {
			last = s.Fixtures[i].Date
		}
	}
	return last
}

// Remaining counts unplayed fixtures.
func (s *Schedule) Remaining() int {
	n := 0
	for i := range s.Fixtures {
		if !s.Fixtures[i].Played {
			n++
		}
	}
	return n
}
