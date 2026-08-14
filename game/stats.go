package game

import (
	"sort"

	"sportsim/engine/model"
)

// PlayerStat is one player's season record, carrying every tally the charts
// rank on so that a frontend can show a leader's supporting figures alongside
// the one that put them there.
type PlayerStat struct {
	PlayerID    uint32
	Name        string
	Club        string
	Position    string
	Apps        int
	Minutes     int
	Goals       int
	Penalties   int // goals from the spot, counted within Goals
	Assists     int
	CleanSheets int
	Yellows     int
	Reds        int
	AvgRating   float64
}

// Scoreline is a notable result, named rather than formatted so a frontend can
// present it however it likes.
type Scoreline struct {
	Home, Away           string
	HomeGoals, AwayGoals int
	Date                 model.Date
}

// LeagueSummary is the division's season in aggregate: what the charts below
// it are measured against.
type LeagueSummary struct {
	Played, Total  int // matches played, of the full fixture list
	Goals          int
	GoalsPerMatch  float64
	HomeWins       int
	Draws          int
	AwayWins       int
	Yellows, Reds  int
	CleanSheets    int
	BiggestWin     *Scoreline // widest margin, nil before a match is played
	HighestScoring *Scoreline
}

// Stats is everything the statistics screen shows for one division: the season
// in aggregate, then a leaderboard for each thing worth leading.
type Stats struct {
	Summary     LeagueSummary
	Scorers     []PlayerStat
	Assists     []PlayerStat
	CleanSheets []PlayerStat
	Ratings     []PlayerStat
	Yellows     []PlayerStat
	Reds        []PlayerStat

	// RatingApps is the number of appearances a player needed to qualify for
	// the ratings chart, so a frontend can say why a name is missing from it.
	RatingApps int
}

// minRatingApps is the fewest appearances that will ever qualify a player for
// the average-rating chart. Half a season's matches is the real threshold; this
// only stops an early-season chart being topped by a single lucky afternoon.
const minRatingApps = 3

// Stats returns the season's statistics for a league, or for the whole game
// world when leagueID is zero. Each chart holds at most limit players.
func (g *Game) Stats(leagueID uint16, limit int) Stats {
	w := g.World

	var st Stats
	st.Summary = g.summarise(leagueID)

	// A player must have featured in half their side's matches to be judged on
	// their average rating, the way published rating charts qualify players.
	rounds := g.roundsPlayed(leagueID)
	st.RatingApps = rounds / 2
	if st.RatingApps < minRatingApps {
		st.RatingApps = minRatingApps
	}

	all := make([]PlayerStat, 0, 640)
	for i := range w.Players {
		p := &w.Players[i]
		if p.ClubID == 0 || p.Apps == 0 {
			continue
		}
		if leagueID != 0 {
			c := w.Club(p.ClubID)
			if c == nil || c.LeagueID != leagueID {
				continue
			}
		}
		all = append(all, PlayerStat{
			PlayerID:    p.ID,
			Name:        p.Name,
			Club:        w.ClubName(p.ClubID),
			Position:    p.Primary().String(),
			Apps:        int(p.Apps),
			Minutes:     int(p.MinutesSum),
			Goals:       int(p.Goals),
			Penalties:   int(p.Penalties),
			Assists:     int(p.Assists),
			CleanSheets: int(p.CleanSheets),
			Yellows:     int(p.Yellows),
			Reds:        int(p.Reds),
			AvgRating:   p.AvgRating(),
		})
	}

	st.Scorers = topBy(all, limit,
		func(s PlayerStat) bool { return s.Goals > 0 },
		func(a, b PlayerStat) bool {
			if a.Goals != b.Goals {
				return a.Goals > b.Goals
			}
			if a.Assists != b.Assists {
				return a.Assists > b.Assists
			}
			return a.Minutes < b.Minutes // fewer minutes for the same haul is the better return
		})

	st.Assists = topBy(all, limit,
		func(s PlayerStat) bool { return s.Assists > 0 },
		func(a, b PlayerStat) bool {
			if a.Assists != b.Assists {
				return a.Assists > b.Assists
			}
			if a.Goals != b.Goals {
				return a.Goals > b.Goals
			}
			return a.Minutes < b.Minutes
		})

	st.CleanSheets = topBy(all, limit,
		func(s PlayerStat) bool { return s.CleanSheets > 0 },
		func(a, b PlayerStat) bool {
			if a.CleanSheets != b.CleanSheets {
				return a.CleanSheets > b.CleanSheets
			}
			return a.Apps < b.Apps // the same tally in fewer games is the better record
		})

	st.Ratings = topBy(all, limit,
		func(s PlayerStat) bool { return s.Apps >= st.RatingApps },
		func(a, b PlayerStat) bool {
			if a.AvgRating != b.AvgRating {
				return a.AvgRating > b.AvgRating
			}
			return a.Apps > b.Apps
		})

	st.Yellows = topBy(all, limit,
		func(s PlayerStat) bool { return s.Yellows > 0 },
		func(a, b PlayerStat) bool {
			if a.Yellows != b.Yellows {
				return a.Yellows > b.Yellows
			}
			return a.Reds > b.Reds
		})

	st.Reds = topBy(all, limit,
		func(s PlayerStat) bool { return s.Reds > 0 },
		func(a, b PlayerStat) bool {
			if a.Reds != b.Reds {
				return a.Reds > b.Reds
			}
			return a.Yellows > b.Yellows
		})

	return st
}

