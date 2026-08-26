# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Git

**Never run `git commit` or `git push` unless the user has explicitly asked for it in that message.**
Finish the work, leave the changes in the working tree, and say what is ready to be committed — the
decision to record or publish a change is the user's, every time. This is not satisfied by permission
given for an earlier commit: authorisation covers the one commit it was given for and does not carry
forward to the next change, however small or obviously correct that change seems.

Staging (`git add`) is included in this — leave the working tree as the user left it. Read-only
commands (`git status`, `git diff`, `git log`) are always fine.

## Commands

```sh
go build -o sportsim ./cmd/sportsim   # build (module name is `sportsim`, import paths are `sportsim/...`)
go run ./cmd/sportsim                 # run
go test ./...                         # full suite, ~15s
go vet ./...
```

Running a single test — the suite is small and every test is named, so target by name:

```sh
go test ./game -run TestSeasonCalibration -v   # slowest (~13s): plays a full 4,676-match league season
go test ./engine/match -run TestDeterminism -v
go test ./ui/tui -run TestScreensRender -v
go test ./... -count=1                         # defeat the cache after touching tuning constants
```

Rebuilding the packed database (only when refreshing player data or adding a season;
`assets/world_2026.dat` is committed):

```sh
go run ./cmd/importer -in data/players.csv -year 2026            # -> assets/world_2026.dat
go run ./cmd/importer -in data/players_16.csv -year 2016 -inspect # what does this file offer?
```

`-year` is required and there is deliberately no default — see invariant 17.

`data/players.csv` is gitignored (11 MB EA FC export) — it is not in a clean clone, so the importer
cannot be run without obtaining it separately. Everything else builds and tests without it. The
older editions are on Kaggle in the same series (`players_15.csv` … `players_23.csv`), and their
column names differ; `-inspect` is what tells you how.

## Architecture

One rule governs the layout: **the simulation never knows what is rendering it.** `engine/*` has no
knowledge of `game`, and `game` has no knowledge of `ui/tui`. Dependencies point one way only:

```
cmd/sportsim ─▶ ui/tui ─▶ game ─▶ engine/* ─▶ engine/model
                            └───▶ assets ─▶ data/pack
                  store ────┘
```

- **`game`** is the façade every frontend calls. It owns `Game{World, Sched, Inbox, rng}` and exposes
  manager actions (`Bid`, `Sell`, `SetFormation`, `SwapLineup`, `AutoSelect`, `OfferContract`) and
  queries (`Squad`, `Table`, `Stats`, `Search`) as plain methods over plain serialisable structs.
  `game/stats.go` compiles the season's charts — scorers, assists, clean sheets, ratings, cards —
  and the division's aggregate summary; a chart is a filter, a sort and a truncation over one pass
  of the players, so adding one is a call to `topBy`, not a new query.
  `game/report.go` builds the `MatchReport` both match screens draw — see invariant 12.
  `game/transfers.go` owns the market: `Search` over a `SearchFilter`, the `Target` rows it
  returns, the shortlist, and `Quote`/`Renewal`, which price a signing without committing to
  it — see invariant 13.
  `game/europe.go` flattens the continental competitions into the plain structs a screen
  draws — `EuroView`, `EuroGroup`, `EuroRound`, `EuroTie` — including the aggregate score of
  a tie, which is worked out from its legs here rather than in a view. See invariant 16.
  `AdvanceDay()` is the heartbeat: it plays the day's fixtures, advances the European
  competitions, applies recovery, settles the week's money on Mondays, trains on the 1st of
  the month, runs the AI market, then moves the clock.
  **A second frontend (HTTP, mobile) is a new package beside `ui/tui`, not a rewrite — so nothing that belongs
  to the simulation may leak into a UI package, and nothing terminal-shaped may leak into `game`.**
  `KickOff()` (in `game/live.go`) is the one place that breaks the once-a-day rhythm: it hands back a
  `LiveMatch` for the managed club's fixture and *holds the rest of the day back* until `AdvanceDay`
  is called. A frontend that calls `KickOff` owes an `AdvanceDay`; one that never calls it sees no
  change at all, because `playFixture` opens a `LiveMatch` for every fixture either way and
  `PlayOut` finishes whatever the manager left. A part-played match is deliberately absent from save
  files — `Game.MatchInProgress()` reports it, and a frontend must refuse to save while it is true,
  since those players have already been run down by the minutes played.
