# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

Inferred from the codebase, not asked: the interface is a React SPA served by the Go
binary and installed as a PWA (`vite-plugin-pwa`, maskable icons, `apple-mobile-web-app-capable`).
The phone experience is mobile **web**, not a native app. The desktop tray (`agentique tray`)
is a Go notification-area controller with no UI surface of its own.

## Users

**Primary — the fluent daily driver.** The author and a small circle of power-user peers:
developers who already understand git worktrees, provider CLIs, tool-permission prompts,
and multi-agent delegation. They run the server on their own machine and supervise several
agents at once. Design for density, speed, and at-a-glance state; explanation and onboarding
are secondary.

**Not the design target, but real:** the public install path (`install.sh` / `install.ps1`,
README, GitHub releases) means first-time OSS adopters do arrive. Multi-human deployments
are also supported (WebAuthn passkeys, first-visitor-becomes-admin, invite tokens). Neither
audience gets to set the design's center of gravity.

## Product Purpose

A GUI for running and supervising **concurrent coding agents across multiple projects**.
Each session drives a provider CLI (Claude or Codex) in its own git worktree, so parallel
agents never clobber each other's working tree. The UI is where the operator starts work,
watches turns stream, approves or denies tool use, reads diffs, merges branches or opens PRs,
and coordinates agents that talk to each other.

Success is operational, not aesthetic: with N agents running, the operator can tell in one
look **which one needs them right now**, and act on it without hunting.

## Positioning

A Go backend driving provider CLIs through `agentkit/runtime`'s neutral `CLISession` /
`CLIConnector` contract — so provider choice is per session, feature parity moves at SDK
pace, and the whole thing ships as **one embedded binary running on your own machine**.

What a neighboring tool could not truthfully copy today: worktree-isolated concurrent
sessions, **plus** multi-agent coordination as a first-class product surface (channels,
`@spawn` delegation with lead auto-approval, session hierarchy, personas, discussions),
**plus** persistent cross-session agent memory (the brain: recall → encode → consolidate,
with a semantic graph), all local and all in one process.

Explicit anti-reference from the project's own history: `pingdotgg/t3code`, whose Node.js +
Effect-TS backend had process-management stalls. The Go runtime is the answer to that.

## Operating Context

- **Where it runs.** A local server (systemd user unit / launchd agent / Windows Scheduled
  Task) on the developer's own machine, opened in a browser at `localhost:9201`, or reached
  over LAN/Tailscale with TLS + passkeys.
- **Two equally weighted scenes** (user-confirmed): a wide screen at the desk supervising
  many concurrent sessions, and a phone away from the desk. Neither is the fallback for the
  other — a surface that degrades on one of them has failed.
- **The work is git.** Worktree per session, branch per session, diff viewing, merge and
  `gh`-backed PR creation from the UI. Projects point at local filesystem paths; the
  database is not portable between machines.
- **Waiting is normal, not exceptional.** Provider CLI init takes ~30–40s on first connect.
  Turns stream over WebSocket, reconnect with backoff, hit rate limits, get compacted, hang
  (there is a watchdog), or get evicted when idle and lazy-resume on the next message.
  Partial and interrupted states are the steady state, not an edge case.
- **Approval is an interruption the operator owns.** Tool-permission prompts, plan-mode
  approvals, and `AskUserQuestion` blocks stop an agent until a human answers. Reaching the
  operator is the point.
- **Vocabulary** used throughout product, code, and UI: project, session, worktree, turn,
  tool call, permission, provider, channel, lead, worker, team, persona, discussion,
  template, fact / memory / area (the brain).

## Capabilities and Constraints

**Surfaces that exist today** (`frontend/src/routes/`): projects list; project overview,
files, and settings; new session; session chat; channels; teams and personas; discussions;
brain; templates; storage.

**Hard constraints:**

- **Local-first, single binary** (user-pinned). The Go binary embeds built frontend assets
  via `embed.FS`. The UI must not depend on a network CDN or a remote service at runtime.
  *Known violation to resolve, not to design around:* `frontend/index.html` currently
  fetches Inter, JetBrains Mono, and Space Grotesk from the Google Fonts CDN. Self-hosting
  those is open work; new work must not add further external asset dependencies.
