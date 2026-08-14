package model

// Club is a team in the game world. Reputation drives transfer appeal, AI
// ambition and attendance; finances constrain what the club can actually do.
type Club struct {
	ID       uint16
	Name     string
	Short    string // 3-4 char abbreviation for tables
	LeagueID uint16
	NationID uint16

	Reputation uint8 // 1-100, derived from squad strength and league tier

	// ---- finances, in whole euros ----
	Balance        int64
	TransferBudget int64
	WageBudget     int64 // weekly wage ceiling
	StadiumCap     uint32
	TicketPrice    uint32

	// ---- infrastructure, 1-100 ----
	TrainingFacilities uint8
	YouthFacilities    uint8
	YouthRecruitment   uint8

	Tactics Tactics

	// Lineup holds the 11 selected player IDs in formation-slot order, then up
	// to 9 substitutes. Zero means an empty slot.
	Lineup [11]uint32
	Bench  [9]uint32

	IsHuman bool // true for the club the player manages
}

// Tactics captures the instructions a manager sets. Values are 0-100 sliders
// unless noted, which keeps the match engine free of special cases.
type Tactics struct {
	Formation Formation

	Mentality  uint8 // 0 very defensive .. 100 very attacking
	Tempo      uint8 // 0 patient .. 100 fast
	Width      uint8 // 0 narrow .. 100 wide
	Pressing   uint8 // 0 deep block .. 100 high press
	Directness uint8 // 0 short passing .. 100 long ball
	LineHeight uint8 // 0 deep .. 100 high line
	Tackling   uint8 // 0 contain .. 100 aggressive

	CaptainID    uint32
	PenaltyTaker uint32
	FreeKicks    uint32
	Corners      uint32
}

// DefaultTactics returns a balanced 4-3-3 setup used for AI clubs and as the
// starting point for a new human manager.
func DefaultTactics() Tactics {
	return Tactics{
		Formation:  F433,
		Mentality:  50,
		Tempo:      55,
		Width:      55,
		Pressing:   55,
		Directness: 40,
		LineHeight: 55,
		Tackling:   50,
	}
}

// Formation identifies a shape. The ordering is persisted, so append only.
type Formation uint8

const (
	F442 Formation = iota
	F433
	F4231
	F4141
	F352
	F532
	F343
	F4411
	F451
	NumFormations
)

type formationDef struct {
	Name  string
	Slots [11]Pos
}

var formations = [NumFormations]formationDef{
	F442:  {"4-4-2", [11]Pos{GK, LB, CB, CB, RB, LM, CM, CM, RM, ST, ST}},
	F433:  {"4-3-3", [11]Pos{GK, LB, CB, CB, RB, CDM, CM, CM, LW, ST, RW}},
	F4231: {"4-2-3-1", [11]Pos{GK, LB, CB, CB, RB, CDM, CDM, LM, CAM, RM, ST}},
	F4141: {"4-1-4-1", [11]Pos{GK, LB, CB, CB, RB, CDM, LM, CM, CM, RM, ST}},
	F352:  {"3-5-2", [11]Pos{GK, CB, CB, CB, LWB, CM, CM, CM, RWB, ST, ST}},
	F532:  {"5-3-2", [11]Pos{GK, LWB, CB, CB, CB, RWB, CDM, CM, CM, ST, ST}},
	F343:  {"3-4-3", [11]Pos{GK, CB, CB, CB, LM, CM, CM, RM, LW, ST, RW}},
	F4411: {"4-4-1-1", [11]Pos{GK, LB, CB, CB, RB, LM, CM, CM, RM, CAM, ST}},
	F451:  {"4-5-1", [11]Pos{GK, LB, CB, CB, RB, LM, CDM, CM, CM, RM, ST}},
}

func (f Formation) String() string {
	if f >= NumFormations {
		return "?"
	}
	return formations[f].Name
}

// Slots returns the eleven positions this formation fields.
func (f Formation) Slots() [11]Pos {
	if f >= NumFormations {
		return formations[F433].Slots
	}
	return formations[f].Slots
}

// AllFormations lists every selectable shape, for UI menus.
func AllFormations() []Formation {
	out := make([]Formation, 0, NumFormations)
	for i := Formation(0); i < NumFormations; i++ {
		out = append(out, i)
	}
	return out
}

// LineCount returns how many players the formation commits to each unit.
func (f Formation) LineCount() (def, mid, att int) {
	for _, p := range f.Slots() {
		switch p.Line() {
		case LineDef:
			def++
		case LineMid:
			mid++
		case LineAtt:
			att++
		}
	}
	return
}
