package tui

// The transfer market screen.
//
// It is built around one idea: the filters are always on screen and always
// applied, so the manager narrows a market of six thousand players down rather
// than having to guess a name before seeing anything. Focus moves between the
// results list and the filter fields with tab, which is what keeps the single
// letter bindings of the rest of the interface working while a filter is being
// typed into.

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"sportsim/engine/model"
	"sportsim/engine/transfer"
	"sportsim/game"
)

// searchLimit caps how many results are held at once. The count of everything
// that matched is reported separately, so the manager still learns how wide
// their net is even when they are only shown the top of it.
const searchLimit = 300

// filterField identifies which control on the filter bar has focus.
// filterNone means the results list has it.
type filterField int

const (
	filterNone filterField = iota
	filterName
	filterPos
	filterAge
	filterRating
	filterFee
	filterScope
	numFilterFields
)

// filterNames label the controls. Index matches the constants above. The fee is
// entered in millions, which the label has to say or the field is a guess.
var filterNames = [numFilterFields]string{"", "NAME", "POS", "MAX AGE", "MIN RAT", "MAX FEE €M", "SHOW"}

// market is the transfer screen's own state: the filters as the manager has set
// them, and the results they currently produce.
type market struct {
	filter  game.SearchFilter
	focus   filterField
	results []game.Target
	matched int
	cur     int

	// Text being typed into the numeric fields is kept as typed rather than
	// parsed back and forth, so a half-entered "2" on the way to "25" does not
	// snap the field to something the manager did not ask for.
	ageText, ratingText, feeText string

	// bid is the open negotiation panel, or nil when none is.
	bid *bidPanel
}

// newMarket starts the screen showing the whole market, best players first.
func newMarket() *market {
	return &market{filter: game.SearchFilter{Position: game.AnyPosition}}
}

// ---------------- filters ----------------

// apply reruns the search. It is called on every edit, which is affordable
// because pricing the market is a single linear pass.
func (mk *market) apply(g *game.Game) {
	mk.filter.MaxAge = atoiOr(mk.ageText, 0)
	mk.filter.MinRating = atoiOr(mk.ratingText, 0)
	mk.filter.MaxFee = int64(atoiOr(mk.feeText, 0)) * 1_000_000
	mk.results, mk.matched = g.Search(mk.filter, searchLimit)
	if mk.cur >= len(mk.results) {
		mk.cur = max(0, len(mk.results)-1)
	}
}

// clear returns every filter to its default, which is "show me everybody".
func (mk *market) clear() {
	scope, sortBy := mk.filter.Scope, mk.filter.Sort
	mk.filter = game.SearchFilter{Position: game.AnyPosition, Scope: scope, Sort: sortBy}
	mk.ageText, mk.ratingText, mk.feeText = "", "", ""
	mk.cur = 0
}

// text returns the current contents of a field as the manager typed it.
func (mk *market) text(f filterField) string {
	switch f {
	case filterName:
		return mk.filter.Name
	case filterPos:
		if mk.filter.Position >= model.NumPos {
			return "any"
		}
		return mk.filter.Position.String()
	case filterAge:
		return mk.ageText
	case filterRating:
		return mk.ratingText
	case filterFee:
		return mk.feeText
	case filterScope:
		return mk.filter.Scope.String()
	}
	return ""
}

// typing reports whether a field takes typed characters rather than being
// cycled with the arrow keys.
func (f filterField) typing() bool {
	return f == filterName || f == filterAge || f == filterRating || f == filterFee
}

func (mk *market) typeInto(f filterField, s string) {
	switch f {
	case filterName:
		if len([]rune(mk.filter.Name)) < 24 {
			mk.filter.Name += s
		}
	case filterAge:
		mk.ageText = appendDigit(mk.ageText, s, 2)
	case filterRating:
		mk.ratingText = appendDigit(mk.ratingText, s, 2)
	case filterFee:
		mk.feeText = appendDigit(mk.feeText, s, 4)
	}
}

