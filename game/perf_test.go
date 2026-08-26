package game_test

import (
	"runtime"
	"testing"
	"time"

	"sportsim/assets"
	"sportsim/game"
)

// TestResourceUse records the figures that matter for a game meant to be small
// and quick: how long a career takes to start, how much memory a live world
// occupies, and how fast a full European season simulates.
func TestResourceUse(t *testing.T) {
	var m0, m1 runtime.MemStats

	start := time.Now()
	g, err := game.New("Perf", 1, game.LatestEdition(), 1)
	if err != nil {
		t.Fatal(err)
	}
	startup := time.Since(start)

	runtime.GC()
	runtime.ReadMemStats(&m0)

	t0 := time.Now()
	matches := 0
	for g.Sched.Remaining() > 0 {
		matches += len(g.AdvanceDay().Results)
	}
	seasonTime := time.Since(t0)

	runtime.ReadMemStats(&m1)
	runtime.KeepAlive(g)

	t.Logf("embedded databases  %d KB across %d season(s)", assets.Size()/1024, len(assets.Editions()))
	t.Logf("startup (load+fixtures) %v", startup.Round(time.Millisecond))
	t.Logf("live heap           %.1f MB", float64(m1.HeapAlloc)/(1<<20))
	t.Logf("full season         %d matches in %v (%.0f matches/sec)",
		matches, seasonTime.Round(time.Millisecond), float64(matches)/seasonTime.Seconds())
	t.Logf("players / clubs     %d / %d", len(g.World.Players), len(g.World.Clubs))
}
