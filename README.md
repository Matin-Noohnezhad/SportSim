# SportSim

A football management simulation with no match graphics — all the squad
building, transfers, tactics and season progression, and none of the twelve
gigabytes of stadium textures.

Real clubs, real players, real squads: 13 divisions across Europe's eight
strongest football countries, 252 clubs and 6,400 players imported from
current EA FC data.

```
 Arsenal  3rd Premier League                        Sat 28 Nov 2026   2026/27   €121.4M

  NEXT MATCH
  Liverpool (away)  2 Dec  — in 4 days

  PREMIER LEAGUE
   1. Manchester City           15  10   3   2   37    8   +29   33  WDWWWW
   2. Chelsea                   15  10   2   3   33   16   +17   32  DWWWWL
   3. Arsenal                   15   9   5   1   23   12   +11   32  WDWDWD
   4. Brighton & Hove Albion    15   9   4   2   23    9   +14   31  WLWDDD

  CLUB
  Balance €36.9M    Transfer budget €121.4M    Wages €3.4M of €3.9M per week
  Squad 23 players    Stadium 56,791    Reputation 94/100
```

## What it costs you

| | |
|---|---|
| Binary | 6.1 MB, single file, no install |
| Game database | 446 KB, embedded in the binary |
| Memory in play | ~4 MB |
| Startup | 20 ms |
| Save file | ~580 KB |
| Full European season | 4,676 matches in ~3 seconds |

There is nothing to download at runtime and nothing to install. The whole
game is one executable.

## Running it

```sh
go build -o sportsim ./cmd/sportsim
./sportsim
```

```
sportsim              start a new career
sportsim -continue    resume the most recent save
sportsim -load FILE   resume a specific save
sportsim -list        list saved careers
```

Saves live in `~/.sportsim/saves`.

### Keys

| Key | Action |
|---|---|
| `space` | advance one day |
| `w` / `m` | fast-forward to your next match / 30 days |
| `s` `t` `l` `f` `r` `i` | squad, tactics, league, fixtures, transfers, inbox |
| `L` | toggle between a minute-by-minute feed and an instant result |
| `S` | save |
| `q` | back, or quit from the home screen |

During a match, when the feed is running you are in the dugout and the keys
change:

| Key | Action |
|---|---|
| `space` | stop and restart the clock |
| `s` | substitutions — pick who comes off, then who replaces them |
| `t` | shape and instructions — formation, mentality, tempo, pressing and the rest |
| `enter` | skip to full time; again to leave and let the day finish |
| `+` / `-` | speed the feed up or slow it down |

The clock stops on its own whenever a panel is open, so you are never hurried
into a decision. Five substitutions, and a goalkeeper can only be replaced by a
goalkeeper. Changing formation keeps the same eleven on the pitch — they move
into the new shape, your keeper stays in goal.

Two things worth knowing. While you are on the touchline the rest of the day is
held back: the other results, wages and the calendar only move once you leave.
And the engine stops picking your substitutions the moment you take charge — it
will still force a change if you leave an injured player on, but the rest are
yours to spend. Watch with `L` off and it manages the match for you, as before.

## Leagues

| Country | Tier 1 | Tier 2 |
|---|---|---|
| England | Premier League | Championship |
| Spain | La Liga | La Liga 2 |
| Germany | Bundesliga | 2. Bundesliga |
| Italy | Serie A | Serie B |
| France | Ligue 1 | Ligue 2 |
| Portugal | Primeira Liga | — |
| Netherlands | Eredivisie | — |
| Türkiye | Süper Lig | — |

Portugal, the Netherlands and Türkiye are top-flight only because the source
dataset does not license their second divisions. Those three leagues therefore
have no promotion or relegation; the other five countries exchange three clubs
each season. The data model supports any number of tiers per country, so a
second division is a single line in `cmd/importer/main.go` if a source for one
turns up.

## How the simulation works

**One engine, however you play a match.** Resolving a match instantly is the
same code as watching it minute by minute, which is the same code as managing it
from the touchline — the instant result is simply the live match with nobody
watching. Stopping the clock costs nothing and neither does any instruction you
give, so a match you paused eight times comes out identical, to the last shot, to
the one you skipped. You cannot reroll a result by watching it.

**Home advantage is real.** The home side gets an edge on possession, on attack
and on defence, and because the attacking edge feeds a threat ratio that is then
raised to a power, a 6% advantage compounds into a much larger share of the
chances. Play the same two squads home and away and the venue alone is worth a
quarter of a goal; over a season the home side takes 56% of all points.

**Ability is derived, not stored.** A player's rating comes from 34 underlying
attributes weighted by the position they are playing. Train a winger and their
rating moves; play them at centre-back and it drops. There is no separate
"overall" number that could drift out of step with the attributes.

