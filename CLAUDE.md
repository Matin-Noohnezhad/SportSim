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
go test ./game -run TestSeasonCalibration -v   # slowest (~13s): plays a full 4,676-match season
go test ./engine/match -run TestDeterminism -v
go test ./ui/tui -run TestScreensRender -v
go test ./... -count=1                         # defeat the cache after touching tuning constants
```

Rebuilding the packed database (only when refreshing player data; `assets/world.dat` is committed):

```sh
go run ./cmd/importer -in data/players.csv -out assets/world.dat
```

`data/players.csv` is gitignored (11 MB EA FC export) — it is not in a clean clone, so the importer
cannot be run without obtaining it separately. Everything else builds and tests without it.

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
  queries (`Squad`, `Table`, `TopScorers`, `Search`) as plain methods over plain serialisable structs.
  `AdvanceDay()` is the heartbeat: it plays the day's fixtures, applies recovery, pays wages on
  Mondays, trains on the 1st of the month, runs the AI market, then moves the clock. **A second
  frontend (HTTP, mobile) is a new package beside `ui/tui`, not a rewrite — so nothing that belongs
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
  **`engine/season`** owns calendar, tables and rollover;
  **`engine/dev`** owns growth/decline/fitness/morale/valuation; **`engine/transfer`** owns pricing,
  negotiation and the AI market; **`engine/rng`** is the single random source.
- **`ui/tui`** is Bubble Tea: `Model` in `app.go` (state + key handling), rendering in `view.go`,
  the match feed and touchline panels in `match.go`, Lip Gloss styles in `styles.go`. `ScreenMatch`
  intercepts keys *before* the global bindings, because from the touchline `s` and `t` are the
  substitution and shape panels rather than the squad and tactics screens.
- **`store`** gob-encodes and gzips a `snapshot` of world + schedule + inbox + RNG state.

### Invariants that hold the design together

Break any of these and something distant fails — usually a calibration test, sometimes only after a
few simulated seasons.

1. **Ability is derived, never stored.** `Player.Rating(pos)` computes from 34 attributes weighted by
   `positionWeights` in `engine/model/player.go`, then scaled by positional familiarity.
   `CurrentAbility()` is the rating in the player's best position. There is deliberately no "overall"
   field that could drift out of step with the attributes — do not add one, and do not cache ratings
   in a struct field. Training moves attributes; the rating follows.
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

### Calibration is a test, not a comment

`TestSeasonCalibration` (`game/soak_test.go`) plays a full European season on every run and asserts
2.50–3.00 goals per match, 39–49% home wins, 20–30% draws, and a believable points spread per league
(68–105 for the champion, 8–45 for the bottom club, normalised to 38 games). Tuning constants live at
the top of `engine/match/sim.go`. **Any change to match simulation, fitness, development or squad
strength must be re-checked against this test** — effects there are non-local and often only show up
across a whole season. When a change legitimately shifts the rates, update both the thresholds and
the calibration table in `README.md`.

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