func (mk *market) backspace(f filterField) {
	switch f {
	case filterName:
		mk.filter.Name = chop(mk.filter.Name)
	case filterAge:
		mk.ageText = chop(mk.ageText)
	case filterRating:
		mk.ratingText = chop(mk.ratingText)
	case filterFee:
		mk.feeText = chop(mk.feeText)
	}
}

// cycle steps a field by one. Position and scope walk their lists; the numeric
// fields nudge, so the arrow keys do something sensible everywhere.
func (mk *market) cycle(f filterField, delta int) {
	switch f {
	case filterPos:
		// The position list runs GK..ST with "any" as one more stop past the end,
		// so the arrow keys reach every option without a separate clear.
		p := int(mk.filter.Position)
		if p > int(model.NumPos) {
			p = int(model.NumPos)
		}
		p += delta
		if p < 0 {
			p = int(model.NumPos)
		} else if p > int(model.NumPos) {
			p = 0
		}
		mk.filter.Position = model.Pos(p)
	case filterScope:
		s := int(mk.filter.Scope) + delta
		if s < 0 {
			s = int(game.NumScopes) - 1
		} else if s >= int(game.NumScopes) {
			s = 0
		}
		mk.filter.Scope = game.Scope(s)
	case filterAge:
		mk.ageText = nudge(mk.ageText, delta, 16, 45)
	case filterRating:
		mk.ratingText = nudge(mk.ratingText, delta, 40, 99)
	case filterFee:
		mk.feeText = nudge(mk.feeText, delta*5, 5, 500)
	}
}

// ---------------- keys ----------------

func (m *Model) keyTransfers(key string) (tea.Model, tea.Cmd) {
	mk := m.mk
	if mk.bid != nil {
		return m.keyBid(key)
	}

	// Only the two keys that move focus are read before it is consulted. Every
	// other binding on this screen is a letter, and a letter typed into a filter
	// field is a letter, not a command — checking them first would make "Nico"
	// unenterable, since the n would be read as the squad-needs shortcut.
	switch key {
	case "tab":
		mk.focus = (mk.focus + 1) % numFilterFields
		return m, nil
	case "shift+tab":
		mk.focus = (mk.focus + numFilterFields - 1) % numFilterFields
		return m, nil
	}

	if mk.focus != filterNone {
		return m.keyFilter(key)
	}

	switch key {
	case "/":
		mk.focus = filterName
		return m, nil
	case "n":
		// The one keystroke answer to "who do I actually need".
		mk.filter.Position = m.g.WeakestPosition()
		mk.filter.Sort = game.SortImproves
		mk.apply(m.g)
		m.setStatus(fmt.Sprintf("Showing %s, where your squad is thinnest.",
			mk.filter.Position), false)
		return m, nil
	case "o":
		mk.filter.Sort = (mk.filter.Sort + 1) % game.NumSorts
		mk.apply(m.g)
		return m, nil
	case "c":
		mk.clear()
		mk.apply(m.g)
		m.setStatus("Filters cleared.", false)
		return m, nil
	case "*":
		if mk.cur < len(mk.results) {
			r := mk.results[mk.cur]
			if m.g.ToggleShortlist(r.PlayerID) {
				m.setStatus(r.Name+" added to your shortlist.", false)
			} else {
				m.setStatus(r.Name+" removed from your shortlist.", false)
			}
			mk.apply(m.g)
		}
		return m, nil
	case "v":
		if mk.cur < len(mk.results) {
			m.viewPlayer = mk.results[mk.cur].PlayerID
			m.prev = ScreenTransfers
			m.screen = ScreenPlayer
		}
		return m, nil
	case "enter":
		if mk.cur < len(mk.results) {
			m.openBid(mk.results[mk.cur].PlayerID)
		}
		return m, nil
	case "left", "[":
		mk.filter.Sort = (mk.filter.Sort + game.NumSorts - 1) % game.NumSorts
		mk.apply(m.g)
		return m, nil
	case "right", "]":
		mk.filter.Sort = (mk.filter.Sort + 1) % game.NumSorts
		mk.apply(m.g)
		return m, nil
	}
	return m.keyList(key, &mk.cur, len(mk.results))
}