// topBy is the shape every chart shares: keep the players a chart is about,
// order them by what it ranks on, and cut the list to length.
func topBy(all []PlayerStat, limit int, keep func(PlayerStat) bool, less func(a, b PlayerStat) bool) []PlayerStat {
	out := make([]PlayerStat, 0, limit*2)
	for _, s := range all {
		if keep(s) {
			out = append(out, s)
		}
	}
	// Stable, over a slice built in player-ID order, so equal records always
	// come out in the same order rather than shuffling between renders.
	sort.SliceStable(out, func(a, b int) bool { return less(out[a], out[b]) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// summarise aggregates a league's played fixtures. Cards and clean sheets come
// from the players' own tallies, since a fixture only records its goals.
func (g *Game) summarise(leagueID uint16) LeagueSummary {
	w := g.World
	var s LeagueSummary

	for i := range g.Sched.Fixtures {
		f := &g.Sched.Fixtures[i]
		if leagueID != 0 && f.LeagueID != leagueID {
			continue
		}
		s.Total++
		if !f.Played {
			continue
		}
		s.Played++
		hg, ag := int(f.HomeGoals), int(f.AwayGoals)
		s.Goals += hg + ag

		switch {
		case hg > ag:
			s.HomeWins++
		case hg < ag:
			s.AwayWins++
		default:
			s.Draws++
		}

		line := Scoreline{
			Home: w.ClubName(f.Home), Away: w.ClubName(f.Away),
			HomeGoals: hg, AwayGoals: ag, Date: f.Date,
		}
		if s.BiggestWin == nil || abs(hg-ag) > abs(s.BiggestWin.HomeGoals-s.BiggestWin.AwayGoals) {
			widest := line
			s.BiggestWin = &widest
		}
		if s.HighestScoring == nil || hg+ag > s.HighestScoring.HomeGoals+s.HighestScoring.AwayGoals {
			highest := line
			s.HighestScoring = &highest
		}
	}
	if s.Played > 0 {
		s.GoalsPerMatch = float64(s.Goals) / float64(s.Played)
	}

	for i := range w.Players {
		p := &w.Players[i]
		if p.ClubID == 0 {
			continue
		}
		if leagueID != 0 {
			c := w.Club(p.ClubID)
			if c == nil || c.LeagueID != leagueID {
				continue
			}
		}
		s.Yellows += int(p.Yellows)
		s.Reds += int(p.Reds)
		s.CleanSheets += int(p.CleanSheets)
	}
	return s
}

// roundsPlayed is the most matches any club in the league has played, which is
// how far into the season the division is.
func (g *Game) roundsPlayed(leagueID uint16) int {
	played := map[uint16]int{}
	most := 0
	for i := range g.Sched.Fixtures {
		f := &g.Sched.Fixtures[i]
		if !f.Played || (leagueID != 0 && f.LeagueID != leagueID) {
			continue
		}
		played[f.Home]++
		played[f.Away]++
		if n := played[f.Home]; n > most {
			most = n
		}
		if n := played[f.Away]; n > most {
			most = n
		}
	}
	return most
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
