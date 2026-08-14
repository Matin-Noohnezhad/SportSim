package match

import "fmt"

// Names resolves the identifiers carried by an Event into display text. Every
// frontend needs the same commentary, so it is generated here rather than in
// any one UI.
type Names struct {
	Player func(uint32) string
	Team   func(uint8) string
}

// Commentary renders an event as a line of match commentary. The event's
// Variant field selects the phrasing, so replaying a match always reads the
// same way.
func (n Names) Commentary(e Event) string {
	p := func(id uint32) string {
		if n.Player == nil || id == 0 {
			return "someone"
		}
		return n.Player(id)
	}
	team := func(t uint8) string {
		if n.Team == nil {
			return "the team"
		}
		return n.Team(t)
	}
	pick := func(v uint8, opts ...string) string { return opts[int(v)%len(opts)] }

	switch e.Type {
	case EvGoal:
		if e.Other != 0 {
			return pick(e.Variant,
				fmt.Sprintf("GOAL! %s finishes off a fine move, set up by %s.", p(e.Player), p(e.Other)),
				fmt.Sprintf("GOAL! %s slots it home after a clever ball from %s.", p(e.Player), p(e.Other)),
				fmt.Sprintf("GOAL! %s finds the corner, %s with the assist.", p(e.Player), p(e.Other)),
				fmt.Sprintf("GOAL! %s squares it and %s cannot miss.", p(e.Other), p(e.Player)),
				fmt.Sprintf("GOAL! %s rifles it in, teed up by %s.", p(e.Player), p(e.Other)),
			)
		}
		return pick(e.Variant,
			fmt.Sprintf("GOAL! %s drives it past the keeper.", p(e.Player)),
			fmt.Sprintf("GOAL! %s picks up the loose ball and buries it.", p(e.Player)),
			fmt.Sprintf("GOAL! A wonderful individual effort from %s.", p(e.Player)),
			fmt.Sprintf("GOAL! %s beats his man and finishes coolly.", p(e.Player)),
			fmt.Sprintf("GOAL! %s pounces on the rebound.", p(e.Player)),
		)

	case EvPenaltyScored:
		return pick(e.Variant,
			fmt.Sprintf("PENALTY — scored! %s sends the keeper the wrong way.", p(e.Player)),
			fmt.Sprintf("PENALTY — scored! %s makes no mistake from twelve yards.", p(e.Player)),
			fmt.Sprintf("PENALTY — scored! Ice cold from %s.", p(e.Player)),
		)

	case EvPenaltyMissed:
		return pick(e.Variant,
			fmt.Sprintf("PENALTY SAVED! %s guesses right and denies %s.", p(e.Other), p(e.Player)),
			fmt.Sprintf("PENALTY MISSED! %s puts it wide of the post.", p(e.Player)),
			fmt.Sprintf("PENALTY SAVED! %s cannot beat %s from the spot.", p(e.Player), p(e.Other)),
		)

	case EvShotSaved:
		return pick(e.Variant,
			fmt.Sprintf("%s tests the keeper — %s holds it.", p(e.Player), p(e.Other)),
			fmt.Sprintf("Good save! %s turns %s's effort around the post.", p(e.Other), p(e.Player)),
			fmt.Sprintf("%s goes for goal but %s is equal to it.", p(e.Player), p(e.Other)),
			fmt.Sprintf("Fine stop from %s to keep %s out.", p(e.Other), p(e.Player)),
		)

	case EvShotOff:
		return pick(e.Variant,
			fmt.Sprintf("%s drags it wide.", p(e.Player)),
			fmt.Sprintf("%s skies it over the bar.", p(e.Player)),
			fmt.Sprintf("A chance for %s, but the finish is poor.", p(e.Player)),
			fmt.Sprintf("%s snatches at it and misses the target.", p(e.Player)),
		)

	case EvWoodwork:
		return pick(e.Variant,
			fmt.Sprintf("Off the post! %s is inches away.", p(e.Player)),
			fmt.Sprintf("Crossbar! %s so nearly has it.", p(e.Player)),
			fmt.Sprintf("So close — %s rattles the woodwork.", p(e.Player)),
		)

	case EvYellow:
		return pick(e.Variant,
			fmt.Sprintf("Yellow card for %s.", p(e.Player)),
			fmt.Sprintf("%s goes into the book for that challenge.", p(e.Player)),
			fmt.Sprintf("Booked — %s catches his man late.", p(e.Player)),
		)

	case EvSecondYellow:
		return fmt.Sprintf("SECOND YELLOW! %s is sent off, %s down to ten.", p(e.Player), team(e.Team))

	case EvRed:
		return fmt.Sprintf("RED CARD! %s is dismissed. %s must play on with ten.", p(e.Player), team(e.Team))

	case EvSub:
		return fmt.Sprintf("Substitution, %s: %s replaces %s.", team(e.Team), p(e.Other), p(e.Player))

	case EvInjury:
		return fmt.Sprintf("%s goes off injured and cannot continue.", p(e.Player))

	case EvHalfTime:
		return fmt.Sprintf("Half time. %d - %d", e.Home, e.Away)

	case EvFullTime:
		return fmt.Sprintf("Full time. %d - %d", e.Home, e.Away)

	case EvShape:
		return fmt.Sprintf("Tactical change from the %s bench.", team(e.Team))
	}
	return ""
}

// Major reports whether an event is significant enough to show in a condensed
// summary, as opposed to the full minute-by-minute feed.
func (e Event) Major() bool {
	switch e.Type {
	case EvGoal, EvPenaltyScored, EvPenaltyMissed, EvRed, EvSecondYellow,
		EvInjury, EvHalfTime, EvFullTime:
		return true
	}
	return false
}
