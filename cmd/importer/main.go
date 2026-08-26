// Command importer converts a raw EA FC / sofifa player CSV into SportSim's
// packed binary database. It runs once at build time; the resulting asset is
// embedded in the game binary, so players never need the CSV or a network
// connection.
//
// One run produces one edition — one season's squads, ratings, values and
// wages — and the game can hold as many as have been imported:
//
//	go run ./cmd/importer -in data/players_16.csv -year 2016
//	go run ./cmd/importer -in data/players.csv    -year 2026
//
// The column names in the dataset have drifted across editions, so -inspect
// reports what a file offers without writing anything:
//
//	go run ./cmd/importer -in data/players_16.csv -year 2016 -inspect
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"sportsim/data/pack"
	"sportsim/engine/model"
)

// leagueSpec describes one division we import, keyed by the dataset's league_id.
// Adding a division is a single line here; nothing else in the game hard-codes
// the league list.
type leagueSpec struct {
	SrcID int
	// SrcNames are the dataset's own spellings of the division, used when an
	// edition carries no numeric league_id. sofifa names a league differently
	// from the way football does ("Spain Primera Division"), and the spelling
	// has changed between editions, so this is a list rather than one string.
	SrcNames   []string
	Name       string
	Country    string
	Code       string
	Tier       uint8
	Promoted   uint8
	Relegated  uint8
	Reputation uint8
	// PrizeMoney is the division's whole broadcast deal for a season, not the
	// champion's cheque: season.PrizeShare splits it across the table, half
	// equally, a quarter on where a club finished and a quarter on how much of
	// the audience it brings.
	PrizeMoney int64
}

// The eight strongest European countries, with second divisions wherever the
// dataset licenses them. Portugal, the Netherlands and Turkey are top-flight
// only because EA does not ship their second tiers.
var wanted = []leagueSpec{
	{13, []string{"English Premier League", "Premier League"},
		"Premier League", "England", "ENG", 1, 0, 3, 95, 1_200_000_000},
	{14, []string{"English League Championship", "EFL Championship", "Championship"},
		"Championship", "England", "ENG", 2, 3, 0, 68, 70_000_000},
	{53, []string{"Spain Primera Division", "Spanish Primera División", "LaLiga EA Sports", "La Liga"},
		"La Liga", "Spain", "ESP", 1, 0, 3, 92, 550_000_000},
	{54, []string{"Spain Segunda División", "Spanish Segunda División", "LaLiga HyperMotion", "La Liga 2"},
		"La Liga 2", "Spain", "ESP", 2, 3, 0, 60, 45_000_000},
	{19, []string{"German 1. Bundesliga", "Bundesliga"},
		"Bundesliga", "Germany", "GER", 1, 0, 3, 90, 430_000_000},
	{20, []string{"German 2. Bundesliga", "2. Bundesliga"},
		"2. Bundesliga", "Germany", "GER", 2, 3, 0, 62, 95_000_000},
	{31, []string{"Italian Serie A", "Serie A"},
		"Serie A", "Italy", "ITA", 1, 0, 3, 89, 440_000_000},
	{32, []string{"Italian Serie B", "Serie B", "Serie BKT"},
		"Serie B", "Italy", "ITA", 2, 3, 0, 60, 35_000_000},
	{16, []string{"French Ligue 1", "Ligue 1", "Ligue 1 Uber Eats"},
		"Ligue 1", "France", "FRA", 1, 0, 3, 82, 195_000_000},
	{17, []string{"French Ligue 2", "Ligue 2", "Ligue 2 BKT"},
		"Ligue 2", "France", "FRA", 2, 3, 0, 55, 27_000_000},
	{308, []string{"Portuguese Liga ZON SAGRES", "Portuguese Primeira Liga", "Liga Portugal", "Primeira Liga"},
		"Primeira Liga", "Portugal", "POR", 1, 0, 0, 76, 47_000_000},
	{10, []string{"Holland Eredivisie", "Dutch Eredivisie", "Eredivisie"},
		"Eredivisie", "Netherlands", "NED", 1, 0, 0, 75, 35_000_000},
	{68, []string{"Turkish Süper Lig", "Türkiye Süper Lig", "Süper Lig"},
		"Süper Lig", "Türkiye", "TUR", 1, 0, 0, 71, 47_000_000},
}

