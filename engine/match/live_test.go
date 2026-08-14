package match_test

import (
	"testing"

	"sportsim/engine/match"
	"sportsim/engine/model"
	"sportsim/engine/rng"
)

func sameResult(t *testing.T, what string, a, b *match.Result) {
	t.Helper()
	if a.HomeGoals != b.HomeGoals || a.AwayGoals != b.AwayGoals {
		t.Fatalf("%s: %d-%d vs %d-%d", what, a.HomeGoals, a.AwayGoals, b.HomeGoals, b.AwayGoals)
	}
	if len(a.Events) != len(b.Events) {
		t.Fatalf("%s: %d events vs %d", what, len(a.Events), len(b.Events))
	}
	for i := range a.Events {
		if a.Events[i] != b.Events[i] {
			t.Fatalf("%s: event %d differs: %+v vs %+v", what, i, a.Events[i], b.Events[i])
		}
	}
	if a.Stats != b.Stats {
		t.Fatalf("%s: box scores differ: %+v vs %+v", what, a.Stats, b.Stats)
	}
}

// TestLiveEqualsSim is the guard on the two presentations staying one engine.
// Watching a match minute by minute must produce exactly the match that
// resolving it instantly would have, and pausing between minutes must cost
// nothing — otherwise a manager could reroll a result by watching it.
func TestLiveEqualsSim(t *testing.T) {
	w := loadWorld(t)
	a, b := w.Leagues[0].ClubIDs[0], w.Leagues[0].ClubIDs[3]

	// Playing a match writes back the fitness and sharpness of everyone involved,
	// so the world has to be wound all the way back before each run for the
	// comparison to mean anything.
	kickoff := append([]model.Player(nil), w.Players...)
	restore := func() { copy(w.Players, kickoff) }

	whole := match.Sim(rng.New(11), side(w, a), side(w, b), 40000)

	// The same match revealed a minute at a time.
	restore()
	l := match.Begin(rng.New(11), side(w, a), side(w, b), 40000)
	minutes := 0
	for l.Step() {
		minutes++
		if l.Minute() != minutes {
			t.Fatalf("clock reads %d after %d steps", l.Minute(), minutes)
		}
	}
	if !l.Done() {
		t.Fatal("match did not finish")
	}
	sameResult(t, "minute by minute", whole, l.Result())

	// And watched partway, then abandoned to the engine.
	restore()
	part := match.Begin(rng.New(11), side(w, a), side(w, b), 40000)
	for i := 0; i < 37; i++ {
		part.Step()
	}
	part.PlayOut()
	sameResult(t, "watched then played out", whole, part.Result())

	// PlayOut on a finished match must be a no-op, so it is always safe to call
	// before reading the result.
	part.PlayOut()
	sameResult(t, "played out twice", whole, part.Result())
}

// TestLiveSubstitution checks a touchline change actually puts the player on the
// pitch, is recorded in the match report, and respects the rules of the game.
func TestLiveSubstitution(t *testing.T) {
	w := loadWorld(t)
	a, b := w.Leagues[0].ClubIDs[0], w.Leagues[0].ClubIDs[1]
	l := match.Begin(rng.New(3), side(w, a), side(w, b), 40000)
	for i := 0; i < 60; i++ {
		l.Step()
	}

	s := l.Side(0)
	slot := -1
	for i := range s.Lineup {
		if s.Lineup[i] != nil && s.Slots[i] != model.GK {
			slot = i
			break
		}
	}
	benchIdx := -1
	for i, p := range s.Bench {
		if !p.IsGK() {
			benchIdx = i
			break
		}
	}
	if slot < 0 || benchIdx < 0 {
		t.Fatal("no outfield player to swap")
	}

	off, on := s.Lineup[slot], s.Bench[benchIdx]
	left := l.SubsLeft(0)
	if err := l.Substitute(0, slot, benchIdx); err != nil {
		t.Fatalf("substitution refused: %v", err)
	}
	if s.Lineup[slot] != on {
		t.Errorf("slot %d holds %v, want %s", slot, s.Lineup[slot], on.Name)
	}
	if l.SubsLeft(0) != left-1 {
		t.Errorf("subs left = %d, want %d", l.SubsLeft(0), left-1)
	}
	var found bool
	for _, e := range l.Result().Events {
		if e.Type == match.EvSub && e.Player == off.ID && e.Other == on.ID {
			found = true
		}
	}
	if !found {
		t.Error("substitution missing from the match report")
	}

	// An outfield player may not be sent on in goal.
	if gk := slotOfGK(s); gk >= 0 {
		if err := l.Substitute(0, gk, benchIdx); err != match.ErrKeeperOnly {
			t.Errorf("outfielder in goal: got %v, want %v", err, match.ErrKeeperOnly)
		}
	}
	// Nor can a manager change a player who is not on the pitch.
	if err := l.Substitute(0, 99, 0); err != match.ErrNotOnPitch {
		t.Errorf("bad slot: got %v, want %v", err, match.ErrNotOnPitch)
	}

	l.PlayOut()
	if err := l.Substitute(0, slot, 0); err != match.ErrMatchOver {
		t.Errorf("after full time: got %v, want %v", err, match.ErrMatchOver)
	}
	// The player who came on must appear in the ratings with minutes played.
	for _, ln := range l.Result().Lines {
		if ln.PlayerID == on.ID && ln.Minutes == 0 {
			t.Error("substitute credited with no minutes")
		}
	}
}

