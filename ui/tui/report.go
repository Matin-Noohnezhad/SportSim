package tui

import (
	"fmt"
	"math"
	"strings"

	"sportsim/game"
)

// matchTab is which page of a match report is on screen.
//
// The same tabs serve a match being watched from the touchline and one opened
// from the fixture list long afterwards, because both are drawn from the same
// game.MatchReport. The commentary is the one tab a stored match cannot offer:
// a fixture keeps the incidents that decided it, not every word said about
// them, so the fixture list starts its tabs at the overview.
type matchTab uint8

const (
	tabFeed matchTab = iota
	tabOverview
	tabMoments
	tabPlayers
	numMatchTabs
)

var matchTabNames = [numMatchTabs]string{"COMMENTARY", "OVERVIEW", "KEY MOMENTS", "PLAYERS"}

// shift moves along the tabs, wrapping round the range that starts at first.
func (t matchTab) shift(first matchTab, delta int) matchTab {
	n := int(numMatchTabs - first)
	i := (int(t-first) + delta%n + n) % n
	return first + matchTab(i)
}

// tabBar draws the row of tabs, marking the open one.
func tabBar(active, first matchTab) string {
	parts := make([]string, 0, int(numMatchTabs))
	for t := first; t < numMatchTabs; t++ {
		label := " " + matchTabNames[t] + " "
		if t == active {
			parts = append(parts, stSelected.Render(label))
		} else {
			parts = append(parts, stMuted.Render(label))
		}
	}
	return "  " + strings.Join(parts, stMuted.Render("│")) + "\n\n"
}

// ---------------- the report screen ----------------

// viewReport shows a match played earlier in the season, opened from the
// fixture list.
func (m *Model) viewReport() string {
	rep := m.report
	if rep == nil {
		return "\n  " + stMuted.Render("No match report for that fixture.") + "\n"
	}
	var b strings.Builder
	b.WriteString("\n  " + stTitle.Render(fmt.Sprintf("%s  %d - %d  %s",
		rep.Home, rep.HomeGoals, rep.AwayGoals, rep.Away)) + "\n")
	b.WriteString("  " + stMuted.Render(fmt.Sprintf("%s   Attendance %s",
		rep.Date.Short(), comma(int64(rep.Attendance)))) + "\n\n")
	b.WriteString(tabBar(m.reportTab, tabOverview))
	b.WriteString(m.reportBody(rep, m.reportTab))
	return b.String()
}

// reportBody draws whichever page of a report is open. The commentary tab is
// the match feed, which only a match on the screen it was played on has, so it
// is drawn by the match view rather than here.
func (m *Model) reportBody(rep *game.MatchReport, tab matchTab) string {
	switch tab {
	case tabMoments:
		return reportMoments(rep)
	case tabPlayers:
		return reportPlayers(rep)
	default:
		return reportOverview(rep)
	}
}

// ---------------- overview ----------------

// reportOverview is the box score, each line split between the two sides in
// proportion so the shape of the match reads without comparing digits.
func reportOverview(rep *game.MatchReport) string {
	var b strings.Builder
	h, a := rep.Stats[0], rep.Stats[1]

	b.WriteString("  " + padVisible(stHeader.Render("MATCH STATS"), 20) +
		stBold.Render(trunc(rep.Home, 24)) + stMuted.Render("  v  ") +
		stBold.Render(trunc(rep.Away, 24)) + "\n\n")

	row := func(label string, hv, av float64, hs, as string) {
		b.WriteString(fmt.Sprintf("  %-18s %8s  %s  %-8s\n",
			label, hs, splitBar(hv, av, 24), as))
	}
	count := func(label string, hv, av uint16) {
		row(label, float64(hv), float64(av), fmt.Sprint(hv), fmt.Sprint(av))
	}

	row("Possession", float64(h.Possession), float64(a.Possession),
		fmt.Sprintf("%d%%", h.Possession), fmt.Sprintf("%d%%", a.Possession))
	count("Shots", h.Shots, a.Shots)
	count("Shots on target", h.OnTarget, a.OnTarget)
	row("Expected goals", h.XG, a.XG, fmt.Sprintf("%.2f", h.XG), fmt.Sprintf("%.2f", a.XG))
	count("Corners", h.Corners, a.Corners)
	count("Fouls", h.Fouls, a.Fouls)
	count("Offsides", h.Offsides, a.Offsides)
	count("Yellow cards", h.Yellows, a.Yellows)
	count("Red cards", h.Reds, a.Reds)
	return b.String()
}