- **Never surface cost or pricing** (user-pinned, also in CLAUDE.md). `totalCost` exists in
  the data model and stays out of every UI, CLI output, and mockup. Usage is subscription-based.
- **State legibility under load** (user-pinned) is the product's core job, not a feature:
  many concurrent sessions each in a distinct state — running, streaming, waiting on
  approval, waiting on a question, rate-limited, hung, stopped, merged, failed — and the one
  that needs you must be findable instantly, at any session count, on either screen size.
- **Provider capability differences are runtime-gated, never assumed.** Codex does not
  natively support fork, plan mode, thinking, subagents, compaction events, MCP reconnect,
  or tool-progress ticks; mid-turn send is emulated via queue-and-replay. UI must read
  `Capabilities()` rather than hard-code a provider's feature set.
- **Feature flags.** Teams/channels coordination and in-app browser tooling are gated behind
  `[experimental] teams` / `browser` in `config.toml`. Team coordination features are
  **additive**: they must not change rendering or turn management for sessions outside a channel.
- **Auth.** WebAuthn passkeys only, no passwords; first visitor becomes admin; further users
  join by invite token. `--disable-auth` exists for trusted single-user hosts.
- **Stack:** React 19, Vite, TanStack Router, Zustand, Tailwind 4, shadcn/ui, PWA with an
  auto-updating service worker. User-selectable theme (light / dark / system), dark by default.

**Explicitly undecided / parked** (from ROADMAP.md, not to be invented as shipped):
cross-project DMs, topology presets, autonomy tuning (Persistent Teams phases 3–5);
split-pane session layout; MCP server management UI; Tauri desktop app; xterm.js terminal;
sibling-session awareness; cross-session housekeeping delegation.

**Deliberately not elevated:** keyboard-driven operation. Shortcuts ship and work, but the
user did not make keyboard flow a binding design commitment in this interview — treat it as
existing capability, not a constraint future work must serve.

## Brand Commitments

- Name: **Agentique**. Lowercase `agentique` as the binary, command, and data-dir name.
- Existing icon set in `frontend/public/`: `icon.svg`, `icon-maskable.svg`, `favicon.ico`,
  `apple-touch-icon.png`, `icon-192.png`, `icon-512.png`.
- No confirmed logo guidelines, wordmark rules, voice/tone document, or copy style guide
  exist. None were established in this interview; do not invent them.

## Evidence on Hand

- **Documentation that is true as-built:** `README.md` (install, config, CLI, architecture),
  `ROADMAP.md` (shipped vs. next vs. parked), `CLAUDE.md` (as-built channel/team, provider,
  brain, and process-lifecycle behavior), `docs/*.md` (16 subsystem and design docs).
- **Real data for realistic states:** the live SQLite database at
  `~/.local/share/agentique/agentique.db` (projects, sessions, session_events, messages,
  teams, tags). Reads are encouraged; writes require explicit approval. Use it rather than
  inventing plausible session lists.
- **A running instance to observe:** the app can be run against an isolated `AGENTIQUE_HOME`
  and screenshotted with Playwright; publicly-routable dev-URL slots exist for live iteration.
- **Absent, and must not be fabricated:** customers, testimonials, case studies, press,
  benchmarks, pricing, usage statistics, and any claim of a hosted or cloud offering.

## Product Principles

1. **The operator's attention is the scarce resource.** Every surface answers "what needs me,
   and where?" before it answers anything else.
2. **Both screens are first-class.** Desk-wide and phone are equally weighted scenes; a
   design that only survives on one of them is incomplete.
3. **Built for the fluent, not the first-timer.** Prefer density, precision, and real
   vocabulary over hand-holding — without making a newcomer's first hour hostile.
4. **Truthful under failure.** Waiting, partial, rate-limited, hung, reconnecting, and
   evicted are designed states with honest representation, not afterthoughts.
5. **Self-contained by construction.** One binary, one machine, no runtime dependency on
   anything remote — including fonts, icons, and assets.