- **`engine/model`** holds the data types with no behaviour beyond derivation: `World`, `Player`,
  `Club`, `League`, `Nation`, `Tactics`, `Formation`, `Pos`, `Date`.
- **`engine/match`** simulates a match: `sim.go` holds the tuning constants and the per-incident
  mechanics, `live.go` owns the clock (`Live`, and the touchline actions `Substitute`, `SetTactics`,
  `TakeCharge`), `side.go` turns a squad into eleven players and a strength. **New match logic goes in
  `Live.playMinute`, never in a caller** — a second minute loop is the one thing invariant 6 forbids.
  **`engine/season`** owns calendar, tables and rollover, and is the one engine package that
  imports another (`engine/match`, for the box score and player lines a played `Fixture` keeps, so
  that a match report has a single definition); `finance.go` holds the club economy — `SeasonGate`,
  `Commercial`, `PrizeShare`, `Revenue`, `RunningCosts`, `TransferBudget` and the European purse,
  see invariant 14; `europe.go` owns the three continental competitions end to end —
  `AssignEuropeanPlaces`, the group and knockout draws, the European calendar and `EuroAdvance`,
  see invariant 16;
  **`engine/dev`** owns growth/decline/fitness/morale/valuation; **`engine/transfer`** owns pricing
  (`AskingPrice`, and `PriceList` for pricing a whole market at once), negotiation (`WageDemand`,
  `Consider`, `HaggleFloor`) and the AI market (`Need`, `RunAI`); **`engine/rng`** is the single
  random source.
- **`ui/tui`** is Bubble Tea: `Model` in `app.go` (state + key handling), rendering in `view.go`,
  the match feed and touchline panels in `match.go`, the tabbed match report in `report.go`, the
  transfer market in `transfers.go`, the continental competitions in `europe.go`, Lip Gloss
  styles in `styles.go`. `ScreenMatch`
  intercepts keys *before* the global bindings, because from the touchline `s` and `t` are the
  substitution and shape panels rather than the squad and tactics screens. `ScreenTransfers`
  intercepts them the same way, but only while a filter field or the bid panel has focus: a
  name being typed into the market must not be read as a request to change screen, and the
  moment focus returns to the results the single letters are global bindings again. That is
  what `market.focus == filterNone` means, and why every filter is edited through it rather
  than through a modal search prompt. `confirmQuit` is a third interceptor and sits ahead of
  the global bindings for the same reason: while the prompt is up, a letter must not answer the
  question by navigating away from it. It opens with `quitYes` false — the irreversible answer
  has to be moved onto, never reached by pressing `q` twice — and only `ctrl+c` still leaves
  without asking, because arguing with the terminal's own way out would be worse than losing a
  day's play. `TestQuitIsConfirmed` drives the whole path. `ScreenTable` and
  `ScreenStats` are two views of one division and share `keyTable`, with `tab` between them; the
  charts sit two abreast on a wide terminal and stack on a narrow one, which is why `statRows` is
  told how many rows of them there will be.
- **`assets`** embeds one packed database per season (`world_YYYY.dat`) and discovers them by
  filename: `Editions()`, `Latest()`, `Load(year)`. Adding a season is importing a file into that
  directory — no code names any year.
- **`store`** gob-encodes and gzips a `snapshot` of world + schedule + inbox + RNG state, and
  `store.Path` is the one place a save file is named (club plus `World.StartYear`).

### Invariants that hold the design together

Break any of these and something distant fails — usually a calibration test, sometimes only after a
few simulated seasons.