// keyFilter edits whichever filter has focus.
func (m *Model) keyFilter(key string) (tea.Model, tea.Cmd) {
	mk := m.mk
	switch key {
	case "enter", "esc":
		mk.focus = filterNone
		return m, nil
	case "backspace":
		mk.backspace(mk.focus)
	case "left", "[":
		mk.cycle(mk.focus, -1)
	case "right", "]":
		mk.cycle(mk.focus, +1)
	case "up", "down", "pgup", "pgdown":
		// Moving through results while a filter is focused is natural enough that
		// refusing it would only be annoying.
		return m.keyList(key, &mk.cur, len(mk.results))
	default:
		// Counted in runes, not bytes: half the names in the database carry an
		// accent, and a byte-length test would refuse to let them be typed.
		if len([]rune(key)) == 1 && mk.focus.typing() {
			mk.typeInto(mk.focus, key)
		} else {
			return m, nil
		}
	}
	mk.apply(m.g)
	return m, nil
}

// ---------------- bidding ----------------

// bidPanel is an offer being composed. It opens pre-filled with terms that
// would be accepted, so signing a player the manager has already decided on is
// still two keystrokes, while the numbers remain there to be argued down.
type bidPanel struct {
	q    *game.Quote
	fee  int64
	wage uint32
	yrs  int
	row  int // 0 fee, 1 wage, 2 years
}

const (
	numBidRows = 3
	// bidPanelHeight is how many lines the panel occupies once bordered, which
	// the list has to give back so the whole screen still fits.
	bidPanelHeight = 14
)

func (m *Model) openBid(playerID uint32) {
	q := m.g.Quote(playerID)
	if q == nil {
		m.setStatus("No such player.", true)
		return
	}
	if q.Blocked != "" {
		m.setStatus(q.Blocked, true)
		return
	}
	m.mk.bid = &bidPanel{q: q, fee: q.Fee, wage: q.Wage, yrs: q.Years}
}

func (m *Model) keyBid(key string) (tea.Model, tea.Cmd) {
	bp := m.mk.bid
	switch key {
	case "esc", "q":
		m.mk.bid = nil
		return m, nil
	case "enter":
		ok, msg := m.g.Bid(bp.q.PlayerID, bp.fee, bp.wage, bp.yrs)
		m.setStatus(msg, !ok)
		if ok {
			m.mk.bid = nil
			m.mk.apply(m.g)
		}
		return m, nil
	case "left", "[", "-":
		bp.adjust(-1)
		return m, nil
	case "right", "]", "+", "=":
		bp.adjust(+1)
		return m, nil
	}
	return m.keyList(key, &bp.row, numBidRows)
}

// adjust steps the focused term. The fee and wage move in proportion to what is
// being asked, so haggling over a hundred million takes as many presses as
// haggling over a hundred thousand.
func (bp *bidPanel) adjust(dir int) {
	switch bp.row {
	case 0:
		if bp.q.AskingPrice == 0 {
			return // a free agent has no fee to argue over
		}
		step := bp.q.AskingPrice / 40
		if step < 25_000 {
			step = 25_000
		}
		bp.fee = clamp64(bp.fee+int64(dir)*step, 0, bp.q.AskingPrice*2)
	case 1:
		step := int64(bp.q.WageDemand) / 20
		if step < 500 {
			step = 500
		}
		bp.wage = uint32(clamp64(int64(bp.wage)+int64(dir)*step, 0, bp.q.WageDemand*3))
	case 2:
		bp.yrs = int(clamp64(int64(bp.yrs+dir), 1, 5))
	}
}

// ---------------- view ----------------