// revenueIndex scales every division's broadcast deal to the era being
// imported. The wage bills and player values come from the CSV and are already
// correct for their season; PrizeMoney above is the 2026 deal, so importing an
// older edition without this would pay 2015 squads out of a 2026 television
// pot and leave every club far richer than it should be.
//
// One factor for all thirteen divisions is deliberate. Invariant 14 rests on
// revenue being distributed as unevenly as wages are, which is a property of
// the spread between leagues rather than of the total; scaling them together
// moves the total and leaves the spread alone. The dip at 2021 is the season
// played behind closed doors.
//
// The figures are estimates from the growth of European broadcast income over
// the decade and have never been checked against a real old dataset.
// TestOlderEditionsStaySolvent is written to settle them — it plays a season in
// the oldest embedded edition and fails either way, too many clubs in the red or
// a giant profiting more than it earned — but it skips while only one edition is
// embedded, so its silence is not approval. See invariant 18: synthetic player
// data cannot validate this, because its wage spread is far flatter than a real
// one and sinks every era alike.
var revenueIndex = map[int]float64{
	2015: 0.48, 2016: 0.54, 2017: 0.61, 2018: 0.68,
	2019: 0.74, 2020: 0.78, 2021: 0.74, 2022: 0.82,
	2023: 0.89, 2024: 0.94, 2025: 0.97, 2026: 1.00,
}

// eraScale is the revenue index for a year, extrapolated at the decade's
// average growth for a season outside the table rather than defaulting to a
// silent 1.0, which would only be right for one year of the twelve.
func eraScale(year int) float64 {
	if f, ok := revenueIndex[year]; ok {
		return f
	}
	lo, hi := 2015, 2026
	if year < lo {
		return revenueIndex[lo] * math.Pow(1.07, float64(year-lo))
	}
	return revenueIndex[hi] * math.Pow(1.03, float64(year-hi))
}

// aliases maps the column name this importer asks for to the spellings older
// editions of the dataset used. The scrape has been renaming columns since
// FIFA 15 — sofifa_id became player_id, marking gained "_awareness", the team_
// prefix became club_ — and the data underneath is the same, so a rename should
// not need a second importer.
//
// Run with -inspect against a new file before trusting this list.
var aliases = map[string][]string{
	"player_id":                      {"sofifa_id"},
	"club_team_id":                   {"team_id", "club_id"},
	"club_name":                      {"team_name"},
	"club_jersey_number":             {"team_jersey_number"},
	"club_position":                  {"team_position"},
	"club_contract_valid_until_year": {"club_contract_valid_until", "contract_valid_until"},
	"nationality_name":               {"nationality", "nation_name"},
	"league_name":                    {"club_league_name"},
	"league_id":                      {"club_league_id"},
	"defending_marking_awareness":    {"defending_marking", "marking"},
}

// attrCols maps each engine attribute to its CSV column name.
var attrCols = [model.NumAttr]string{
	model.AtkCrossing:      "attacking_crossing",
	model.AtkFinishing:     "attacking_finishing",
	model.AtkHeading:       "attacking_heading_accuracy",
	model.AtkShortPass:     "attacking_short_passing",
	model.AtkVolleys:       "attacking_volleys",
	model.SklDribbling:     "skill_dribbling",
	model.SklCurve:         "skill_curve",
	model.SklFreeKick:      "skill_fk_accuracy",
	model.SklLongPass:      "skill_long_passing",
	model.SklBallControl:   "skill_ball_control",
	model.MovAcceleration:  "movement_acceleration",
	model.MovSprintSpeed:   "movement_sprint_speed",
	model.MovAgility:       "movement_agility",
	model.MovReactions:     "movement_reactions",
	model.MovBalance:       "movement_balance",
	model.PowShotPower:     "power_shot_power",
	model.PowJumping:       "power_jumping",
	model.PowStamina:       "power_stamina",
	model.PowStrength:      "power_strength",
	model.PowLongShots:     "power_long_shots",
	model.MenAggression:    "mentality_aggression",
	model.MenInterceptions: "mentality_interceptions",
	model.MenPositioning:   "mentality_positioning",
	model.MenVision:        "mentality_vision",
	model.MenPenalties:     "mentality_penalties",
	model.MenComposure:     "mentality_composure",
	model.DefMarking:       "defending_marking_awareness",
	model.DefStanding:      "defending_standing_tackle",
	model.DefSliding:       "defending_sliding_tackle",
	model.GKDiving:         "goalkeeping_diving",
	model.GKHandling:       "goalkeeping_handling",
	model.GKKicking:        "goalkeeping_kicking",
	model.GKPositioning:    "goalkeeping_positioning",
	model.GKReflexes:       "goalkeeping_reflexes",
}

