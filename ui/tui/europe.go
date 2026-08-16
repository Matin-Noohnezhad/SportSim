package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"sportsim/engine/model"
	"sportsim/engine/season"
	"sportsim/game"
)

// The European screen: the three continental competitions, shown as their group
// tables until the groups are over and as a bracket afterwards.
//
// It is two views of one competition the way the league and statistics screens
// are two views of one division, and tab moves between them for the same
// reason: the groups are still worth reading in April, and the bracket exists
// from the moment the draw is made.

// euroColumn is how wide one group table is drawn. Two fit side by side on a
// normal terminal; on a narrow one they stack.
const euroColumn = 52

// keyEurope moves between the competitions and between the two views of one.
func (m *Model) keyEurope(key string) (tea.Model, tea.Cmd) {
	views := m.g.Europe()
	switch key {
	case "left", "[":
		if m.euroComp > 0 {
			m.euroComp--
		}
	case "right", "]":
		if m.euroComp < len(views)-1 {
			m.euroComp++
		}
	case "tab":
		m.euroBracket = !m.euroBracket
	}
	return m, nil
}

// openEurope shows the competition the managed club is in, since that is the
// one the manager came to look at.
func (m *Model) openEurope() {
	m.screen = ScreenEurope
	for i, v := range m.g.Europe() {
		if v.Entered {
			m.euroComp = i
			m.euroBracket = v.Stage != season.StageGroup
			return
		}
	}
}

// europeLine is the one line the home screen gives Europe: which competition
// the club is in and how far it has got, or that it did not qualify.
func (m *Model) europeLine() string {
	comp := m.g.OurCompetition()
	if comp == model.NoComp {
		return "  " + stHeader.Render("EUROPE") + "\n  " +
			stMuted.Render("No European football this season.") + "\n\n"
	}
	var b strings.Builder
	b.WriteString("  " + stHeader.Render("EUROPE") + stMuted.Render("   [e] the competitions in full") + "\n")

	for _, v := range m.g.Europe() {
		if !v.Entered {
			continue
		}
		switch {
		case v.Winner == m.g.Club().Name:
			b.WriteString("  " + stGood.Render(fmt.Sprintf("Winners of the %s.", v.Name)) + "\n\n")
		case !v.StillIn:
			b.WriteString("  " + stBad.Render(fmt.Sprintf("Out of the %s.", v.Name)) + "\n\n")
		case v.Stage == season.StageGroup && v.OurGroup > 0:
			b.WriteString(fmt.Sprintf("  %s, %s\n\n", stBold.Render(v.Name),
				stMuted.Render("Group "+season.GroupLabel(v.OurGroup))))
		default:
			b.WriteString(fmt.Sprintf("  %s, %s\n\n", stBold.Render(v.Name),
				stMuted.Render(v.StageName)))
		}
		return b.String()
	}
	// Qualified, but the draw has not been made yet.
	b.WriteString(fmt.Sprintf("  %s\n\n", stBold.Render(comp.String())))
	return b.String()
}

func (m *Model) viewEurope() string {
	views := m.g.Europe()
	if len(views) == 0 {
		return "\n  " + stMuted.Render("No European competitions are being played this season.") + "\n"
	}
	if m.euroComp >= len(views) {
		m.euroComp = len(views) - 1
	}
	v := views[m.euroComp]

	var b strings.Builder
	b.WriteString("\n  " + stTitle.Render(v.Name) + stMuted.Render("   "+v.StageName) + "\n")

	// What the managed club's own standing in it is, which is the reason the
	// manager opened the screen and would otherwise take hunting for.
	switch {
	case v.Winner != "":
		b.WriteString("  " + stGood.Render(fmt.Sprintf("%s won it.", v.Winner)) + "\n")
	case v.Entered && v.StillIn:
		where := "still in it"
		if v.OurGroup > 0 && v.Stage == season.StageGroup {
			where = "in Group " + season.GroupLabel(v.OurGroup)
		}
		b.WriteString("  " + stGood.Render(fmt.Sprintf("%s are %s.",
			m.g.Club().Name, where)) + "\n")
	case v.Entered:
		b.WriteString("  " + stBad.Render(m.g.Club().Name+" are out of this competition.") + "\n")
	default:
		b.WriteString("  " + stMuted.Render("Your club is not in this competition.") + "\n")
	}
	b.WriteString("\n")

	if m.euroBracket {
		b.WriteString(m.euroBracketBody(v))
	} else {
		b.WriteString(m.euroGroupsBody(v))
	}
	return b.String()
}

