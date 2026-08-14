package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"sportsim/engine/match"
	"sportsim/engine/model"
	"sportsim/game"
)

// matchPanel is the touchline overlay currently open over the match feed.
type matchPanel uint8

const (
	panelNone matchPanel = iota
	panelSubs
	panelShape
)

// matchView presents a match as a minute-by-minute feed, a completed report, or
// a game the manager is taking charge of from the touchline.
//
// When live is set the minutes are being simulated as they are shown, and the
// manager can substitute and reshape between them. Otherwise the match is
// already over and the feed is pure presentation, revealing events that were
// decided before the screen opened. Both read the same event stream from the
// same engine, so a watched match and a skipped one can never disagree.
type matchView struct {
	g    *game.Game
	live *game.LiveMatch // nil when reviewing a finished match
	res  *match.Result

	names  match.Names
	feed   []string // commentary lines revealed so far
	shown  int      // number of events revealed
	minute int
	speed  time.Duration
	paused bool
	done   bool

	// tab is which page of the match is on screen. The statistics pages do not
	// stop the clock: they are a way of watching the match, not a decision to
	// be made, and the feed carries on collecting behind them.
	tab matchTab

	// Touchline controls.
	panel    matchPanel
	pitchCur int
	benchCur int
	onBench  bool // choosing who comes on, having chosen who goes off
	shapeCur int
}

func newNames(g *game.Game, res *match.Result) match.Names {
	return match.Names{
		Player: func(id uint32) string {
			if p := g.World.Player(id); p != nil {
				return p.Name
			}
			return "someone"
		},
		Team: func(t uint8) string {
			if t == 0 {
				return g.World.ClubName(res.HomeClub)
			}
			return g.World.ClubName(res.AwayClub)
		},
	}
}

// newMatchView opens a finished match, either revealed minute by minute or all
// at once.
func newMatchView(g *game.Game, res *match.Result, live bool) *matchView {
	mv := &matchView{g: g, res: res, speed: 220 * time.Millisecond, names: newNames(g, res)}
	if !live {
		mv.finish()
	}
	return mv
}

// newLiveView opens a match the manager is taking charge of. Nothing beyond
// kickoff has been simulated yet.
func newLiveView(g *game.Game, lm *game.LiveMatch) *matchView {
	res := lm.Result()
	return &matchView{g: g, live: lm, res: res, speed: 220 * time.Millisecond, names: newNames(g, res)}
}

// advance moves the match on by a minute: simulating it when the manager is in
// charge, revealing it when the match is already played.
func (mv *matchView) advance() {
	if mv.done || mv.paused || mv.panel != panelNone {
		return
	}
	if mv.live != nil {
		mv.live.Step()
		mv.minute = mv.live.Minute()
		mv.reveal()
		if mv.live.Done() {
			mv.whistle()
		}
		return
	}

	mv.minute++
	if mv.minute > 90 {
		mv.finish()
		return
	}
	mv.reveal()
}

// reveal moves newly known events into the commentary feed. A live match only
// has events up to the minute just played; a finished one is held back to the
// minute on the clock.
func (mv *matchView) reveal() {
	for mv.shown < len(mv.res.Events) {
		e := mv.res.Events[mv.shown]
		if mv.live == nil && int(e.Minute) > mv.minute {
			return
		}
		mv.shown++
		if line := mv.names.Commentary(e); line != "" {
			mv.feed = append(mv.feed, fmt.Sprintf("%3d'  %s", e.Minute, line))
		}
		if e.Type == match.EvFullTime {
			mv.whistle()
			return
		}
	}
}

// whistle ends the match and turns the screen into the match report, which is
// what a manager wants once there is nothing left to watch. The commentary is
// still a tab away, and now holds the whole ninety minutes.
func (mv *matchView) whistle() {
	mv.done = true
	if mv.tab == tabFeed {
		mv.tab = tabOverview
	}
}

// report is the match as the statistics tabs draw it, rebuilt each frame so a
// watched match's figures move with the clock.
func (mv *matchView) report() *game.MatchReport { return mv.g.ResultReport(mv.res) }