1. **Ability is derived, never stored.** `Player.Rating(pos)` computes from 34 attributes weighted by
   `positionWeights` in `engine/model/player.go`, then scaled by positional familiarity.
   `CurrentAbility()` is the rating in the player's best position. There is deliberately no "overall"
   field that could drift out of step with the attributes — do not add one, and do not cache ratings
   in a struct field. Training moves attributes; the rating follows.

   A player has **up to three natural positions**, not one. The dataset's `player_positions` column
   is comma-separated and `parsePositions` keeps all of it: of the 6,411 players in `world.dat`,
   2,358 list one position, 2,133 list two and 1,920 list three. `Primary()` is only the first of
   them; `NaturalPositions()` is all of them and `PlaysPos` tests the whole list. Anything that
   asks "can this player do this job" — a search filter, a squad-depth count, the familiarity
   penalty in `Rating` — must use the list, because a filter that consulted `Primary()` alone
   would miss two thirds of the candidates.
2. **Everything random comes from `engine/rng`.** Never use `math/rand`. `rng.Derive(seed, key)` gives
   a match its own generator keyed on the fixture, so a match is reproducible regardless of what else
   happened that day and adding a draw elsewhere does not shift match outcomes. Save files persist the
   generator state (`Game.RNGState` / `game.Restore`) so a loaded career continues the same stream.
   `TestDeterminism` and `TestSaveRoundTrip` guard this.
3. **Chances are shared, not created.** In `engine/match/sim.go`, team strength decides only how the
   minute's chance is *split* (`threatExponent`); the *number* of chances is near-constant
   (`baseChanceRate`). Making chance count grow with the mismatch inflates scoring across the whole
   calendar as squads wear down unevenly. This is the single most load-bearing modelling decision.
4. **IDs are dense indices, not map keys.** An entity's ID is its slice index plus one; `0` means
   "none" (free agent, empty lineup slot, bye). `World.Player/Club/League/Nation` do the `id-1`
   lookup and return `nil` for `0`. Preserve this when adding entities.
5. **Persisted enums are append-only.** `Pos`, `Formation`, `EventType` and the pack format's field
   order are written into save files and `assets/world.dat`. Insert a value in the middle and every
   existing save silently misreads. Append, and bump `store.formatVersion` / `pack.version` when the
   layout itself changes.
6. **One engine, however a match is played.** `match.Sim` *is* `match.Begin` followed by
   `Live.PlayOut` — there is no second code path, and there must never be one. A match resolved
   instantly, one revealed minute by minute, and one managed from the touchline all run the same
   per-minute loop in `Live.playMinute`. Two things keep that honest and both are load-bearing:
   pausing consumes no randomness, and neither does any touchline instruction, so a match played in
   fragments is bit-identical to one played straight through (`TestLiveEqualsSim` asserts this down to
   the box score). A manager who watches must not be able to reroll a result by watching.
   The one thing that *does* legitimately differ is who picks the substitutions — see invariant 10.
7. **The league list is data, not code.** `wanted` in `cmd/importer/main.go` is the only place
   divisions are enumerated; tiers, promotion and relegation counts flow from there through the pack
   into `model.League`. Adding a division is one line plus a re-import.

   The *continental* list is not in the pack — it is not in the dataset — but it is still data:
   `competitions` in `engine/model/europe.go` is the only place the three tournaments are
   enumerated, and their size, group count and per-league cap all flow from there. Nothing else
   hard-codes "32" or "eight groups", and `season.AssignEuropeanPlaces` shares the places out from
   the reputations it finds rather than from a table of countries.
8. **No club plays three league games running at the same ground.** `roundRobin` in
   `engine/season/season.go` uses the canonical venue assignment — home or away follows the parity of
   a club's distance from the circle's stationary pivot, and that distance falls by one every round —
   which is what keeps clubs alternating. It leaves each club exactly one venue repeat per half, at
   the round it meets the pivot, so `Generate` shifts the second half on by one round to stop that
   repeat landing next to the halfway-point one. Assigning venues by anything else (position in the
   pairing loop, club identity, a coin flip) gives clubs runs of a dozen away games. `TestVenueAlternation`
   guards this across every league size from 4 to 26.
9. **A side's strength is recomputed from scratch, so nothing may be bolted on afterwards.**
   `Side.recompute` is called again on every substitution and every change of shape. Any penalty
   applied by multiplying `attack`/`defence`/`midfield` *after* it — as the red-card penalty once was
   — is silently handed back at the next change. State the cause on the `Side` (`sentOff`) and apply
   it inside `recompute`.
