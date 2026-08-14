package model

import "math"

// Pos is a playing position. The ordering is stable and is persisted in save
// files and the packed data asset, so new positions must only be appended.
type Pos uint8

const (
	GK Pos = iota
	CB
	LB
	RB
	LWB
	RWB
	CDM
	CM
	CAM
	LM
	RM
	LW
	RW
	CF
	ST
	NumPos
	NoPos Pos = 255
)

var posNames = [NumPos]string{
	"GK", "CB", "LB", "RB", "LWB", "RWB", "CDM", "CM", "CAM", "LM", "RM", "LW", "RW", "CF", "ST",
}

func (p Pos) String() string {
	if p >= NumPos {
		return "--"
	}
	return posNames[p]
}

func ParsePos(s string) (Pos, bool) {
	for i, n := range posNames {
		if n == s {
			return Pos(i), true
		}
	}
	return NoPos, false
}

// Line groups positions into the broad units the match engine reasons about.
type Line uint8

const (
	LineGK Line = iota
	LineDef
	LineMid
	LineAtt
)

var posLine = [NumPos]Line{
	GK: LineGK, CB: LineDef, LB: LineDef, RB: LineDef, LWB: LineDef, RWB: LineDef,
	CDM: LineMid, CM: LineMid, CAM: LineMid, LM: LineMid, RM: LineMid,
	LW: LineAtt, RW: LineAtt, CF: LineAtt, ST: LineAtt,
}

func (p Pos) Line() Line {
	if p >= NumPos {
		return LineMid
	}
	return posLine[p]
}

// Attribute indices. Stored as a fixed [NumAttr]uint8 array on every player,
// which keeps a player at a predictable ~90 bytes plus interned name strings.
const (
	AtkCrossing = iota
	AtkFinishing
	AtkHeading
	AtkShortPass
	AtkVolleys
	SklDribbling
	SklCurve
	SklFreeKick
	SklLongPass
	SklBallControl
	MovAcceleration
	MovSprintSpeed
	MovAgility
	MovReactions
	MovBalance
	PowShotPower
	PowJumping
	PowStamina
	PowStrength
	PowLongShots
	MenAggression
	MenInterceptions
	MenPositioning
	MenVision
	MenPenalties
	MenComposure
	DefMarking
	DefStanding
	DefSliding
	GKDiving
	GKHandling
	GKKicking
	GKPositioning
	GKReflexes
	NumAttr
)

// AttrNames is display-order metadata for UIs. Index matches the constants above.
var AttrNames = [NumAttr]string{
	"Crossing", "Finishing", "Heading", "Short Pass", "Volleys",
	"Dribbling", "Curve", "Free Kick", "Long Pass", "Ball Control",
	"Acceleration", "Sprint Speed", "Agility", "Reactions", "Balance",
	"Shot Power", "Jumping", "Stamina", "Strength", "Long Shots",
	"Aggression", "Interceptions", "Positioning", "Vision", "Penalties",
	"Composure", "Marking", "Standing Tackle", "Sliding Tackle",
	"GK Diving", "GK Handling", "GK Kicking", "GK Positioning", "GK Reflexes",
}

const (
	FootRight uint8 = 0
	FootLeft  uint8 = 1
)

const (
	WorkLow uint8 = iota
	WorkMedium
	WorkHigh
)

// Player holds both the immutable scouting data imported from the dataset and
// the mutable career state that evolves as seasons are played. Static fields
// come from the packed asset; dynamic fields are initialised at new-game time.
type Player struct {
	ID       uint32
	ClubID   uint16
	NationID uint16

	Name     string // short display name, e.g. "J. Bellingham"
	FullName string

	BirthYear  uint16
	BirthMonth uint8
	BirthDay   uint8

	HeightCM uint8
	WeightKG uint8

	Positions    [3]Pos
	NumPositions uint8

	Attr [NumAttr]uint8

	Potential   uint8 // ceiling of current ability
	Foot        uint8
	WeakFoot    uint8 // 1-5
	SkillMoves  uint8 // 1-5
	IntRep      uint8 // 1-5
	WorkRateAtk uint8
	WorkRateDef uint8

	ValueEUR      uint32 // market value in whole euros
	WageEUR       uint32 // weekly wage in whole euros
	ContractUntil uint16 // year the contract expires
	Jersey        uint8

	// ---- dynamic career state ----

	Fitness    uint8  // 0-100, drains with minutes played
	Sharpness  uint8  // 0-100, match fitness, rises with game time
	Morale     uint8  // 0-100
	Form       int8   // -10..+10 rolling performance
	InjuryDays uint16 // days remaining out injured
	Suspension uint8  // matches remaining suspended

	// Season-to-date tallies, reset each new season.
	Apps       uint16
	Goals      uint16
	Assists    uint16
	MinutesSum uint32
	Yellows    uint8
	Reds       uint8
	RatingSum  uint32 // sum of match ratings * 100, divided by Apps for average
}