func (m *Model) viewTransfers() string {
	g, mk := m.g, m.mk
	c := g.Club()
	var b strings.Builder

	b.WriteString("\n  " + stTitle.Render("Transfer market") + "   " +
		stMuted.Render(transfer.WindowName(g.World.Date)) + "\n")
	b.WriteString(fmt.Sprintf("  Budget %s    Wage room %s/wk    %s\n\n",
		money(c.TransferBudget), money(c.WageBudget-g.World.WageBill(c.ID)),
		stMuted.Render(fmt.Sprintf("%d shortlisted", g.ShortlistSize()))))

	b.WriteString("  " + mk.filterBar() + "\n")
	b.WriteString("  " + stMuted.Render(fmt.Sprintf("sorted by %s  ·  %s",
		mk.filter.Sort, mk.countLine())) + "\n\n")

	b.WriteString("  " + stHeader.Render(fmt.Sprintf("%-20s %-11s %3s %4s %4s %6s %-18s %9s %8s",
		"NAME", "POSITION", "AGE", "RAT", "POT", "FIT", "CLUB", "ASKING", "WAGE")) + "\n")

	if len(mk.results) == 0 {
		b.WriteString("\n  " + stMuted.Render("Nobody matches. Press [c] to clear the filters.") + "\n")
		return b.String()
	}

	// The bid panel is drawn below the list rather than over it, so the list is
	// shortened to make room: the manager keeps sight of the alternatives they
	// are choosing between while they compose the offer.
	visible := m.height - 14
	if mk.bid != nil {
		visible -= bidPanelHeight
	}
	if visible < 3 {
		visible = 3
	}
	start := 0
	if mk.cur >= visible {
		start = mk.cur - visible + 1
	}
	for i := start; i < len(mk.results) && i < start+visible; i++ {
		b.WriteString("  " + m.targetLine(mk.results[i], i == mk.cur) + "\n")
	}

	if mk.bid != nil {
		return b.String() + "\n" + indent(mk.bid.view(), 4)
	}
	return b.String()
}

// targetLine renders one player. The row is styled before it is selected, so
// that the affordability and improvement cues survive being highlighted.
func (m *Model) targetLine(r game.Target, selected bool) string {
	mark := " "
	if r.Shortlisted {
		mark = "★"
	}

	fee := transfer.Money(r.Fee)
	if r.FreeAgent {
		fee = "free"
	}

	club := trunc(r.Club, 18)
	if r.Expiring {
		club = trunc(r.Club, 16) + " ⧗" // deal running out, so cheap this summer
	}

	fit := "  –"
	if r.Improves > 0 {
		fit = fmt.Sprintf("%+d %s", r.Improves, r.ImprovesAt)
	}

	line := fmt.Sprintf("%s%-19s %-11s %3d %4s %4d %6s %-18s %9s %8s",
		mark, trunc(r.Name, 19), trunc(r.Positions, 11), r.Age,
		ratingStyle(r.Rating).Render(fmt.Sprintf("%d", r.Rating)), r.Potential,
		fitStyle(r.Improves).Render(fit), club,
		affordStyle(r.Affordable).Render(fmt.Sprintf("%9s", fee)),
		transfer.Money(r.Wage))
	return m.selectable(line, selected)
}

// filterWidths sizes each control to its longest value, so the bar does not
// reflow as the manager types.
var filterWidths = [numFilterFields]int{
	filterName: 14, filterPos: 4, filterAge: 3, filterRating: 3,
	filterFee: 4, filterScope: 14,
}

// filterBar draws every filter, whether it is set or not, so that the manager
// can see what the market can be narrowed by without having to be told. Fields
// that are typed into are bracketed and take a cursor; fields that cycle
// through a list are drawn between arrows, so the two behave visibly
// differently before either is touched.
func (mk *market) filterBar() string {
	parts := make([]string, 0, numFilterFields)
	for f := filterName; f < numFilterFields; f++ {
		val := mk.text(f)
		width := filterWidths[f]
		focused := f == mk.focus

		shown := val
		if shown == "" {
			shown = "any"
		}
		shown = trunc(shown, width)
		if focused && f.typing() {
			shown = trunc(val, width-1) + "▏"
		}

		padded := shown + strings.Repeat(" ", max(width-lenVisible(shown), 0))
		box := "[" + padded + "]"
		if !f.typing() {
			box = "‹" + padded + "›"
		}

		label := filterNames[f]
		switch {
		case focused:
			box, label = stSelected.Render(box), stTitle.Render(label)
		case val != "" && val != "any" && val != game.ScopeAll.String():
			box, label = stBold.Render(box), stHeader.Render(label)
		default:
			box, label = stMuted.Render(box), stHeader.Render(label)
		}
		parts = append(parts, label+" "+box)
	}
	return strings.Join(parts, "  ")
}