func main() {
	in := flag.String("in", "data/players.csv", "source EA FC / sofifa player CSV")
	out := flag.String("out", "", "destination packed database (default assets/world_<year>.dat)")
	year := flag.Int("year", 0, "the season this edition starts, e.g. 2016 for 2016/17")
	inspect := flag.Bool("inspect", false, "report what the CSV offers and exit without writing")
	flag.Parse()

	if *year == 0 {
		fmt.Fprintln(os.Stderr, "import failed: -year is required; the CSV does not say which season it holds")
		os.Exit(1)
	}
	if *out == "" {
		*out = fmt.Sprintf("assets/world_%d.dat", *year)
	}

	if *inspect {
		if err := inspectCSV(*in); err != nil {
			fmt.Fprintln(os.Stderr, "inspect failed:", err)
			os.Exit(1)
		}
		return
	}

	d, err := build(*in, *year)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import failed:", err)
		os.Exit(1)
	}

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import failed:", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := pack.Encode(f, d); err != nil {
		fmt.Fprintln(os.Stderr, "encode failed:", err)
		os.Exit(1)
	}

	st, _ := f.Stat()
	fmt.Printf("wrote %s\n", *out)
	fmt.Printf("  %s season, %d leagues, %d clubs, %d players, %d nations\n",
		model.SeasonLabel(d.Year), len(d.Leagues), len(d.Clubs), len(d.Players), len(d.Nations))
	fmt.Printf("  %.1f KB packed (%.1f bytes/player)\n",
		float64(st.Size())/1024, float64(st.Size())/float64(len(d.Players)))
}

// header resolves the columns this importer asks for against the spellings a
// particular edition actually uses.
type header struct {
	idx map[string]int // canonical name -> column index
	via map[string]string
}

// resolve builds a header from a CSV's first row, consulting aliases for any
// name the file does not carry directly.
func resolve(row []string) *header {
	raw := make(map[string]int, len(row))
	for i, h := range row {
		raw[strings.TrimSpace(h)] = i
	}
	h := &header{idx: make(map[string]int, len(row)), via: make(map[string]string)}
	for name, i := range raw {
		h.idx[name] = i
	}
	for canonical, alts := range aliases {
		if _, ok := h.idx[canonical]; ok {
			continue
		}
		for _, alt := range alts {
			if i, ok := raw[alt]; ok {
				h.idx[canonical] = i
				h.via[canonical] = alt
				break
			}
		}
	}
	return h
}

func (h *header) has(name string) bool { _, ok := h.idx[name]; return ok }