// splitBar shows how one statistic divides between the two sides: the home
// share grows from the left, the away share meets it from the right. The two
// halves differ in shape as well as colour, so the split still reads on a
// terminal with no colour at all.
func splitBar(h, a float64, width int) string {
	if h+a <= 0 {
		return stMuted.Render(strings.Repeat("·", width))
	}
	filled := int(math.Round(h / (h + a) * float64(width)))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return stTitle.Render(strings.Repeat("█", filled)) +
		stWarn.Render(strings.Repeat("▒", width-filled))
}

// ---------------- key moments ----------------

// reportMoments lists what actually decided the match — the goals and the
// sendings-off, in the order they happened — and deliberately nothing else.
func reportMoments(rep *game.MatchReport) string {
	var b strings.Builder
	b.WriteString("  " + stHeader.Render(fmt.Sprintf("%4s  %-14s %-20s %-20s %-18s %s",
		"MIN", "EVENT", "PLAYER", "ASSIST", "CLUB", "SCORE")) + "\n")

	if len(rep.Moments) == 0 {
		b.WriteString("  " + stMuted.Render("Nothing to report: no goals, no sendings-off.") + "\n")
		return b.String()
	}

	for _, mo := range rep.Moments {
		st := stGood.Bold(true)
		switch mo.Kind {
		case game.MomentPenalty:
			st = stWarn.Bold(true)
		case game.MomentRed, game.MomentSecondYellow:
			st = stBad.Bold(true)
		}
		score := ""
		if mo.Kind.Goal() {
			score = stBold.Render(fmt.Sprintf("%d-%d", mo.HomeGoals, mo.AwayGoals))
		}
		assist := stMuted.Render(trunc(mo.Assist, 20))
		line := fmt.Sprintf("  %3d'  %s %s %s %s %s",
			mo.Minute,
			padVisible(st.Render(mo.Kind.String()), 14),
			padVisible(trunc(mo.Player, 20), 20),
			padVisible(assist, 20),
			stMuted.Render(padVisible(trunc(mo.Club, 18), 18)),
			score)
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	return b.String()
}

// ---------------- player ratings ----------------

// reportPlayers rates the managed club's side. The other team's ratings are
// nobody's business but their manager's, and are not kept.
func reportPlayers(rep *game.MatchReport) string {
	var b strings.Builder
	title := "YOUR PLAYERS"
	if c := rep.OurClub(); c != "" {
		title = strings.ToUpper(c)
	}
	b.WriteString("  " + stHeader.Render(fmt.Sprintf("%-26s %-4s %5s %3s %3s  %s",
		title, "POS", "MIN", "G", "A", "RATING")) + "\n")

	if len(rep.Players) == 0 {
		b.WriteString("  " + stMuted.Render("Ratings are settled at full time.") + "\n")
		return b.String()
	}

	for _, p := range rep.Players {
		mark := ""
		switch {
		case p.Red:
			mark = stBad.Render(" R")
		case p.Yellow:
			mark = stWarn.Render(" Y")
		case p.Injured:
			mark = stBad.Render(" +")
		}
		rs := stGood
		switch {
		case p.Rating < 6:
			rs = stBad
		case p.Rating < 7:
			rs = stMuted
		}
		b.WriteString(fmt.Sprintf("  %-26s %-4s %5d %3d %3d  %s%s\n",
			trunc(p.Name, 26), p.Position, p.Minutes, p.Goals, p.Assists,
			rs.Render(fmt.Sprintf("%6.1f", p.Rating)), mark))
	}
	return b.String()
}
