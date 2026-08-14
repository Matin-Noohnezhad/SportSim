package game_test

import (
	"testing"

	"sportsim/engine/model"
	"sportsim/game"
)

// TestTouchlineChangesAreForOneMatch checks that nothing decided in the dugout
// outlives the final whistle.
//
// The shape, the instructions and the substitutions are all responses to how one
// game is going, and the manager who goes three at the back to see out a lead has
// not asked to play that way in November. What the club takes into the next
// fixture is the selection made on the tactics screen, untouched.
func TestTouchlineChangesAreForOneMatch(t *testing.T) {
	g, err := game.New("Gaffer", 1, 4242)
	if err != nil {
		t.Fatal(err)
	}
	g.AutoSelect()

	var lm *game.LiveMatch
	for i := 0; i < 60 && lm == nil; i++ {
		if lm = g.KickOff(); lm == nil {
			g.AdvanceDay()
		}
	}
	if lm == nil {
		t.Fatal("the managed club never played")
	}

	c := g.Club()
	wantTactics := c.Tactics
	wantLineup, wantBench := c.Lineup, c.Bench

	// An hour gone and the manager tears the plan up.
	for i := 0; i < 60; i++ {
		lm.Step()
	}

	shape := model.Formation((int(wantTactics.Formation) + 1) % int(model.NumFormations))
	if ok, msg := lm.SetFormation(shape); !ok {
		t.Fatalf("switching shape from the touchline: %s", msg)
	}

	instructions := lm.Tactics()
	instructions.Mentality = 100
	if wantTactics.Mentality == 100 {
		instructions.Mentality = 0
	}
	if ok, msg := lm.SetTactics(instructions); !ok {
		t.Fatalf("passing on instructions: %s", msg)
	}

	off, on := outfield(lm.OnPitch()), outfield(lm.Bench())
	if off < 0 || on < 0 {
		t.Fatal("no outfield substitution available to make")
	}
	subsBefore := lm.SubsLeft()
	if ok, msg := lm.Substitute(off, on); !ok {
		t.Fatalf("making a substitution: %s", msg)
	}
	if lm.SubsLeft() != subsBefore-1 {
		t.Fatalf("%d substitutions left after making one, %d before", lm.SubsLeft(), subsBefore)
	}

	// The changes must be in force for the rest of this match...
	if got := lm.Tactics(); got.Formation != shape || got.Mentality != instructions.Mentality {
		t.Errorf("side playing %s at mentality %d, was told %s at %d",
			got.Formation, got.Mentality, shape, instructions.Mentality)
	}

	// ...and gone the moment it is over.
	g.AdvanceDay()

	if c.Tactics != wantTactics {
		t.Errorf("club tactics %+v after the match, %+v before it", c.Tactics, wantTactics)
	}
	if c.Lineup != wantLineup {
		t.Errorf("club lineup %v after the match, %v before it", c.Lineup, wantLineup)
	}
	if c.Bench != wantBench {
		t.Errorf("club bench %v after the match, %v before it", c.Bench, wantBench)
	}
}

// outfield returns the index of the first player in a touchline list who is not
// a goalkeeper, or -1. Swapping a keeper for an outfielder is refused, so a test
// substitution has to avoid both ends of it.
func outfield(ps []game.LivePlayer) int {
	for i, p := range ps {
		if !p.Keeper {
			return i
		}
	}
	return -1
}
