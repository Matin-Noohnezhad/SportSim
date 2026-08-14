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

**One engine, two presentations.** A match is fully simulated the moment it is
played, producing a complete event stream. "Watching" a match minute by minute
just reveals events that have already been decided. The quick result and the
detailed feed can never disagree, because there is only one engine.

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

The engine is calibrated against real top-division rates, and the numbers below
are asserted by `TestSeasonCalibration`, which plays a full 4,676-match season
on every test run:

| | Simulated | Real |
|---|---|---|
| Goals per match | 2.69 | ~2.75 |
| Home / away goals | 1.51 / 1.18 | 1.55 / 1.20 |
| Shots per match | 25.2 | ~25 |
| Shots on target | 8.6 | ~8.5 |
| Corners | 9.9 | ~10.5 |
| Fouls | 22.8 | ~22 |
| Yellow / red cards | 3.6 / 0.12 | 3.9 / 0.11 |
| Home wins / draws | 44.0% / 23.4% | 44% / 26% |

A representative simulated season:

```
Premier League    85..23 pts   Manchester City      Ligue 1        90..21 pts   Paris Saint-Germain
La Liga          105..16 pts   Real Madrid          Primeira Liga  82..26 pts   SL Benfica
Bundesliga        78..25 pts   FC Bayern München    Eredivisie     78..18 pts   Feyenoord
Serie A           85..25 pts   Inter                Süper Lig      87..22 pts   Galatasaray SK
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
engine/match      match simulation, commentary
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
| `TestMultiSeason` | three seasons leave league sizes, squads and ages intact |
| `TestSaveRoundTrip` | a save reloads and continues on the same random stream |
| `TestScreensRender` | every screen renders at every cursor position |
| `TestResourceUse` | reports binary, memory and speed figures |
```