10. **The engine does not spend a human manager's substitutions.** `Live.TakeCharge` marks a side as
    managed from the touchline, and `maybeSub` then makes only injury-forced changes for it. This is
    the one respect in which watching a match differs from skipping it, and it has to: somebody must
    pick the subs, and when nobody is in the dugout that has to be the engine.
    `TestTakeChargeKeepsSubs` guards it.
11. **A season tally is a running total on the player, and it must agree with the fixtures.** The
    match engine reports one `match.PlayerLine` per player per game; `game.playFixture` translates it
    into a `dev.Performance` and `dev.AfterMatch` folds it into `Apps`, `Goals`, `Penalties`,
    `Assists`, `CleanSheets`, `Yellows`, `Reds`, `MinutesSum` and `RatingSum`. Three rules hold that
    together, and `TestSeasonStats` checks all of them: **penalties are counted inside goals**, never
    beside them, so `Penalties <= Goals` always; **a clean sheet is a goalkeeping record**, credited
    only to the keeper and only after `cleanSheetMinutes` on the pitch; and **every new tally must be
    cleared in `season.resetSeasonStats`**, or a striker carries ninety goals into next season
    (`TestStatsResetEachSeason`). Adding a field to the tallies also means bumping
    `store.formatVersion` — see invariant 5.

    A tally covers **every competition the player appeared in**, European nights included, because
    there is one set of counters and one `dev.AfterMatch` that folds a match into them. That is why
    `TestSeasonStats` totals *all* fixtures against the players' tallies, and why the statistics
    screen's charts are a season's record rather than a division's. Its aggregate summary is the
    other way round — `game.summarise` filters fixtures by `LeagueID`, so it is domestic only. Both
    are labelled as what they are; splitting the counters would double nine fields on every player
    and a save file with them.
