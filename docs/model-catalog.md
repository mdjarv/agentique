# Model catalog

The list of models in every picker (composer, swarm composer, template form,
agent-profile form) comes from one wire request, `providers.models`, served by
`backend/internal/providers`.

The design goal is narrow and specific: **a new upstream model release must not
require an agentique release.** Everything below follows from that.

## Why a hard-coded list goes stale, and what doesn't

The Claude CLI accepts *aliases* (`opus`, `sonnet`, `haiku`, `fable`) which
always point at the newest model in their family, and it reports the concrete ID
it resolved to in its `init` event:

```
$ claude --print --output-format json --model opus 'ok'
{"type":"system","subtype":"init",...,"model":"claude-opus-5",...}
```

So the *slugs* never went stale — agentique was already running the new model.
Only the **labels** did ("Opus 4.8" for what is actually Opus 5). The catalog
therefore keeps the alias list and derives every label.

## The layers

`Catalog.ListModels` assembles each provider's list from four layers, weakest
first. Each layer degrades to the one below it on any failure — a catalog
request never fails, because a stale label beats an empty picker.

| Layer | Source | Gives |
|---|---|---|
| base | `claudeAliases` (claude) / `codexcli.ListModels` (codex) | the slugs |
| learned | `model_resolutions` table | live version labels |
| cli | `~/.claude.json` → `additionalModelOptionsCache` | models the CLI advertises beyond the built-ins |
| config | `[models]` in `config.toml` | an explicit override that replaces the provider's list |

`ProviderModels.Source` names the strongest layer that applied
(`static` / `learned` / `cache` / `fallback` / `config`), so the frontend can
show staleness hints.

### learned — the self-healing part

`EventPipeline.handleInit` already captured the resolved model ID; it now also
fires `OnResolvedModel`, and `persistResolvedModel`
(`internal/session/model_resolution.go`) makes two writes:

- `sessions.resolved_model` — history: what this conversation actually ran on.
- `model_resolutions (provider, slug, resolved_id)` — the catalog's learning
  signal, upserted so a later release re-points the alias in place.

Only alias → concrete pairs are recorded. A session started on a pinned ID
teaches the catalog nothing and is skipped.

`ModelDisplayName` (in `internal/providers/claude.go`) then renders
`claude-opus-5` → "Opus 5". It parses structure rather than consulting a lookup
table and does **not** allowlist the family token, so `claude-fable-5` →
"Fable 5" and an unannounced family renders correctly too. This is a deliberate
generalization of `claudecli.ModelDisplayName`, which only knows the
opus/sonnet/haiku tiers and returns `claude-fable-5` verbatim.

An unobserved alias falls back to the bare family name ("Opus"), which is never
*wrong* — only less specific.

### cli — new families without a release

`additionalModelOptionsCache` is where the CLI records models the account can
reach beyond the built-in set (today: the Fable 1M variant). Entries whose slug
duplicates an alias's resolved ID are skipped so the same model never appears
twice under two names.

### config — the escape hatch

```toml
[[models.claude]]
slug = "opus"          # passed to the CLI verbatim; alias or pinned ID
display = "Opus 5"     # picker label; defaults to slug
description = ""       # optional secondary text
```

A non-empty list **replaces** that provider's generated list — it does not merge.
That is the point: it is the last resort for anything auto-detection misses, so
it must be predictable.

## Frontend

`frontend/src/lib/model-catalog.ts` owns the catalog helpers. `ModelId` is a
bare `string`, not a union — a closed union would reintroduce exactly the
release dependency this design removes. The static `FALLBACK_MODELS` list covers
only the window before the first `providers.models` response, and its labels are
family names without versions on purpose.

## Adding a provider

Add a `<provider>Models` method that returns `ProviderModels`, call it from
`ListModels`, and call `applyOverride` first so the config layer keeps working.
