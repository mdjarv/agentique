# Code Review — 2025-03-25

Consolidated findings from 4 parallel reviews (FE bugs, BE bugs, FE architecture, BE architecture).
See `code-review-2026-03-25.html` for the full visual report.

## Cross-Cutting Themes

1. **Disconnected state systems** — FE hook local state vs Zustand store vs BE broadcasts. Store mutations scattered across lib/, hooks/, components/.
2. **Session package god object** — BE session/ handles lifecycle, git, streaming, state machine, broadcasting, approvals. 800+ lines in session.go.
3. **~~Concurrency gaps~~** — Investigated: WS write loop cleaned up (single goroutine, no real race), seqInTurn and broadcast-deadlock were false positives. TOCTOU tightened.
4. **No interfaces = untestable** — BE: Manager, store.Queries, gitops all concrete. FE: global WS singleton, getState() in libs.
5. **~~Silent failures everywhere~~** — BE DB errors all handled (PersistError type + logging). FE still swallows rejections with console.error. No error boundaries.

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

- [ ] **BE: Session package overloaded** — 5 concerns: lifecycle, git, streaming, state machine, broadcasting. `session/*.go`
- [ ] **BE: No query interfaces** — store.Queries concrete everywhere. Can't test without real DB. `manager.go, service.go, git_service.go`
- [ ] **BE: Manager god object** — Registry + lifecycle + state repair + shutdown. fixStates() is a hack. `session/manager.go`
- [ ] **BE: WS handler boilerplate** — 20+ handlers repeat unmarshal/validate/call/respond. ~80 lines duplicated. `ws/handlers.go`
- [ ] **BE: Implicit state machine** — Transitions scattered across setState, TryLockForMerge, Close, processEvent. `session/state.go, session.go`
- [ ] **FE: Global WS singleton** — Untestable, no DI. `hooks/useWebSocket.ts`
- [ ] **FE: Store mutations in lib/** — session-actions.ts and session-history.ts directly mutate stores. `lib/session-actions.ts, lib/session-history.ts`
- [ ] **FE: useGlobalSubscriptions god-hook** — 200+ lines, 7+ event types, mixes processing/mutations/navigation. `hooks/useGlobalSubscriptions.ts`
- [ ] **FE: useGitActions mega-hook** — 8 unrelated state machines bundled. `hooks/useGitActions.ts`
- [ ] **FE: Chat store mixes domain/UI** — SessionData contains domain, UI, derived, and transient state. `stores/chat-store.ts`
- [ ] **FE: Weak result types** — MergeResult.status is string, no discriminated union. `lib/session-actions.ts`
- [ ] **FE: Streaming store fragile indexing** — content_block index to toolId lookup silently drops data. `stores/streaming-store.ts:35-57`

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

## Attack Order

1. P0 concurrency bugs (WS write loop, seqInTurn, broadcast deadlock)
2. P0 orphaned processes
3. P1 state corruption (Close() bypass, TOCTOU, silent DB failures)
4. P1 frontend UX gaps (silent failures, frozen buttons)
5. P2 backend interfaces (start with SessionQueries)
6. P2 session package split (extract GitService, split Session)
7. P2 frontend WS provider (replace singleton with context)
8. P2 frontend store cleanup (split hooks, move mutations, discriminated unions)
9. P3 polish (error boundaries, scroll, config)
