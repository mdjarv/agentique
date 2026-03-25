# Frontend State Sync — Options Assessment

Researched 2026-03-25. Context: the current `useGlobalSubscriptions` god-hook manually maps WS push events to Zustand store mutations. Works but is brittle, hard to extend, and doesn't scale well to new entity types.

## Current Architecture

- WS connection is a module-level singleton (`WsClient`)
- `useGlobalSubscriptions` subscribes to 7+ event types, imperatively mutates Zustand stores
- Components select from Zustand with manual selectors (`useChatStore(s => s.sessions[id]?.meta)`)
- No built-in loading/error/stale states per entity — all hand-rolled

## Options

### Option A: TanStack Query as cache layer

Keep WS as-is, use TanStack Query as the state layer instead of raw Zustand for server state.

- WS push events call `queryClient.setQueryData(["session", id], data)` — direct cache update
- Components use `useQuery(["session", id])` — automatic re-renders, loading/error states
- Set `staleTime: Infinity` — data only changes via WS pushes
- Zustand shrinks to purely-local UI state (panel collapsed, active tab)

**Pros:** Battle-tested, excellent devtools, built-in loading/error/stale per query key.
**Cons:** Significant rewrite of data consumption layer. Still manual event→cache mapping.
**Maturity:** Production-ready. TanStack Query v5 is stable.

### Option B: Zustand + formalized WS middleware

Keep Zustand, formalize the WS→store sync as declarative event→action mappings.

**Pros:** Minimal change. Better organized than current god-hook.
**Cons:** Doesn't solve the core problem — still manual mapping. Just cleaner.

### Option C: TanStack DB with custom WS collection

TanStack DB (v0.5, March 2026) is a client-side reactive database with pluggable collection backends. Write a custom `wsCollectionOptions` that syncs entities via our WS protocol.

- Define a collection per entity type (sessions, events, projects)
- WS push events call `bulkUpsert` / `incrementalPatch` on the collection
- Components use `useLiveQuery` — differential dataflow, only changed rows trigger re-renders
- Mutations go through `onInsert`/`onUpdate`/`onDelete` → WS requests
- Optimistic mutations built-in

**Pros:** Live queries with sub-ms re-renders. Replaces both Zustand (for server state) and the manual event mapping. Typed collections with schema. Purpose-built for exactly this problem.
**Cons:** v0.5 — early. Custom WS collection API exists and is documented but few community examples beyond Electric/PowerSync. Early adopter risk.
**Maturity:** Beta. Collection options creator API is documented. Electric and PowerSync integrations are production-quality reference implementations.

**Spike approach:** Write `wsCollectionOptions` for just sessions (highest-churn entity). If it works, migrate events and projects. If too rough, fall back to Option A.

Docs: https://tanstack.com/db/latest/docs/guides/collection-options-creator

### Option D: Study TanStack DB internals, build purpose-built sync

Clone `TanStack/db` and study its differential dataflow implementation and collection abstraction. Use the design patterns to improve our own `useGlobalSubscriptions` into a purpose-built sync engine tailored to our WS protocol.

- We don't need the full generality of TanStack DB's pluggable collections
- But the core patterns (differential dataflow, live queries, optimistic state, collection abstraction) are exactly what our god-hook is a primitive version of
- Building our own means zero dependency risk and a perfect fit for our protocol
- Can evolve incrementally — start by refactoring the god-hook into collection-like abstractions

**Pros:** No dependency on a v0.5 library. Perfect fit for our protocol. Educational — the team deeply understands the sync layer. Can cherry-pick patterns without buying the whole framework.
**Cons:** More work upfront. Risk of reinventing wheels. Need to understand differential dataflow well enough to implement (or simplify) it.
**Maturity:** N/A — custom build.

Repo: https://github.com/TanStack/db

## What others use in similar situations

| Tool | Use case | Sync approach |
|------|----------|---------------|
| ElectricSQL | Postgres → client mirror | Logical replication stream |
| PowerSync | Offline-first apps | SQLite sync from server DB |
| Zero (Rocicorp) | Server-authoritative sync | Custom sync protocol |
| Triplit | Full-stack with built-in sync | Built-in DB + sync |
| Liveblocks | Collaborative features | Zustand middleware, WS-backed |
| Yjs/Automerge/Loro | Collaborative editing (CRDT) | Peer-to-peer or server relay |

Most of these are database-backed sync engines (Postgres/SQLite → client). They don't fit Agentique where the "database" is ephemeral session state from Claude CLI processes. TanStack DB's collection abstraction is the closest fit because it's sync-engine agnostic.

## Recommendation

**Short-term:** No change. Current architecture works. The code review P2 items (WS singleton, store mutations in lib, chat store domain/UI) are deferred — they're patterns that constrain but don't break things.

**Medium-term:** Spike Option C (TanStack DB) or Option D (study + build) when adding a new entity type or when the current sync layer becomes a bottleneck. The spike should target sessions as the first collection.

**Decision point:** When TanStack DB reaches 1.0 or when we need offline support / optimistic mutations, revisit this doc.