// euroGroupsBody lays the group tables out, two abreast where there is room.
func (m *Model) euroGroupsBody(v game.EuroView) string {
	if len(v.Groups) == 0 {
		return "  " + stMuted.Render("The draw has not been made.") + "\n"
	}
	tables := make([][]string, 0, len(v.Groups))
	for _, gr := range v.Groups {
		tables = append(tables, m.euroGroup(gr))
	}

	var b strings.Builder
	if m.width < 2*euroColumn+4 {
		for _, t := range tables {
			for _, line := range t {
				b.WriteString("  " + line + "\n")
			}
			b.WriteString("\n")
		}
		return b.String()
	}
	for i := 0; i < len(tables); i += 2 {
		right := []string(nil)
		if i+1 < len(tables) {
			right = tables[i+1]
		}
		for _, line := range euroPair(tables[i], right) {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// euroGroup draws one group table: the same points and the same tie-breaks as a
// league table, in the narrower column two of them need to sit side by side.
func (m *Model) euroGroup(gr game.EuroGroup) []string {
	out := make([]string, 0, len(gr.Rows)+1)
	out = append(out, stHeader.Render(padVisible(fmt.Sprintf("GROUP %s%s", gr.Label,
		strings.Repeat(" ", 18))+" P  W  D  L    GD  PTS", euroColumn)))

	us := m.g.World.HumanClubID
	for i, r := range gr.Rows {
		marker := " "
		if i < 2 {
			marker = stGood.Render("│") // the two that go through
		}
		line := fmt.Sprintf("%d.%s %s %2d %2d %2d %2d %+5d %4d",
			i+1, marker, padVisible(trunc(m.g.World.ClubName(r.ClubID), 20), 20),
			r.Played, r.Won, r.Drawn, r.Lost, r.GoalDiff(), r.Points)
		if r.ClubID == us {
			line = stBold.Render(line)
		}
		out = append(out, line)
	}
	return out
}

func euroPair(left, right []string) []string {
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = strings.TrimRight(padVisible(l, euroColumn+2)+r, " ")
	}
	return out
}

// euroBracketBody draws the knockout rounds, earliest first, with each tie's
// aggregate and the legs it was made up of.
func (m *Model) euroBracketBody(v game.EuroView) string {
	if len(v.Rounds) == 0 {
		return "  " + stMuted.Render("The knockout draw is made when the group stage is over.") + "\n"
	}
	var b strings.Builder
	for _, rd := range v.Rounds {
		b.WriteString("  " + stHeader.Render(strings.ToUpper(rd.Name)) + "\n")
		for _, t := range rd.Ties {
			b.WriteString("  " + m.euroTie(t) + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Model) euroTie(t game.EuroTie) string {
	score := stMuted.Render("  -  ")
	if len(t.Legs) > 0 && t.Legs[0].Played {
		score = fmt.Sprintf("%d - %d", t.HomeGoals, t.AwayGoals)
	}
	line := fmt.Sprintf("%s %s %s",
		padVisible(trunc(t.Home, 24), 24), score, padVisible(trunc(t.Away, 24), 24))

	var notes []string
	if t.ShootoutHome != 0 || t.ShootoutAway != 0 {
		notes = append(notes, fmt.Sprintf("%d-%d on penalties", t.ShootoutHome, t.ShootoutAway))
	}
	for _, leg := range t.Legs {
		if leg.Played {
			notes = append(notes, fmt.Sprintf("%s %d-%d", leg.Date.Short(), leg.HomeGoals, leg.AwayGoals))
		} else {
			notes = append(notes, leg.Date.Short())
		}
	}
	if t.Winner != "" {
		notes = append(notes, trunc(t.Winner, 20)+" through")
	}
	line += "  " + stMuted.Render(strings.Join(notes, "   "))
	if t.Ours {
		return stBold.Render(line)
	}
	return line
}