// countLine says how much of the market is on screen, so the manager can tell a
// filter that found nothing from one that found more than fits.
func (mk *market) countLine() string {
	if mk.matched > len(mk.results) {
		return fmt.Sprintf("showing the top %d of %d matches", len(mk.results), mk.matched)
	}
	if mk.matched == 1 {
		return "1 player"
	}
	return fmt.Sprintf("%d players", mk.matched)
}

// view draws the negotiation panel over the list.
func (bp *bidPanel) view() string {
	q := bp.q
	var b strings.Builder

	b.WriteString(stTitle.Render(fmt.Sprintf("Bid for %s", q.Name)) +
		stMuted.Render(fmt.Sprintf("  %s · %d · rated %d, potential %d",
			q.Positions, q.Age, q.Rating, q.Potential)) + "\n\n")

	if q.AskingPrice > 0 {
		b.WriteString(fmt.Sprintf("  %-14s %s\n",
			trunc(q.Club, 14)+" want", stBold.Render(transfer.Money(q.AskingPrice))))
	} else {
		b.WriteString("  " + stMuted.Render("Free agent — no fee to pay") + "\n")
	}
	b.WriteString(fmt.Sprintf("  %-14s %s\n\n", "He wants",
		stBold.Render(transfer.Money(q.WageDemand)+"/wk")))

	rows := []struct {
		label string
		value string
		ok    bool
	}{
		{"Fee", transfer.Money(bp.fee), bp.fee <= q.Budget},
		{"Wage", transfer.Money(int64(bp.wage)) + "/wk", int64(bp.wage) <= q.WageRoom},
		{"Years", strconv.Itoa(bp.yrs), true},
	}
	for i, r := range rows {
		val := fmt.Sprintf("%12s", r.value)
		if !r.ok {
			val = stBad.Render(val)
		}
		line := fmt.Sprintf("  %-7s %s   ", r.label, val)
		if i == bp.row {
			line = stSelected.Render(fmt.Sprintf("  %-7s %12s   ", r.label, r.value))
			if !r.ok {
				line += stBad.Render(" over budget")
			}
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + stMuted.Render(fmt.Sprintf("  budget %s · wage room %s/wk",
		transfer.Money(q.Budget), transfer.Money(q.WageRoom))) + "\n")
	b.WriteString(stMuted.Render("  [↑↓] field  [←→] adjust  [enter] submit  [esc] cancel"))

	return stPanel.Render(b.String())
}

// ---------------- helpers ----------------

// fitStyle colours how much a signing would improve the side.
func fitStyle(gain int) lipgloss.Style {
	switch {
	case gain >= 5:
		return stGood.Bold(true)
	case gain > 0:
		return stGood
	default:
		return stMuted
	}
}

// affordStyle marks a fee the club cannot currently meet. Such a player is
// still shown rather than hidden: the manager may be planning a sale to fund
// the move, and a market that silently omitted them would mislead.
func affordStyle(ok bool) lipgloss.Style {
	if ok {
		return stBold
	}
	return stBad
}

func atoiOr(s string, fallback int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return fallback
}

func appendDigit(s, r string, maxLen int) string {
	if len(r) != 1 || r[0] < '0' || r[0] > '9' || len(s) >= maxLen {
		return s
	}
	return s + r
}

func chop(s string) string {
	if r := []rune(s); len(r) > 0 {
		return string(r[:len(r)-1])
	}
	return s
}

// nudge steps a numeric field. From empty the first press lands at whichever
// end of the range it was heading for, rather than stepping off zero; stepping
// back off the near end clears the field, which is how a filter is turned off
// without reaching for backspace.
func nudge(s string, delta, lo, hi int) string {
	v, err := strconv.Atoi(s)
	if err != nil {
		if delta < 0 {
			return strconv.Itoa(hi)
		}
		return strconv.Itoa(lo)
	}
	switch v += delta; {
	case v < lo:
		return ""
	case v > hi:
		v = hi
	}
	return strconv.Itoa(v)
}

func clamp64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// indent shifts a block of text right, for laying a panel over a list.
func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
