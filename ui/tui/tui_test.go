package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"sportsim/engine/model"
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

	m := &Model{g: g, screen: ScreenHome, width: 120, height: 40, swapFrom: -1, liveMode: false, mk: newMarket()}
	m.tableLeague = g.Club().LeagueID

	// Play far enough in to have results, injuries and a populated inbox.
	for i := 0; i < 120; i++ {
		rep := g.AdvanceDay()
		if rep.HumanResult != nil {
			m.mv = newMatchView(g, rep.HumanResult, false)
		}
	}
	m.mk.apply(g)
	m.viewPlayer = g.Squad()[0].PlayerID
	m.seasonReport = []string{"Someone wins the league."}

	m.report = g.FixtureReport(g.Sched.RecentFor(g.Club().ID, 1)[0])

	screens := map[string]Screen{
		"home": ScreenHome, "squad": ScreenSquad, "tactics": ScreenTactics,
		"table": ScreenTable, "stats": ScreenStats, "europe": ScreenEurope,
		"fixtures": ScreenFixtures, "transfers": ScreenTransfers,
		"inbox": ScreenInbox, "player": ScreenPlayer, "match": ScreenMatch,
		"report": ScreenReport, "seasonEnd": ScreenSeasonEnd, "newGame": ScreenNewGame,
	}
	for name, sc := range screens {
		m.screen = sc
		out := m.View()
		if strings.TrimSpace(out) == "" {
			t.Errorf("%s screen rendered nothing", name)
		}
	}

	// Every tab of a match report is a layout of its own, on both the screen a
	// match is watched on and the one a past match is opened on.
	for tb := tabFeed; tb < numMatchTabs; tb++ {
		m.screen, m.mv.tab = ScreenMatch, tb
		if strings.TrimSpace(m.View()) == "" {
			t.Errorf("match tab %s rendered nothing", matchTabNames[tb])
		}
		if tb == tabFeed {
			continue // a stored match keeps no commentary
		}
		m.screen, m.reportTab = ScreenReport, tb
		if strings.TrimSpace(m.View()) == "" {
			t.Errorf("report tab %s rendered nothing", matchTabNames[tb])
		}
	}

	// The statistics charts stack instead of sitting side by side on a narrow
	// terminal, which is a second layout to keep from panicking.
	m.width, m.screen = 70, ScreenStats
	if strings.TrimSpace(m.View()) == "" {
		t.Error("stats screen rendered nothing at narrow width")
	}
	m.width = 120

	// Cursors at the far end of each list must not panic either.
	m.squadCur = len(g.Squad()) - 1
	m.mk.cur = len(m.mk.results) - 1
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
	m := &Model{g: g, screen: ScreenHome, width: 120, height: 40, swapFrom: -1, mk: newMarket()}
	m.tableLeague = g.Club().LeagueID

	keys := []string{"s", "down", "down", "up", "enter", "esc", "t", "a", "]", "[",
		"down", "enter", "down", "enter", "right", "left", "l", "right", "left",
		"tab", "right", "left", "tab", "e", "right", "right", "right", "left",
		"tab", "tab", "f", "r", "/", "esc", "i", "h", "L", " "}
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "down", "up", "left", "right", "enter", "esc", "tab":
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
	m := &Model{g: g, screen: ScreenHome, width: 120, height: 40, swapFrom: -1, liveMode: true, mk: newMarket()}
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

