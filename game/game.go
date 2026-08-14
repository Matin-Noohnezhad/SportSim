// Package game is the façade every frontend talks to.
//
// It owns the game state and exposes the complete set of manager actions and
// queries as plain method calls over plain structs. Nothing here knows whether
// it is being driven by a terminal, an HTTP handler or a mobile app, which is
// what allows a second frontend to be added without touching the simulation.
package game

import (
	"fmt"
	"math"
	"sort"

	"sportsim/assets"
	"sportsim/engine/dev"
	"sportsim/engine/match"
	"sportsim/engine/model"
	"sportsim/engine/rng"
	"sportsim/engine/season"
	"sportsim/engine/transfer"
)

// Game is a running career.
type Game struct {
	World *model.World
	Sched *season.Schedule
	Inbox []Message

	rng *rng.R
}

// seasonWrapDays is how long the final tables stay on screen after the last
// fixture before the world rolls into the next campaign.
const seasonWrapDays = 3

// Message is one item of news for the manager's inbox.
type Message struct {
	Date model.Date
	Kind string // "result", "transfer", "injury", "board", "season"
	Text string
}

// DayReport is what happened on a single simulated day.
type DayReport struct {
	Date        model.Date
	HumanResult *match.Result // nil if the managed club did not play
	Results     []*season.Fixture
	News        []string
	SeasonEnded bool
	Outcome     *season.Outcome
}

// NewWorld loads the embedded database into a fresh, unstarted world.
func NewWorld(seed uint64) (*model.World, error) {
	d, err := assets.Load()
	if err != nil {
		return nil, fmt.Errorf("loading game database: %w", err)
	}
	w := &model.World{
		Players: d.Players, Clubs: d.Clubs, Leagues: d.Leagues, Nations: d.Nations,
		SeasonYear: 2026,
		Date:       model.NewDate(2026, 7, 1),
		Seed:       seed,
	}
	for i := range w.Players {
		p := &w.Players[i]
		p.Fitness, p.Sharpness, p.Morale, p.Form = 100, 50, 72, 0
	}
	// A few clubs ship with squads too small to field a side; top them up before
	// anyone tries to pick a team.
	season.EnsureSquads(w, rng.New(seed^0x5EED))
	return w, nil
}

// New starts a career managing the given club.
func New(managerName string, clubID uint16, seed uint64) (*Game, error) {
	w, err := NewWorld(seed)
	if err != nil {
		return nil, err
	}
	w.ManagerName = managerName
	w.HumanClubID = clubID
	if c := w.Club(clubID); c != nil {
		c.IsHuman = true
	} else {
		return nil, fmt.Errorf("no club with id %d", clubID)
	}

	g := &Game{World: w, rng: rng.New(seed)}
	g.Sched = season.Generate(w, w.SeasonYear, g.rng)
	g.post("board", fmt.Sprintf("Welcome to %s, %s. The board expects steady progress this season.",
		w.ClubName(clubID), managerName))
	return g, nil
}

func (g *Game) post(kind, text string) {
	g.Inbox = append(g.Inbox, Message{Date: g.World.Date, Kind: kind, Text: text})
	// The inbox is a rolling log, not an archive.
	if len(g.Inbox) > 400 {
		g.Inbox = g.Inbox[len(g.Inbox)-400:]
	}
}

// Club returns the managed club.
func (g *Game) Club() *model.Club { return g.World.HumanClub() }

// League returns the division the managed club plays in.
func (g *Game) League() *model.League {
	if c := g.Club(); c != nil {
		return g.World.League(c.LeagueID)
	}
	return nil
}

// ---------------------------------------------------------------- day loop

