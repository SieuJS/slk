# Workspace status presets — design

Issue: https://github.com/gammons/slk/issues/356 (approved)
Author: SieuJS

## Problem

The `Ctrl+S` status menu (`internal/ui/presencemenu`) only sets presence
(Active/Away) and DND snooze. Slack's actual custom status (free text +
emoji + expiration, `users.profile.set`) is never exposed from `slk`,
even though the app already reads and caches *other* users' custom
status (`status_emoji`/`status_text` in `internal/cache/users.go`) to
show peer status in the sidebar and DM headers. There is no way to set
something like "Working from home" or "On vacation" without switching
to the Slack desktop/web client.

Custom status in Slack is scoped **per workspace** — your profile,
including status, belongs to each workspace membership independently.
Presence/DND are already handled this way in `slk`
(`presenceController.byTeam` in `internal/ui/presence.go`, keyed by
team ID); the new feature must follow the same scoping.

## Decisions (from issue discussion + first prototype, now reverted)

1. **Presets, not a free-text editor.** Two starter rows: "Working from
   home" (🏠) and "On vacation" (🌴). Free-text/custom-emoji entry (like
   Slack's own status editor) is explicitly out of scope for this pass.
2. **Expiration:** WFH auto-clears at end of day (local time, 23:59:59).
   Vacation has no expiration — cleared manually. This was a judgment
   call, not from the issue; flag it in review if a reviewer wants
   different defaults.
3. **Presence and custom status are independent**, matching real Slack.
   This was *not* the first instinct — the first prototype cleared the
   custom status whenever Active/Away was picked, which is wrong and
   doesn't match Slack's actual behavior. Picking Active/Away must only
   touch `Presence`, never `StatusText`/`StatusEmoji`.
4. **An explicit "Clear status" row**, shown only once a status is
   actually set (same conditional-row pattern already used for "End
   snooze / DND" when DND is active).
5. **Visible feedback is required** — today nothing in the UI shows
   what status is currently set. Two surfaces:
   - A statusbar segment (e.g. `🏠 Working from home`), hidden when no
     status is set.
   - A `✓` checkmark on the matching preset row when the menu is
     reopened (same `current` mechanism `buildItems` already uses for
     Active/Away).

## Where the pieces live (prior pass's touch points — useful as a map,
re-derive line-by-line from current `main` rather than trusting exact
line numbers here)

- `internal/slack/client.go` — `SlackAPI` interface is a hand-picked
  subset of the real `slack-go` client (see its own comment block for
  why — deliberately excludes whole-workspace enumeration calls).
  `SetUserCustomStatusContext(ctx, statusText, statusEmoji string,
  statusExpiration int64) error` already exists on the underlying
  `slack-go` client but isn't in the interface — add it, then add a
  `Client.SetCustomStatus(ctx, text, emoji string, expiration int64)
  error` wrapper mirroring the existing `SetUserPresence`/`SetSnooze`
  wrappers just below `GetUserPresence`.
- `internal/slack/client_test.go` — the interface is satisfied by a
  hand-written `mockSlackAPI` with one `xxxFn` field + method per
  interface method (see `setUserPresenceContextFn` /
  `SetUserPresenceContext` for the pattern). Adding to the interface
  requires adding a matching field + method here or the package stops
  compiling.
- `internal/core/types.go` — `PresenceAction` enum (`iota`-based, not
  serialized anywhere — safe to append new values at the end). Needs
  new actions for WFH, Vacation, and Clear-status.
- `internal/ui/presencemenu/model.go` — `buildItems(presence string,
  dndActive bool) []item` composes the menu rows; it will need a third
  parameter for "what custom status is currently active" so it can mark
  the right row `current: true` and conditionally append "Clear
  status". `OpenWith` is the single call site. Existing rows are
  Active/Away/seven snooze options/custom-snooze; End-DND is appended
  conditionally already — follow that exact pattern for Clear-status.
