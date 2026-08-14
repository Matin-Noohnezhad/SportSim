package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"sportsim/game"
)

// TestScreensRender exercises every screen's rendering path. The views index
// into squads, tables and fixture lists by cursor position, so a smoke test that
// simply calls each one catches the out-of-range panics that would otherwise
// only show up mid-career.
func TestScreensRender(t *testing.T) {
	g, err := game.New("Tester", 1, 7)
	if err != nil {
		t.Fatal(err)
	}
	g.AutoSelect()

	m := &Model{g: g, screen: ScreenHome, width: 120, height: 40, swapFrom: -1, liveMode: false}
	m.tableLeague = g.Club().LeagueID

	// Play far enough in to have results, injuries and a populated inbox.
	for i := 0; i < 120; i++ {
		rep := g.AdvanceDay()
		if rep.HumanResult != nil {
			m.mv = newMatchView(g, rep.HumanResult, false)
		}
	}
	m.runSearch()
	m.viewPlayer = g.Squad()[0].PlayerID
	m.seasonReport = []string{"Someone wins the league."}

	screens := map[string]Screen{
		"home": ScreenHome, "squad": ScreenSquad, "tactics": ScreenTactics,
		"table": ScreenTable, "fixtures": ScreenFixtures, "transfers": ScreenTransfers,
		"inbox": ScreenInbox, "player": ScreenPlayer, "match": ScreenMatch,
		"seasonEnd": ScreenSeasonEnd, "newGame": ScreenNewGame,
	}
	for name, sc := range screens {
		m.screen = sc
		out := m.View()
		if strings.TrimSpace(out) == "" {
			t.Errorf("%s screen rendered nothing", name)
		}
	}

	// Cursors at the far end of each list must not panic either.
	m.squadCur = len(g.Squad()) - 1
	m.transferCur = len(m.searchResult) - 1
	m.tacticsCur = 26
	m.inboxCur = len(g.Inbox) - 1
	for _, sc := range screens {
		m.screen = sc
		_ = m.View()
	}
}

// TestKeyNavigation drives the model through its key handlers the way a player
// would, checking that navigation never panics and that actions land.
func TestKeyNavigation(t *testing.T) {
	g, _ := game.New("Tester", 1, 11)
	g.AutoSelect()
	m := &Model{g: g, screen: ScreenHome, width: 120, height: 40, swapFrom: -1}
	m.tableLeague = g.Club().LeagueID

	keys := []string{"s", "down", "down", "up", "enter", "esc", "t", "a", "]", "[",
		"down", "enter", "down", "enter", "right", "left", "l", "right", "left",
		"f", "r", "/", "esc", "i", "h", "L", " "}
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "down", "up", "left", "right", "enter", "esc":
			msg = tea.KeyMsg{Type: keyType(k)}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var mm tea.Model = m
		mm, _ = m.Update(msg)
		m = mm.(*Model)
		if strings.TrimSpace(m.View()) == "" && !m.quitting {
			t.Fatalf("blank screen after key %q", k)
		}
	}
}

