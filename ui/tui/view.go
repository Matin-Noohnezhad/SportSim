package tui

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"sportsim/engine/model"
	"sportsim/engine/season"
	"sportsim/engine/transfer"
	"sportsim/game"
)

func (m *Model) View() string {
	if m.quitting {
		return "Thanks for playing SportSim.\n"
	}
	if m.screen == ScreenNewGame {
		return m.viewNewGame()
	}
	if m.g == nil {
		return "loading..."
	}

	var body string
	switch m.screen {
	case ScreenHome:
		body = m.viewHome()
	case ScreenSquad:
		body = m.viewSquad()
	case ScreenTactics:
		body = m.viewTactics()
	case ScreenTable:
		body = m.viewTable()
	case ScreenFixtures:
		body = m.viewFixtures()
	case ScreenTransfers:
		body = m.viewTransfers()
	case ScreenInbox:
		body = m.viewInbox()
	case ScreenPlayer:
		body = m.viewPlayerDetail()
	case ScreenMatch:
		body = m.viewMatch()
	case ScreenSeasonEnd:
		body = m.viewSeasonEnd()
	}
	return m.header() + "\n" + body + "\n" + m.footer()
}

// ---------------- chrome ----------------

func (m *Model) header() string {
	w := m.g.World
	c := m.g.Club()
	l := m.g.League()

	pos := ""
	if l != nil {
		rows := m.g.Table(l.ID)
		if p := season.Position(rows, c.ID); p > 0 {
			pos = fmt.Sprintf("  %s %s", ordinal(p), l.Name)
		}
	}
	left := stTitle.Render(" "+c.Name) + stMuted.Render(pos)
	right := stMuted.Render(fmt.Sprintf("%s   %s   %s ",
		w.Date.Format(), model.SeasonLabel(w.SeasonYear), transfer.Money(c.TransferBudget)))

	pad := m.width - lenVisible(left) - lenVisible(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

func (m *Model) footer() string {
	keys := "[space] day  [w] to next match  [m] +30 days  [s]quad [t]actics [l]eague [f]ixtures t[r]ansfers [i]nbox  [S]ave  [q]uit"
	switch m.screen {
	case ScreenSquad:
		keys = "[↑↓] move  [enter] profile  [c] offer contract  [x] sell  [space] advance day  [q] back"
	case ScreenTactics:
		keys = "[↑↓] move  [enter] swap two players  [a] auto pick  [ ] [ ] formation  [←→] adjust slider  [q] back"
	case ScreenTable:
		keys = "[←→] change division  [space] advance day  [q] back"
	case ScreenTransfers:
		keys = "[/] search  [↑↓] move  [enter] bid  [v] profile  [q] back"
	case ScreenMatch:
		if m.mv != nil && !m.mv.done {
			keys = "[enter] skip to full time  [+/-] speed  "
		} else {
			keys = "[enter] continue"
		}
	case ScreenSeasonEnd:
		keys = "[any key] continue to the new season"
	}
	line := stBar.Width(max(m.width-2, 20)).Render(keys)
	if m.status != "" {
		st := stStatusOK
		if m.statusErr {
			st = stStatusErr
		}
		return st.Render("  "+m.status) + "\n" + line
	}
	return line
}

// ---------------- new game ----------------

var (
	previewOnce  sync.Once
	previewWorld *model.World
)

func loadPreview() *model.World {
	previewOnce.Do(func() {
		w, err := game.NewWorld(1)
		if err == nil {
			previewWorld = w
		}
	})
	return previewWorld
}

func previewLeagues() []*model.League {
	w := loadPreview()
	if w == nil {
		return nil
	}
	out := make([]*model.League, 0, len(w.Leagues))
	for i := range w.Leagues {
		out = append(out, &w.Leagues[i])
	}
	return out
}

func previewClubs(leagueID uint16) []*model.Club {
	w := loadPreview()
	l := w.League(leagueID)
	if l == nil {
		return nil
	}
	out := make([]*model.Club, 0, len(l.ClubIDs))
	for _, id := range l.ClubIDs {
		out = append(out, w.Club(id))
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Reputation > out[b].Reputation })
	return out
}

func (m *Model) viewNewGame() string {
	var b strings.Builder
	b.WriteString("\n  " + stTitle.Render("SportSim") + stMuted.Render("  —  football management, no pitch required") + "\n\n")

	b.WriteString("  Manager name: " + stBold.Render(m.nameInput+"▏") + "\n\n")

	leagues := previewLeagues()
	if leagues == nil {
		return b.String() + "  " + stBad.Render("Could not load the game database.") + "\n"
	}

	if m.pickStage == 0 {
		b.WriteString(stHeader.Render("  CHOOSE A DIVISION") + "\n")
		for i, l := range leagues {
			line := fmt.Sprintf(" %-16s %-12s tier %d   %2d clubs ", l.Name, l.Country, l.Tier, len(l.ClubIDs))
			if i == m.pickLeague {
				b.WriteString("  " + stSelected.Render(line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		b.WriteString("\n  " + stMuted.Render("[↑↓] choose  [enter] continue  type to set your name  [esc] quit") + "\n")
		return b.String()
	}

	clubs := previewClubs(uint16(m.pickLeague + 1))
	b.WriteString(stHeader.Render(fmt.Sprintf("  CHOOSE A CLUB IN THE %s", strings.ToUpper(leagues[m.pickLeague].Name))) + "\n")
	b.WriteString(stMuted.Render(fmt.Sprintf("  %-26s %5s %10s %12s %10s", "CLUB", "REP", "STADIUM", "BUDGET", "WAGES/WK")) + "\n")

	start := 0
	if m.pickClub > 12 {
		start = m.pickClub - 12
	}
	for i := start; i < len(clubs) && i < start+18; i++ {
		c := clubs[i]
		line := fmt.Sprintf(" %-26s %5d %10s %12s %10s ",
			trunc(c.Name, 26), c.Reputation, comma(int64(c.StadiumCap)),
			transfer.Money(c.TransferBudget), transfer.Money(c.WageBudget))
		if i == m.pickClub {
			b.WriteString("  " + stSelected.Render(line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n  " + stMuted.Render("[↑↓] choose  [enter] start career  [backspace] back") + "\n")
	return b.String()
}

// ---------------- home ----------------

func (m *Model) viewHome() string {
	g := m.g
	c := g.Club()
	var b strings.Builder

	// Next fixture.
	b.WriteString("\n")
	if f := g.NextFixture(); f != nil {
		opp, venue := f.Away, "home"
		if f.Away == c.ID {
			opp, venue = f.Home, "away"
		}
		days := int(f.Date - g.World.Date)
		when := fmt.Sprintf("in %d days", days)
		switch days {
		case 0:
			when = "today"
		case 1:
			when = "tomorrow"
		}
		b.WriteString("  " + stHeader.Render("NEXT MATCH") + "\n")
		b.WriteString(fmt.Sprintf("  %s (%s)  %s  %s\n\n",
			stBold.Render(g.World.ClubName(opp)), venue,
			stMuted.Render(f.Date.Short()), stMuted.Render("— "+when)))
	} else {
		b.WriteString("  " + stMuted.Render("No fixtures remaining this season.") + "\n\n")
	}

	// Mini league table around the managed club.
	if l := g.League(); l != nil {
		rows := g.Table(l.ID)
		pos := season.Position(rows, c.ID)
		b.WriteString("  " + stHeader.Render(strings.ToUpper(l.Name)) + "\n")
		lo := pos - 3
		if lo < 1 {
			lo = 1
		}
		hi := lo + 5
		if hi > len(rows) {
			hi = len(rows)
			lo = max(1, hi-5)
		}
		for i := lo - 1; i < hi; i++ {
			b.WriteString("  " + m.tableRow(i+1, rows[i], rows[i].ClubID == c.ID) + "\n")
		}
		b.WriteString("\n")
	}

	// Club standing and finances.
	b.WriteString("  " + stHeader.Render("CLUB") + "\n")
	wage := g.World.WageBill(c.ID)
	b.WriteString(fmt.Sprintf("  Balance %s    Transfer budget %s    Wages %s of %s per week\n",
		money(c.Balance), money(c.TransferBudget), money(wage), money(c.WageBudget)))
	b.WriteString(fmt.Sprintf("  Squad %d players    Stadium %s    Reputation %d/100    %s\n\n",
		g.World.SquadSize(c.ID), comma(int64(c.StadiumCap)), c.Reputation,
		stMuted.Render(transfer.WindowName(g.World.Date))))

	// Latest news.
	b.WriteString("  " + stHeader.Render("INBOX") + "\n")
	n := len(g.Inbox)
	for i := max(0, n-6); i < n; i++ {
		msg := g.Inbox[i]
		b.WriteString(fmt.Sprintf("  %s  %s\n", stMuted.Render(msg.Date.Short()), trunc(msg.Text, max(m.width-16, 30))))
	}
	if n == 0 {
		b.WriteString("  " + stMuted.Render("Nothing yet.") + "\n")
	}
	return b.String()
}

// ---------------- squad ----------------

func (m *Model) viewSquad() string {
	rows := m.g.Squad()
	var b strings.Builder
	b.WriteString("\n  " + stHeader.Render(fmt.Sprintf(
		"%-3s %-22s %-4s %3s %4s %4s %5s %5s %5s %4s %3s %3s %6s %10s",
		"", "NAME", "POS", "AGE", "RAT", "POT", "FIT", "MOR", "FORM", "APP", "G", "A", "AVG", "VALUE")) + "\n")

	visible := m.height - 8
	if visible < 5 {
		visible = 5
	}
	start := 0
	if m.squadCur >= visible {
		start = m.squadCur - visible + 1
	}

	for i := start; i < len(rows) && i < start+visible; i++ {
		r := rows[i]
		sel := i == m.squadCur

		name := trunc(r.Name, 22)
		if r.Status != "" {
			name = trunc(r.Name, 15) + " " + stBad.Render(trunc(r.Status, 6))
		}
		avg := "  -  "
		if r.AvgRating > 0 {
			avg = fmt.Sprintf("%5.2f", r.AvgRating)
		}
		form := fmt.Sprintf("%+d", r.Form)
		if r.Form == 0 {
			form = "0"
		}

		line := fmt.Sprintf("%-3d %-22s %-4s %3d %4s %4d %5d %5d %5s %4d %3d %3d %6s %10s",
			i+1, name, r.Position, r.Age,
			ratingStyle(r.Rating).Render(fmt.Sprintf("%d", r.Rating)), r.Potential,
			r.Fitness, r.Morale, form, r.Apps, r.Goals, r.Assists, avg,
			transfer.Money(r.Value))

		if sel {
			b.WriteString("  " + stSelected.Render(padVisible(line, m.width-4)) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}

	c := m.g.Club()
	b.WriteString("\n  " + stMuted.Render(fmt.Sprintf(
		"%d players    wage bill %s/wk of %s    transfer budget %s",
		len(rows), transfer.Money(m.g.World.WageBill(c.ID)),
		transfer.Money(c.WageBudget), transfer.Money(c.TransferBudget))) + "\n")
	return b.String()
}

// ---------------- tactics ----------------

func (m *Model) viewTactics() string {
	g := m.g
	c := g.Club()
	t := c.Tactics
	slots := t.Formation.Slots()

	var b strings.Builder
	b.WriteString("\n  " + stHeader.Render("FORMATION ") + stBold.Render(t.Formation.String()) +
		stMuted.Render("   [ and ] to change") + "\n\n")

	nameOf := func(id uint32) (string, int, int, string) {
		p := g.World.Player(id)
		if p == nil {
			return stMuted.Render("(empty)"), 0, 0, ""
		}
		st := ""
		if !p.Available() {
			st = "unavailable"
		}
		return p.Name, int(p.CurrentAbility()), int(p.Fitness), st
	}

	b.WriteString(stHeader.Render("  STARTING ELEVEN") + "\n")
	for i := 0; i < 11; i++ {
		name, rating, fit, st := nameOf(c.Lineup[i])
		mark := " "
		if m.swapFrom == i {
			mark = ">"
		}
		line := fmt.Sprintf("%s %-4s %-24s %3s  fit %3d %s",
			mark, slots[i].String(), trunc(name, 24),
			ratingStyle(rating).Render(fmt.Sprintf("%d", rating)), fit, stBad.Render(st))
		b.WriteString("  " + m.selectable(line, i == m.tacticsCur) + "\n")
	}

	b.WriteString("\n" + stHeader.Render("  SUBSTITUTES") + "\n")
	for i := 0; i < 9; i++ {
		name, rating, fit, _ := nameOf(c.Bench[i])
		mark := " "
		if m.swapFrom == 11+i {
			mark = ">"
		}
		line := fmt.Sprintf("%s %-4s %-24s %3s  fit %3d", mark, "sub", trunc(name, 24),
			ratingStyle(rating).Render(fmt.Sprintf("%d", rating)), fit)
		b.WriteString("  " + m.selectable(line, 11+i == m.tacticsCur) + "\n")
	}

	b.WriteString("\n" + stHeader.Render("  TEAM INSTRUCTIONS") + "\n")
	sliders := []struct {
		name   string
		v      uint8
		lo, hi string
	}{
		{"Mentality", t.Mentality, "defensive", "attacking"},
		{"Tempo", t.Tempo, "patient", "fast"},
		{"Width", t.Width, "narrow", "wide"},
		{"Pressing", t.Pressing, "deep block", "high press"},
		{"Directness", t.Directness, "short", "long ball"},
		{"Line height", t.LineHeight, "deep", "high line"},
		{"Tackling", t.Tackling, "contain", "aggressive"},
	}
	for i, s := range sliders {
		line := fmt.Sprintf("  %-12s %-11s %s %-11s %3d",
			s.name, stMuted.Render(s.lo), bar(int(s.v), 24), stMuted.Render(s.hi), s.v)
		b.WriteString("  " + m.selectable(line, 20+i == m.tacticsCur) + "\n")
	}
	return b.String()
}

func (m *Model) selectable(line string, sel bool) string {
	if sel {
		return stSelected.Render(padVisible(line, m.width-4))
	}
	return line
}

// bar renders a 0-100 value as a gauge.
func bar(v, width int) string {
	filled := v * width / 100
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return stTitle.Render(strings.Repeat("█", filled)) +
		stMuted.Render(strings.Repeat("░", width-filled))
}

// ---------------- league table ----------------

func (m *Model) tableRow(pos int, r season.Row, highlight bool) string {
	form := ""
	for _, ch := range r.Form {
		form += formStyle(ch).Render(string(ch))
	}
	line := fmt.Sprintf("%2d. %-24s %3d %3d %3d %3d %4d %4d %+5d %4d  %s",
		pos, trunc(m.g.World.ClubName(r.ClubID), 24), r.Played, r.Won, r.Drawn, r.Lost,
		r.GoalsFor, r.GoalsAgainst, r.GoalDiff(), r.Points, form)
	if highlight {
		return stBold.Render(line)
	}
	return line
}

func (m *Model) viewTable() string {
	l := m.g.World.League(m.tableLeague)
	if l == nil {
		return "\n  no league selected\n"
	}
	rows := m.g.Table(l.ID)
	var b strings.Builder
	b.WriteString("\n  " + stTitle.Render(l.Name) + stMuted.Render(fmt.Sprintf("  %s, tier %d", l.Country, l.Tier)) + "\n\n")
	b.WriteString("  " + stHeader.Render(fmt.Sprintf("%-28s %3s %3s %3s %3s %4s %4s %5s %4s  %s",
		"", "P", "W", "D", "L", "GF", "GA", "GD", "PTS", "FORM")) + "\n")

	promo, releg := int(l.Promoted), int(l.Relegated)
	for i, r := range rows {
		marker := " "
		switch {
		case i < promo || (l.Tier == 1 && i < 4):
			marker = stGood.Render("│")
		case i >= len(rows)-releg:
			marker = stBad.Render("│")
		}
		b.WriteString(" " + marker + m.tableRow(i+1, r, r.ClubID == m.g.World.HumanClubID) + "\n")
	}

	b.WriteString("\n  " + stHeader.Render("LEADING SCORERS") + "\n")
	for _, s := range m.g.TopScorers(l.ID, 5) {
		b.WriteString(fmt.Sprintf("  %-24s %-24s %2d goals, %d assists\n",
			trunc(s.Name, 24), trunc(s.Club, 24), s.Goals, s.Assists))
	}
	return b.String()
}

// ---------------- fixtures ----------------

func (m *Model) viewFixtures() string {
	g := m.g
	c := g.Club()
	var b strings.Builder
	b.WriteString("\n  " + stTitle.Render("Fixtures and results") + "\n\n")

	var mine []*season.Fixture
	for i := range g.Sched.Fixtures {
		f := &g.Sched.Fixtures[i]
		if f.Home == c.ID || f.Away == c.ID {
			mine = append(mine, f)
		}
	}
	sort.SliceStable(mine, func(a, b int) bool { return mine[a].Date < mine[b].Date })

	visible := m.height - 8
	if visible < 6 {
		visible = 6
	}
	// Centre the list on the next unplayed match.
	next := 0
	for i, f := range mine {
		if !f.Played {
			next = i
			break
		}
	}
	start := max(0, next-visible/2)

	for i := start; i < len(mine) && i < start+visible; i++ {
		f := mine[i]
		opp, venue := g.World.ClubName(f.Away), "H"
		if f.Away == c.ID {
			opp, venue = g.World.ClubName(f.Home), "A"
		}
		if !f.Played {
			b.WriteString(fmt.Sprintf("  %-12s %s  %-26s %s\n",
				f.Date.Short(), venue, trunc(opp, 26), stMuted.Render("—")))
			continue
		}
		ours, theirs := int(f.HomeGoals), int(f.AwayGoals)
		if f.Away == c.ID {
			ours, theirs = theirs, ours
		}
		res, st := "D", stMuted
		switch {
		case ours > theirs:
			res, st = "W", stGood
		case ours < theirs:
			res, st = "L", stBad
		}
		b.WriteString(fmt.Sprintf("  %-12s %s  %-26s %s %s\n",
			f.Date.Short(), venue, trunc(opp, 26),
			st.Render(fmt.Sprintf("%s %d-%d", res, ours, theirs)),
			stMuted.Render(scorerLine(g, f))))
	}
	return b.String()
}

// scorerLine summarises who scored in a played fixture.
func scorerLine(g *game.Game, f *season.Fixture) string {
	if len(f.Goals) == 0 {
		return ""
	}
	parts := make([]string, 0, len(f.Goals))
	for _, gl := range f.Goals {
		if p := g.World.Player(gl.Scorer); p != nil {
			parts = append(parts, fmt.Sprintf("%s %d'", p.Name, gl.Minute))
		}
	}
	return strings.Join(parts, ", ")
}

// ---------------- transfers ----------------

func (m *Model) viewTransfers() string {
	g := m.g
	c := g.Club()
	var b strings.Builder
	b.WriteString("\n  " + stTitle.Render("Transfer market") + "   " +
		stMuted.Render(transfer.WindowName(g.World.Date)) + "\n")
	b.WriteString(fmt.Sprintf("  Budget %s    Wage room %s/wk\n\n",
		money(c.TransferBudget), money(c.WageBudget-g.World.WageBill(c.ID))))

	prompt := m.searchInput
	if m.searchActive {
		prompt += "▏"
	}
	b.WriteString("  " + stHeader.Render("SEARCH ") + stBold.Render(prompt) +
		stMuted.Render("   press / to search by name, enter to bid") + "\n\n")

	if len(m.searchResult) == 0 {
		b.WriteString("  " + stMuted.Render("No players listed. Press / and type a name, then enter.") + "\n")
		return b.String()
	}

	b.WriteString("  " + stHeader.Render(fmt.Sprintf("%-22s %-4s %3s %4s %4s %-20s %10s %9s",
		"NAME", "POS", "AGE", "RAT", "POT", "CLUB", "ASKING", "WAGE")) + "\n")

	visible := m.height - 12
	if visible < 5 {
		visible = 5
	}
	start := 0
	if m.transferCur >= visible {
		start = m.transferCur - visible + 1
	}
	for i := start; i < len(m.searchResult) && i < start+visible; i++ {
		r := m.searchResult[i]
		line := fmt.Sprintf("%-22s %-4s %3d %4s %4d %-20s %10s %9s",
			trunc(r.Name, 22), r.Position, r.Age,
			ratingStyle(r.Rating).Render(fmt.Sprintf("%d", r.Rating)), r.Potential,
			trunc(r.Status, 20), transfer.Money(r.Value), transfer.Money(r.Wage))
		b.WriteString("  " + m.selectable(line, i == m.transferCur) + "\n")
	}
	return b.String()
}

// ---------------- inbox ----------------

func (m *Model) viewInbox() string {
	var b strings.Builder
	b.WriteString("\n  " + stTitle.Render("Inbox") + "\n\n")
	n := len(m.g.Inbox)
	if n == 0 {
		return b.String() + "  " + stMuted.Render("Nothing yet.") + "\n"
	}
	visible := m.height - 8
	if visible < 5 {
		visible = 5
	}
	for i := max(0, n-visible); i < n; i++ {
		msg := m.g.Inbox[i]
		icon, st := "•", stMuted
		switch msg.Kind {
		case "result":
			icon, st = "⚽", stBold
		case "transfer":
			icon, st = "⇄", stTitle
		case "injury":
			icon, st = "✚", stBad
		case "season":
			icon, st = "★", stGood
		case "board":
			icon, st = "▸", stWarn
		}
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			st.Render(icon), stMuted.Render(msg.Date.Short()),
			trunc(msg.Text, max(m.width-16, 30))))
	}
	return b.String()
}

// ---------------- player profile ----------------

func (m *Model) viewPlayerDetail() string {
	p := m.g.World.Player(m.viewPlayer)
	if p == nil {
		return "\n  no player selected\n"
	}
	w := m.g.World
	var b strings.Builder

	b.WriteString("\n  " + stTitle.Render(p.FullName) + "\n")
	pos := make([]string, 0, 3)
	for i := uint8(0); i < p.NumPositions; i++ {
		pos = append(pos, p.Positions[i].String())
	}
	b.WriteString(fmt.Sprintf("  %s   age %d   %s   %s   %d cm, %d kg\n",
		strings.Join(pos, "/"), w.Age(p), w.NationName(p.NationID), w.ClubName(p.ClubID),
		p.HeightCM, p.WeightKG))
	b.WriteString(fmt.Sprintf("  Ability %s of %d potential    Value %s    Wage %s/wk    Contract to %d\n\n",
		ratingStyle(int(p.CurrentAbility())).Render(fmt.Sprintf("%.0f", p.CurrentAbility())),
		p.Potential, transfer.Money(int64(p.ValueEUR)), transfer.Money(int64(p.WageEUR)), p.ContractUntil))

	b.WriteString(fmt.Sprintf("  Fitness %s   Morale %s   Form %+d   Season: %d apps, %d goals, %d assists, %.2f avg\n\n",
		meterStyle(int(p.Fitness)).Render(fmt.Sprintf("%d", p.Fitness)),
		meterStyle(int(p.Morale)).Render(fmt.Sprintf("%d", p.Morale)),
		p.Form, p.Apps, p.Goals, p.Assists, p.AvgRating()))

	// Attributes in three columns.
	b.WriteString(stHeader.Render("  ATTRIBUTES") + "\n")
	perCol := (model.NumAttr + 2) / 3
	for row := 0; row < perCol; row++ {
		line := "  "
		for col := 0; col < 3; col++ {
			i := col*perCol + row
			if i >= model.NumAttr {
				continue
			}
			v := int(p.Attr[i])
			line += fmt.Sprintf("%-17s %s   ", model.AttrNames[i],
				ratingStyle(v).Render(fmt.Sprintf("%2d", v)))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n  " + stMuted.Render("[esc] back") + "\n")
	return b.String()
}

// ---------------- season end ----------------

func (m *Model) viewSeasonEnd() string {
	var b strings.Builder
	b.WriteString("\n  " + stTitle.Render("Season review") + "\n\n")
	visible := m.height - 8
	for i, h := range m.seasonReport {
		if i >= visible {
			b.WriteString("  " + stMuted.Render(fmt.Sprintf("...and %d more", len(m.seasonReport)-i)) + "\n")
			break
		}
		st := stMuted
		switch {
		case strings.Contains(h, "win the"):
			st = stGood
		case strings.Contains(h, "relegated"):
			st = stBad
		case strings.Contains(h, "promoted"):
			st = stTitle
		}
		b.WriteString("  " + st.Render(h) + "\n")
	}
	return b.String()
}

// ---------------- text helpers ----------------

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func centre(s string, w int) string {
	r := []rune(s)
	if len(r) >= w {
		return s
	}
	left := (w - len(r)) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", w-len(r)-left)
}

// lenVisible counts printable width, ignoring ANSI escape sequences.
func lenVisible(s string) int {
	n, esc := 0, false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc && r == 'm':
			esc = false
		case !esc:
			n++
		}
	}
	return n
}

// padVisible pads a styled string to a printable width.
func padVisible(s string, w int) string {
	if d := w - lenVisible(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func comma(v int64) string {
	s := fmt.Sprint(v)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func money(v int64) string {
	s := transfer.Money(v)
	if v < 0 {
		return stBad.Render(s)
	}
	return s
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
