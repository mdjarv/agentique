# Capabilities wire shape on `SessionInfo`

Date: 2026-05-25

## Context

The agentkit `runtime` exposes a `Capabilities` struct per `CLISession`
(flat booleans: `PlanMode`, `Resume`, `MidTurnSendMessage`, `Thinking`,
`Subagents`, `RateLimitEvents`, `CompactionEvents`, `Effort`, …). Until
this commit, agentique's frontend rendered every chat control regardless
of provider — codex sessions silently had a plan-mode toggle that no-ops,
a mid-turn send composer that returns `ErrNotSupported`, an attachment
button the codex `userInput` rejects, and a resume button that secretly
starts a fresh conversation.

`docs/tech-debt.md` P1 "Codex feature flags are off but not surfaced in
UI" tracks this gap.

## Decision

1. **Add a `WireCapabilities` struct** in `backend/internal/session`
   mirroring `runtime.Capabilities` flatly, with one extra agentique-only
   flag (`Attachments`) since the upstream struct does not represent
   attachment support.
2. **Attach it to `SessionInfo` and `CreateSessionResult`** as
   `Capabilities *WireCapabilities`. The pointer makes the field
   omittable from older or unknown providers without breaking JSON
   consumers that don't have the field yet.
3. **Recompute statically from `Provider`** via
   `capabilitiesForProvider(provider string) WireCapabilities` rather
   than reading `s.cli.Capabilities()` per snapshot. Two reasons:
   - Frontend gating must work for **offline** sessions too (the user
     should not see a resume button on a stopped codex session that
     promises to restore conversation). Reading from `s.cli` only works
     when the session is live.
   - The capability values are a deterministic function of
     `provider + agentkit adapter version`. We control the agentkit
     pseudo-version (`docs/tech-debt.md` P3) and bump it deliberately —
     the static lookup is reviewed at bump time.

## Trade-offs considered

| Option | Why not chosen |
|---|---|
| Persist capabilities in a `sessions.capabilities` column | Caps are not session state; recomputing avoids a migration and drift. |
| Read `s.cli.Capabilities()` per snapshot, fall back to static | Adds a code path that only exercises for live sessions; the static lookup already matches what the adapter reports today. If they diverge, the frontend would gate based on what the adapter says **right now** vs. what it said when the snapshot was built — confusing under reconnects. |
| Send the full `runtime.Capabilities` struct over the wire untouched | Requires either re-exporting the agentkit type or accepting a Go-side import in `wire.go`. The flat copy keeps `backend/internal/session` as the wire surface. |

## Update protocol

When bumping `github.com/allbin/agentkit` in `go.mod`, diff
`runtime/cli/{claude,codex}/*.go`'s `Capabilities()` methods against
`capabilitiesForProvider` in `backend/internal/session/capabilities.go`.
The unit tests in `capabilities_test.go` lock the expected snapshot for
each provider — if they drift, the tests fail and the frontend gating
would silently mis-render until the lookup is updated.
