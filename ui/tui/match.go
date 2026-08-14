package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"sportsim/engine/match"
	"sportsim/game"
)

// matchView presents a finished match either as a completed report or as a
// minute-by-minute feed.
//
// The match is already fully simulated before this screen opens — "watching" it
// is purely presentation, revealing events that have already been decided. That
// is why the quick result and the detailed feed can never disagree.
type matchView struct {
	g   *game.Game
	res *match.Result

	names  match.Names
	feed   []string // commentary lines revealed so far
	shown  int      // number of events revealed
	minute int
	speed  time.Duration
	done   bool
}

func newMatchView(g *game.Game, res *match.Result, live bool) *matchView {
	mv := &matchView{
		g:     g,
		res:   res,
		speed: 220 * time.Millisecond,
		names: match.Names{
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
		},
	}
	if !live {
		mv.finish()
	}
	return mv
}

// advance reveals the next slice of the match, up to one minute of action.
func (mv *matchView) advance() {
	if mv.done {
		return
	}
	mv.minute++
	if mv.minute > 90 {
		mv.finish()
		return
	}
	for mv.shown < len(mv.res.Events) && int(mv.res.Events[mv.shown].Minute) <= mv.minute {
		e := mv.res.Events[mv.shown]
		mv.shown++
		if line := mv.names.Commentary(e); line != "" {
			mv.feed = append(mv.feed, fmt.Sprintf("%3d'  %s", e.Minute, line))
		}
		if e.Type == match.EvFullTime {
			mv.done = true
			return
		}
	}
}

// finish reveals the whole match at once.
func (mv *matchView) finish() {
	for mv.shown < len(mv.res.Events) {
		e := mv.res.Events[mv.shown]
		mv.shown++
		if line := mv.names.Commentary(e); line != "" {
			mv.feed = append(mv.feed, fmt.Sprintf("%3d'  %s", e.Minute, line))
		}
	}
	mv.minute = 90
	mv.done = true
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

// score returns the scoreline as revealed so far, which during a live feed is
// not necessarily the final one.
func (mv *matchView) score() (int, int) {
	h, a := 0, 0
	for i := 0; i < mv.shown && i < len(mv.res.Events); i++ {
		h, a = int(mv.res.Events[i].Home), int(mv.res.Events[i].Away)
	}
	return h, a
}

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
	if mv.done {
		clock = "FT"
	}

	b.WriteString(stTitle.Render(fmt.Sprintf("  %s  %d - %d  %s", home, h, a, away)))
	b.WriteString("   " + stMuted.Render(clock))
	b.WriteString("\n")
	b.WriteString(stMuted.Render(fmt.Sprintf("  Attendance %s\n\n", comma(int64(mv.res.Attendance)))))

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
		case strings.Contains(line, "Substitution"):
			style = stMuted
		}
		b.WriteString("  " + style.Render(line) + "\n")
	}

	if mv.done {
		b.WriteString("\n")
		b.WriteString(m.matchStats())
	}
	return b.String()
}

// matchStats renders the box score and the managed club's player ratings.
func (m *Model) matchStats() string {
	mv := m.mv
	res := mv.res
	var b strings.Builder

	row := func(label string, hv, av string) {
		b.WriteString(fmt.Sprintf("  %8s  %-22s  %-8s\n", hv, centre(label, 22), av))
	}
	b.WriteString(stHeader.Render("  HOME      MATCH STATS             AWAY") + "\n")
	row("Possession", fmt.Sprintf("%d%%", res.Stats[0].Possession), fmt.Sprintf("%d%%", res.Stats[1].Possession))
	row("Shots", fmt.Sprint(res.Stats[0].Shots), fmt.Sprint(res.Stats[1].Shots))
	row("On target", fmt.Sprint(res.Stats[0].OnTarget), fmt.Sprint(res.Stats[1].OnTarget))
	row("Expected goals", fmt.Sprintf("%.2f", res.Stats[0].XG), fmt.Sprintf("%.2f", res.Stats[1].XG))
	row("Corners", fmt.Sprint(res.Stats[0].Corners), fmt.Sprint(res.Stats[1].Corners))
	row("Fouls", fmt.Sprint(res.Stats[0].Fouls), fmt.Sprint(res.Stats[1].Fouls))
	row("Cards", fmt.Sprintf("%dy %dr", res.Stats[0].Yellows, res.Stats[0].Reds),
		fmt.Sprintf("%dy %dr", res.Stats[1].Yellows, res.Stats[1].Reds))

	// Ratings for the managed club only.
	ours := uint8(0)
	if res.AwayClub == m.g.World.HumanClubID {
		ours = 1
	}
	b.WriteString("\n" + stHeader.Render("  YOUR PLAYERS                MIN   G   A  RATING") + "\n")
	for _, ln := range res.Lines {
		if ln.Team != ours || ln.Minutes == 0 {
			continue
		}
		p := m.g.World.Player(ln.PlayerID)
		if p == nil {
			continue
		}
		mark := ""
		switch {
		case ln.Red:
			mark = stBad.Render(" R")
		case ln.Yellow:
			mark = stWarn.Render(" Y")
		case ln.Injury > 0:
			mark = stBad.Render(" +")
		}
		rs := stGood
		switch {
		case ln.Rating < 6:
			rs = stBad
		case ln.Rating < 7:
			rs = stMuted
		}
		b.WriteString(fmt.Sprintf("  %-24s %5d %3d %3d   %s%s\n",
			trunc(p.Name, 24), ln.Minutes, ln.Goals, ln.Assists,
			rs.Render(fmt.Sprintf("%.1f", ln.Rating)), mark))
	}
	return b.String()
}