// TestFixtureReportNavigation opens a match played earlier in the season from
// the fixture list, the way a manager looking back over a result would: press f
// for the calendar, move up to a match already played, and open it.
func TestFixtureReportNavigation(t *testing.T) {
	g, _ := game.New("Tester", 1, 31)
	g.AutoSelect()
	m := &Model{g: g, screen: ScreenHome, width: 120, height: 40, swapFrom: -1, liveMode: false, mk: newMarket()}
	m.tableLeague = g.Club().LeagueID
	for i := 0; i < 60; i++ {
		g.AdvanceDay()
	}

	press := func(k string) {
		t.Helper()
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		switch k {
		case "up", "down", "enter", "esc", "tab":
			msg = tea.KeyMsg{Type: keyType(k)}
		}
		var mm tea.Model = m
		mm, _ = m.Update(msg)
		m = mm.(*Model)
		if strings.TrimSpace(m.View()) == "" && !m.quitting {
			t.Fatalf("blank screen after key %q", k)
		}
	}

	// The list opens on the next match to be played, which has no report yet.
	press("f")
	fixtures := m.myFixtures()
	if m.fixtureCur != nextFixtureIndex(fixtures) {
		t.Fatalf("cursor at %d, want the next unplayed match at %d",
			m.fixtureCur, nextFixtureIndex(fixtures))
	}
	press("enter")
	if m.screen == ScreenReport {
		t.Error("an unplayed fixture opened a match report")
	}

	// The one before it has been played, so it has.
	press("up")
	played := fixtures[m.fixtureCur]
	if !played.Played {
		t.Fatal("the fixture above the next one has not been played")
	}
	press("enter")
	if m.screen != ScreenReport || m.report == nil {
		t.Fatal("enter did not open the match report")
	}
	if m.report.HomeGoals != int(played.HomeGoals) || m.report.AwayGoals != int(played.AwayGoals) {
		t.Errorf("report shows %d-%d, the fixture was %d-%d",
			m.report.HomeGoals, m.report.AwayGoals, played.HomeGoals, played.AwayGoals)
	}

	// The tabs cycle, and never stray onto the commentary a stored match has no
	// record of.
	for i := 0; i < int(numMatchTabs-tabOverview); i++ {
		if m.reportTab == tabFeed {
			t.Fatal("a stored match offered the commentary tab")
		}
		press("tab")
	}
	if m.reportTab != tabOverview {
		t.Errorf("the tabs stopped on %s rather than coming round to the overview",
			matchTabNames[m.reportTab])
	}

	press("esc")
	if m.screen != ScreenFixtures {
		t.Error("esc did not go back to the fixture list")
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
	case "tab":
		return tea.KeyTab
	}
	return tea.KeyEsc
}

