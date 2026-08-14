package season

import (
	"fmt"
	"testing"

	"sportsim/engine/model"
	"sportsim/engine/rng"
)

// leagueOf builds a world holding a single league of n clubs, which is all the
// fixture list depends on.
func leagueOf(n int) *model.World {
	w := &model.World{}
	w.Nations = []model.Nation{{ID: 1, Name: "Testland", Code: "TST"}}
	ids := make([]uint16, 0, n)
	for i := 1; i <= n; i++ {
		w.Clubs = append(w.Clubs, model.Club{ID: uint16(i), Name: fmt.Sprintf("Club %d", i), LeagueID: 1, NationID: 1})
		ids = append(ids, uint16(i))
	}
	w.Leagues = []model.League{{ID: 1, Name: "Test League", NationID: 1, Tier: 1, ClubIDs: ids}}
	return w
}

// venues returns each club's season in order: 'H' for a home game, 'A' away.
func venues(s *Schedule) map[uint16][]byte {
	byClub := make(map[uint16][]byte)
	for i := range s.Fixtures {
		f := &s.Fixtures[i]
		byClub[f.Home] = append(byClub[f.Home], 'H')
		byClub[f.Away] = append(byClub[f.Away], 'A')
	}
	return byClub
}

// TestVenueAlternation is the guard on the fixture list's most visible property:
// clubs alternate home and away, and although the alternation has to break
// somewhere, it may never break twice running. Three league games in a row at
// the same ground is the bug this pins down.
func TestVenueAlternation(t *testing.T) {
	for n := 4; n <= 26; n++ {
		s := Generate(leagueOf(n), 2025, rng.New(uint64(n)))
		for club, seq := range venues(s) {
			run := 1
			for i := 1; i < len(seq); i++ {
				if seq[i] != seq[i-1] {
					run = 1
					continue
				}
				if run++; run > 2 {
					t.Errorf("%d-club league: club %d has %d games in a row at the same venue: %s", n, club, run, seq)
					break
				}
			}
		}
	}
}

// TestRoundRobinComplete checks the fixture list is still a proper double
// round-robin after the venue work: every pair meets twice, once at each ground.
func TestRoundRobinComplete(t *testing.T) {
	for _, n := range []int{17, 18, 20} {
		s := Generate(leagueOf(n), 2025, rng.New(uint64(n)))
		met := make(map[[2]uint16]int)
		for i := range s.Fixtures {
			f := &s.Fixtures[i]
			met[[2]uint16{f.Home, f.Away}]++
		}
		for a := uint16(1); a <= uint16(n); a++ {
			for b := a + 1; b <= uint16(n); b++ {
				if got := met[[2]uint16{a, b}]; got != 1 {
					t.Errorf("%d-club league: club %d hosted club %d %d times, want 1", n, a, b, got)
				}
				if got := met[[2]uint16{b, a}]; got != 1 {
					t.Errorf("%d-club league: club %d hosted club %d %d times, want 1", n, b, a, got)
				}
			}
		}
		for club, seq := range venues(s) {
			if len(seq) != 2*(n-1) {
				t.Errorf("%d-club league: club %d played %d games, want %d", n, club, len(seq), 2*(n-1))
			}
		}
	}
}

// TestFixturesNeverBackToBack checks the second half's one-round shift does not
// pit the same two clubs against each other in consecutive rounds.
func TestFixturesNeverBackToBack(t *testing.T) {
	for _, n := range []int{17, 18, 20} {
		s := Generate(leagueOf(n), 2025, rng.New(uint64(n)))
		last := make(map[uint16]uint16) // club -> previous opponent
		byRound := make(map[uint16][]*Fixture)
		maxRound := uint16(0)
		for i := range s.Fixtures {
			f := &s.Fixtures[i]
			byRound[f.Round] = append(byRound[f.Round], f)
			if f.Round > maxRound {
				maxRound = f.Round
			}
		}
		for rd := uint16(1); rd <= maxRound; rd++ {
			now := make(map[uint16]uint16)
			for _, f := range byRound[rd] {
				if last[f.Home] == f.Away {
					t.Errorf("%d-club league: clubs %d and %d meet in rounds %d and %d", n, f.Home, f.Away, rd-1, rd)
				}
				now[f.Home], now[f.Away] = f.Away, f.Home
			}
			last = now
		}
	}
}