// finish plays or reveals the rest of the match at once.
func (mv *matchView) finish() {
	if mv.live != nil {
		mv.live.PlayOut()
	}
	mv.minute = 90
	for mv.shown < len(mv.res.Events) {
		e := mv.res.Events[mv.shown]
		mv.shown++
		if line := mv.names.Commentary(e); line != "" {
			mv.feed = append(mv.feed, fmt.Sprintf("%3d'  %s", e.Minute, line))
		}
	}
	mv.panel = panelNone
	mv.whistle()
}

func (mv *matchView) faster() {
	if mv.speed > 40*time.Millisecond {
		mv.speed = mv.speed * 2 / 3
	}
}

func (mv *matchView) slower() {
	if mv.speed < 2*time.Second {
		mv.speed = mv.speed * 3 / 2
	}
}

// managing reports whether the manager can still act on this match.
func (mv *matchView) managing() bool { return mv.live != nil && !mv.done }

// score returns the scoreline as revealed so far, which during a live feed is
// not necessarily the final one.
func (mv *matchView) score() (int, int) {
	h, a := 0, 0
	for i := 0; i < mv.shown && i < len(mv.res.Events); i++ {
		h, a = int(mv.res.Events[i].Home), int(mv.res.Events[i].Away)
	}
	return h, a
}

// ----- rendering -----

func (m *Model) viewMatch() string {
	mv := m.mv
	if mv == nil {
		return ""
	}
	g := m.g
	h, a := mv.score()

	var b strings.Builder

	home := g.World.ClubName(mv.res.HomeClub)
	away := g.World.ClubName(mv.res.AwayClub)
	clock := fmt.Sprintf("%d'", mv.minute)
	switch {
	case mv.done:
		clock = "FT"
	case mv.paused || mv.panel != panelNone:
		clock = fmt.Sprintf("%d'  PAUSED", mv.minute)
	}

	b.WriteString(stTitle.Render(fmt.Sprintf("  %s  %d - %d  %s", home, h, a, away)))
	b.WriteString("   " + stMuted.Render(clock))
	b.WriteString("\n")
	b.WriteString(stMuted.Render(fmt.Sprintf("  Attendance %s\n", comma(int64(mv.res.Attendance)))))
	if mv.managing() {
		b.WriteString(stMuted.Render(fmt.Sprintf("  %d substitutions left   [s] subs  [t] shape  [space] pause\n",
			mv.live.SubsLeft())))
	}
	b.WriteString("\n")

	if mv.panel != panelNone {
		b.WriteString(m.matchPanel())
		return b.String()
	}

	b.WriteString(tabBar(mv.tab, tabFeed))
	if mv.tab != tabFeed {
		b.WriteString(m.reportBody(mv.report(), mv.tab))
		return b.String()
	}

	// Commentary feed, showing the most recent lines that fit on screen.
	height := m.height - 16
	if height < 6 {
		height = 6
	}
	feed := mv.feed
	if len(feed) > height {
		feed = feed[len(feed)-height:]
	}
	for _, line := range feed {
		style := lipgloss.NewStyle().Foreground(colText)
		switch {
		case strings.Contains(line, "GOAL!"):
			style = stGood.Bold(true)
		case strings.Contains(line, "PENALTY"):
			style = stWarn.Bold(true)
		case strings.Contains(line, "RED CARD") || strings.Contains(line, "SECOND YELLOW"):
			style = stBad.Bold(true)
		case strings.Contains(line, "Yellow card") || strings.Contains(line, "Booked"):
			style = stWarn
		case strings.Contains(line, "Half time") || strings.Contains(line, "Full time"):
			style = stTitle
		case strings.Contains(line, "Substitution") || strings.Contains(line, "Tactical change"):
			style = stMuted
		}
		b.WriteString("  " + style.Render(line) + "\n")
	}
	return b.String()
}

// matchPanel renders whichever touchline overlay is open.
func (m *Model) matchPanel() string {
	mv := m.mv
	if mv.live == nil {
		return ""
	}
	if mv.panel == panelShape {
		return m.shapePanel()
	}
	return m.subsPanel()
}

