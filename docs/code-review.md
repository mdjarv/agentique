# Code Review — 2026-03-25

Consolidated findings from 4 parallel reviews (FE bugs, BE bugs, FE architecture, BE architecture).
See `code-review-2026-03-25.html` for the full visual report.

## Cross-Cutting Themes

1. **Disconnected state systems** — FE hook local state vs Zustand store vs BE broadcasts. Store mutations still scattered in FE lib/. Rebase conflict fix was the first instance addressed.
2. **~~Session package god object~~** — Partially resolved: state machine in state.go, message generation in internal/msggen, fixStates replaced with startup recovery. Event loop and broadcasting remain in session.go.
3. **~~Concurrency gaps~~** — Investigated: WS write loop cleaned up (single goroutine, no real race), seqInTurn and broadcast-deadlock were false positives. TOCTOU tightened.
4. **~~No interfaces = untestable~~** — BE resolved: consumer-scoped query interfaces + sqlc Querier. FE global WS singleton and getState() in libs remain.
5. **~~Silent failures everywhere~~** — BE DB errors all handled (PersistError type + logging). FE still swallows some rejections with console.error. No error boundaries.

## P0 — Fix Now

- [x] **BE: WS write loop race** — ~~P0~~ Downgraded to P3: single goroutine, no actual race. Cleaned up drain path to hold lock across deadline+writes. `ws/conn.go:81-107`
- [x] **BE: seqInTurn data race** — False positive. All accesses are under mu or happen-before event loop starts.
- [x] **BE: Broadcast-during-lock deadlock** — False positive. broadcastState() unlocks mu before calling broadcast. No lock overlap.
- [x] **BE: Orphaned CLI processes** — Already handled: SIGINT/SIGTERM trigger CloseAll() via signal handler in serve.go:63-82. SIGKILL/OOM can't be caught; PID tracking deferred unless it becomes a real problem.
- [x] **FE: Queued message race** — False positive. Microtasks run before next macro-task (WS message), so session.deleted can't interleave. Catch block handles errors.

## P1 — Fix Soon

- [x] **BE: Close() bypasses state machine** — Intentional. Close() is a shutdown override — the event loop may have set Failed, and Failed->Stopped isn't a normal transition but is correct for shutdown. Well-commented.
- [x] **BE: TOCTOU in Manager.Get()** — Merge now captures live session once and reuses. Not a real race (merge lock prevents eviction), but cleaner.
- [x] **BE: Silent DB update failures** — All 16 silenced errors fixed. Service methods return `PersistError` (callers can use errors.As). Git/session internals log at warn/error level. New `errors.go` with `PersistError` type.
- [x] **BE: Worktree restore silent fallback** — Now returns error instead of silently falling back to project root. Session won't resume in wrong directory.
- [x] **FE: ApprovalBanner error swallowed** — False positive. `.catch()` is at end of chain, catches both resolveApproval and setAutoApprove failures. Success path intentionally doesn't reset submitting (banner disappears).
- [x] **FE: subscribeAndLoad silent failure** — Added toast.error for both project.subscribe and session.list failures.
- [x] **BE: Pending approval/question drop** — False positive. Buffered channel (cap 1) accepts exactly one response. Duplicates get "already resolved" error returned to caller. Not silent.
- [x] **Both: Result types mix error/success** — BE pattern is by design: `error` = couldn't attempt, `Status` = domain outcome (like http.Response). FE weak typing (string status) tracked in P2 "Weak result types".

## P2 — Structural Debt

- [x] **BE: Session package overloaded** — Partial: state machine consolidated in state.go, message generation extracted to internal/msggen package. Remaining: event loop and broadcasting still in session.go.
- [x] **BE: No query interfaces** — Consumer-scoped interfaces in session/queries.go (5 interfaces). sqlc Querier enabled. `*store.Queries` satisfies all implicitly.
- [x] **BE: Manager god object** — Partial: replaced fixStates() hack with RecoverStaleSessions (runs once at startup via sqlc query). List methods now only overlay live state, no more dead-session correction per call. Registry/lifecycle split deferred.
- [x] **BE: WS handler boilerplate** — Generic handleRequest[P,R] with Validatable interface. 25 handlers reduced to one-liner closures. `ws/handle.go`
- [x] **BE: Implicit state machine** — All state methods now in state.go: State(), setState(), TryLockForMerge(), UnlockMerge(), validateTransition(). Close() bypass remains intentional (see P1 assessment).
- [x] **FE: Global WS singleton** — Deferred. Works fine at runtime; testability concern only matters when FE tests exist. See `docs/frontend-state-sync.md` for broader state sync assessment.
- [x] **FE: Store mutations in lib/** — Deferred. Command pattern (request + update store) is pragmatically fine. Would be addressed by any of the state sync options (A-D) in `docs/frontend-state-sync.md`.
- [x] **FE: useGlobalSubscriptions god-hook** — Won't fix. Single effect is simpler for reconnection coordination. Splitting adds file sprawl and implicit ordering for minimal testability gain.
- [x] **FE: useGitActions mega-hook** — Split into 7 focused hooks (useSessionDiff, useMergeSession, etc). useGitActions is now a thin facade.
- [x] **FE: Chat store mixes domain/UI** — Deferred. Would be naturally resolved by migrating server state to TanStack Query or DB (Options A/C/D), leaving Zustand for UI-only state.
- [x] **FE: Weak result types** — MergeResult, RebaseResult, CreatePRResult, CleanResult now use discriminated unions with exhaustive narrowing.
- [x] **FE: Streaming store fragile indexing** — Removed index indirection. appendToolInput takes toolId directly. Index tracking moved to module-scoped Map in useGlobalSubscriptions.

## P3 — Polish

- [ ] **BE: Watchdog timer edge case** — Resets on event but state may be Idle. Unnecessary warnings. `session/session.go:294-295`
- [ ] **BE: git GC errors dropped** — Repeated failures accumulate loose objects. `session/git_service.go:204,616`
- [ ] **BE: Conflict file list unbounded** — Thousands of files could exceed WS message limits. `session/git_service.go:173`
- [ ] **FE: Object URL memory leak** — createObjectURL previews may not be revoked on unmount. `components/chat/MessageComposer.tsx:186`
- [ ] **FE: Scroll thrashing during streaming** — Auto-scroll on every text change. Can't scroll up. `components/chat/MessageList.tsx:47-54`
- [ ] **FE: No error boundaries** — Component throw crashes entire app. Throughout.
- [ ] **FE: Session sorting duplicated** — useKeyboardShortcuts duplicates sidebar sort logic. `hooks/useKeyboardShortcuts.ts`
- [ ] **BE: Hardcoded magic constants** — Timeouts, sizes not configurable. `session/session.go`
- [ ] **BE: Hub/broadcast circular dep risk** — ws/ holds session.Service; session/ broadcasts via ws/Hub. `ws/handler.go, session/handler.go`

## Remaining Work

**P2 — all resolved or deferred.** See `docs/frontend-state-sync.md` for FE state architecture options.

**P3 (9 items — low priority polish):**
1. BE: Watchdog timer edge case
2. BE: git GC error logging
3. BE: Conflict file list cap
4. FE: Object URL cleanup on unmount
5. FE: Scroll thrashing during streaming
6. FE: Error boundaries
7. FE: Session sorting dedup
8. BE: Magic constants → config
9. BE: Hub/broadcast circular dep