// Primary returns the player's best position.
func (p *Player) Primary() Pos {
	if p.NumPositions == 0 {
		return CM
	}
	return p.Positions[0]
}

// PlaysPos reports whether the player lists pos among their natural positions.
func (p *Player) PlaysPos(pos Pos) bool {
	for i := uint8(0); i < p.NumPositions && i < 3; i++ {
		if p.Positions[i] == pos {
			return true
		}
	}
	return false
}

// Age returns the player's age in whole years on the given date.
func (p *Player) Age(year int, month, day int) int {
	a := year - int(p.BirthYear)
	if month < int(p.BirthMonth) || (month == int(p.BirthMonth) && day < int(p.BirthDay)) {
		a--
	}
	if a < 0 {
		return 0
	}
	return a
}

// A returns an attribute as an int, for convenient arithmetic.
func (p *Player) A(i int) int { return int(p.Attr[i]) }

// IsGK reports whether this player is a goalkeeper.
func (p *Player) IsGK() bool { return p.Primary() == GK }

// Available reports whether the player can be selected for a match.
func (p *Player) Available() bool { return p.InjuryDays == 0 && p.Suspension == 0 }

// AvgRating returns the season average match rating, or 0 with no appearances.
func (p *Player) AvgRating() float64 {
	if p.Apps == 0 {
		return 0
	}
	return float64(p.RatingSum) / 100 / float64(p.Apps)
}

// AttrWeight ties an attribute index to how much it matters somewhere.
type AttrWeight struct {
	Attr int
	W    float64
}

// PositionWeights returns the attributes that define competence at a position.
// Training uses it to invest a player's development where it actually helps.
func PositionWeights(p Pos) []AttrWeight {
	if p >= NumPos {
		p = CM
	}
	return positionWeights[p]
}

