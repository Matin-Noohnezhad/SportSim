package assets_test

import (
	"testing"

	"sportsim/assets"
)

// TestEditionsAgreeWithTheirFiles checks the two places a season is written
// down. A file is named for the season it holds and its header says the same
// thing, and if those ever disagree the game would start a 2016 career with a
// 2026 calendar — dating every player and contract in it wrongly.
func TestEditionsAgreeWithTheirFiles(t *testing.T) {
	years := assets.Editions()
	if len(years) == 0 {
		t.Fatal("no player database is embedded")
	}

	for i, y := range years {
		if i > 0 && y <= years[i-1] {
			t.Errorf("editions are not in ascending order: %v", years)
		}

		d, err := assets.Load(y)
		if err != nil {
			t.Fatalf("loading %d: %v", y, err)
		}
		if d.Year != y {
			t.Errorf("%d: header says %d", y, d.Year)
		}
		if len(d.Players) == 0 || len(d.Clubs) == 0 {
			t.Errorf("%d: %d players in %d clubs", y, len(d.Players), len(d.Clubs))
		}
		// An edition may not license every division, but a division that
		// survives the import must have clubs in it: season.Generate would
		// otherwise build a fixture list for nobody.
		for _, l := range d.Leagues {
			if len(l.ClubIDs) == 0 {
				t.Errorf("%d: %s has no clubs", y, l.Name)
			}
		}
		t.Logf("%d: %d players, %d clubs, %d leagues", y, len(d.Players), len(d.Clubs), len(d.Leagues))
	}

	if !assets.Has(assets.Latest()) {
		t.Error("Latest names a season that is not embedded")
	}
	if _, err := assets.Load(1066); err == nil {
		t.Error("loading a season that does not exist should fail")
	}
}