- `internal/ui/presence.go` — `presenceController.byTeam` holds a
  `workspaceStatus` struct per team (`Presence`, `DNDEnabled`,
  `DNDEndTS` today). Add `StatusText`/`StatusEmoji` fields here, and
  extend the `Apply` switch. Needs a `CustomStatus(teamID) (text, emoji
  string)` accessor alongside the existing `Status(teamID)`.
- `internal/ui/mode_normal.go` — the `Ctrl+S` key handler
  (`a.keys.PresenceMenu` case) is the one production call site for
  `presenceMenu.OpenWith`; it needs to also look up and pass through the
  current custom status before opening.
- `internal/ui/presence_action.go` — `applyPresenceAction` is the single
  chokepoint both the menu and the `/away`-family slash commands
  (`internal/ui/slash_builtins.go`) go through; it applies to local
  state optimistically, then hands off to `presenceSvc.SetStatus`. The
  statusbar update call lives here.
- `internal/ui/statusbar/model.go` — `Model.SetStatus(presence,
  dndEnabled, dndEndTS)` + `View()` is the rendering side; needs a
  parallel `SetCustomStatus(text, emoji string)` plus a new segment in
  `View()`, following the existing Presence/DND segment's style
  pattern (`lipgloss.NewStyle().Foreground(...).Background(styles.
  SurfaceDark)`).
- `cmd/slk/main.go` — the `setStatus` closure (installed via
  `core.NewPresenceService`) is where `core.PresenceAction` actually
  turns into a Slack API call, on `router.Active().Client`. This is
  where the WFH/Vacation/Clear-status `Client.SetCustomStatus` calls go,
  and where the end-of-day expiration gets computed.

## Known test-fragility traps hit during the first pass

These aren't test *requirements* — just things that broke last time and
will break again if not written defensively:

- `internal/ui/mode_presence_menu_test.go` hardcodes the unfiltered row
  count in a constant (`presenceMenuAllRows`) and walks the cursor with
  a fixed number of `j`/down presses in several cases — both need
  updating for however many new rows land, and the exact count should be
  computed from `len(buildItems(...))` or similar rather than
  re-guessed.
- The same file's "a printable key filters the menu" case filters by the
  literal rune `"a"` and asserts an exact row count — "On vacation"
  substring-matches "a" via "vacation", so adding that row changes the
  expected count there too (filter matches prefix first, then
  substring — see `(m *Model) filter()`).
- `internal/ui/presencemenu/model_test.go` has a couple of tests that
  walk the cursor down by a hardcoded number of `j` presses to reach a
  specific row ("Snooze for 20 minutes" at a fixed offset, "Snooze
  custom..." after a fixed number of steps) — same fragility.
- `internal/ui/golden_dialogs_test.go` / `testdata/golden/
  dialog_presence_menu.ansi` is a golden snapshot of the rendered menu
  box; it must be regenerated with `go test ./internal/ui/ -run
  TestGolden_Dialogs -update` once the row count changes, and the diff
  should be reviewed (only that one golden file, nothing else) before
  committing.
- No Go toolchain was preinstalled in this environment; `go`,
  `golangci-lint` were installed via `brew install go golangci-lint`
  to actually build/test rather than guessing.

## Out of scope

- Free-text / custom-emoji status entry.
- Auto-clearing vacation status after N days.
- Syncing status across workspaces (each workspace's status is
  independent, intentionally).

## Next step

Fresh branch off `main`, TDD per `superpowers:test-driven-development`:
write the failing tests for each piece above first (client wiring,
`presenceController.Apply`/`CustomStatus`, `buildItems` current-row +
conditional Clear-status row, statusbar segment, the Active/Away-must-
not-clear-status invariant specifically, since that was the actual bug
caught in review last time), then implement to green. Pre-PR checklist
per `AGENTS.md`: `go build ./...`, `go vet ./...`, `go test ./...
-race`, `gofmt -l .` empty, `golangci-lint run` clean.