**Chances are shared, not created.** Team strength decides how a match's chances
are *split*, not how many there are. A mismatch produces a 20–6 shot count, the
way real football does, rather than forty shots. This matters more than it
sounds: making chance creation grow with the mismatch inflates scoring across
the entire calendar whenever squads are unevenly worn down.

**Everything is seeded.** One master seed drives every random draw, and each
match derives its own generator from the fixture. Replaying a save reproduces
the same history exactly.

### Calibration

The engine is calibrated against real top-division rates, and every figure below
is measured on each test run over a full 4,676-match slate.

Scorelines come from `TestSeasonCalibration`, which plays an actual season with
squads that tire, lose form and pick up injuries as it goes — that test is the
binding target:

| | Simulated | Real |
|---|---|---|
| Goals per match | 2.72 | ~2.75 |
| Home / away goals | 1.48 / 1.24 | 1.55 / 1.20 |
| Home wins / draws | 43.6% / 23.7% | 44% / 26% |
| Home share of all points | 55.9% | ~56% |

The box score comes from `TestEngineSanity`, which plays the same fixtures with
every squad in neutral condition, so it isolates the match engine from a season's
wear and tear:

| | Simulated | Real |
|---|---|---|
| Shots per match | 24.9 | ~25 |
| Shots on target | 8.5 | ~8.5 |
| Corners | 10.2 | ~10.5 |
| Fouls | 21.8 | ~22 |
| Yellow / red cards | 3.75 / 0.15 | 3.9 / 0.11 |

A representative simulated season:

```
Premier League    82..24 pts   Manchester City      Ligue 1        73..26 pts   Paris Saint-Germain
La Liga           95..22 pts   Real Madrid          Primeira Liga  88..29 pts   SL Benfica
Bundesliga        80..26 pts   FC Bayern München    Eredivisie     78..20 pts   AZ Alkmaar
Serie A           85..24 pts   Inter                Süper Lig      91..18 pts   Galatasaray SK
```

## Architecture

The simulation knows nothing about the terminal. Every frontend goes through
the `game` package, which exposes manager actions and queries as plain methods
over plain structs — directly serialisable, so an HTTP handler or a mobile
client is a new package beside `ui/tui`, not a rewrite.

```
cmd/sportsim      terminal entry point
cmd/importer      build-time CSV → packed database (run once)

game/             ← the façade every frontend calls
engine/model      players, clubs, leagues, positions, tactics
engine/match      match simulation, live clock and touchline, commentary
engine/season     fixtures, tables, promotion, relegation, rollover
engine/dev        growth, decline, fitness, morale, valuation
engine/transfer   asking prices, negotiation, AI market
engine/rng        deterministic seeded random source

data/pack         compact binary database format
assets/           embedded database (world.dat)
store/            save and load
ui/tui            terminal frontend  ← one of potentially several
```

Go was chosen for the single static binary, the low memory footprint, and
cross-compilation: `GOOS=windows go build ./cmd/sportsim` produces a Windows
executable with no other changes.

## Rebuilding the database

`assets/world.dat` is committed, so this is only needed to refresh the player
data:

```sh
go run ./cmd/importer -in data/players.csv -out assets/world.dat
```

The importer reads an EA FC player CSV and writes the packed format: a
deduplicated string table plus fixed-width records, 71 bytes per player. Club
reputation, stadium capacity and finances are not in the source data and are
derived from squad strength, normalised against the range the dataset actually
spans.

## Tests

```sh
go test ./...
```

| Test | What it guards |
|---|---|
| `TestSeasonCalibration` | a full season stays within real football's rates |
| `TestEngineSanity` | the engine alone stays in plausible bounds |
| `TestDeterminism` | one seed reproduces a match exactly |
| `TestLiveEqualsSim` | a match watched in fragments is identical to one played straight through |
| `TestTakeChargeKeepsSubs` | the engine does not spend a manager's substitutions |
| `TestLiveSubstitution` `TestLiveReshape` | touchline changes land, and obey the rules of the game |
| `TestVenueAlternation` | no club plays three league games running at the same ground |
| `TestRoundRobinComplete` | every pair still meets twice, once at each ground |
| `TestMultiSeason` | three seasons leave league sizes, squads and ages intact |
| `TestSaveRoundTrip` | a save reloads and continues on the same random stream |
| `TestScreensRender` | every screen renders at every cursor position |
| `TestKeyNavigation` | every screen's key bindings move the cursor without panicking |
| `TestTouchlineControl` | a match is managed from kickoff to full time by keypress |
| `TestNewGameFlow` | the club picker starts a career end to end |
| `TestResourceUse` | reports binary, memory and speed figures |
```
