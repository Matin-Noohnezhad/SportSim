package model

// Competition identifies one of the continental tournaments a club plays in
// alongside its domestic league.
//
// The ordering is persisted in save files, so values may only be appended — see
// the note on Formation. NoComp is deliberately zero, so a club that has not
// qualified for anything needs no special case.
type Competition uint8

const (
	NoComp Competition = iota
	ChampionsLeague
	EuropaLeague
	ConferenceLeague
	NumCompetitions
)

// competitionDef is the shape of a tournament: how many clubs enter it and how
// many groups they are drawn into.
//
// The three sizes are what the eight top divisions in the game can actually
// fill. Real UEFA spreads ninety-odd group places across fifty-five countries;
// here there are eight, and 64 places is the share of them that would send
// roughly the same fraction of each division into Europe as the real
// coefficient list does — about eleven English clubs, about five Turkish ones.
// Making the Champions League any larger would put over half of every top flight
// into it and the competition would stop meaning anything.
type competitionDef struct {
	Name     string
	Short    string
	Entrants int
	Groups   int
	// MaxPerLeague caps how many clubs one division may send. Without it the
	// two richest leagues take a third of the Champions League between them.
	MaxPerLeague int
	// Reputation stands in for the standing of the competition when a club
	// weighs up what qualifying for it is worth.
	Reputation uint8
}

var competitions = [NumCompetitions]competitionDef{
	NoComp:           {"No European football", "—", 0, 0, 0, 0},
	ChampionsLeague:  {"UEFA Champions League", "UCL", 32, 8, 5, 100},
	EuropaLeague:     {"UEFA Europa League", "UEL", 16, 4, 3, 78},
	ConferenceLeague: {"UEFA Conference League", "UECL", 16, 4, 3, 62},
}

func (c Competition) def() competitionDef {
	if c >= NumCompetitions {
		return competitions[NoComp]
	}
	return competitions[c]
}

// String is the competition's full name, as a table heading would give it.
func (c Competition) String() string { return c.def().Name }

// Short is the three or four letter abbreviation used in fixture lists.
func (c Competition) Short() string { return c.def().Short }

// Entrants is how many clubs the group stage is drawn from.
func (c Competition) Entrants() int { return c.def().Entrants }

// Groups is how many groups those entrants are drawn into. Every group holds
// four clubs and the top two of each go through, so the size of the knockout
// bracket follows from this alone.
func (c Competition) Groups() int { return c.def().Groups }

// MaxPerLeague is the most clubs one division may send to the competition.
func (c Competition) MaxPerLeague() int { return c.def().MaxPerLeague }

// Reputation is how much standing the competition carries.
func (c Competition) Reputation() uint8 { return c.def().Reputation }

// AllCompetitions lists the continental tournaments, strongest first, for menus
// and for anything that has to work through all of them.
func AllCompetitions() []Competition {
	out := make([]Competition, 0, NumCompetitions-1)
	for c := ChampionsLeague; c < NumCompetitions; c++ {
		out = append(out, c)
	}
	return out
}