// subsPanel lists the eleven and then the bench, so a change reads the way it is
// made: pick who comes off, then pick who replaces them.
func (m *Model) subsPanel() string {
	mv := m.mv
	var b strings.Builder

	pitch := mv.live.OnPitch()
	bench := mv.live.Bench()

	b.WriteString(stHeader.Render("  ON THE PITCH               POS  FIT") + "\n")
	for i, p := range pitch {
		b.WriteString("  " + m.selectable(livePlayerLine(p), !mv.onBench && i == mv.pitchCur) + "\n")
	}

	b.WriteString("\n" + stHeader.Render("  SUBSTITUTES                POS  FIT") + "\n")
	if len(bench) == 0 {
		b.WriteString("  " + stMuted.Render("Nobody left on the bench.") + "\n")
	}
	for i, p := range bench {
		b.WriteString("  " + m.selectable(livePlayerLine(p), mv.onBench && i == mv.benchCur) + "\n")
	}

	hint := "[enter] choose a replacement   [esc] back to the match"
	if mv.onBench {
		hint = "[enter] make the change   [esc] pick someone else"
	}
	b.WriteString("\n  " + stMuted.Render(hint) + "\n")
	return b.String()
}

// livePlayerLine renders one player in a touchline list.
func livePlayerLine(p game.LivePlayer) string {
	mark := " "
	switch {
	case p.Injured:
		mark = stBad.Render("+")
	case p.Booked:
		mark = stWarn.Render("y")
	case p.Goals > 0:
		mark = stGood.Render("*")
	}
	fit := stGood
	switch {
	case p.Fitness < 55:
		fit = stBad
	case p.Fitness < 72:
		fit = stWarn
	}
	return fmt.Sprintf("%s %-24s %4s  %s", mark, trunc(p.Name, 24), p.Pos,
		fit.Render(fmt.Sprintf("%3d", p.Fitness)))
}

// liveSlider is one instruction the manager can change from the touchline.
type liveSlider struct {
	name   string
	get    func(*model.Tactics) *uint8
	lo, hi string
}

var liveSliders = []liveSlider{
	{"Mentality", func(t *model.Tactics) *uint8 { return &t.Mentality }, "defensive", "attacking"},
	{"Tempo", func(t *model.Tactics) *uint8 { return &t.Tempo }, "patient", "fast"},
	{"Width", func(t *model.Tactics) *uint8 { return &t.Width }, "narrow", "wide"},
	{"Pressing", func(t *model.Tactics) *uint8 { return &t.Pressing }, "deep block", "high press"},
	{"Directness", func(t *model.Tactics) *uint8 { return &t.Directness }, "short", "long ball"},
	{"Line height", func(t *model.Tactics) *uint8 { return &t.LineHeight }, "deep", "high line"},
	{"Tackling", func(t *model.Tactics) *uint8 { return &t.Tackling }, "contain", "aggressive"},
}

// shapePanel is the formation and the seven team instructions, changeable
// without taking anyone off.
func (m *Model) shapePanel() string {
	mv := m.mv
	t := mv.live.Tactics()
	var b strings.Builder

	b.WriteString(stHeader.Render("  SHAPE AND INSTRUCTIONS") + "\n")
	b.WriteString("  " + m.selectable(fmt.Sprintf("  %-12s %s", "Formation",
		stTitle.Render(t.Formation.String())), mv.shapeCur == 0) + "\n\n")

	for i, s := range liveSliders {
		v := *s.get(&t)
		line := fmt.Sprintf("  %-12s %-11s %s %-11s %3d",
			s.name, stMuted.Render(s.lo), bar(int(v), 24), stMuted.Render(s.hi), v)
		b.WriteString("  " + m.selectable(line, mv.shapeCur == i+1) + "\n")
	}

	b.WriteString("\n  " + stMuted.Render(
		"[←/→] adjust   [esc] back to the match   changing shape keeps the same eleven on the pitch") + "\n")
	b.WriteString("  " + stMuted.Render(
		"Touchline changes last this match only; the tactics screen sets the club's shape for good.") + "\n")
	return b.String()
}
