// Package season owns the competition calendar: fixture lists, league tables
// and the promotion and relegation that closes out a campaign.
package season

import (
	"sort"

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

// Fixture is one scheduled match.
type Fixture struct {
	LeagueID uint16
	Round    uint16
	Date     model.Date
	Home     uint16
	Away     uint16

	Played     bool
	HomeGoals  uint8
	AwayGoals  uint8
	Attendance uint32
	Goals      []Goal
}

// Schedule is every fixture in the game world for one season.
type Schedule struct {
	Year     int
	Fixtures []Fixture
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
			for ri, pairs := range rounds {
				idx := half*len(rounds) + ri
				for _, p := range pairs {
					home, away := p[0], p[1]
					// Reverse the fixture in the second half of the season.
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
	return s
}

// roundRobin pairs every club with every other exactly once, using the circle
// method. Home and away are alternated between rounds so no club is given a
// lopsided run of home games.
func roundRobin(clubs []uint16) [][][2]uint16 {
	n := len(clubs)
	list := append([]uint16(nil), clubs...)
	// A bye (club 0) lets the circle method handle odd-sized leagues.
	if n%2 == 1 {
		list = append(list, 0)
		n++
	}

	rounds := make([][][2]uint16, 0, n-1)
	for rd := 0; rd < n-1; rd++ {
		pairs := make([][2]uint16, 0, n/2)
		for i := 0; i < n/2; i++ {
			a, b := list[i], list[n-1-i]
			if a == 0 || b == 0 {
				continue // the club drawn against the bye does not play
			}
			// Alternating avoids one club always being at home in early rounds.
			if (rd+i)%2 == 0 {
				pairs = append(pairs, [2]uint16{a, b})
			} else {
				pairs = append(pairs, [2]uint16{b, a})
			}
		}
		rounds = append(rounds, pairs)

		// Rotate all but the first entry.
		last := list[n-1]
		copy(list[2:], list[1:n-1])
		list[1] = last
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
	idx := make(map[uint16]int, len(l.ClubIDs))
	rows := make([]Row, 0, len(l.ClubIDs))
	for _, c := range l.ClubIDs {
		idx[c] = len(rows)
		rows = append(rows, Row{ClubID: c, Form: make([]byte, 0, 6)})
	}

	for i := range s.Fixtures {
		f := &s.Fixtures[i]
		if f.LeagueID != leagueID || !f.Played {
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