// TestTouchlineControl drives a match the way a manager would work one: advance
// days until kickoff, pause, make a substitution, change shape, then see the
// game out. It is a smoke test over the whole live path, from the key handlers
// down to the engine, and it checks the day only moves on once the match is over.
func TestTouchlineControl(t *testing.T) {
	g, _ := game.New("Tester", 1, 23)
	g.AutoSelect()
	m := &Model{g: g, screen: ScreenHome, width: 120, height: 40, swapFrom: -1, liveMode: true}
	m.tableLeague = g.Club().LeagueID

	press := func(k string) {
		t.Helper()
		var msg tea.KeyMsg
		switch k {
		case "down", "up", "left", "right", "enter", "esc":
			msg = tea.KeyMsg{Type: keyType(k)}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var mm tea.Model = m
		mm, _ = m.Update(msg)
		m = mm.(*Model)
		if strings.TrimSpace(m.View()) == "" && !m.quitting {
			t.Fatalf("blank screen after key %q", k)
		}
	}

	// Advance until the managed club kicks off.
	for i := 0; i < 60 && m.screen != ScreenMatch; i++ {
		press(" ")
	}
	if m.screen != ScreenMatch || m.mv == nil || m.mv.live == nil {
		t.Fatal("never reached a match under touchline control")
	}
	date := g.World.Date

	// Play a stretch of it, then stop the clock.
	for i := 0; i < 60; i++ {
		m.mv.advance()
	}
	press(" ")
	if !m.mv.paused {
		t.Error("space did not pause the match")
	}
	if g.World.Date != date {
		t.Error("the day moved on while the match was still being played")
	}

	// A substitution: open the panel, choose a player, choose a replacement.
	before := m.mv.live.SubsLeft()
	pitch := m.mv.live.OnPitch()
	press("s")
	if m.mv.panel != panelSubs {
		t.Fatal("s did not open the substitutions panel")
	}
	press("down")
	off := pitch[m.mv.pitchCur]
	press("enter")
	if !m.mv.onBench {
		t.Fatal("enter did not move on to the bench")
	}
	// The bench always leads with the reserve keeper, who cannot come on for an
	// outfield player, so move past them.
	for m.mv.live.Bench()[m.mv.benchCur].Keeper {
		press("down")
	}
	on := m.mv.live.Bench()[m.mv.benchCur]
	press("enter")

	if m.mv.live.SubsLeft() != before-1 {
		t.Fatalf("subs left = %d, want %d — the change was refused: %s",
			m.mv.live.SubsLeft(), before-1, m.status)
	}
	if m.mv.panel != panelNone {
		t.Error("the panel stayed open after the change was made")
	}
	nowOn := map[uint32]bool{}
	for _, p := range m.mv.live.OnPitch() {
		nowOn[p.PlayerID] = true
	}
	if !nowOn[on.PlayerID] {
		t.Errorf("%s did not come on", on.Name)
	}
	if nowOn[off.PlayerID] {
		t.Errorf("%s did not come off", off.Name)
	}

	// A change of shape, which must keep the same eleven on the pitch.
	press("t")
	if m.mv.panel != panelShape {
		t.Fatal("t did not open the shape panel")
	}
	shape := m.mv.live.Tactics().Formation
	press("right")
	if m.mv.live.Tactics().Formation == shape {
		t.Errorf("formation did not change from %s: %s", shape, m.status)
	}
	if got := len(m.mv.live.OnPitch()); got != 11 {
		t.Errorf("%d players on the pitch after reshaping, want 11", got)
	}
	press("down")
	mentality := m.mv.live.Tactics().Mentality
	press("right")
	if m.mv.live.Tactics().Mentality != mentality+5 {
		t.Errorf("mentality = %d, want %d", m.mv.live.Tactics().Mentality, mentality+5)
	}
	press("esc")

	// See the match out, then leave — only now should the day resolve.
	press("enter")
	if !m.mv.done {
		t.Fatal("enter did not run the match to full time")
	}
	if g.World.Date != date {
		t.Error("the day moved on before the manager left the touchline")
	}
	press("enter")
	if m.screen == ScreenMatch {
		t.Error("still on the match screen after full time")
	}
	if g.World.Date == date {
		t.Error("the day did not resolve after the match")
	}
	if f := g.Sched.Fixtures; true {
		played := false
		for i := range f {
			if f[i].Played && (f[i].Home == g.World.HumanClubID || f[i].Away == g.World.HumanClubID) {
				played = true
			}
		}
		if !played {
			t.Error("the watched match was never recorded")
		}
	}
}

func keyType(k string) tea.KeyType {
	switch k {
	case "down":
		return tea.KeyDown
	case "up":
		return tea.KeyUp
	case "left":
		return tea.KeyLeft
	case "right":
		return tea.KeyRight
	case "enter":
		return tea.KeyEnter
	}
	return tea.KeyEsc
}

// TestNewGameFlow drives the club picker the way a player first meets it:
// type a name, choose a division, choose a club, and land in a live career.
func TestNewGameFlow(t *testing.T) {
	m := &Model{screen: ScreenNewGame, width: 110, height: 36, swapFrom: -1}

	send := func(msgs ...tea.KeyMsg) {
		t.Helper()
		for _, msg := range msgs {
			var mm tea.Model = m
			mm, _ = m.Update(msg)
			m = mm.(*Model)
		}
	}
	runes := func(s string) []tea.KeyMsg {
		out := make([]tea.KeyMsg, 0, len(s))
		for _, r := range s {
			out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		return out
	}

	send(runes("Matin")...)
	if m.nameInput != "Matin" {
		t.Fatalf("name input = %q, want %q", m.nameInput, "Matin")
	}

	// Move down two divisions, then pick the third club in it.
	send(tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown})
	send(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pickStage != 1 {
		t.Fatal("did not advance to club selection")
	}
	send(tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown})
	send(tea.KeyMsg{Type: tea.KeyEnter})

	if m.g == nil {
		t.Fatal("no career was started")
	}
	if m.screen != ScreenHome {
		t.Fatalf("screen = %v, want home", m.screen)
	}
	if m.g.World.ManagerName != "Matin" {
		t.Errorf("manager name = %q", m.g.World.ManagerName)
	}
	c := m.g.Club()
	if c == nil || !c.IsHuman {
		t.Fatal("managed club not set")
	}
	// A career must start with a legal eleven already picked.
	for i, id := range c.Lineup {
		if id == 0 {
			t.Fatalf("lineup slot %d is empty at kickoff", i)
		}
	}
	t.Logf("started career: %s at %s", m.g.World.ManagerName, c.Name)

	// And the first day must simulate without incident.
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if strings.TrimSpace(m.View()) == "" {
		t.Fatal("blank screen after advancing a day")
	}
}
