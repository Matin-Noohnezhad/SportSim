// Package tui is the terminal frontend.
//
// It is one of potentially several frontends: everything it does goes through
// the game package's façade, and it holds no simulation logic of its own. A web
// or mobile client would sit alongside it rather than replace it.
package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"sportsim/engine/match"
	"sportsim/engine/model"
	"sportsim/game"
	"sportsim/store"
)

// Screen identifies which view is on top.
type Screen int

const (
	ScreenNewGame Screen = iota
	ScreenHome
	ScreenSquad
	ScreenTactics
	ScreenTable
	ScreenStats
	ScreenFixtures
	ScreenTransfers
	ScreenInbox
	ScreenPlayer
	ScreenMatch
	ScreenSeasonEnd
)

// Model is the whole terminal application state.
type Model struct {
	g      *game.Game
	screen Screen
	prev   Screen

	width, height int
	status        string
	statusErr     bool
	quitting      bool

	// New-game club picker.
	pickLeague int
	pickClub   int
	pickStage  int // 0 = choosing league, 1 = choosing club
	nameInput  string

	// Cursors for the list screens.
	squadCur    int
	tacticsCur  int
	tableLeague uint16
	fixtureCur  int
	transferCur int
	inboxCur    int
	viewPlayer  uint32

	// Transfer search.
	searchInput  string
	searchActive bool
	searchResult []game.SquadRow

	// Selection editing on the tactics screen.
	swapFrom int

	// Match viewing.
	mv       *matchView
	liveMode bool

	// End-of-season report.
	seasonReport []string

	// Advancing multiple days at once.
	autoUntil int // remaining days to fast-forward, 0 when idle
}

// New builds the starting model. When path is non-empty the career is loaded
// from that save file instead of starting the new-game flow.
func New(path string) (*Model, error) {
	m := &Model{screen: ScreenNewGame, liveMode: true, swapFrom: -1}
	if path != "" {
		g, err := store.Load(path)
		if err != nil {
			return nil, err
		}
		m.g = g
		m.screen = ScreenHome
		m.tableLeague = g.Club().LeagueID
	}
	return m, nil
}

func (m *Model) Init() tea.Cmd { return nil }

// tickMsg drives the minute-by-minute match feed.
type tickMsg time.Time

// dayMsg continues a multi-day fast-forward without blocking the UI.
type dayMsg struct{}