// positionWeights maps each position to the attributes that define competence
// there, and how much each matters. Weights need not sum to any fixed total;
// Rating normalises by the sum.
var positionWeights = [NumPos][]AttrWeight{
	GK:  {{GKReflexes, 3}, {GKDiving, 3}, {GKPositioning, 2.5}, {GKHandling, 2}, {GKKicking, 1}, {MovReactions, 2}, {MenComposure, 1}},
	CB:  {{DefMarking, 3}, {DefStanding, 3}, {DefSliding, 1.5}, {AtkHeading, 2}, {PowStrength, 2}, {MenInterceptions, 2.5}, {MovReactions, 1.5}, {PowJumping, 1}, {AtkShortPass, 1}, {MenComposure, 1}, {MovSprintSpeed, 1}},
	LB:  {{DefMarking, 2}, {DefStanding, 2.2}, {DefSliding, 1.5}, {MovSprintSpeed, 2}, {MovAcceleration, 1.8}, {PowStamina, 2}, {AtkCrossing, 1.8}, {MenInterceptions, 1.8}, {SklBallControl, 1}, {AtkShortPass, 1.2}},
	RB:  {{DefMarking, 2}, {DefStanding, 2.2}, {DefSliding, 1.5}, {MovSprintSpeed, 2}, {MovAcceleration, 1.8}, {PowStamina, 2}, {AtkCrossing, 1.8}, {MenInterceptions, 1.8}, {SklBallControl, 1}, {AtkShortPass, 1.2}},
	LWB: {{MovSprintSpeed, 2.4}, {PowStamina, 2.4}, {AtkCrossing, 2.2}, {DefStanding, 1.8}, {DefMarking, 1.5}, {SklDribbling, 1.5}, {AtkShortPass, 1.4}, {MenInterceptions, 1.5}},
	RWB: {{MovSprintSpeed, 2.4}, {PowStamina, 2.4}, {AtkCrossing, 2.2}, {DefStanding, 1.8}, {DefMarking, 1.5}, {SklDribbling, 1.5}, {AtkShortPass, 1.4}, {MenInterceptions, 1.5}},
	CDM: {{MenInterceptions, 3}, {DefStanding, 2.5}, {DefMarking, 2}, {AtkShortPass, 2.2}, {SklLongPass, 1.8}, {PowStrength, 1.8}, {PowStamina, 1.8}, {MovReactions, 1.5}, {MenComposure, 1.5}, {MenAggression, 1.2}},
	CM:  {{AtkShortPass, 3}, {SklLongPass, 2.2}, {MenVision, 2.5}, {SklBallControl, 2.2}, {PowStamina, 2}, {MenComposure, 1.8}, {SklDribbling, 1.5}, {MenInterceptions, 1.5}, {DefStanding, 1.2}, {MovReactions, 1.5}},
	CAM: {{MenVision, 3}, {AtkShortPass, 2.5}, {SklDribbling, 2.5}, {SklBallControl, 2.5}, {MenPositioning, 2}, {AtkFinishing, 1.8}, {SklCurve, 1.5}, {MenComposure, 1.8}, {PowLongShots, 1.5}, {MovAgility, 1.5}},
	LM:  {{AtkCrossing, 2.4}, {SklDribbling, 2.4}, {MovSprintSpeed, 2.2}, {MovAcceleration, 2}, {AtkShortPass, 1.8}, {PowStamina, 1.8}, {SklBallControl, 1.8}, {MovAgility, 1.5}},
	RM:  {{AtkCrossing, 2.4}, {SklDribbling, 2.4}, {MovSprintSpeed, 2.2}, {MovAcceleration, 2}, {AtkShortPass, 1.8}, {PowStamina, 1.8}, {SklBallControl, 1.8}, {MovAgility, 1.5}},
	LW:  {{SklDribbling, 3}, {MovAcceleration, 2.6}, {MovSprintSpeed, 2.6}, {AtkCrossing, 2}, {SklBallControl, 2.2}, {AtkFinishing, 1.8}, {MovAgility, 1.8}, {SklCurve, 1.4}},
	RW:  {{SklDribbling, 3}, {MovAcceleration, 2.6}, {MovSprintSpeed, 2.6}, {AtkCrossing, 2}, {SklBallControl, 2.2}, {AtkFinishing, 1.8}, {MovAgility, 1.8}, {SklCurve, 1.4}},
	CF:  {{AtkFinishing, 2.8}, {SklBallControl, 2.4}, {MenPositioning, 2.4}, {AtkShortPass, 2}, {SklDribbling, 2.2}, {MenComposure, 2}, {MenVision, 1.6}, {PowShotPower, 1.6}},
	ST:  {{AtkFinishing, 3.2}, {MenPositioning, 2.8}, {PowShotPower, 2}, {AtkHeading, 1.8}, {MovAcceleration, 1.8}, {MenComposure, 2}, {PowStrength, 1.6}, {SklBallControl, 1.6}, {MovReactions, 1.6}},
}

// Rating is the player's current ability when fielded at pos, on the familiar
// 1-99 scale. It is derived from the underlying attributes rather than stored,
// so training and decline flow through to selection and match play consistently.
func (p *Player) Rating(pos Pos) float64 {
	if pos >= NumPos {
		pos = p.Primary()
	}
	w := positionWeights[pos]
	var sum, tot float64
	for _, x := range w {
		sum += float64(p.Attr[x.Attr]) * x.W
		tot += x.W
	}
	base := sum / tot

	// A keeper played outfield, or an outfielder in goal, is close to useless.
	if (pos == GK) != p.IsGK() {
		return math.Max(1, base*0.35)
	}
	return base * p.familiarity(pos)
}

// familiarity penalises playing someone away from their natural position.
func (p *Player) familiarity(pos Pos) float64 {
	if p.PlaysPos(pos) {
		return 1.0
	}
	best := 0.72 // fully out of position
	for i := uint8(0); i < p.NumPositions && i < 3; i++ {
		if f := posAffinity(p.Positions[i], pos); f > best {
			best = f
		}
	}
	return best
}

// posAffinity scores how transferable competence at position a is to position b.
func posAffinity(a, b Pos) float64 {
	if a == b {
		return 1.0
	}
	if a.Line() == b.Line() {
		// Same unit, but flanks and centre are still different jobs.
		if sameFlank(a, b) {
			return 0.93
		}
		return 0.85
	}
	// Adjacent units transfer better than distant ones.
	d := int(a.Line()) - int(b.Line())
	if d < 0 {
		d = -d
	}
	switch d {
	case 1:
		return 0.80
	case 2:
		return 0.72
	}
	return 0.65
}

func flankOf(p Pos) int {
	switch p {
	case LB, LWB, LM, LW:
		return -1
	case RB, RWB, RM, RW:
		return 1
	}
	return 0
}

func sameFlank(a, b Pos) bool { return flankOf(a) == flankOf(b) }

// CurrentAbility is the player's rating in their best position.
func (p *Player) CurrentAbility() float64 { return p.Rating(p.Primary()) }
