// Command importer converts the raw EA FC player CSV into SportSim's packed
// binary database. It runs once at build time; the resulting asset is embedded
// in the game binary, so players never need the CSV or a network connection.
//
//	go run ./cmd/importer -in data/players.csv -out assets/world.dat
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
	SrcID      int
	Name       string
	Country    string
	Code       string
	Tier       uint8
	Promoted   uint8
	Relegated  uint8
	Reputation uint8
	PrizeMoney int64
}

// The eight strongest European countries, with second divisions wherever the
// dataset licenses them. Portugal, the Netherlands and Turkey are top-flight
// only because EA does not ship their second tiers.
var wanted = []leagueSpec{
	{13, "Premier League", "England", "ENG", 1, 0, 3, 95, 175_000_000},
	{14, "Championship", "England", "ENG", 2, 3, 0, 68, 12_000_000},
	{53, "La Liga", "Spain", "ESP", 1, 0, 3, 92, 140_000_000},
	{54, "La Liga 2", "Spain", "ESP", 2, 3, 0, 60, 8_000_000},
	{19, "Bundesliga", "Germany", "GER", 1, 0, 3, 90, 110_000_000},
	{20, "2. Bundesliga", "Germany", "GER", 2, 3, 0, 62, 9_000_000},
	{31, "Serie A", "Italy", "ITA", 1, 0, 3, 89, 120_000_000},
	{32, "Serie B", "Italy", "ITA", 2, 3, 0, 60, 7_500_000},
	{16, "Ligue 1", "France", "FRA", 1, 0, 3, 82, 85_000_000},
	{17, "Ligue 2", "France", "FRA", 2, 3, 0, 55, 6_000_000},
	{308, "Primeira Liga", "Portugal", "POR", 1, 0, 0, 76, 40_000_000},
	{10, "Eredivisie", "Netherlands", "NED", 1, 0, 0, 75, 38_000_000},
	{68, "Süper Lig", "Türkiye", "TUR", 1, 0, 0, 71, 30_000_000},
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
	in := flag.String("in", "data/players.csv", "source EA FC player CSV")
	out := flag.String("out", "assets/world.dat", "destination packed database")
	flag.Parse()

	d, err := build(*in)
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
	fmt.Printf("  %d leagues, %d clubs, %d players, %d nations\n",
		len(d.Leagues), len(d.Clubs), len(d.Players), len(d.Nations))
	fmt.Printf("  %.1f KB packed (%.1f bytes/player)\n",
		float64(st.Size())/1024, float64(st.Size())/float64(len(d.Players)))
}

func build(path string) (*pack.Data, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.ReuseRecord = true
	r.FieldsPerRecord = -1

	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	col := make(map[string]int, len(head))
	for i, h := range head {
		col[strings.TrimSpace(h)] = i
	}
	for _, need := range []string{"player_id", "short_name", "league_id", "club_team_id", "overall", "potential"} {
		if _, ok := col[need]; !ok {
			return nil, fmt.Errorf("CSV is missing required column %q", need)
		}
	}

	specByID := make(map[int]*leagueSpec, len(wanted))
	for i := range wanted {
		specByID[wanted[i].SrcID] = &wanted[i]
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
	// which keeps the UI's league list stable across re-imports.
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
			PrizeMoney: s.PrizeMoney,
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

		get := func(name string) string {
			i, ok := col[name]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}

		srcLeague := atoiF(get("league_id"))
		spec, ok := specByID[srcLeague]
		if !ok {
			continue
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

		y, m, dd := parseDOB(get("dob"), atoiF(get("age")))
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
			p.ValueEUR = uint32(estimateValue(ca, p.Potential, ageFrom(y, m, dd)))
		}
		if p.WageEUR == 0 {
			p.WageEUR = uint32(clamp(int(float64(p.ValueEUR)/260), 500, math.MaxInt32))
		}
		p.ContractUntil = uint16(clamp(atoiF(get("club_contract_valid_until_year")), 2026, 2040))

		d.Players = append(d.Players, p)
		clubOverall[cid] = append(clubOverall[cid], int(math.Round(ca)))
	}

	if len(d.Players) == 0 {
		return nil, fmt.Errorf("no players matched the configured leagues; is this the right CSV?")
	}

	finaliseClubs(d, clubOverall, leagueOfClub, specByID)
	return d, nil
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

// ---------- parsing helpers ----------

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
// the age column when the dataset omits it.
func parseDOB(s string, age int) (y, m, d int) {
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
	return 2026 - age, 1, 1
}

func ageFrom(y, m, d int) int {
	a := 2026 - y
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