func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func nextDay() tea.Cmd {
	return func() tea.Msg { return dayMsg{} }
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		if m.screen == ScreenMatch && m.mv != nil && !m.mv.done {
			// advance does nothing while the match is paused, so the timer keeps
			// running rather than being torn down and restarted on every pause.
			m.mv.advance()
			if m.mv.done {
				return m, nil
			}
			return m, tick(m.mv.speed)
		}
		return m, nil

	case dayMsg:
		if m.autoUntil > 0 {
			m.autoUntil--
			if cmd := m.advanceOneDay(); cmd != nil {
				m.autoUntil = 0
				return m, cmd
			}
			if m.autoUntil > 0 {
				return m, nextDay()
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A running fast-forward is interruptible by any key.
	if m.autoUntil > 0 {
		m.autoUntil = 0
		m.setStatus("Stopped.", false)
		return m, nil
	}

	if m.screen == ScreenNewGame {
		return m.keyNewGame(msg)
	}
	if m.searchActive {
		return m.keySearch(msg)
	}

	key := msg.String()

	// A match in progress owns the keyboard: from the touchline s and t are the
	// substitution and shape panels, not the squad and tactics screens, and the
	// manager cannot wander off to the transfer market mid-game.
	if m.screen == ScreenMatch && m.mv != nil {
		return m.keyMatch(key)
	}

	// Global bindings, available from every screen.
	switch key {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "q":
		if m.screen == ScreenHome {
			m.quitting = true
			return m, tea.Quit
		}
		m.screen = ScreenHome
		return m, nil
	case "esc":
		if m.screen == ScreenPlayer {
			m.screen = m.prev
		} else {
			m.screen = ScreenHome
		}
		return m, nil
	case "h":
		m.screen = ScreenHome
		return m, nil
	case "s":
		m.screen = ScreenSquad
		return m, nil
	case "t":
		m.screen = ScreenTactics
		return m, nil
	case "l":
		m.screen = ScreenTable
		if m.tableLeague == 0 {
			m.tableLeague = m.g.Club().LeagueID
		}
		return m, nil
	case "f":
		m.screen = ScreenFixtures
		return m, nil
	case "r":
		m.screen = ScreenTransfers
		return m, nil
	case "i":
		m.screen = ScreenInbox
		return m, nil
	case "S":
		return m, m.save()
	case "L":
		m.liveMode = !m.liveMode
		m.setStatus(fmt.Sprintf("Match viewing: %s", map[bool]string{true: "minute by minute", false: "instant result"}[m.liveMode]), false)
		return m, nil
	case " ":
		return m, m.advanceOneDay()
	case "w":
		// Fast-forward to the next match involving the managed club.
		m.autoUntil = 60
		return m, nextDay()
	case "m":
		m.autoUntil = 30
		return m, nextDay()
	}

	// Screen-specific bindings.
	switch m.screen {
	case ScreenSquad:
		return m.keySquad(key)
	case ScreenTactics:
		return m.keyTactics(key)
	case ScreenTable, ScreenStats:
		return m.keyTable(key)
	case ScreenFixtures:
		return m.keyList(key, &m.fixtureCur, 200)
	case ScreenTransfers:
		return m.keyTransfers(key)
	case ScreenInbox:
		return m.keyList(key, &m.inboxCur, len(m.g.Inbox))
	case ScreenSeasonEnd:
		m.screen = ScreenHome
		return m, nil
	}
	return m, nil
}

// advanceOneDay simulates a day and reacts to anything that needs the player's
// attention, such as their own match finishing or a season ending.
//
// When live viewing is on, the managed club's own fixture is handed to the
// manager first and the rest of the day waits: the other results, wages and the
// clock only follow once they have left the touchline. See finishDay.
func (m *Model) advanceOneDay() tea.Cmd {
	if m.liveMode {
		if lm := m.g.KickOff(); lm != nil {
			m.autoUntil = 0
			m.mv = newLiveView(m.g, lm)
			m.screen = ScreenMatch
			return tick(m.mv.speed)
		}
	}

	rep := m.g.AdvanceDay()

	if rep.SeasonEnded {
		m.autoUntil = 0
		m.seasonReport = rep.Outcome.Headlines
		m.screen = ScreenSeasonEnd
		m.tableLeague = m.g.Club().LeagueID
		return nil
	}

	if rep.HumanResult != nil {
		m.autoUntil = 0
		m.mv = newMatchView(m.g, rep.HumanResult, m.liveMode)
		m.screen = ScreenMatch
		if m.liveMode {
			return tick(m.mv.speed)
		}
		return nil
	}

	m.setStatus("", false)
	return nil
}

// keyList handles cursor movement for a simple scrolling list.
func (m *Model) keyList(key string, cur *int, n int) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if *cur > 0 {
			*cur--
		}
	case "down", "j":
		if *cur < n-1 {
			*cur++
		}
	case "pgup":
		*cur -= 10
		if *cur < 0 {
			*cur = 0
		}
	case "pgdown":
		*cur += 10
		if *cur > n-1 {
			*cur = n - 1
		}
	case "home", "g":
		*cur = 0
	case "end", "G":
		*cur = n - 1
	}
	return m, nil
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status, m.statusErr = s, isErr
}

func (m *Model) save() tea.Cmd {
	// The players on the pitch have already been run down by the minutes played
	// so far, and an in-progress match is not part of a save file, so reloading
	// mid-game would make them serve those minutes twice.
	if m.g.MatchInProgress() {
		m.setStatus("Finish the match before saving.", true)
		return nil
	}
	name := strings.ReplaceAll(m.g.World.ClubName(m.g.World.HumanClubID), " ", "_")
	path := filepath.Join(store.Dir(), name+".sav")
	if err := store.Save(m.g, path); err != nil {
		m.setStatus("Save failed: "+err.Error(), true)
		return nil
	}
	m.setStatus("Saved to "+path, false)
	return nil
}

