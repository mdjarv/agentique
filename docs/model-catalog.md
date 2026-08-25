# Model catalog

The list of models in every picker (composer, swarm composer, template form,
agent-profile form) comes from one wire request, `providers.models`, served by
`backend/internal/providers`.

The design goal is narrow and specific: **a new upstream model release must not
require an agentique release.** Everything below follows from that.

## Why a hard-coded list goes stale, and what doesn't

The Claude CLI accepts aliases which follow the newest model in a family, and it
reports the concrete ID it resolved to in its `init` event:

```
$ claude --print --output-format json --model 'opus[1m]' 'ok'
{"type":"system","subtype":"init",...,"model":"claude-opus-5[1m]",...}
```

The picker only needs one choice per family. Agentique uses the 1M aliases for
Sonnet and Opus, but labels them "Sonnet" and "Opus" because context size and
version are execution details. The exact version appears only on a session that
received it from the provider. This keeps the global picker stable across
Agentique releases without hiding what a particular session ran.

## The layers

`Catalog.ListModels` assembles each provider's list from four layers, weakest
first. Each layer degrades to the one below it on any failure — a catalog
request never fails, because a stale label beats an empty picker.

| Layer | Source | Gives |
|---|---|---|
| base | `claudeAliases` (claude) / `codexcli.ListModels` (codex) | the slugs |
| learned | `model_resolutions` table | alias and CLI-option de-duplication |
| cli | `~/.claude.json` → `additionalModelOptionsCache` | models the CLI advertises beyond the built-ins |
| config | `[models]` in `config.toml` | an explicit override that replaces the provider's list |

`ProviderModels.Source` names the strongest layer that applied
(`static` / `learned` / `cache` / `fallback` / `config`), so the frontend can
show staleness hints.

### learned: exact versions belong to sessions

`EventPipeline.handleInit` captures the resolved model ID and fires
`OnResolvedModel`. `persistResolvedModel`
(`internal/session/model_resolution.go`) makes two writes:

- `sessions.resolved_model` — history: what this conversation actually ran on.
- `model_resolutions (provider, slug, resolved_id)` — enough information to
  recognize a concrete CLI option already covered by an alias.

Only alias → concrete pairs are recorded. A session started on a pinned ID
teaches the catalog nothing and is skipped.

The pipeline also broadcasts `session.model-resolved`, so the active session can
show "Opus 5" as soon as the provider reports `claude-opus-5`. Session lists
carry the persisted `resolvedModel` after reload or reconnect. Changing a
session's configured model clears the old resolved value and returns the
display to its family name.

### cli — new families without a release

`additionalModelOptionsCache` is where the CLI records models the account can
reach beyond the built-in set. The catalog derives a family name and skips an
entry when its slug or family is already represented, so version and context
variants cannot create duplicate choices.

### config — the escape hatch

```toml
[[models.claude]]
slug = "opus[1m]"      # passed to the CLI verbatim; alias or pinned ID
display = "Opus"       # picker label; defaults to slug
description = ""       # optional secondary text
```

A non-empty list **replaces** that provider's generated list — it does not merge.
That is the point: it is the last resort for anything auto-detection misses, so
it must be predictable.

## Frontend

`frontend/src/lib/model-catalog.ts` owns the catalog helpers. `ModelId` is a bare
`string`, not a union. A closed union would add the release dependency this
design removes. The static `FALLBACK_MODELS` list covers only the window before
the first `providers.models` response and uses the same family-only labels.
`sessionModelLabel` combines that configured family with `resolvedModel` for
session-specific display.

## Adding a provider

Add a `<provider>Models` method that returns `ProviderModels`, call it from
`ListModels`, and call `applyOverride` first so the config layer keeps working.