// AdvanceDay simulates one day and reports what happened.
func (g *Game) AdvanceDay() DayReport {
	w := g.World
	rep := DayReport{Date: w.Date}

	// 1. Matches scheduled for today.
	played := map[uint32]bool{}
	for _, i := range g.Sched.On(w.Date) {
		f := &g.Sched.Fixtures[i]
		if f.Played {
			continue
		}
		res := g.playFixture(f)
		rep.Results = append(rep.Results, f)
		for _, ln := range res.Lines {
			played[ln.PlayerID] = true
		}
		if f.Home == w.HumanClubID || f.Away == w.HumanClubID {
			rep.HumanResult = res
			g.post("result", g.describeResult(f))
		}
	}

	// 2. Daily recovery, healing and sharpness decay.
	for i := range w.Players {
		p := &w.Players[i]
		if p.Potential == 0 {
			continue // retired
		}
		wasInjured := p.InjuryDays > 0
		dev.Daily(g.rng, p, played[p.ID])
		if wasInjured && p.InjuryDays == 0 && p.ClubID == w.HumanClubID {
			g.post("injury", fmt.Sprintf("%s has recovered and is available for selection.", p.Name))
		}
	}

	// 3. Wages, paid weekly on Mondays.
	if w.Date.Weekday() == 1 {
		g.payWages()
	}

	// 4. Training and development, applied monthly.
	if w.Date.Day() == 1 {
		g.develop()
	}

	// 5. The transfer market.
	if news := transfer.RunAI(w, g.rng, 6); len(news) > 0 {
		rep.News = append(rep.News, news...)
		for _, n := range news {
			g.post("transfer", n)
		}
	}

	// 6. Move the clock on. The season is not wrapped up the instant the last
	// whistle blows: a few days are left on the calendar so the manager can
	// look over the final tables and scoring charts before the summer begins.
	w.Date = w.Date.AddDays(1)
	if g.Sched.Complete() && w.Date > g.Sched.LastDate()+seasonWrapDays {
		rep.SeasonEnded = true
		rep.Outcome = g.endSeason()
	}
	return rep
}

// playFixture simulates one match and folds the result into the world.
func (g *Game) playFixture(f *season.Fixture) *match.Result {
	w := g.World
	home, away := w.Club(f.Home), w.Club(f.Away)
	hs := g.buildSide(f.Home)
	as := g.buildSide(f.Away)

	att := attendance(w, home, away, g.rng)
	// Deriving the generator per fixture keeps a match reproducible regardless
	// of what else happened that day.
	r := rng.Derive(w.Seed, uint64(f.Date)*100003+uint64(f.Home)*397+uint64(f.Away))
	res := match.Sim(r, hs, as, att)

	f.Played = true
	f.HomeGoals = uint8(res.HomeGoals)
	f.AwayGoals = uint8(res.AwayGoals)
	f.Attendance = att

	for _, ev := range res.Events {
		if ev.Type == match.EvGoal || ev.Type == match.EvPenaltyScored {
			f.Goals = append(f.Goals, season.Goal{
				Minute:  ev.Minute,
				Away:    ev.Team == 1,
				Penalty: ev.Type == match.EvPenaltyScored,
				Scorer:  ev.Player,
				Assist:  ev.Other,
			})
		}
	}

	// Season tallies, form and morale.
	homeWon := res.HomeGoals > res.AwayGoals
	drew := res.HomeGoals == res.AwayGoals
	for _, ln := range res.Lines {
		p := w.Player(ln.PlayerID)
		if p == nil {
			continue
		}
		dev.AfterMatch(p, int(ln.Minutes), ln.Goals, ln.Assists, ln.Rating, ln.Yellow, ln.Red, ln.Injury)
		won := (ln.Team == 0) == homeWon && !drew
		dev.MoraleForResult(p, won, drew)

		if ln.Injury > 0 && p.ClubID == w.HumanClubID {
			g.post("injury", fmt.Sprintf("%s picked up an injury and will be out for %d days.", p.Name, ln.Injury))
		}
	}

	// Serving a suspension is counted per match, not per day.
	g.tickSuspensions(f.Home)
	g.tickSuspensions(f.Away)

	// Gate receipts.
	if home != nil {
		home.Balance += int64(att) * int64(home.TicketPrice)
	}
	return res
}

// tickSuspensions counts down bans for a club's players who missed this match.
func (g *Game) tickSuspensions(clubID uint16) {
	for i := range g.World.Players {
		p := &g.World.Players[i]
		if p.ClubID == clubID && p.Suspension > 0 {
			p.Suspension--
		}
	}
}

// buildSide prepares a club for kickoff, using the manager's chosen eleven when
// one has been set and is still legal, and the best available side otherwise.
func (g *Game) buildSide(clubID uint16) *match.Side {
	w := g.World
	c := w.Club(clubID)
	squad := w.Squad(clubID)

	if c.IsHuman {
		if lineup, bench, ok := g.explicitLineup(c, squad); ok {
			return match.NewSide(clubID, c.Name, c.Reputation, c.Tactics, lineup, bench)
		}
	}
	lineup, bench := match.AutoPick(squad, c.Tactics)
	return match.NewSide(clubID, c.Name, c.Reputation, c.Tactics, lineup, bench)
}