// field reads a column from a row, returning "" for a column this edition does
// not carry — every caller has a sensible fallback for a missing value, and
// the required columns are checked once up front.
func (h *header) field(rec []string, name string) string {
	i, ok := h.idx[name]
	if !ok || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

// required are the columns without which no useful player can be built. The
// division a player belongs to is not among them: it may be given either as a
// numeric league_id or by name, and specFor accepts whichever is present.
var required = []string{"player_id", "short_name", "club_team_id", "overall", "potential"}

func openCSV(path string) (*os.File, *csv.Reader, *header, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	r := csv.NewReader(f)
	r.ReuseRecord = true
	r.FieldsPerRecord = -1

	row, err := r.Read()
	if err != nil {
		f.Close()
		return nil, nil, nil, fmt.Errorf("reading header: %w", err)
	}
	return f, r, resolve(row), nil
}

func build(path string, year int) (*pack.Data, error) {
	f, r, h, err := openCSV(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	for _, need := range required {
		if !h.has(need) {
			return nil, fmt.Errorf("CSV is missing required column %q; run with -inspect to see what it has", need)
		}
	}
	if !h.has("league_id") && !h.has("league_name") {
		return nil, fmt.Errorf("CSV says nothing about divisions: neither league_id nor league_name is present")
	}

	specByID := make(map[int]*leagueSpec, len(wanted))
	specByName := make(map[string]*leagueSpec, len(wanted)*3)
	for i := range wanted {
		specByID[wanted[i].SrcID] = &wanted[i]
		for _, n := range wanted[i].SrcNames {
			specByName[foldName(n)] = &wanted[i]
		}
	}

	d := &pack.Data{}

	nationID := make(map[string]uint16)
	clubID := make(map[int]uint16)
	clubOverall := make(map[uint16][]int) // for reputation and finances
	leagueOfClub := make(map[uint16]int)  // club -> source league id

	getNation := func(name string) uint16 {
		name = strings.TrimSpace(name)
		if name == "" {
			name = "Unknown"
		}
		if id, ok := nationID[name]; ok {
			return id
		}
		id := uint16(len(d.Nations) + 1)
		nationID[name] = id
		d.Nations = append(d.Nations, model.Nation{ID: id, Name: name, Code: nationCode(name)})
		return id
	}

	// Leagues are created up front so their IDs follow the order in `wanted`,
	// which keeps the UI's league list stable across re-imports. An edition that
	// does not license one of them leaves it empty, and dropEmptyLeagues clears
	// it away once the players have been read.
	era := eraScale(year)
	leagueID := make(map[int]uint16, len(wanted))
	for i := range wanted {
		s := &wanted[i]
		id := uint16(i + 1)
		leagueID[s.SrcID] = id
		d.Leagues = append(d.Leagues, model.League{
			ID:         id,
			Name:       s.Name,
			Country:    s.Country,
			Tier:       s.Tier,
			Promoted:   s.Promoted,
			Relegated:  s.Relegated,
			Reputation: s.Reputation,
			PrizeMoney: int64(float64(s.PrizeMoney) * era),
		})
	}

	seen := make(map[int]bool) // dataset player_id, guards duplicate rows

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading row: %w", err)
		}

		get := func(name string) string { return h.field(rec, name) }

		// Which division a player is in is given by number in recent editions and
		// only by name in older ones, so accept either.
		srcLeague := atoiF(get("league_id"))
		spec, ok := specByID[srcLeague]
		if !ok {
			if spec, ok = specByName[foldName(get("league_name"))]; !ok {
				continue
			}
			srcLeague = spec.SrcID
		}
		srcClub := atoiF(get("club_team_id"))
		if srcClub == 0 {
			continue // no club: free agents are not imported in v1
		}
		if get("club_loaned_from") != "" {
			continue // loanee rows duplicate the parent club's player
		}
		pid := atoiF(get("player_id"))
		if pid == 0 || seen[pid] {
			continue
		}
		seen[pid] = true

		// Register the club on first sighting.
		cid, ok := clubID[srcClub]
		if !ok {
			cid = uint16(len(d.Clubs) + 1)
			clubID[srcClub] = cid
			name := get("club_name")
			nat := getNation(spec.Country)
			d.Clubs = append(d.Clubs, model.Club{
				ID:       cid,
				Name:     name,
				Short:    shortName(name),
				LeagueID: leagueID[srcLeague],
				NationID: nat,
				Tactics:  model.DefaultTactics(),
			})
			leagueOfClub[cid] = srcLeague
			li := leagueID[srcLeague] - 1
			d.Leagues[li].ClubIDs = append(d.Leagues[li].ClubIDs, cid)
		}

		p := model.Player{
			ID:       uint32(len(d.Players) + 1),
			ClubID:   cid,
			NationID: getNation(get("nationality_name")),
			Name:     get("short_name"),
			FullName: get("long_name"),
			HeightCM: u8(atoiF(get("height_cm")), 150, 215),
			WeightKG: u8(atoiF(get("weight_kg")), 50, 120),
			Jersey:   uint8(clamp(atoiF(get("club_jersey_number")), 0, 99)),
		}

		y, m, dd := parseDOB(get("dob"), atoiF(get("age")), year)
		p.BirthYear, p.BirthMonth, p.BirthDay = uint16(y), uint8(m), uint8(dd)

		for i := 0; i < model.NumAttr; i++ {
			p.Attr[i] = u8(atoiF(get(attrCols[i])), 1, 99)
		}

		parsePositions(&p, get("player_positions"))

		// Potential is stored relative to the FIFA overall, then re-anchored to
		// our own attribute-derived ability so the two scales stay consistent.
		ovr := atoiF(get("overall"))
		pot := atoiF(get("potential"))
		gap := pot - ovr
		if gap < 0 {
			gap = 0
		}
		ca := p.CurrentAbility()
		p.Potential = u8(int(math.Round(ca))+gap, 1, 99)

		p.Foot = model.FootRight
		if strings.EqualFold(get("preferred_foot"), "Left") {
			p.Foot = model.FootLeft
		}
		p.WeakFoot = u8(atoiF(get("weak_foot")), 1, 5)
		p.SkillMoves = u8(atoiF(get("skill_moves")), 1, 5)
		p.IntRep = u8(atoiF(get("international_reputation")), 1, 5)
		p.WorkRateAtk, p.WorkRateDef = parseWorkRate(get("work_rate"))

		p.ValueEUR = uint32(clamp(atoiF(get("value_eur")), 0, math.MaxInt32))
		p.WageEUR = uint32(clamp(atoiF(get("wage_eur")), 0, math.MaxInt32))
		if p.ValueEUR == 0 {
			p.ValueEUR = uint32(estimateValue(ca, p.Potential, ageFrom(y, m, dd, year)))
		}
		if p.WageEUR == 0 {
			p.WageEUR = uint32(clamp(int(float64(p.ValueEUR)/260), 500, math.MaxInt32))
		}
		// A contract cannot already have expired in the season being imported, and
		// nobody signs for fourteen years.
		p.ContractUntil = uint16(clamp(atoiF(get("club_contract_valid_until_year")), year, year+14))

		d.Players = append(d.Players, p)
		clubOverall[cid] = append(clubOverall[cid], int(math.Round(ca)))
	}

	if len(d.Players) == 0 {
		return nil, fmt.Errorf("no players matched the configured leagues; run with -inspect to see which divisions the CSV names")
	}

	finaliseClubs(d, clubOverall, leagueOfClub, specByID)
	dropEmptyLeagues(d)
	d.Year = year
	return d, nil
}

