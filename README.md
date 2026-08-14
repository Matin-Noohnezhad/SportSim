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
| Memory in play | ~5 MB |
| Startup | 20 ms |
| Save file | ~830 KB, a season of match reports included |
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
| `←` `→` | on the league screen, change division; on the market, change the sort |
| `tab` | switch view: the table and the season statistics, or a match report's pages |
| `enter` | on the fixture list, open a played match's report |
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
| `tab` `←` `→` | commentary, overview, key moments, player ratings |
| `enter` | skip to full time; again to leave and let the day finish |
| `+` / `-` | speed the feed up or slow it down |

The clock stops on its own whenever a panel is open, so you are never hurried
into a decision. Five substitutions, and a goalkeeper can only be replaced by a
goalkeeper. Changing formation keeps the same eleven on the pitch — they move
into the new shape, your keeper stays in goal.

**Everything you do from the dugout lasts ninety minutes and no longer.** Go
three at the back to see out a lead, throw a striker on, push the line up chasing
a goal — at full time the club reverts to the shape, instructions and eleven you
picked on the squad and tactics screens. Those are the lasting decisions, and
they are where a permanent change is made; the touchline is for this match.

Two more things worth knowing. While you are on the touchline the rest of the day
is held back: the other results, wages and the calendar only move once you leave.
And the engine stops picking your substitutions the moment you take charge — it
will still force a change if you leave an injured player on, but the rest are
yours to spend. Watch with `L` off and it manages the match for you, as before.

## Match reports

Every match has four pages, and `tab` moves between them: the commentary, an
overview, the key moments and your players' ratings. They are there while the
match is being played — the clock keeps running behind them, so checking the
shot count costs you nothing — and they are what the screen settles on at full
time.

```
  COMMENTARY │ OVERVIEW │ KEY MOMENTS │ PLAYERS

  MATCH STATS         Athletic Club  v  Girona FC

  Possession              60%  ██████████████▒▒▒▒▒▒▒▒▒▒  40%
  Shots                    18  █████████████████████▒▒▒  3
  Shots on target           7  █████████████████████▒▒▒  1
  Expected goals         2.61  ██████████████████████▒▒  0.22
  Corners                   3  █████████▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒  5
  Fouls                     9  █████████▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒  14
  Offsides                  0  ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒  1
  Yellow cards              0  ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒  3
  Red cards                 0  ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒  1
```

Key moments is the match in the half-dozen lines that decided it — the goals,
who assisted them, which came from the spot, and the sendings-off — in the order
they happened, with the score as it stood after each:

```
   MIN  EVENT          PLAYER               ASSIST               CLUB               SCORE
    1'  GOAL           Jauregizar           Nico Williams        Athletic Club      1-0
    6'  GOAL           Iñaki Williams       Nico Williams        Athletic Club      2-0
   26'  RED CARD       David López                               Girona FC
   81'  PENALTY        Sancet                                    Athletic Club      3-0
   84'  GOAL           Nico Williams        Yuri Berchiche       Athletic Club      4-0
```

The same report is on `f`, the fixture list: `enter` on any match you have
already played reopens it, months later, exactly as it read at full time. A
played fixture keeps its box score and its incidents, and your own matches keep
their player ratings too — the commentary is the one page a match you are no
longer watching cannot offer.

## The transfer market

`r` opens the market on all 6,400-odd players in the world, best first. You
narrow it down rather than guessing a name: the filter bar is always on screen,
`tab` steps through it, and the list re-searches as you type.

```
  NAME [any           ]  POS ‹CM  ›  MAX AGE [23 ]  MIN RAT [80 ]  MAX FEE €M [any ]  SHOW ‹all players   ›
  sorted by rating  ·  14 players

  NAME                 POSITION    AGE  RAT  POT    FIT CLUB                  ASKING     WAGE
 ★ J. Bellingham       CAM/CM       22   88   93  +4 CM Real Madrid          €332.7M    €200k
   Pedri               CM/CDM/CAM   23   89   93  +5 CM FC Barcelona         €384.8M    €170k
   W. Endrick          CM/CAM       21   82   90  +1 CM Olympique Lyonnais ⧗  €48.2M     €61k
```

Fields you type into are bracketed; fields that cycle through a list sit between
arrows and move with `←` `→`. Three columns are worth explaining:

- **POSITION** is every position the player is natural in, not just their best.
  Two thirds of the database list two or three, and a search for a centre
  midfielder that only looked at the first would miss most of them.
- **FIT** is what signing them would actually do to your side: how many rating
  points they would add over the best you already have, and where. A dash means
  they would not improve you. It is the column that answers "good compared with
  what?".
- **ASKING** is the selling club's price, not the book value, and it turns red
  when it is beyond your budget — such players are still listed, because you may
  be planning a sale to fund the move. `⧗` beside a club marks a deal running
  out at the end of the season, which is why the fee is a fraction of the value.

| Key | Action |
|---|---|
| `tab` `shift+tab` | move between the results and the filter fields |
| `/` | jump straight to the name filter |
| `enter` | on the list, open the bid panel; in a filter, go back to the list |
| `*` | shortlist a player, or take them off it |
| `n` | fill the filters with the position your squad is thinnest in |
| `o` `←` `→` | change the sort: rating, improvement, potential, age, fee, wage |
| `c` | clear the filters |
| `v` | full profile |

