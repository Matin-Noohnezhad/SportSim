package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sportsim/engine/model"
)

// header builds a CSV header row using the column names of a given era. Older
// editions of the dataset spell several columns differently, and the importer
// is expected to read both without a second code path.
func csvHeader(old bool) []string {
	cols := []string{
		"player_id", "short_name", "long_name", "player_positions", "overall",
		"potential", "value_eur", "wage_eur", "age", "dob", "height_cm", "weight_kg",
		"club_team_id", "club_name", "league_id", "league_name",
		"club_jersey_number", "club_contract_valid_until_year", "club_loaned_from",
		"nationality_name", "preferred_foot", "weak_foot", "skill_moves",
		"international_reputation", "work_rate",
	}
	for i := 0; i < model.NumAttr; i++ {
		cols = append(cols, attrCols[i])
	}
	if !old {
		return cols
	}
	rename := map[string]string{
		"player_id":                      "sofifa_id",
		"club_team_id":                   "team_id",
		"club_jersey_number":             "team_jersey_number",
		"club_contract_valid_until_year": "club_contract_valid_until",
		"nationality_name":               "nationality",
		"defending_marking_awareness":    "defending_marking",
	}
	out := make([]string, len(cols))
	for i, c := range cols {
		if r, ok := rename[c]; ok {
			out[i] = r
			continue
		}
		out[i] = c
	}
	// The oldest editions carry no numeric league id at all, only a name.
	for i, c := range out {
		if c == "league_id" {
			out[i] = "unused_column"
		}
	}
	return out
}