// dropEmptyLeagues removes divisions the edition turned out not to license and
// closes the gap in the IDs behind them.
//
// Every division in `wanted` is created before a single player is read, so an
// edition that ships no Serie B would otherwise leave a league with no clubs in
// it — and season.Generate would dutifully build a fixture list for nobody.
// IDs are dense indices (invariant 4), so the survivors are renumbered and
// every club's LeagueID is remapped with them.
func dropEmptyLeagues(d *pack.Data) {
	kept := make([]model.League, 0, len(d.Leagues))
	remap := make(map[uint16]uint16, len(d.Leagues))
	for i := range d.Leagues {
		l := &d.Leagues[i]
		if len(l.ClubIDs) == 0 {
			continue
		}
		id := uint16(len(kept) + 1)
		remap[l.ID] = id
		l.ID = id
		kept = append(kept, *l)
	}
	d.Leagues = kept
	for i := range d.Clubs {
		d.Clubs[i].LeagueID = remap[d.Clubs[i].LeagueID]
	}
}

// finaliseClubs derives reputation, stadium size and finances from squad
// strength, since the source dataset carries none of them.
//
// Squad averages occupy a narrow band (roughly 61-84 across Europe), so
// reputation is normalised against the range actually observed in the data
// rather than against absolute thresholds. That keeps the scale sensible if the
// dataset is later refreshed with different overall inflation.
func finaliseClubs(d *pack.Data, squads map[uint16][]int, leagueOf map[uint16]int, specs map[int]*leagueSpec) {
	// Pass 1: first-team strength per club, and the range it spans.
	strength := make(map[uint16]float64, len(d.Clubs))
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := range d.Clubs {
		c := &d.Clubs[i]
		ov := squads[c.ID]
		sort.Sort(sort.Reverse(sort.IntSlice(ov)))
		// The best 16 players define a club's standing; fringe squad filler
		// should not drag a strong first team down.
		n := len(ov)
		if n > 16 {
			n = 16
		}
		sum := 0
		for j := 0; j < n; j++ {
			sum += ov[j]
		}
		avg := 60.0
		if n > 0 {
			avg = float64(sum) / float64(n)
		}
		strength[c.ID] = avg
		lo = math.Min(lo, avg)
		hi = math.Max(hi, avg)
	}
	span := hi - lo
	if span < 1 {
		span = 1
	}

	// Squad value and wage bill in one sweep, rather than rescanning the player
	// list once per club.
	squadValue := make(map[uint16]int64, len(d.Clubs))
	wageBill := make(map[uint16]int64, len(d.Clubs))
	for i := range d.Players {
		p := &d.Players[i]
		squadValue[p.ClubID] += int64(p.ValueEUR)
		wageBill[p.ClubID] += int64(p.WageEUR)
	}

	// Pass 2: map strength onto reputation, finances and infrastructure.
	for i := range d.Clubs {
		c := &d.Clubs[i]

		spec := specs[leagueOf[c.ID]]
		leagueRep := 60.0
		if spec != nil {
			leagueRep = float64(spec.Reputation)
		}

		norm := math.Pow((strength[c.ID]-lo)/span, 0.85)
		// Mostly squad quality, with the division's standing as a modifier: a
		// strong second-tier side is respected, but not mistaken for a giant.
		rep := 0.78*(12+norm*84) + 0.22*leagueRep
		c.Reputation = u8(int(math.Round(rep)), 8, 97)

		// Stadium capacity scales super-linearly with reputation, jittered
		// deterministically so clubs of equal standing are not identical.
		base := 2000 + math.Pow(float64(c.Reputation), 2.42)
		c.StadiumCap = uint32(base * (0.85 + 0.30*jitter(c.Name)))
		c.TicketPrice = model.DefaultTicketPrice(c.Reputation)

		sv, wb := squadValue[c.ID], wageBill[c.ID]

		c.Balance = sv/12 + int64(c.StadiumCap)*300
		c.TransferBudget = sv / 9
		c.WageBudget = wb*115/100 + 20_000

		c.TrainingFacilities = u8(int(c.Reputation)+int(10*jitter(c.Name+"t"))-4, 20, 99)
		c.YouthFacilities = u8(int(c.Reputation)+int(14*jitter(c.Name+"y"))-6, 15, 99)
		c.YouthRecruitment = u8(int(c.Reputation)+int(16*jitter(c.Name+"r"))-8, 10, 99)
	}
}

