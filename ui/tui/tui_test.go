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
