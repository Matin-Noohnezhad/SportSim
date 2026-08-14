package model

import "sort"

// League is one division within a country.
type League struct {
	ID       uint16
	Name     string
	NationID uint16
	Country  string
	Tier     uint8 // 1 = top flight

	ClubIDs []uint16

	Promoted  uint8 // clubs going up at season end
	Relegated uint8 // clubs going down at season end

	Reputation uint8 // 1-100, drives prize money and transfer appeal
	PrizeMoney int64 // paid to the champion; scaled down the table
}

// Nation is a country, used for player nationality and league grouping.
type Nation struct {
	ID   uint16
	Name string
	Code string // 3-letter code
}

// World is the complete game state. Everything a save file needs lives here.
//
// Players and Clubs are dense slices; an entity's ID is its index plus one, so
// lookups are array indexing rather than map probes. Zero is reserved as "none".
type World struct {
	Players []Player
	Clubs   []Club
	Leagues []League
	Nations []Nation

	Date       Date
	SeasonYear int

	// HumanClubID is the club the user manages, or 0 while unemployed.
	HumanClubID uint16

	// ManagerName is the user's chosen name.
	ManagerName string

	// Shortlist holds the players the manager is keeping an eye on. It belongs to
	// the manager rather than the club, so it survives a change of job.
	Shortlist []uint32

	Seed uint64 // master RNG seed, keeps a save deterministic
}

// Player returns a pointer to the player with the given ID, or nil.
func (w *World) Player(id uint32) *Player {
	if id == 0 || int(id) > len(w.Players) {
		return nil
	}
	return &w.Players[id-1]
}

// Club returns a pointer to the club with the given ID, or nil.
func (w *World) Club(id uint16) *Club {
	if id == 0 || int(id) > len(w.Clubs) {
		return nil
	}
	return &w.Clubs[id-1]
}

// League returns a pointer to the league with the given ID, or nil.
func (w *World) League(id uint16) *League {
	if id == 0 || int(id) > len(w.Leagues) {
		return nil
	}
	return &w.Leagues[id-1]
}

// Nation returns a pointer to the nation with the given ID, or nil.
func (w *World) Nation(id uint16) *Nation {
	if id == 0 || int(id) > len(w.Nations) {
		return nil
	}
	return &w.Nations[id-1]
}

// NationName resolves a nationality to its display name.
func (w *World) NationName(id uint16) string {
	if n := w.Nation(id); n != nil {
		return n.Name
	}
	return "Unknown"
}

// ClubName resolves a club ID to its display name, tolerating free agents.
func (w *World) ClubName(id uint16) string {
	if c := w.Club(id); c != nil {
		return c.Name
	}
	return "Free Agent"
}

// Squad returns every player contracted to a club, strongest first.
func (w *World) Squad(clubID uint16) []*Player {
	out := make([]*Player, 0, 32)
	for i := range w.Players {
		if w.Players[i].ClubID == clubID {
			out = append(out, &w.Players[i])
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return out[a].CurrentAbility() > out[b].CurrentAbility()
	})
	return out
}

// SquadSize counts players contracted to a club without allocating.
func (w *World) SquadSize(clubID uint16) int {
	n := 0
	for i := range w.Players {
		if w.Players[i].ClubID == clubID {
			n++
		}
	}
	return n
}

// WageBill returns the club's total weekly wage commitment.
func (w *World) WageBill(clubID uint16) int64 {
	var t int64
	for i := range w.Players {
		if w.Players[i].ClubID == clubID {
			t += int64(w.Players[i].WageEUR)
		}
	}
	return t
}

// HumanClub returns the managed club, or nil if unemployed.
func (w *World) HumanClub() *Club { return w.Club(w.HumanClubID) }

// Age is the age of a player today, in the world's current time.
func (w *World) Age(p *Player) int {
	return p.Age(w.Date.Year(), w.Date.Month(), w.Date.Day())
}

// LeaguesByTier returns leagues sorted by country then tier, for menus.
func (w *World) LeaguesByTier() []*League {
	out := make([]*League, 0, len(w.Leagues))
	for i := range w.Leagues {
		out = append(out, &w.Leagues[i])
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Country != out[b].Country {
			return out[a].Country < out[b].Country
		}
		return out[a].Tier < out[b].Tier
	})
	return out
}