// explicitLineup resolves the manager's saved selection, rejecting it if any
// chosen player is now injured, suspended or sold.
func (g *Game) explicitLineup(c *model.Club, squad []*model.Player) ([11]*model.Player, []*model.Player, bool) {
	var lineup [11]*model.Player
	used := map[uint32]bool{}
	for i, id := range c.Lineup {
		if id == 0 {
			return lineup, nil, false
		}
		p := g.World.Player(id)
		if p == nil || p.ClubID != c.ID || !p.Available() || used[id] {
			return lineup, nil, false
		}
		used[id] = true
		lineup[i] = p
	}
	bench := make([]*model.Player, 0, 9)
	for _, id := range c.Bench {
		if id == 0 {
			continue
		}
		if p := g.World.Player(id); p != nil && p.ClubID == c.ID && p.Available() && !used[id] {
			used[id] = true
			bench = append(bench, p)
		}
	}
	// Top the bench up with whoever is left, so substitutions remain possible.
	for _, p := range squad {
		if len(bench) >= 9 {
			break
		}
		if !used[p.ID] && p.Available() {
			used[p.ID] = true
			bench = append(bench, p)
		}
	}
	return lineup, bench, true
}

// attendance estimates a crowd from stadium size, the visitors' pull and how
// the home side is doing.
func attendance(w *model.World, home, away *model.Club, r *rng.R) uint32 {
	if home == nil {
		return 0
	}
	fill := 0.58 + float64(home.Reputation)/280
	if away != nil {
		fill += float64(away.Reputation) / 900 // a glamour tie sells more tickets
	}
	fill += r.Norm(0, 0.06)
	fill = math.Max(0.30, math.Min(fill, 1.0))
	return uint32(float64(home.StadiumCap) * fill)
}

// payWages debits every club's weekly wage bill.
func (g *Game) payWages() {
	w := g.World
	bill := make(map[uint16]int64, len(w.Clubs))
	for i := range w.Players {
		p := &w.Players[i]
		if p.ClubID != 0 {
			bill[p.ClubID] += int64(p.WageEUR)
		}
	}
	for i := range w.Clubs {
		c := &w.Clubs[i]
		c.Balance -= bill[c.ID]
		if c.IsHuman && c.Balance < 0 {
			g.post("board", fmt.Sprintf("The club is in the red: %s. The board is concerned.",
				transfer.Money(c.Balance)))
		}
	}
}

// develop runs a month of training across every active player.
func (g *Game) develop() {
	w := g.World
	// Minutes available so far this season set the benchmark for game time.
	maxMinutes := 0.0
	for i := range w.Players {
		if m := float64(w.Players[i].MinutesSum); m > maxMinutes {
			maxMinutes = m
		}
	}
	if maxMinutes < 1 {
		maxMinutes = 1
	}

	for i := range w.Players {
		p := &w.Players[i]
		if p.Potential == 0 {
			continue
		}
		facilities := uint8(50)
		if c := w.Club(p.ClubID); c != nil {
			facilities = c.TrainingFacilities
		}
		before := p.CurrentAbility()
		dev.Monthly(g.rng, p, w.Age(p), dev.Context{
			TrainingFacilities: facilities,
			MinutesShare:       float64(p.MinutesSum) / maxMinutes,
		})
		after := p.CurrentAbility()

		if p.ClubID == w.HumanClubID && after-before >= 1.5 {
			g.post("board", fmt.Sprintf("%s has been training exceptionally well (%.0f → %.0f).",
				p.Name, before, after))
		}
		p.ValueEUR = dev.Value(p, w.Age(p))
	}
}

// endSeason rolls the world into the next campaign.
func (g *Game) endSeason() *season.Outcome {
	w := g.World
	out := season.Rollover(w, g.Sched, g.rng)

	for _, h := range out.Headlines {
		g.post("season", h)
	}
	w.SeasonYear++
	w.Date = model.NewDate(w.SeasonYear, 7, 1)
	g.Sched = season.Generate(w, w.SeasonYear, g.rng)

	// A manager's lineup rarely survives a summer intact; clear it so the side
	// is auto-picked until the manager sets a new one.
	if c := g.Club(); c != nil {
		c.Lineup = [11]uint32{}
		c.Bench = [9]uint32{}
	}
	g.post("season", fmt.Sprintf("The %s season begins.", model.SeasonLabel(w.SeasonYear)))
	return out
}

