# Code Review — 2025-03-25

Consolidated findings from 4 parallel reviews (FE bugs, BE bugs, FE architecture, BE architecture).
See `code-review-2026-03-25.html` for the full visual report.

## Cross-Cutting Themes

1. **Disconnected state systems** — FE hook local state vs Zustand store vs BE broadcasts. Store mutations scattered across lib/, hooks/, components/.
2. **Session package god object** — BE session/ handles lifecycle, git, streaming, state machine, broadcasting, approvals. 800+ lines in session.go.
3. **Concurrency gaps** — WS write loop races, unprotected seqInTurn, TOCTOU in Manager.Get(), broadcast-during-lock deadlocks.
4. **No interfaces = untestable** — BE: Manager, store.Queries, gitops all concrete. FE: global WS singleton, getState() in libs.
5. **Silent failures everywhere** — BE ignores 10+ DB update errors. FE swallows rejections with console.error. No error boundaries.

## P0 — Fix Now

- [ ] **BE: WS write loop race** — Mutex unlocked between SetWriteDeadline and WriteJSON in drain path. Two goroutines can write simultaneously on shutdown. `ws/conn.go:81-107`
- [ ] **BE: seqInTurn data race** — Field accessed from event loop goroutine and Query() without consistent mutex protection. `session/session.go:168,191,378`
- [ ] **BE: Broadcast-during-lock deadlock** — broadcastState() called holding mu; if send channel full, closes connection which may acquire same lock. `session/session.go:437-456`
- [ ] **BE: Orphaned CLI processes** — Manager.Create() uses background context. Server crash leaves Claude CLI processes running. `session/manager.go:115`
- [ ] **FE: Queued message race** — queueMicrotask uses stale sessionId after dequeueMessage. Session deletion between dequeue and execution submits to dead session. `hooks/useGlobalSubscriptions.ts:123-140`

## P1 — Fix Soon

- [ ] **BE: Close() bypasses state machine** — Direct `s.state = finalState` skips validateTransition. Can persist invalid state to DB. `session/session.go:750`
- [ ] **BE: TOCTOU in Manager.Get()** — GitService calls mgr.Get() twice per merge/rebase. Session can be evicted between calls. `session/git_service.go:147-156,259-268`
- [ ] **BE: Silent DB update failures** — 10+ places where DB writes ignored with `_`. State diverges on failure. `service.go:450,484,502; manager.go:227`
- [ ] **BE: Worktree restore silent fallback** — Failed restore on resume falls back to project root. Session may modify main branch. `session/service.go:570-578`
- [ ] **FE: ApprovalBanner error swallowed** — setAutoApprove failure after resolveApproval: .then() swallows error, button freezes. `components/chat/ApprovalBanner.tsx:80-104`
- [ ] **FE: subscribeAndLoad silent failure** — Project/session list failures logged to console only. Empty sidebar, no error shown. `hooks/useGlobalSubscriptions.ts:72-79`
- [ ] **BE: Pending approval/question drop** — ResolveApproval select with default silently drops if channel full. Claude waits forever. `session/session.go:631-636,688-693`
- [ ] **Both: Result types mix error/success** — MergeResult has both (result, error) return AND result.Error. FE uses string status, no discriminated unions. `git_service.go; session-actions.ts`

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