// ---------------- new game ----------------

func (m *Model) keyNewGame(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	leagues := previewLeagues()

	switch msg.String() {
	case "ctrl+c", "esc":
		m.quitting = true
		return m, tea.Quit

	case "up", "k":
		if m.pickStage == 0 && m.pickLeague > 0 {
			m.pickLeague--
		} else if m.pickStage == 1 && m.pickClub > 0 {
			m.pickClub--
		}
	case "down", "j":
		if m.pickStage == 0 && m.pickLeague < len(leagues)-1 {
			m.pickLeague++
		} else if m.pickStage == 1 {
			if n := len(previewClubs(uint16(m.pickLeague + 1))); m.pickClub < n-1 {
				m.pickClub++
			}
		}
	case "enter":
		if m.pickStage == 0 {
			m.pickStage = 1
			m.pickClub = 0
			return m, nil
		}
		clubs := previewClubs(uint16(m.pickLeague + 1))
		if m.pickClub >= len(clubs) {
			return m, nil
		}
		name := strings.TrimSpace(m.nameInput)
		if name == "" {
			name = "Manager"
		}
		g, err := game.New(name, clubs[m.pickClub].ID, uint64(time.Now().UnixNano()))
		if err != nil {
			m.setStatus(err.Error(), true)
			return m, nil
		}
		g.AutoSelect() // start with the best available eleven picked
		m.g = g
		m.tableLeague = g.Club().LeagueID
		m.screen = ScreenHome
		m.setStatus(fmt.Sprintf("You are now the manager of %s.", g.Club().Name), false)
	case "backspace":
		if m.pickStage == 1 {
			m.pickStage = 0
		} else if len(m.nameInput) > 0 {
			r := []rune(m.nameInput)
			m.nameInput = string(r[:len(r)-1])
		}
	default:
		if m.pickStage == 0 && len(msg.String()) == 1 && len([]rune(m.nameInput)) < 24 {
			m.nameInput += msg.String()
		}
	}
	return m, nil
}

// ---------------- squad ----------------

func (m *Model) keySquad(key string) (tea.Model, tea.Cmd) {
	rows := m.g.Squad()
	switch key {
	case "enter":
		if m.squadCur < len(rows) {
			m.viewPlayer = rows[m.squadCur].PlayerID
			m.prev = ScreenSquad
			m.screen = ScreenPlayer
		}
		return m, nil
	case "x":
		if m.squadCur < len(rows) {
			ok, msg := m.g.Sell(rows[m.squadCur].PlayerID)
			m.setStatus(msg, !ok)
		}
		return m, nil
	case "c":
		if m.squadCur < len(rows) {
			r := rows[m.squadCur]
			// Offer the going rate for four more years.
			ok, msg := m.g.OfferContract(r.PlayerID, uint32(float64(r.Wage)*1.15)+500, 4)
			m.setStatus(msg, !ok)
		}
		return m, nil
	}
	return m.keyList(key, &m.squadCur, len(rows))
}

// ---------------- tactics ----------------

func (m *Model) keyTactics(key string) (tea.Model, tea.Cmd) {
	c := m.g.Club()
	switch key {
	case "a":
		m.g.AutoSelect()
		m.setStatus("Best available eleven selected.", false)
		return m, nil
	case "[":
		f := c.Tactics.Formation
		if f > 0 {
			m.g.SetFormation(f - 1)
			m.g.AutoSelect()
		}
		return m, nil
	case "]":
		f := c.Tactics.Formation
		if f < model.NumFormations-1 {
			m.g.SetFormation(f + 1)
			m.g.AutoSelect()
		}
		return m, nil
	case "enter":
		if m.swapFrom < 0 {
			m.swapFrom = m.tacticsCur
			m.setStatus("Select a second player to swap with.", false)
		} else {
			m.g.SwapLineup(m.swapFrom, m.tacticsCur)
			m.swapFrom = -1
			m.setStatus("Swapped.", false)
		}
		return m, nil
	case "left", "H":
		adjustSlider(c, m.tacticsCur, -5)
		return m, nil
	case "right", "J":
		adjustSlider(c, m.tacticsCur, +5)
		return m, nil
	}
	return m.keyList(key, &m.tacticsCur, 20)
}