// ---------- inspect ----------

// inspectCSV reports what an edition's CSV offers without writing anything.
//
// The dataset's column names have drifted across editions and its division
// names are its own, so a new file is far more likely to fail on a rename than
// on anything structural. Printing what matched, what matched only through an
// alias and what is missing turns that from a debugging session into a one-line
// addition to `aliases` or to a spec's SrcNames.
func inspectCSV(path string) error {
	f, r, h, err := openCSV(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Printf("columns in %s\n", path)
	names := append([]string(nil), required...)
	names = append(names, "league_id", "league_name", "club_name", "dob", "age",
		"player_positions", "value_eur", "wage_eur", "club_contract_valid_until_year",
		"club_loaned_from", "nationality_name", "preferred_foot", "work_rate")
	for i := 0; i < model.NumAttr; i++ {
		names = append(names, attrCols[i])
	}
	// A division may be given by number or by name, so neither alone is missed.
	eitherWay := map[string]bool{"league_id": true, "league_name": true}
	haveDivision := h.has("league_id") || h.has("league_name")

	missing := 0
	for _, n := range names {
		switch {
		case h.via[n] != "":
			fmt.Printf("  %-32s <- %s (alias)\n", n, h.via[n])
		case h.has(n):
			fmt.Printf("  %-32s ok\n", n)
		case eitherWay[n] && haveDivision:
			fmt.Printf("  %-32s absent, covered by the other\n", n)
		default:
			fmt.Printf("  %-32s MISSING\n", n)
			missing++
		}
	}

	byName := make(map[string]*leagueSpec, len(wanted)*3)
	for i := range wanted {
		for _, n := range wanted[i].SrcNames {
			byName[foldName(n)] = &wanted[i]
		}
	}

	// One pass over the rows: which divisions the file names, how many rows each
	// has, and how many of them this importer would actually claim.
	type tally struct {
		name  string
		rows  int
		spec  *leagueSpec
		byNum bool
	}
	seen := map[string]*tally{}
	rows, claimed := 0, 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading row: %w", err)
		}
		rows++
		name := h.field(rec, "league_name")
		id := atoiF(h.field(rec, "league_id"))
		key := name
		if key == "" {
			key = fmt.Sprintf("league_id %d", id)
		}
		t, ok := seen[key]
		if !ok {
			t = &tally{name: key}
			for i := range wanted {
				if wanted[i].SrcID == id {
					t.spec, t.byNum = &wanted[i], true
				}
			}
			if t.spec == nil {
				t.spec = byName[foldName(name)]
			}
			seen[key] = t
		}
		t.rows++
		if t.spec != nil {
			claimed++
		}
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool { return seen[keys[a]].rows > seen[keys[b]].rows })

	fmt.Printf("\ndivisions in %s\n", path)
	for _, k := range keys {
		t := seen[k]
		switch {
		case t.spec == nil:
			fmt.Printf("  %-42s %5d rows  -\n", trim(t.name, 42), t.rows)
		case t.byNum:
			fmt.Printf("  %-42s %5d rows  -> %s (by league_id)\n", trim(t.name, 42), t.rows, t.spec.Name)
		default:
			fmt.Printf("  %-42s %5d rows  -> %s (by name)\n", trim(t.name, 42), t.rows, t.spec.Name)
		}
	}

	fmt.Printf("\n%d rows, %d in a wanted division\n", rows, claimed)
	if missing > 0 {
		fmt.Printf("%d columns unmatched: add the file's spelling to `aliases` in this file\n", missing)
	}
	if claimed == 0 {
		fmt.Println("no division matched: add the spellings above to the SrcNames of the matching spec in `wanted`")
	}
	return nil
}

