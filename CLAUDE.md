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
- **`engine/model`** holds the data types with no behaviour beyond derivation: `World`, `Player`,
  `Club`, `League`, `Nation`, `Tactics`, `Formation`, `Pos`, `Date`.
- **`engine/match`** simulates a match; **`engine/season`** owns calendar, tables and rollover;
  **`engine/dev`** owns growth/decline/fitness/morale/valuation; **`engine/transfer`** owns pricing,
  negotiation and the AI market; **`engine/rng`** is the single random source.
- **`ui/tui`** is Bubble Tea: `Model` in `app.go` (state + key handling), rendering in `view.go`,
  the match feed in `match.go`, Lip Gloss styles in `styles.go`.
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
6. **One engine, two presentations.** A match is fully simulated at kickoff and produces a complete
   event stream; "watching" it minute by minute only reveals events already decided. Never add a
   separate quick-result path — the two could then disagree.
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