// writeCSV builds a small but complete dataset: two divisions of four clubs
// with a squad each, written in either the current or the older spelling.
func writeCSV(t *testing.T, old bool, leagues []*leagueSpec, leagueName func(*leagueSpec) string) string {
	t.Helper()
	head := csvHeader(old)
	idx := make(map[string]int, len(head))
	for i, h := range head {
		idx[h] = i
	}
	set := func(row []string, canonical, v string) {
		// Look the column up under whichever name this era uses.
		for _, name := range append([]string{canonical}, aliases[canonical]...) {
			if i, ok := idx[name]; ok {
				row[i] = v
				return
			}
		}
	}

	path := filepath.Join(t.TempDir(), "players.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := csv.NewWriter(f)
	if err := out.Write(head); err != nil {
		t.Fatal(err)
	}

	pid := 1
	for _, spec := range leagues {
		for club := 1; club <= 4; club++ {
			clubID := spec.SrcID*100 + club
			for n := 0; n < 18; n++ {
				row := make([]string, len(head))
				for i := range row {
					row[i] = "60"
				}
				set(row, "player_id", fmt.Sprint(pid))
				set(row, "short_name", fmt.Sprintf("Player %d", pid))
				set(row, "long_name", fmt.Sprintf("Player Number %d", pid))
				set(row, "player_positions", "CM, CB")
				set(row, "overall", "70")
				set(row, "potential", "78")
				set(row, "value_eur", "5000000")
				set(row, "wage_eur", "20000")
				set(row, "age", "25")
				set(row, "dob", "1998-03-04")
				set(row, "height_cm", "180")
				set(row, "weight_kg", "75")
				set(row, "club_team_id", fmt.Sprint(clubID))
				set(row, "club_name", fmt.Sprintf("%s Club %d", spec.Code, club))
				set(row, "league_id", fmt.Sprint(spec.SrcID))
				set(row, "league_name", leagueName(spec))
				set(row, "club_jersey_number", fmt.Sprint(n+1))
				set(row, "club_contract_valid_until_year", "2018")
				set(row, "club_loaned_from", "")
				set(row, "nationality_name", spec.Country)
				set(row, "preferred_foot", "Right")
				set(row, "weak_foot", "3")
				set(row, "skill_moves", "3")
				set(row, "international_reputation", "1")
				set(row, "work_rate", "Medium/Medium")
				if err := out.Write(row); err != nil {
					t.Fatal(err)
				}
				pid++
			}
		}
	}
	out.Flush()
	if err := out.Error(); err != nil {
		t.Fatal(err)
	}
	return path
}

func specFor(t *testing.T, name string) *leagueSpec {
	t.Helper()
	for i := range wanted {
		if wanted[i].Name == name {
			return &wanted[i]
		}
	}
	t.Fatalf("no spec named %q", name)
	return nil
}

// TestOldColumnNamesStillImport is the point of the alias table: the sofifa
// scrape has renamed columns repeatedly since FIFA 15, and the data underneath
// is the same, so a rename must not need a second importer.
func TestOldColumnNamesStillImport(t *testing.T) {
	pl := specFor(t, "Premier League")
	laLiga := specFor(t, "La Liga")
	byName := func(s *leagueSpec) string { return s.SrcNames[0] }

	modern := writeCSV(t, false, []*leagueSpec{pl, laLiga}, byName)
	ancient := writeCSV(t, true, []*leagueSpec{pl, laLiga}, byName)

	a, err := build(modern, 2016)
	if err != nil {
		t.Fatalf("current spelling: %v", err)
	}
	b, err := build(ancient, 2016)
	if err != nil {
		t.Fatalf("older spelling: %v", err)
	}

	if len(a.Players) != len(b.Players) || len(a.Clubs) != len(b.Clubs) {
		t.Fatalf("older spelling produced %d players in %d clubs, current gave %d in %d",
			len(b.Players), len(b.Clubs), len(a.Players), len(a.Clubs))
	}
	if len(b.Players) == 0 {
		t.Fatal("no players imported at all")
	}
	// The old file has no league_id column, so this also proves the fall-back
	// to matching a division by its name.
	if len(b.Leagues) != 2 {
		t.Errorf("divisions matched by name: got %d, want 2", len(b.Leagues))
	}
	for i := range a.Players {
		if a.Players[i].Name != b.Players[i].Name || a.Players[i].Attr != b.Players[i].Attr {
			t.Fatalf("player %d differs between spellings", i)
		}
	}
}

// TestYearSetsTheEra checks the three things the -year flag is for. A contract
// that has already run out and a player aged against the wrong year are both
// silent: the import succeeds and the career is simply wrong.
func TestYearSetsTheEra(t *testing.T) {
	pl := specFor(t, "Premier League")
	byName := func(s *leagueSpec) string { return s.SrcNames[0] }
	path := writeCSV(t, false, []*leagueSpec{pl}, byName)

	d, err := build(path, 2016)
	if err != nil {
		t.Fatal(err)
	}
	if d.Year != 2016 {
		t.Errorf("pack year = %d, want 2016", d.Year)
	}
	for i := range d.Players {
		if int(d.Players[i].ContractUntil) < 2016 {
			t.Fatalf("player %d is already out of contract in 2016", i)
		}
	}

	// The same file imported as a different era must be paid differently: the
	// squads are era-correct but the broadcast deal in `wanted` is 2026's.
	old, err := build(path, 2016)
	if err != nil {
		t.Fatal(err)
	}
	now, err := build(path, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if old.Leagues[0].PrizeMoney >= now.Leagues[0].PrizeMoney {
		t.Errorf("2016 broadcast pot %d is not smaller than 2026's %d",
			old.Leagues[0].PrizeMoney, now.Leagues[0].PrizeMoney)
	}
}

// TestUnlicensedDivisionsAreDropped guards invariant 4. Every division in
// `wanted` is created before a player is read, so an edition that ships no
// Serie B would leave an empty league behind — and season.Generate would build
// a fixture list for nobody.
func TestUnlicensedDivisionsAreDropped(t *testing.T) {
	pl := specFor(t, "Premier League")
	turkey := specFor(t, "Süper Lig")
	byName := func(s *leagueSpec) string { return s.SrcNames[0] }
	path := writeCSV(t, false, []*leagueSpec{pl, turkey}, byName)

	d, err := build(path, 2016)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Leagues) != 2 {
		t.Fatalf("got %d divisions, want the 2 the CSV actually has", len(d.Leagues))
	}
	for i, l := range d.Leagues {
		if l.ID != uint16(i+1) {
			t.Errorf("division %d has id %d; ids must stay dense", i, l.ID)
		}
		if len(l.ClubIDs) == 0 {
			t.Errorf("%s survived with no clubs", l.Name)
		}
	}
	// Every club must still point at a division that exists after renumbering.
	for i := range d.Clubs {
		id := d.Clubs[i].LeagueID
		if id == 0 || int(id) > len(d.Leagues) {
			t.Fatalf("%s points at division %d, which is not in the pack", d.Clubs[i].Name, id)
		}
	}
}

// TestUnknownDivisionNameIsReported checks the failure a new edition is most
// likely to hit: a division this importer wants, spelled a way it has never
// seen. It must fail loudly rather than quietly importing nothing.
func TestUnknownDivisionNameIsReported(t *testing.T) {
	pl := specFor(t, "Premier League")
	path := writeCSV(t, true, []*leagueSpec{pl}, func(*leagueSpec) string {
		return "Barclays Premiership Of Association Football"
	})

	if _, err := build(path, 2016); err == nil {
		t.Fatal("an unrecognised division name should fail the import, not empty it")
	} else if !strings.Contains(err.Error(), "-inspect") {
		t.Errorf("error should point at -inspect, got: %v", err)
	}
}