// TestNewGameFlow drives the club picker the way a player first meets it:
// type a name, choose a division, choose a club, and land in a live career.
func TestNewGameFlow(t *testing.T) {
	m := &Model{screen: ScreenNewGame, width: 110, height: 36, swapFrom: -1, mk: newMarket()}

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

// TestTransferMarket drives the market screen the way a manager scouting a
// signing would: narrow the list with the filter bar, shortlist someone,
// then open the negotiation panel and adjust an offer.
//
// The screen swallows single letters while a filter has focus, which is what
// lets a name be typed without `s` jumping to the squad; the test checks both
// halves of that, since getting it wrong makes the interface unusable in one
// mode or unreachable in the other.
func TestTransferMarket(t *testing.T) {
	g, _ := game.New("Tester", 1, 23)
	g.AutoSelect()
	m := &Model{g: g, screen: ScreenHome, width: 130, height: 40, swapFrom: -1, mk: newMarket()}
	m.tableLeague = g.Club().LeagueID

	send := func(keys ...string) {
		t.Helper()
		for _, k := range keys {
			var msg tea.KeyMsg
			switch k {
			case "tab":
				msg = tea.KeyMsg{Type: tea.KeyTab}
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			case "backspace":
				msg = tea.KeyMsg{Type: tea.KeyBackspace}
			case "up", "down", "left", "right":
				msg = tea.KeyMsg{Type: keyType(k)}
			default:
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
			}
			var mm tea.Model = m
			mm, _ = m.Update(msg)
			m = mm.(*Model)
			if strings.TrimSpace(m.View()) == "" {
				t.Fatalf("blank screen after key %q", k)
			}
		}
	}

	// The market opens populated rather than empty: a manager should not have to
	// guess a name before the screen shows them anything.
	send("r")
	if m.screen != ScreenTransfers {
		t.Fatal("r did not open the transfer market")
	}
	if len(m.mk.results) == 0 {
		t.Fatal("the market opened with no players listed")
	}
	all := m.mk.matched

	// Narrowing by position has to bite, and has to find players who list the
	// position second or third rather than only those whose best it is. The
	// cycle starts at "any" and the first step lands on GK, which is the one
	// position nobody holds as a second job, so walk on to centre midfield.
	send("tab", "tab")
	if m.mk.focus != filterPos {
		t.Fatalf("focus = %v, want the position field", m.mk.focus)
	}
	for i := 0; i <= int(model.CM); i++ {
		send("right")
	}
	pos := m.mk.filter.Position
	if pos != model.CM {
		t.Fatalf("position field = %v, want CM", pos)
	}
	if m.mk.matched >= all {
		t.Errorf("filtering to %s did not narrow the market (%d of %d)", pos, m.mk.matched, all)
	}
	secondary := 0
	for _, r := range m.mk.results {
		p := g.World.Player(r.PlayerID)
		if !p.PlaysPos(pos) {
			t.Fatalf("%s (%s) does not play %s", r.Name, r.Positions, pos)
		}
		if p.Primary() != pos {
			secondary++
		}
	}
	if secondary == 0 {
		t.Errorf("no player was matched on a secondary position; only best positions are being searched")
	}

	// A letter typed into a filter must edit it, not trigger a global binding.
	narrowed := m.mk.matched
	send("tab", "2", "3")
	if m.mk.ageText != "23" {
		t.Fatalf("age field = %q, want %q", m.mk.ageText, "23")
	}
	if m.mk.matched >= narrowed {
		t.Error("an age ceiling did not narrow the market further")
	}
	for _, r := range m.mk.results {
		if r.Age > 23 {
			t.Fatalf("%s is %d, past the age ceiling", r.Name, r.Age)
		}
	}
	if m.screen != ScreenTransfers {
		t.Fatal("typing into a filter navigated away from the market")
	}

	// Every other binding on this screen is a letter, so a name made only of
	// them has to survive being typed: n, o, c and v are all commands on the
	// list and all ordinary characters in the name field.
	send("esc", "/")
	if m.mk.focus != filterName {
		t.Fatal("/ did not focus the name filter")
	}
	sortWas, posWas := m.mk.filter.Sort, m.mk.filter.Position
	send("n", "o", "c", "v")
	if m.mk.filter.Name != "nocv" {
		t.Fatalf("name filter = %q, want %q — a shortcut fired while typing",
			m.mk.filter.Name, "nocv")
	}
	if m.mk.filter.Sort != sortWas || m.mk.filter.Position != posWas {
		t.Error("typing into the name field changed the sort or the position filter")
	}
	if m.screen != ScreenTransfers {
		t.Fatal("typing into the name field navigated away")
	}
	send("backspace", "backspace", "backspace", "backspace")
	if m.mk.filter.Name != "" {
		t.Fatalf("backspace left %q", m.mk.filter.Name)
	}

	// Back on the list, the same letters are commands again.
	send("esc")
	if m.mk.focus != filterNone {
		t.Fatal("esc did not return focus to the results")
	}
	if len(m.mk.results) == 0 {
		t.Skip("no player matches the filters this seed produced")
	}

	// Shortlisting round-trips through the scope filter.
	target := m.mk.results[0]
	send("*")
	if g.ShortlistSize() != 1 {
		t.Fatalf("shortlist holds %d players, want 1", g.ShortlistSize())
	}
	send("c") // clearing the filters must not clear the shortlist
	if g.ShortlistSize() != 1 {
		t.Error("clearing the filters emptied the shortlist")
	}
	if m.mk.matched != all {
		t.Errorf("cleared filters matched %d, want the full market of %d", m.mk.matched, all)
	}
	send("tab", "tab", "tab", "tab", "tab", "tab", "right", "esc")
	if m.mk.filter.Scope != game.ScopeShortlist {
		t.Fatalf("scope = %v, want the shortlist", m.mk.filter.Scope)
	}
	if m.mk.matched != 1 || m.mk.results[0].PlayerID != target.PlayerID {
		t.Fatalf("shortlist scope showed %d players, want just %s", m.mk.matched, target.Name)
	}

	// The bid panel opens pre-filled with terms that would be accepted, and the
	// arrow keys have to move real money.
	send("enter")
	if m.mk.bid == nil {
		t.Fatal("enter did not open the bid panel")
	}
	bp := m.mk.bid
	if bp.fee != bp.q.AskingPrice || bp.wage != uint32(bp.q.WageDemand) {
		t.Error("the panel did not open on terms that would be accepted")
	}
	opening := bp.fee
	send("left", "left")
	if bp.fee >= opening {
		t.Errorf("the fee did not come down: %d then %d", opening, bp.fee)
	}
	if bp.fee < transferHaggleFloor(bp.q.AskingPrice) {
		t.Log("two presses already took the offer under what the club would accept")
	}
	send("down", "right")
	if bp.wage <= uint32(bp.q.WageDemand) {
		t.Error("the wage did not go up")
	}
	send("esc")
	if m.mk.bid != nil {
		t.Error("esc did not close the bid panel")
	}

	// And a bid that meets both demands has to actually sign the player.
	before := g.World.Player(target.PlayerID).ClubID
	send("enter")
	m.mk.bid.fee = m.mk.bid.q.AskingPrice
	m.mk.bid.wage = uint32(m.mk.bid.q.WageDemand)
	send("enter")
	if got := g.World.Player(target.PlayerID).ClubID; got == before {
		t.Logf("bid for %s was turned down: %s", target.Name, m.status)
	} else if got != g.World.HumanClubID {
		t.Fatalf("%s ended up at club %d, not ours", target.Name, got)
	}
}

// transferHaggleFloor mirrors what a selling club will come down to, so the
// test can tell a rejected lowball from a broken adjustment.
func transferHaggleFloor(ask int64) int64 { return ask * 92 / 100 }

// TestQuitIsConfirmed drives the way out of the game by keypress.
//
// q on the home screen used to end the career on the spot, which is a lot to
// hang on one letter when the neighbouring screens all use the same key to mean
// "go back" and nothing is saved automatically. The prompt has to open on No, so
// that leaving takes a deliberate move onto Yes and then enter — pressing q
// twice, or enter straight away, must keep the manager in their job.
func TestQuitIsConfirmed(t *testing.T) {
	g, _ := game.New("Tester", 1, 77)
	m := &Model{g: g, screen: ScreenHome, width: 120, height: 40, swapFrom: -1, mk: newMarket()}

	press := func(k string) tea.Cmd {
		t.Helper()
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		switch k {
		case "up", "down", "left", "right", "enter", "tab", "esc":
			msg = tea.KeyMsg{Type: keyType(k)}
		}
		var mm tea.Model = m
		mm, cmd := m.Update(msg)
		m = mm.(*Model)
		if strings.TrimSpace(m.View()) == "" && !m.quitting {
			t.Fatalf("blank screen after key %q", k)
		}
		return cmd
	}

	if cmd := press("q"); cmd != nil || m.quitting {
		t.Fatal("q quit the game outright instead of asking")
	}
	if !m.confirmQuit {
		t.Fatal("q did not open the confirmation")
	}
	if m.quitYes {
		t.Error("the confirmation opened on Yes; it must open on No")
	}

	// Enter without moving keeps the career, which is the whole point of the
	// cursor starting where it does.
	if cmd := press("enter"); cmd != nil || m.quitting {
		t.Fatal("enter on No quit the game")
	}
	if m.confirmQuit {
		t.Error("answering No left the prompt on screen")
	}

	// Escape is the other way out of the question.
	press("q")
	press("esc")
	if m.confirmQuit || m.quitting {
		t.Error("esc did not dismiss the confirmation")
	}

	// While it is up it owns the keyboard: a letter that would normally change
	// screen must not answer the question by walking away from it.
	press("q")
	press("s")
	if !m.confirmQuit {
		t.Fatal("a global binding escaped the confirmation")
	}
	if m.screen != ScreenHome {
		t.Errorf("s changed the screen to %v while the prompt was up", m.screen)
	}

	// Moving onto Yes and confirming is the one path that ends the game.
	press("right")
	if !m.quitYes {
		t.Fatal("right did not move the cursor onto Yes")
	}
	if cmd := press("enter"); cmd == nil {
		t.Fatal("enter on Yes did not quit")
	}
	if !m.quitting {
		t.Error("the model does not know it is quitting")
	}
}
