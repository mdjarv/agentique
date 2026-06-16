# Extracting the memory core into agentkit

The persistent-memory feature was built **liftable-first**: the reusable engine
lives in `backend/internal/memory/**` with a hard dependency boundary, and all
agentique-specific policy lives in `backend/internal/brain/**`. This document is
the mechanical playbook for moving the engine into `github.com/allbin/agentkit`
when a second consumer (formica, hittat) needs it.

See `docs/brain-memory.md` for the feature architecture; this doc is only about
the lift.

## What moves vs. what stays

**Moves verbatim → `github.com/allbin/agentkit/memory`** (the engine — no agentique
concepts, no product policy):

| Path now | Path in agentkit |
|---|---|
| `backend/internal/memory/` (core) | `memory/` |
| `backend/internal/memory/filestore/` | `memory/filestore/` |
| `backend/internal/memory/chroma/` | `memory/chroma/` |
| `backend/internal/memory/embedhttp/` | `memory/embedhttp/` |

**Stays in agentique → `backend/internal/brain/`** (the policy — projects, env
config, REST/MCP surfaces, Claude-CLI extraction):

- `brain.go` (Service: project→scope mapping, fingerprint persistence)
- `mcp.go` (agent-facing MemoryAdd/MemorySearch)
- `http.go` (Brain tab REST API)
- `extractor.go` (HaikuExtractor — agentique's `memory.Extractor` impl via claudecli)
- `transcript.go` (session_events → transcript)
- `cmd/agentique/brain.go` (the `backfill` command)

The dependency flows one way: `brain` → `memory`. There is **no** `memory` →
`brain` (or any agentique) import.

## Why it lifts cleanly — the invariant to preserve

The entire engine depends only on the **standard library**,
`github.com/google/uuid`, and `gopkg.in/yaml.v3` — both already in agentkit's
`go.mod`. Verify before (and after) lifting:

```sh
cd backend
go list -deps ./internal/memory/... \
  | grep -E 'github.com|gopkg.in' \
  | grep -v 'mdjarv/agentique/backend/internal/memory'
# Expected output — exactly these two lines, nothing else:
#   github.com/google/uuid
#   gopkg.in/yaml.v3
```

If anything else appears, the boundary has been violated — fix it before lifting.

**Rules that keep the boundary intact (enforce in review):**
- `internal/memory/**` must never import `internal/brain`, `internal/store`,
  `internal/session`, `claudecli`, `runtime`, `slog`-for-policy, or any agentique package.
- Model calls (extraction) and embeddings are **interfaces** (`Extractor`,
  `Embedder`) — implementations live in the consumer. The engine never calls an
  LLM or an embeddings API directly.
- The vector backend talks to Chroma over plain `net/http`; no Chroma SDK, no CGo.

## Mechanical steps

1. Copy/move the four directories into the agentkit repo under `memory/`.
2. Rewrite the import path across the moved tree **and** agentique's consumers:
   ```
   github.com/mdjarv/agentique/backend/internal/memory  →  github.com/allbin/agentkit/memory
   ```
   (covers `/filestore`, `/chroma`, `/embedhttp`, and the sibling imports inside
   chroma/filestore that reference the core package).
3. In agentique, update `internal/brain/*.go` and `cmd/agentique/brain.go` imports
   to the new `agentkit/memory` paths.
4. `go mod tidy` in both repos. agentkit needs no new direct dependency (uuid +
   yaml already present); agentique keeps them transitively.
5. Run the moved tests in agentkit — they are self-contained: `filestore` and the
   core use `t.TempDir()`; the chroma integration test is gated on
   `CHROMA_TEST_URL` (skips without a live server). Then run agentique's
   `internal/brain` + full suite to confirm the consumer still builds.

## Public API surface (the contract consumers depend on)

Package `memory`:
- **Types:** `Record`, `Scope`, `Category`, `Source`, `Query`, `Result`, `Hit`,
  `Candidate`, `Fact`, `Report`, `Change`, `DecayPolicy`, `ConsolidateOptions`
- **Interfaces:** `Store`, `Searcher` (optional vector capability), `Embedder`,
  `Extractor`
- **Funcs/consts:** `New`, `Recall`, `BumpUses`, `Consolidate`, `IsTextDuplicate`,
  `FindDuplicate`, `CosineSimilarity`, `ScopeGlobal`, `DefaultRecallK`,
  `DefaultDuplicateThreshold`, `ErrNotFound`
- **Subpackages:** `filestore.New(dir)`; `chroma.NewClient/NewStore/WithErrorHandler`;
  `embedhttp.New/WithAPIKey/WithHTTPClient`

A consumer supplies: a `Store` (use `filestore.New`, optionally wrapped by
`chroma.NewStore`), optionally an `Embedder` and `Extractor`, and its own scope
semantics. That's the whole integration.

## Pull-only-what-you-use

The subpackages are independent. A consumer that wants keyword recall over files
imports only `memory` + `memory/filestore` — `chroma` and `embedhttp` (the only
parts that reach the network) are not pulled in. This matches agentkit's
"small and independent; pull only what you use" philosophy.

## When (per agentkit's own rules)

agentkit lifts code only when **≥2 consumers need it AND the API is stable**
(`agentkit/docs/CONSUMERS.md`). The API has now been exercised end-to-end in
agentique (MCP tools, REST, backfill, consolidation). The remaining gate is the
second consumer — formica or hittat adopting. Until then the engine stays here,
validated, behind this clean boundary.