func trim(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// ---------- parsing helpers ----------

// foldName normalises a division name for matching, so that a stray accent,
// case or double space in one edition does not fail to match the next.
func foldName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func parsePositions(p *model.Player, s string) {
	p.Positions = [3]model.Pos{model.NoPos, model.NoPos, model.NoPos}
	n := uint8(0)
	for _, part := range strings.Split(s, ",") {
		if n >= 3 {
			break
		}
		if pos, ok := model.ParsePos(strings.TrimSpace(part)); ok {
			p.Positions[n] = pos
			n++
		}
	}
	if n == 0 {
		p.Positions[0] = model.CM
		n = 1
	}
	p.NumPositions = n
}

func parseWorkRate(s string) (atk, def uint8) {
	parts := strings.SplitN(s, "/", 2)
	rate := func(x string) uint8 {
		switch strings.TrimSpace(x) {
		case "High":
			return model.WorkHigh
		case "Low":
			return model.WorkLow
		}
		return model.WorkMedium
	}
	if len(parts) == 2 {
		return rate(parts[0]), rate(parts[1])
	}
	return model.WorkMedium, model.WorkMedium
}

// parseDOB reads an ISO date, falling back to a synthetic birthday derived from
// the age column when the dataset omits it. The age is only meaningful against
// the season the edition was published for, which is why year is passed in
// rather than being a constant.
func parseDOB(s string, age, year int) (y, m, d int) {
	if len(s) >= 10 {
		y, _ = strconv.Atoi(s[0:4])
		m, _ = strconv.Atoi(s[5:7])
		d, _ = strconv.Atoi(s[8:10])
		if y > 1950 && m >= 1 && m <= 12 && d >= 1 && d <= 31 {
			return y, m, d
		}
	}
	if age <= 0 {
		age = 24
	}
	return year - age, 1, 1
}

func ageFrom(y, m, d, year int) int {
	a := year - y
	if m > 7 {
		a--
	}
	return a
}

// estimateValue fills in a market value for the rare rows that lack one.
func estimateValue(ca float64, pot uint8, age int) int {
	v := math.Pow(math.Max(ca-38, 1), 3.1) * 900
	if age < 24 {
		v *= 1 + float64(int(pot)-int(ca))*0.05
	}
	if age > 30 {
		v *= math.Max(0.15, 1-float64(age-30)*0.16)
	}
	return int(math.Min(v, 250_000_000))
}

// atoiF parses integers that the dataset sometimes writes as floats ("53.0").
func atoiF(s string) int {
	if s == "" {
		return 0
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func u8(v, lo, hi int) uint8 { return uint8(clamp(v, lo, hi)) }

// jitter returns a stable pseudo-random value in [0,1) derived from a string,
// so re-importing the same CSV always produces byte-identical output.
func jitter(s string) float64 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return float64(h.Sum32()%10000) / 10000
}

// shortName builds a table-friendly abbreviation from a club name.
func shortName(name string) string {
	drop := map[string]bool{"FC": true, "AFC": true, "CF": true, "SC": true, "AC": true,
		"SS": true, "US": true, "AS": true, "RC": true, "CD": true, "UD": true,
		"de": true, "of": true, "and": true, "&": true, "1899": true, "1. ": true}
	words := strings.Fields(name)
	kept := make([]string, 0, len(words))
	for _, w := range words {
		if drop[w] {
			continue
		}
		kept = append(kept, w)
	}
	if len(kept) == 0 {
		kept = words
	}
	if len(kept) == 1 {
		r := []rune(kept[0])
		if len(r) > 3 {
			r = r[:3]
		}
		return strings.ToUpper(string(r))
	}
	var b strings.Builder
	for _, w := range kept {
		r := []rune(w)
		if len(r) > 0 {
			b.WriteRune(r[0])
		}
		if b.Len() >= 3 {
			break
		}
	}
	return strings.ToUpper(b.String())
}

// nationCode produces a three-letter code for a country name.
func nationCode(name string) string {
	known := map[string]string{
		"England": "ENG", "Spain": "ESP", "Germany": "GER", "Italy": "ITA",
		"France": "FRA", "Portugal": "POR", "Netherlands": "NED", "Türkiye": "TUR",
		"Brazil": "BRA", "Argentina": "ARG", "Belgium": "BEL", "Croatia": "CRO",
		"Scotland": "SCO", "Wales": "WAL", "Republic of Ireland": "IRL",
		"Norway": "NOR", "Sweden": "SWE", "Denmark": "DEN", "Poland": "POL",
		"Switzerland": "SUI", "Austria": "AUT", "Serbia": "SRB", "Uruguay": "URU",
		"Colombia": "COL", "Nigeria": "NGA", "Senegal": "SEN", "Morocco": "MAR",
		"Japan": "JPN", "Korea Republic": "KOR", "United States": "USA",
	}
	if c, ok := known[name]; ok {
		return c
	}
	r := []rune(strings.ToUpper(name))
	for len(r) < 3 {
		r = append(r, 'X')
	}
	return string(r[:3])
}