// adjustSlider nudges whichever instruction the cursor sits on. Slider rows are
// laid out after the twenty selection rows.
func adjustSlider(c *model.Club, cur, delta int) {
	sliders := []*uint8{
		&c.Tactics.Mentality, &c.Tactics.Tempo, &c.Tactics.Width,
		&c.Tactics.Pressing, &c.Tactics.Directness, &c.Tactics.LineHeight,
		&c.Tactics.Tackling,
	}
	i := cur - 20
	if i < 0 || i >= len(sliders) {
		return
	}
	v := int(*sliders[i]) + delta
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	*sliders[i] = uint8(v)
}

// ---------------- table and statistics ----------------

// keyTable serves both league screens, which are two views of one division:
// the standings and the season's statistics, with tab between them.
func (m *Model) keyTable(key string) (tea.Model, tea.Cmd) {
	ls := m.g.World.LeaguesByTier()
	idx := 0
	for i, l := range ls {
		if l.ID == m.tableLeague {
			idx = i
		}
	}
	switch key {
	case "left", "[":
		if idx > 0 {
			m.tableLeague = ls[idx-1].ID
		}
	case "right", "]":
		if idx < len(ls)-1 {
			m.tableLeague = ls[idx+1].ID
		}
	case "tab":
		if m.screen == ScreenStats {
			m.screen = ScreenTable
		} else {
			m.screen = ScreenStats
		}
	}
	return m, nil
}

// ---------------- transfers ----------------

func (m *Model) keyTransfers(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "/":
		m.searchActive = true
		m.searchInput = ""
		return m, nil
	case "enter":
		if m.transferCur < len(m.searchResult) {
			r := m.searchResult[m.transferCur]
			ok, msg := m.g.Bid(r.PlayerID, r.Value, uint32(float64(r.Wage)*1.2)+1000, 4)
			m.setStatus(msg, !ok)
			if ok {
				m.runSearch()
			}
		}
		return m, nil
	case "v":
		if m.transferCur < len(m.searchResult) {
			m.viewPlayer = m.searchResult[m.transferCur].PlayerID
			m.prev = ScreenTransfers
			m.screen = ScreenPlayer
		}
		return m, nil
	}
	return m.keyList(key, &m.transferCur, len(m.searchResult))
}

func (m *Model) keySearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searchActive = false
		m.runSearch()
	case "esc":
		m.searchActive = false
	case "backspace":
		if r := []rune(m.searchInput); len(r) > 0 {
			m.searchInput = string(r[:len(r)-1])
		}
	default:
		if s := msg.String(); len(s) == 1 {
			m.searchInput += s
		}
	}
	return m, nil
}

func (m *Model) runSearch() {
	m.searchResult = m.g.Search(game.SearchFilter{
		Name:     m.searchInput,
		Position: model.NumPos, // no position filter
	}, 200)
	m.transferCur = 0
}

// ---------------- match ----------------

func (m *Model) keyMatch(key string) (tea.Model, tea.Cmd) {
	mv := m.mv
	if key == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}
	if mv.panel != panelNone {
		return m.keyTouchline(key)
	}

	switch key {
	case "enter", "q", "esc":
		if mv.done {
			return m, m.leaveMatch()
		}
		mv.finish() // skip to full time
		return m, nil
	case " ":
		if mv.done {
			return m, m.leaveMatch()
		}
		mv.paused = !mv.paused
		return m, nil
	case "s":
		if mv.managing() {
			mv.panel, mv.onBench, mv.pitchCur, mv.benchCur = panelSubs, false, 0, 0
		}
		return m, nil
	case "t":
		if mv.managing() {
			mv.panel, mv.shapeCur = panelShape, 0
		}
		return m, nil
	case "+", "=":
		mv.faster()
		return m, nil
	case "-":
		mv.slower()
		return m, nil
	}
	return m, nil
}