// describeResult renders a one-line summary of the managed club's match.
func (g *Game) describeResult(f *season.Fixture) string {
	w := g.World
	verdict := "draw with"
	us, them := f.Home, f.Away
	ug, tg := int(f.HomeGoals), int(f.AwayGoals)
	if f.Away == w.HumanClubID {
		us, them = f.Away, f.Home
		ug, tg = tg, ug
	}
	switch {
	case ug > tg:
		verdict = "beat"
	case ug < tg:
		verdict = "lose to"
	}
	_ = us
	return fmt.Sprintf("%s %s %s %d-%d.", w.ClubName(w.HumanClubID), verdict, w.ClubName(them), ug, tg)
}

// ---------------------------------------------------------------- queries

// SquadRow is one player as the squad screen shows them.
type SquadRow struct {
	PlayerID  uint32
	Name      string
	Position  string
	Age       int
	Rating    int
	Potential int
	Fitness   int
	Morale    int
	Form      int
	Nation    string
	Value     int64
	Wage      int64
	Contract  int
	Apps      int
	Goals     int
	Assists   int
	AvgRating float64
	Status    string // "", "Injured (12d)", "Suspended (2)"
	Starting  bool
}

// Squad returns the managed club's players, strongest first.
func (g *Game) Squad() []SquadRow { return g.SquadOf(g.World.HumanClubID) }

// SquadOf returns any club's players.
func (g *Game) SquadOf(clubID uint16) []SquadRow {
	w := g.World
	c := w.Club(clubID)
	starting := map[uint32]bool{}
	if c != nil {
		for _, id := range c.Lineup {
			starting[id] = true
		}
	}

	players := w.Squad(clubID)
	out := make([]SquadRow, 0, len(players))
	for _, p := range players {
		status := ""
		switch {
		case p.InjuryDays > 0:
			status = fmt.Sprintf("Injured %dd", p.InjuryDays)
		case p.Suspension > 0:
			status = fmt.Sprintf("Banned %d", p.Suspension)
		}
		out = append(out, SquadRow{
			PlayerID:  p.ID,
			Name:      p.Name,
			Position:  p.Primary().String(),
			Age:       w.Age(p),
			Rating:    int(math.Round(p.CurrentAbility())),
			Potential: int(p.Potential),
			Fitness:   int(p.Fitness),
			Morale:    int(p.Morale),
			Form:      int(p.Form),
			Nation:    w.NationName(p.NationID),
			Value:     int64(p.ValueEUR),
			Wage:      int64(p.WageEUR),
			Contract:  int(p.ContractUntil),
			Apps:      int(p.Apps),
			Goals:     int(p.Goals),
			Assists:   int(p.Assists),
			AvgRating: p.AvgRating(),
			Status:    status,
			Starting:  starting[p.ID],
		})
	}
	return out
}

// Table returns the standings for a league.
func (g *Game) Table(leagueID uint16) []season.Row {
	return season.Table(g.World, g.Sched, leagueID)
}

// NextFixture returns the managed club's next match.
func (g *Game) NextFixture() *season.Fixture {
	return g.Sched.NextFor(g.World.HumanClubID)
}

// ScorerRow is one line of a scoring chart.
type ScorerRow struct {
	PlayerID uint32
	Name     string
	Club     string
	Goals    int
	Assists  int
	Apps     int
}