12. **A match is shown through one report, however it is opened.** `game.MatchReport` is what both
    match screens draw: the touchline builds one from the live `match.Result` every frame, the
    fixture list rebuilds one from what `season.Fixture` kept of a match played months ago, and
    `TestMatchReport` asserts the two agree field for field. Anything a screen wants to show about a
    match belongs on that struct, filled in by both builders — a view that reaches past it into
    `match.Result` works on the touchline and shows nothing from the fixture list. What a fixture
    keeps is deliberately narrow: the box score and the incidents that decided the match (goals and
    sendings-off), plus player ratings for the managed club's own matches only, since a division's
    worth would be a squad of lines per fixture. Widening it costs the save file 4,676 times over,
    so weigh it, and bump `store.formatVersion` when you do — see invariant 5.
    The statistics tabs do not stop the clock: the touchline panels of invariant 10 are decisions
    and pause the match, a page of figures is not.

    A touchline decision is also **spent when the whistle goes**. Shape, instructions and
    substitutions all live on the `match.Side`, which is built fresh from `Club.Tactics`,
    `Club.Lineup` and `Club.Bench` at every kickoff and thrown away at full time, so a manager who
    goes three at the back to see out a lead has not asked to play that way in November. Nothing on
    `LiveMatch` may write back to the club — `SetFormation` and `SetTactics` once did, and it
    quietly rewrote a selection the manager had made in cold blood. The lasting decisions are made
    on the squad and tactics screens through `Game.SetFormation`, `AutoSelect` and `SwapLineup`,
    which are the only places `Club.Tactics`/`Lineup`/`Bench` change (besides
    `season.Rollover`'s summer clear-out). `TestTouchlineChangesAreForOneMatch` guards it.
13. **Asking what a signing would cost must not cost anything.** `game.Quote` and
    `transfer.WageDemand` draw no randomness and change no state, so the market screen can price
    a player on every keystroke and open a bid panel without the act of looking altering what
    happens next. `TestQuoteIsFree` asserts the RNG state, the player's club and the budget are
    all untouched by repeated quoting. This is the same bargain as invariant 6: a manager must
    not be able to reroll an outcome by inspecting it. Anything the bid panel wants to show has
    to be derivable without a draw — if a future negotiation needs randomness, it belongs in
    `Consider` at the moment the offer is actually made, never in the quote.

    Pricing the whole market is also a *linear* pass, not a quadratic one. `AskingPrice` consults
    the player's place in their club's pecking order, which costs a scan of the world; asking it
    six thousand times over is slow enough that the screen cannot re-search as the manager types.
    `transfer.NewPriceList` establishes every pecking order once and `Search` prices from that.
    Reaching for `AskingPrice` inside a loop over players puts the quadratic cost straight back.
14. **A club's books balance across five flows, and they only disagree slowly.** Everything lives
    in `engine/season/finance.go`. In: **gate receipts**, banked match by match in
    `game.playFixture`; **commercial income**, credited weekly; **broadcast money**, settled
    once a year in `season.payPrizeMoney`; and **European prize money**, paid as the rounds are
    reached in `season.EuroAdvance`. Out, both weekly in `game.settleWeek`: **wages** and
    **running costs**.

    Four rules keep them honest.

    - **Gate receipts belong to the match they were taken at**, so nothing may add a season's
      worth again at the rollover — doing exactly that paid every club twice for the same
      nineteen home games, invisibly, because ticket prices were zero at the time.
    - **Running costs are sized against revenue, not against the wage bill**, so a club cannot
      sell its way out of its overheads. Wages alone were once the only outgoing and the world's
      money grew by €10bn a season.
    - **Revenue has to be as skewed as the wage bills are.** This is the load-bearing one, and
      three separate attempts to fix the giants by tuning costs failed on it. At a stable world
      total a club is solvent exactly when its share of world revenue exceeds its share of world
      wages, which is a property of the *distributions* and cannot be tuned away with a single
      constant. Real Madrid pay 4.8% of the world's wages, so they need about 4.8% of its
      revenue; modelling only gate and a flat league pot gave them 1.0% and they lost €114m a
      year at a running-cost share of *zero*. `season.Commercial` is what closes it — sponsorship
      and merchandise are the largest stream in real football (€594m of Real Madrid's €1,161m)
      and the game had none of it.
    - **`League.PrizeMoney` is the division's whole broadcast deal, not the champion's cheque.**
      `PrizeShare` splits it half equally, a quarter on finishing position and a quarter on
      market size, and the shares sum to one. The market quarter matters as much as the total:
      while the pot was paid out flat, a mid-table Getafe drew the same television money as Real
      Madrid.

    `TicketPrice` and `Tactics` are **derived at load** in `pack`'s read loop rather than stored,
    for the reason in invariant 1: a second copy drifts. It was `TicketPrice` being computed by
    the importer and then never written to the pack that made every club charge nothing on the
    gate, leaving wages the only flow that moved and balances falling in a straight line all year.
    A club's income is not something to add a field for without checking `pack` actually persists
    it.

    - **European money has to pass through `Revenue`, not around it.** `season.ExpectedEuro` is
      what puts a club's continental campaign into `Revenue`, and `Revenue` is what sizes both
      `RunningCosts` and `TransferBudget`. Crediting the purse straight to `Balance` without it
      would give thirty-two clubs a large income that nothing spends against, which is the same
      drift that budgets-from-the-balance produced. `TestEuropeanMoneyIsEarned` guards it.

    **Transfer budgets are led by revenue and topped up by savings, and the top-up is bounded**
    (`season.TransferBudget`, used by `rebalanceBudgets`). All spending is gated on
    `Club.TransferBudget` — `transfer.CanAfford` never looks at `Balance` — so while the budget
    was half of whatever had accumulated, any drift in the books eventually handed every club
    unlimited buying power. Cutting it loose from the balance entirely fixed that and went too
    far the other way: the world banks around €4bn a season, so six seasons in Barcelona sit on
    €1.5bn and were still offered €324m, and a career of careful trading changed nothing a
    manager could see. The settlement is a war chest — a club holds `workingCapitalShare` of a
    season's revenue back as the money it runs on and puts `warChestShare` of the rest into the
    budget, capped at `warChestCap` of revenue, with the balance still the ceiling. **The cap is
    the load-bearing part**: it is sized against the club's *own* revenue, so the drift cannot
    converge every club on a bottomless purse and flatten the market's hierarchy.
    `TestTransferBudgetComesFromRevenue` guards the bound and `TestSavingsAreSpendable` guards
    the top-up; changing one without the other is how this drifts back to a flat share.

    None of this touches the drift itself, and the drift is real rather than slow: the world's
    money grows about 30% of its starting total every season, because revenue exceeds wages plus
    `runningCostShare` for nearly every club. Transfers are zero-sum between clubs, so spending
    more of it does not slow the growth — only the flows in `settleWeek` and `payPrizeMoney`
    can. Anyone tempted to close it should expect a calibration job, not a constant: the giants'
    solvency problem in this invariant was three failed attempts at exactly that.

    `TestPrizeMoneyPaidOnce`, `TestPrizeMoneyFollowsTheAudience`, `TestCommercialCarriesTheGiants`,
    `TestRunningCostsScaleWithRevenue`, `TestTransferBudgetComesFromRevenue` and the money checks
    in `TestMultiSeason` guard all of it. The figures were calibrated against the Deloitte
    Football Money League, and `game.Finances` is the one query a frontend should use.
15. **Nobody moves club for a pay cut.** `dev.WageFor` draws the wage curve for a player of a
    given standing, and it is flatter than the wages the squads were imported on: right through
    the middle of the league, but a quarter of what an international already earns. Club wage
    budgets, by contrast, come straight from the imported bill (`WageBudget = bill * 1.15`), so
    quoting demands from the bare curve lets every signing halve the buyer's wage bill and the
    market costs nothing. `dev.WageAsk` anchors the ask at the player's current terms, and every
    negotiation — `transfer.WageDemand`, `RunAI`, `Sell`, `OfferContract`, `Renewal`, and the
    renewals in `season.Rollover` — goes through it. `WageFor` is still correct for a player who
    has no current deal to anchor on: a regen being generated, or a free agent being picked up.
16. **A continental season is built while it is played, and it may never stall.** Everything is in
    `engine/season/europe.go`. A league fixture list is written out in August; a European one
    cannot be, because a knockout bracket is drawn from the clubs that came through the round
    before it. So `Generate` lays out only the group stage, and `EuroAdvance` — step 2 of
    `game.AdvanceDay`, before the clock moves — settles any round whose matches have all been
    played, draws the next one, appends its fixtures and pays what reaching it is worth.

    Four things hold that together.

    - **`EuroAdvance` runs before the season-end check, on the same day.** The final is scheduled
      the moment the semi-finals are settled, so `Sched.Complete()` never reports true with a round
      still to be drawn. Move the call after the clock and a career ends in April with three
      trophies unwon.
    - **Continental fixtures belong to no division.** `Fixture.Comp` is what tells the two apart
      and a European fixture's `LeagueID` is zero, so it can never reach a league table, a
      promotion place or `game.summarise`. `Fixture.Continental()` is the test to use.
    - **No club may be asked to play twice in a day.** `AdvanceDay` plays every fixture on the
      date and `KickOff` hands the manager the first one, so a European night that landed on a
      league Saturday would run the same players down twice and show the manager only one of the
      matches. The `calendar` type is what prevents it: it holds every club's committed days and
      `night()` walks outwards from the target date until it finds one free. Schedule a European
      fixture any other way and the clash comes straight back.
    - **Qualification is read off the final tables, before promotion and relegation move anyone.**
      `Rollover` calls `qualifyForEurope` ahead of `applyPromotionRelegation` for that reason:
      `Table` is built from `League.ClubIDs`, which relegation rewrites. A club's place is then
      `Club.Competition`, career state that stands for the whole season whatever happens to its
      league position — and the one thing `Revenue` consults to know what a club will earn abroad.

    `TestEuropeanSeason` plays a full campaign and checks all of it: three competitions from the
    draw to a trophy, every tie settled, every round halving the field, and nobody double-booked.

17. **A career's start year comes from the pack, never from a literal.** A game database is one
    edition of the source dataset — one season's squads, ratings, values and wages — and the year
    it holds travels with it as `pack.Data.Year`. `game.NewWorld` reads that and sets
    `World.SeasonYear`, `World.StartYear` and `World.Date` from it. There is no default and no
    fallback: an edition started in the wrong year dates every player's age and every contract's
    expiry silently, and the career simply plays out wrong.

    The year used to be written down in four places — `game.NewWorld`, the contract clamp in the
    importer, and both of `parseDOB`/`ageFrom`. All four now take it as a parameter. Putting a
    literal year back into any of them is the failure this invariant exists to prevent;
    `TestCareerStartsInItsEdition` checks all of it, including that nobody starts out of contract.

    Five things follow from it.

    - **`assets` discovers editions, it does not list them.** Files are named `world_YYYY.dat` and
      `Editions()` parses the year out of the name; `Load(year)` then checks the name and the
      header agree, because they are two statements of one fact. Adding a season is importing a
      file into `assets/` — no code changes at all. This is invariant 7's reasoning applied to
      seasons: the list is data.
    - **`World.StartYear` never moves, `SeasonYear` does.** The start year is what
      `store.Path` names a save file with, so that two careers at the same club in different eras
      are two files and one career stays one file across a decade of rollovers.
      `TestStartYearOutlivesTheSeason` and `TestPathNamesTheCareer` guard the pair.
    - **A division an edition did not license is dropped, not left empty.** Every entry in the
      importer's `wanted` is created before a single player is read, so an edition shipping no
      Serie B would leave a league with no clubs and `season.Generate` would build a fixture list
      for nobody. `dropEmptyLeagues` removes them and renumbers the survivors — IDs are dense
      (invariant 4), so every club's `LeagueID` is remapped with them. This is also why the club
      picker reads `league.ID` rather than assuming it is the cursor plus one.
    - **A file may hold every edition at once, and only one may be read.** The tidied
      distributions bundle FIFA 15 to 23 into a single CSV tagged with `fifa_version`;
      importing all of it would put nine copies of every player into one world. `build`
      filters on the version matching `-year` whenever the column is present.
      `fifaVersionFor` is the conversion and it is off by one on purpose: a title ships in
      the September of the season it covers and is numbered for the year after, so FIFA 15
      holds 2014/15. Getting that backwards labels every edition a season late, which is
      this invariant's failure wearing a different hat —
      `TestOneEditionIsTakenFromABundle` and `TestFifaVersionMatchesItsSeason` guard it.
    - **The dataset's column names are not stable, so the importer reads both spellings.**
      `sofifa_id` became `player_id`, `defending_marking` gained `_awareness`, the `team_` prefix
      became `club_`, and the oldest files carry no numeric `league_id` at all — only a name, in
      sofifa's own wording ("Spain Primera Division"). The `aliases` table and each spec's
      `SrcNames` are where those live, and `-inspect` prints what a file offers so that a new
      edition costs a line rather than a debugging session. An unrecognised division must fail the
      import loudly, never import nothing quietly — `TestUnknownDivisionNameIsReported`.

18. **Money is era-scaled at import, because only half of it comes from the dataset.** Wages and
    player values are in the CSV and are already correct for their season. `League.PrizeMoney` in
    the importer's `wanted` is not: it is today's broadcast deal, and paying a 2015 squad out of a
    2026 television pot makes every club far richer than it was. `revenueIndex` in
    `cmd/importer/main.go` is one factor per year that scales all thirteen divisions together.

    Together, not individually, and that is the load-bearing part: invariant 14 rests on revenue
    being distributed as unevenly as wages are, which is a property of the *spread* between leagues
    rather than of the total. Scaling them by one number moves the total and leaves the spread
    alone. Giving each division its own per-year figure would need 156 researched numbers and would
    put that spread at risk for a gain nothing yet measures.

    **The figures in `revenueIndex` are estimates and have never been checked against real data.**
    `TestOlderEditionsStaySolvent` is written to settle them — it plays a season in the oldest
    embedded edition and fails in both directions, too many clubs in the red or a giant profiting
    more than it earned — but it skips while only one edition is embedded, which is the state of
    the repository. Anyone importing a real old CSV should expect to tune the index against it, and
    should not read the test's silence as approval.

    A caution learned the expensive way: **synthetic player data cannot validate this.** A
    generated dataset has a far flatter wage and value spread than a real one, so it puts clubs of
    every era into the red regardless of the index — a fabricated 2024 edition came out *worse*
    than a fabricated 2016 one. The failure looks exactly like a mis-set index and is not one.

### Calibration is a test, not a comment

`TestSeasonCalibration` (`game/soak_test.go`) plays a full season across Europe on every run and asserts
2.50–3.00 goals per match, 39–49% home wins, 20–30% draws, 6–14% of goals from the spot, and a
believable points spread per league (68–110 for the champion, 8–45 for the bottom club, normalised to
38 games). It measures **league fixtures only** — `Fixture.Continental()` skips the European ties,
which are played by the same engine but between clubs of a different spread, and counting them in
would move every figure for a reason that is not tuning. Tuning constants live at the top of
`engine/match/sim.go`. **Any change to match
simulation, fitness, development or squad strength must be re-checked against this test** — effects
there are non-local and often only show up across a whole season. When a change legitimately shifts
the rates, update both the thresholds and the calibration table in `README.md`.

Two of those constants pay for each other and cannot be moved alone. `penaltyRate` is the share of
chances given from the spot, and a penalty converts at nearly ten times an open chance, so raising it
inflates league-wide scoring unless `baseXG` comes down to pay for it. Penalties are also a
*strength-independent* source of goals — anyone converts at about the same rate — so they quietly
level the league: cutting them widens the points spread even with scoring held constant, which is
what pushed the champion's band to 110.

A player's match rating is a second thing measured across a whole season rather than one game. It
must not carry a standing bonus for a position: a rating is compared against other players' on the
statistics screen, so a flat reward — the keeper bonus that once read `0.30 * (4 - conceded)` — puts
every keeper in the game top of the chart without having saved a thing. Judge a contribution against
what is typical (`typicalConceded`), so the adjustment averages zero across a season and only real
performance moves it. Rating also feeds form, which feeds side strength, so this is a calibration
change too, never a cosmetic one.

## Conventions

The existing code is the specification for style. Match it rather than importing habits from
elsewhere; a patch should be indistinguishable from what is already there.

- **Comments explain *why*, never *what*.** The codebase's comments justify decisions ("Letting the
  count itself grow with the mismatch is what inflates league-wide scoring"), they do not narrate
  syntax. If a comment restates the code, delete it and let the code speak.
- **Write for the reader six months out.** Name things in football's own vocabulary — `Fixture`,
  `Rollover`, `AskingPrice`, `Sharpness` — so a function's purpose is legible without reading its
  body. Prefer a longer, obvious expression to a clever compact one.
- **Small functions with one job**, at a consistent level of abstraction: `AdvanceDay` reads as a
  numbered list of steps and delegates each one. Keep that shape when extending it.
- **Keep layers honest.** New simulation logic goes in the `engine/*` package that owns that concern;
  `game` orchestrates and translates to plain structs; `ui/tui` only renders and dispatches keys.
  Business rules in a view function are a defect, even when they work.
- Doc-comment every package and every exported identifier, in full sentences, as the existing files
  do. Use British spelling in prose ("normalise", "defence") to match.
- Struct fields are grouped by lifetime with `// ---- section ----` banners (static import data vs.
  mutable career state); long files use `// ----- name -----` rules to separate concerns. Follow suit.
- Standard-library-first: the only dependencies are Bubble Tea and Lip Gloss for the terminal. Do not
  add a module without a strong reason.
- `gofmt` output only; keep `go vet ./...` clean.

## Keeping the docs current

`CLAUDE.md` and `README.md` are part of the deliverable, not afterthoughts. **Before finishing any
change, update both if it affected them** — do not leave that for a later pass:

- **README.md** — user-facing: keybindings, CLI flags, league table, the calibration figures, the
  architecture tree, the resource/perf numbers. If a change alters what a player sees or the numbers
  the engine produces, the README is stale until it is edited.
- **CLAUDE.md** — this file: the invariants above, package responsibilities, commands, and any new
  rule a future session would otherwise have to rediscover by reading the source. If you spend effort
  working something out that is not written here, that discovery belongs in this file.

Both must describe the code as it is now, not as it was intended.