The shortlist is kept with your career, so it survives a save. `SHOW` switches
the list between everybody, your shortlist, free agents, and players whose
contracts expire this summer.

`enter` opens the negotiation panel rather than firing a blind bid. It tells you
what the club wants and what the player wants, and opens pre-filled with terms
that would be accepted — so signing someone you have already decided on is still
`enter` `enter` — but the fee, the wage and the contract length are all yours to
move first. Clubs will come down a little from the asking price and no further,
so there is real money in trying.

```
  ╭─────────────────────────────────────────────────────────╮
  │ Bid for B. Saka  RW/RM · 24 · rated 86, potential 88    │
  │                                                         │
  │   Arsenal want   €123.1M                                │
  │   He wants       €230k/wk                               │
  │                                                         │
  │   Fee          €113.2M                                  │
  │   Wage        €230k/wk                                  │
  │   Years              4                                  │
  │                                                         │
  │   budget €158.2M · wage room €778.7k/wk                 │
  │   [↑↓] field  [←→] adjust  [enter] submit  [esc] cancel │
  ╰─────────────────────────────────────────────────────────╯
```

Nobody moves club for a pay cut. A player under contract asks for at least what
they already earn, which is what keeps wage demands and wage budgets — both
derived from the squads as imported — on the same scale.

## Statistics

`tab` on the league screen turns the table into the division's season in full:
the aggregate — matches played, goals per match, the home/draw/away split, cards,
clean sheets and the biggest win so far — and then six charts, for goals, assists,
clean sheets, average match rating, yellow cards and red cards.

```
  TOP SCORERS                                    TOP ASSISTS
   1 J. Alvarez        Atlético Madrid  24 9 pen  1 J. Bellingham   Real Madrid    10  6 gls
   2 R. Lewandowski    FC Barcelona     22 5 pen  2 Raphinha        FC Barcelona    8 15 gls
```

Goals scored from the spot are counted separately and shown beside the tally,
here and on a player's profile, because a striker on twenty-four with nine
penalties is not the same player as one on twenty-four without. Penalties go to
the club's nominated taker, so they concentrate the way they do in real
football. Clean sheets are a goalkeeping record and are only credited to a
keeper who saw at least an hour of the match out. The rating chart asks for
appearances in half the matches played so far, which is why its qualifying bar
is written into its heading.

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
| Goals per match | 2.74 | ~2.75 |
| Home / away goals | 1.51 / 1.23 | 1.55 / 1.20 |
| Home wins / draws | 44.8% / 22.7% | 44% / 26% |
| Home share of all points | 56.7% | ~56% |
| Penalties per match | 0.27 | ~0.27 |
| Goals from the spot | 9.8% | ~9% |
| Goals assisted | 70.4% | ~70% |

The box score comes from `TestEngineSanity`, which plays the same fixtures with
every squad in neutral condition, so it isolates the match engine from a season's
wear and tear:

| | Simulated | Real |
|---|---|---|
| Shots per match | 24.9 | ~25 |
| Shots on target | 8.4 | ~8.5 |
| Corners | 10.3 | ~10.5 |
| Fouls | 21.8 | ~22 |
| Yellow / red cards | 3.81 / 0.15 | 3.9 / 0.11 |

A representative simulated season:

```
Premier League    85..24 pts   Liverpool            Ligue 1        76..22 pts   Paris Saint-Germain
La Liga           94..14 pts   Real Madrid          Primeira Liga  88..23 pts   SL Benfica
Bundesliga        83..19 pts   FC Bayern München    Eredivisie     79..16 pts   AZ Alkmaar
Serie A           91..10 pts   Inter                Süper Lig      95..19 pts   Galatasaray SK
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
| `TestTouchlineChangesAreForOneMatch` | nothing decided in the dugout outlives the final whistle |
| `TestVenueAlternation` | no club plays three league games running at the same ground |
| `TestRoundRobinComplete` | every pair still meets twice, once at each ground |
| `TestMultiSeason` | three seasons leave league sizes, squads and ages intact |
| `TestSaveRoundTrip` | a save reloads and continues on the same random stream |
| `TestSeasonStats` | the charts agree with the fixtures they were compiled from |
| `TestMatchReport` | a match reopened from the fixture list reads as it did at full time |
| `TestFixtureReportNavigation` | the fixture list opens a played match by keypress |
| `TestStatsResetEachSeason` | no season tally survives the summer |
| `TestMarketSearch` | every filter bites, a player is found by any of their positions, and the whole market searches fast enough to run on a keystroke |
| `TestQuoteIsFree` | pricing a signing moves no money, no player and no random state |
| `TestShortlist` | the shortlist round-trips through the search scope |
| `TestImprovementIsAgainstOurSquad` | the fit column measures a target against your own players, in a position they actually play |
| `TestTransferMarket` | the market is filtered, sorted, shortlisted and bid on by keypress |
| `TestScreensRender` | every screen renders at every cursor position |
| `TestKeyNavigation` | every screen's key bindings move the cursor without panicking |
| `TestTouchlineControl` | a match is managed from kickoff to full time by keypress |
| `TestNewGameFlow` | the club picker starts a career end to end |
| `TestResourceUse` | reports binary, memory and speed figures |
```