// TestLiveReshape checks a mid-match formation change keeps the same eleven on
// the pitch, with the keeper still in goal.
func TestLiveReshape(t *testing.T) {
	w := loadWorld(t)
	a, b := w.Leagues[0].ClubIDs[0], w.Leagues[0].ClubIDs[1]
	l := match.Begin(rng.New(5), side(w, a), side(w, b), 40000)
	for i := 0; i < 30; i++ {
		l.Step()
	}

	s := l.Side(0)
	before := map[uint32]bool{}
	for _, p := range s.Lineup {
		if p != nil {
			before[p.ID] = true
		}
	}
	keeper := s.Lineup[slotOfGK(s)]

	tac := s.Tactics
	tac.Formation = model.F352
	tac.Mentality = 80
	if err := l.SetTactics(0, tac); err != nil {
		t.Fatalf("tactical change refused: %v", err)
	}

	if s.Tactics.Formation != model.F352 || s.Slots != model.F352.Slots() {
		t.Errorf("shape is %v, want 3-5-2", s.Tactics.Formation)
	}
	if s.Tactics.Mentality != 80 {
		t.Errorf("mentality is %d, want 80", s.Tactics.Mentality)
	}
	after := map[uint32]bool{}
	for _, p := range s.Lineup {
		if p != nil {
			after[p.ID] = true
		}
	}
	if len(after) != len(before) {
		t.Errorf("%d players on the pitch after reshaping, was %d", len(after), len(before))
	}
	for id := range before {
		if !after[id] {
			t.Errorf("player %d left the pitch on a change of shape", id)
		}
	}
	if gk := slotOfGK(s); s.Lineup[gk] != keeper {
		t.Errorf("keeper is %v after reshaping, want %s", s.Lineup[gk], keeper.Name)
	}

	tac.Formation = model.NumFormations
	if err := l.SetTactics(0, tac); err != match.ErrNoFormation {
		t.Errorf("bad formation: got %v, want %v", err, match.ErrNoFormation)
	}
}

// TestTakeChargeKeepsSubs holds the line on whose substitutions they are. A
// manager working the touchline must find their five changes still in hand,
// rather than spent for them by the engine — but an injury they have not dealt
// with still forces one, because nobody should be left playing a limping man.
func TestTakeChargeKeepsSubs(t *testing.T) {
	w := loadWorld(t)
	// The engine only reaches for a tired player below 62 fitness, which a squad
	// starting fresh barely gets to inside ninety minutes. Kicking off already
	// weary is what makes discretionary changes common enough to compare.
	for i := range w.Players {
		w.Players[i].Fitness = 72
	}
	kickoff := append([]model.Player(nil), w.Players...)
	a, b := w.Leagues[0].ClubIDs[0], w.Leagues[0].ClubIDs[1]

	// subsMade counts the changes the engine made for the home side, and how
	// many of those an injury forced.
	subsMade := func(res *match.Result) (total, forced int) {
		injured := map[uint32]bool{}
		for _, e := range res.Events {
			if e.Team == 0 && e.Type == match.EvInjury {
				injured[e.Player] = true
			}
		}
		for _, e := range res.Events {
			if e.Team == 0 && e.Type == match.EvSub {
				total++
				if injured[e.Player] {
					forced++
				}
			}
		}
		return total, forced
	}

	var loose, managed, managedForced int
	for seed := uint64(1); seed <= 40; seed++ {
		copy(w.Players, kickoff)
		free := match.Sim(rng.New(seed), side(w, a), side(w, b), 40000)
		n, _ := subsMade(free)
		loose += n

		copy(w.Players, kickoff)
		l := match.Begin(rng.New(seed), side(w, a), side(w, b), 40000)
		l.TakeCharge(0)
		l.PlayOut()
		n, f := subsMade(l.Result())
		managed += n
		managedForced += f
	}

	// Without a real gap between the two there is nothing being demonstrated.
	if loose < 20 {
		t.Fatalf("the engine made only %d changes over 40 matches, too few to prove anything", loose)
	}
	if managed != managedForced {
		t.Errorf("engine made %d changes for a managed side, only %d of them forced by injury",
			managed, managedForced)
	}
	t.Logf("over 40 matches: %d changes when the engine manages, %d when a human does (all injuries)",
		loose, managed)
}

func slotOfGK(s *match.Side) int {
	for i, pos := range s.Slots {
		if pos == model.GK {
			return i
		}
	}
	return -1
}