// TopScorers returns the leading scorers in a league, or across the whole world
// when leagueID is zero.
func (g *Game) TopScorers(leagueID uint16, limit int) []ScorerRow {
	w := g.World
	out := make([]ScorerRow, 0, 64)
	for i := range w.Players {
		p := &w.Players[i]
		if p.Goals == 0 || p.ClubID == 0 {
			continue
		}
		if leagueID != 0 {
			c := w.Club(p.ClubID)
			if c == nil || c.LeagueID != leagueID {
				continue
			}
		}
		out = append(out, ScorerRow{
			PlayerID: p.ID, Name: p.Name, Club: w.ClubName(p.ClubID),
			Goals: int(p.Goals), Assists: int(p.Assists), Apps: int(p.Apps),
		})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Goals != out[b].Goals {
			return out[a].Goals > out[b].Goals
		}
		return out[a].Assists > out[b].Assists
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// SearchPlayers finds transfer targets matching a name fragment and filters.
type SearchFilter struct {
	Name      string
	MaxValue  int64
	MinAge    int
	MaxAge    int
	Position  model.Pos
	MinRating int
}

// Search returns players matching the filter, best first.
func (g *Game) Search(f SearchFilter, limit int) []SquadRow {
	w := g.World
	out := make([]SquadRow, 0, limit*2)
	for i := range w.Players {
		p := &w.Players[i]
		if p.Potential == 0 || p.ClubID == w.HumanClubID {
			continue
		}
		age := w.Age(p)
		if f.MinAge > 0 && age < f.MinAge {
			continue
		}
		if f.MaxAge > 0 && age > f.MaxAge {
			continue
		}
		if f.Position < model.NumPos && !p.PlaysPos(f.Position) {
			continue
		}
		if f.MinRating > 0 && int(p.CurrentAbility()) < f.MinRating {
			continue
		}
		if f.MaxValue > 0 && transfer.AskingPrice(w, p) > f.MaxValue {
			continue
		}
		if f.Name != "" && !containsFold(p.Name, f.Name) && !containsFold(p.FullName, f.Name) {
			continue
		}
		row := SquadRow{
			PlayerID: p.ID, Name: p.Name, Position: p.Primary().String(), Age: age,
			Rating: int(math.Round(p.CurrentAbility())), Potential: int(p.Potential),
			Nation: w.NationName(p.NationID), Value: transfer.AskingPrice(w, p),
			Wage: int64(p.WageEUR), Contract: int(p.ContractUntil),
			Goals: int(p.Goals), Apps: int(p.Apps),
			Status: w.ClubName(p.ClubID),
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Rating > out[b].Rating })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func containsFold(hay, needle string) bool {
	h, n := []rune(lower(hay)), []rune(lower(needle))
	if len(n) == 0 || len(n) > len(h) {
		return len(n) == 0
	}
	for i := 0; i+len(n) <= len(h); i++ {
		ok := true
		for j := range n {
			if h[i+j] != n[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func lower(s string) string {
	r := []rune(s)
	for i, c := range r {
		if c >= 'A' && c <= 'Z' {
			r[i] = c + 32
		}
	}
	return string(r)
}

// ---------------------------------------------------------------- actions

// SetFormation changes the managed club's shape and clears the saved eleven,
// since the old selection no longer maps onto the new slots.
func (g *Game) SetFormation(f model.Formation) {
	c := g.Club()
	if c == nil || f >= model.NumFormations {
		return
	}
	c.Tactics.Formation = f
	c.Lineup = [11]uint32{}
	c.Bench = [9]uint32{}
}

// AutoSelect fills the managed club's lineup with the best available eleven.
func (g *Game) AutoSelect() {
	c := g.Club()
	if c == nil {
		return
	}
	lineup, bench := match.AutoPick(g.World.Squad(c.ID), c.Tactics)
	for i, p := range lineup {
		if p != nil {
			c.Lineup[i] = p.ID
		} else {
			c.Lineup[i] = 0
		}
	}
	c.Bench = [9]uint32{}
	for i, p := range bench {
		if i >= 9 {
			break
		}
		c.Bench[i] = p.ID
	}
}

// SwapLineup exchanges the players in two lineup slots, or moves a bench player
// into the eleven. Slots 0-10 are the starting side; 11-19 are the bench.
func (g *Game) SwapLineup(a, b int) {
	c := g.Club()
	if c == nil {
		return
	}
	get := func(i int) uint32 {
		if i < 11 {
			return c.Lineup[i]
		}
		return c.Bench[i-11]
	}
	set := func(i int, v uint32) {
		if i < 11 {
			c.Lineup[i] = v
		} else {
			c.Bench[i-11] = v
		}
	}
	if a < 0 || b < 0 || a >= 20 || b >= 20 {
		return
	}
	x, y := get(a), get(b)
	set(a, y)
	set(b, x)
}

// Bid offers for a player. It returns a human-readable outcome.
func (g *Game) Bid(playerID uint32, fee int64, wage uint32, years int) (bool, string) {
	w := g.World
	p := w.Player(playerID)
	if p == nil {
		return false, "No such player."
	}
	if p.ClubID == w.HumanClubID {
		return false, "That player already plays for you."
	}
	if !transfer.Window(w.Date) {
		return false, "The transfer window is closed."
	}
	if ok, why := transfer.CanAfford(w, w.HumanClubID, fee, wage); !ok {
		return false, why
	}

	o := transfer.Offer{PlayerID: playerID, From: w.HumanClubID, Fee: fee, Wage: wage, Years: years}
	resp := transfer.Consider(w, g.rng, o)

	switch {
	case !resp.ClubAccepted:
		return false, fmt.Sprintf("%s want %s for %s.",
			w.ClubName(p.ClubID), transfer.Money(resp.ClubCounter), p.Name)
	case !resp.PlayerAccepted:
		return false, fmt.Sprintf("%s wants %s per week to sign.",
			p.Name, transfer.Money(int64(resp.WageDemand)))
	}

	from := w.ClubName(p.ClubID)
	transfer.Complete(w, o)
	msg := fmt.Sprintf("%s signs from %s for %s.", p.Name, from, transfer.Money(fee))
	g.post("transfer", msg)
	return true, msg
}

// Sell puts a player up for sale and resolves it immediately if a buyer bites.
func (g *Game) Sell(playerID uint32) (bool, string) {
	w := g.World
	p := w.Player(playerID)
	if p == nil || p.ClubID != w.HumanClubID {
		return false, "That player is not yours to sell."
	}
	if !transfer.Window(w.Date) {
		return false, "The transfer window is closed."
	}
	if w.SquadSize(w.HumanClubID) <= 16 {
		return false, "You cannot go below sixteen players."
	}

	ask := transfer.AskingPrice(w, p)
	// Look for a club that both wants the player and can pay.
	var best *model.Club
	for i := range w.Clubs {
		c := &w.Clubs[i]
		if c.IsHuman || c.TransferBudget < ask {
			continue
		}
		if p.CurrentAbility() < float64(c.Reputation)*0.72 {
			continue // not good enough to interest them
		}
		if best == nil || c.Reputation > best.Reputation {
			best = c
		}
	}
	if best == nil {
		return false, fmt.Sprintf("No club has come in for %s at %s.", p.Name, transfer.Money(ask))
	}

	wage := dev.WageFor(p, w.Age(p), best.Reputation)
	o := transfer.Offer{PlayerID: playerID, From: best.ID, Fee: ask, Wage: wage, Years: 3}
	transfer.Complete(w, o)
	msg := fmt.Sprintf("%s sold to %s for %s.", p.Name, best.Name, transfer.Money(ask))
	g.post("transfer", msg)
	return true, msg
}

// OfferContract renews a player's deal on the managed club's terms.
func (g *Game) OfferContract(playerID uint32, wage uint32, years int) (bool, string) {
	w := g.World
	p := w.Player(playerID)
	if p == nil || p.ClubID != w.HumanClubID {
		return false, "That player is not yours."
	}
	c := g.Club()
	want := dev.WageFor(p, w.Age(p), c.Reputation)
	if wage < want {
		return false, fmt.Sprintf("%s is holding out for %s per week.", p.Name, transfer.Money(int64(want)))
	}
	if w.WageBill(c.ID)-int64(p.WageEUR)+int64(wage) > c.WageBudget {
		return false, "That would take you over your wage budget."
	}
	p.WageEUR = wage
	p.ContractUntil = uint16(w.Date.SeasonYear() + years)
	p.Morale = min8(p.Morale+10, 100)
	msg := fmt.Sprintf("%s signs a new deal until %d.", p.Name, p.ContractUntil)
	g.post("transfer", msg)
	return true, msg
}

func min8(a, b uint8) uint8 {
	if a < b {
		return a
	}
	return b
}

// RNGState exposes the random generator's state so a save can resume the exact
// stream it was on, keeping a loaded career deterministic.
func (g *Game) RNGState() [4]uint64 { return g.rng.State() }

// Restore rebuilds a game from saved parts. It is the counterpart to a save
// file's decode step and is not used during normal play.
func Restore(w *model.World, s *season.Schedule, inbox []Message, state [4]uint64) *Game {
	r := rng.New(w.Seed)
	r.SetState(state)
	return &Game{World: w, Sched: s, Inbox: inbox, rng: r}
}