// keyTouchline drives the substitution and shape panels. The clock is stopped
// while either is open, so a manager is never hurried into a decision.
func (m *Model) keyTouchline(key string) (tea.Model, tea.Cmd) {
	mv := m.mv
	if mv.panel == panelShape {
		return m.keyShape(key)
	}

	pitch := mv.live.OnPitch()
	bench := mv.live.Bench()
	switch key {
	case "esc", "q":
		if mv.onBench {
			mv.onBench = false
		} else {
			mv.panel = panelNone
		}
		return m, nil
	case "enter":
		if !mv.onBench {
			if len(bench) == 0 {
				m.setStatus("Nobody left on the bench.", true)
				return m, nil
			}
			mv.onBench, mv.benchCur = true, 0
			return m, nil
		}
		ok, msg := mv.live.Substitute(mv.pitchCur, mv.benchCur)
		m.setStatus(msg, !ok)
		if ok {
			mv.reveal()
			mv.onBench, mv.panel = false, panelNone
		}
		return m, nil
	}

	if mv.onBench {
		return m.keyList(key, &mv.benchCur, len(bench))
	}
	return m.keyList(key, &mv.pitchCur, len(pitch))
}

// keyShape drives the formation and instruction panel. Row 0 is the formation;
// the rest are the sliders, in the order shapePanel lists them.
func (m *Model) keyShape(key string) (tea.Model, tea.Cmd) {
	mv := m.mv
	t := mv.live.Tactics()

	switch key {
	case "esc", "q", "enter":
		mv.panel = panelNone
		return m, nil
	case "left", "right", "[", "]", "H", "J":
		delta := 5
		if key == "left" || key == "[" || key == "H" {
			delta = -5
		}
		if mv.shapeCur == 0 {
			f := int(t.Formation) + delta/5
			if f < 0 || f >= int(model.NumFormations) {
				return m, nil
			}
			ok, msg := mv.live.SetFormation(model.Formation(f))
			m.setStatus(msg, !ok)
			if ok {
				mv.reveal()
			}
			return m, nil
		}
		v := int(*liveSliders[mv.shapeCur-1].get(&t)) + delta
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		*liveSliders[mv.shapeCur-1].get(&t) = uint8(v)
		if ok, msg := mv.live.SetTactics(t); !ok {
			m.setStatus(msg, true)
		}
		mv.reveal()
		return m, nil
	}
	return m.keyList(key, &mv.shapeCur, len(liveSliders)+1)
}

// leaveMatch closes the match screen. A match the manager took charge of has
// held up the rest of the day, which now runs.
func (m *Model) leaveMatch() tea.Cmd {
	live := m.mv != nil && m.mv.live != nil
	m.mv = nil
	m.screen = ScreenHome
	if !live {
		return nil
	}
	return m.finishDay()
}

// finishDay resolves everything the watched match was holding up: the other
// fixtures, recovery, wages, the transfer market and the clock.
func (m *Model) finishDay() tea.Cmd {
	rep := m.g.AdvanceDay()
	if rep.SeasonEnded {
		m.autoUntil = 0
		m.seasonReport = rep.Outcome.Headlines
		m.screen = ScreenSeasonEnd
		m.tableLeague = m.g.Club().LeagueID
		return nil
	}
	m.setStatus("", false)
	return nil
}

// resultOf is a small helper for the views.
func scoreLine(g *game.Game, r *match.Result) string {
	return fmt.Sprintf("%s %d - %d %s",
		g.World.ClubName(r.HomeClub), r.HomeGoals, r.AwayGoals, g.World.ClubName(r.AwayClub))
}
