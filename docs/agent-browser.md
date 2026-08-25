# The agent's browser

Every session has a Playwright browser, in every mode, with no flag. Chrome stays
down until the first browser tool call. `experimental.browser` gates only whether
a human can *watch* it in a panel.

## Why the flag shrank

`experimental.browser` used to gate two different things that got conflated:
agent browser automation (common, headless is fine, nobody needs to watch) and
the integrated browser panel (a human driving a live Chrome in the UI, rare).

The first was welded inside the second. The `agentique-playwright` MCP connected
by `--cdp-endpoint` to the *same* Chrome the panel launched and screencast, so an
agent could not take a screenshot unless the flag was on **and** somebody had
opened the panel. With the flag off, which is the default, the agent had no
browser at all. Meanwhile the always-on session-files preamble still advertised
`browser_take_screenshot`, a fossil of the original intent that screenshots be
baseline. That dangling reference is what made agents report "the Playwright MCP
should be here but isn't connected".

Now the panel is a pure bonus: a live view of the agent's own browser, never a
second one.

Two things stayed off the table. There is no separately-named second browser MCP:
the agent always has exactly one toolset, and the panel only changes whether that
browser is visible. And Chrome is never launched eagerly, because most sessions
never browse.

## The two constraints that forced the shape

**An MCP server's launch args are fixed for the session lifetime.**
`ReconnectMCPServer` sends only `mcp_reconnect {serverName}`; it re-dials an
existing config and cannot change args. So one `agentique-playwright` cannot morph
between "headless self-managed" and "`--cdp-endpoint`" at runtime. The agent's
browser MCP has to use one fixed config for the whole session.

**`@playwright/mcp` advertises its tools even when the CDP endpoint is down.**
Probed directly: `npx @playwright/mcp --headless --cdp-endpoint <dead-port>`
completes the `initialize` handshake and returns all 23 `browser_*` tools from
`tools/list`. The CDP connection is attempted lazily on first tool use, governed
by `--cdp-timeout`, default 30s. So the agent sees the tools from turn 1 with
Chrome still down, and Chrome can be launched just in time on the first call,
well inside the connect timeout.

Together those force it: the MCP is **always** `--cdp-endpoint` pointed at an
agentique-managed Chrome, so the panel can view it, and the Chrome launches
**lazily**, so idle sessions pay nothing.

## The configuration

Every session, regardless of the flag:

```
npx @playwright/mcp --cdp-endpoint http://127.0.0.1:<port> \
    --output-dir <session-files-dir>
```

The port is pre-allocated at session create; Chrome is not launched.

`--output-dir` points screenshots straight at the session files directory, so
output is immediately embeddable through `/api/sessions/<id>/files/...` with no
copy step. **This only holds when the agent calls `browser_take_screenshot`
without a `filename`.** A bare `filename` resolves cwd-relative instead, which is
why the preamble tells the agent to omit it. The default path also returns the
image inline as a fallback, and drops a small `page-*.yml` snapshot beside each
png.

There are no profile or launch flags on the MCP. In `--cdp-endpoint` mode it
connects to an agentique-launched Chrome rather than launching its own, so
headlessness and the profile are governed by agentique's launch.
`launchOnPort` uses `--headless=new` with a persistent per-session profile at a
stable path keyed by session, reused across relaunches within that session. `Stop`
kills Chrome and leaves the profile directory, so persistence is automatic.

## Lazy launch, on two different hooks

`EnsureBrowser` launches Chrome on the pre-allocated port if it is not already
running, waits for CDP ready, and returns. It is idempotent and
concurrency-safe — `handlePendingChange` runs on a goroutine, so several browser
tool calls can race it, and a per-session mutex keeps that to one launch. The
first call pays a few hundred milliseconds of cold start; later calls are no-ops.

**`EnsureBrowser` never touches the CLI control channel.** It does not send
`mcp_reconnect`. The MCP connects lazily at tool-execution time, so having Chrome
up before the call is approved is sufficient; the MCP attaches itself when the
approved tool runs. That is the crux simplification: there is no "reconnect while
an approval is pending", so the control-channel re-entrancy risk is designed out
rather than mitigated.

Which hook fires depends on the approval mode.

**The approval pump**, for `AutoApproveOff` modes (default, acceptEdits, plan,
auto). `handlePendingChange` runs on every tool approval before the call executes,
detects a `mcp__agentique-playwright__*` tool name by prefix, and ensures Chrome
is up.

**The tool interceptor**, for `fullAuto`. This is the one hook that runs before
the `AutoApproveAll` short-circuit, because interceptor lookup precedes the mode
check in `handleToolPermission`. `fullAuto` maps to `runtime.AutoApproveAll`,
which the runtime resolves inside its own tool-permission callback by returning
Allow immediately: no `PendingApproval`, no `PendingChangeEvent`, so
`handlePendingChange` never runs. The browser tool then dispatched against a port
with nothing listening and `@playwright/mcp` failed with a raw CDP
`ECONNREFUSED` — no agentique deny, no prompt. That was the live failure in the
default autonomous mode.

`interceptBrowserTool` is registered for every browser tool name. It calls
`EnsureBrowser` when the session is in `fullAuto` and deliberately no-ops in the
other modes, so the pump stays the single launch point there.

The interceptor map is keyed by **exact tool name** with no prefix matching, which
is why the browser tool names are enumerated. `isBrowserTool`'s prefix check still
backs the pump path, so a missing entry degrades only `fullAuto`, and only until
the first listed tool (navigate, snapshot, screenshot — the natural first action)
brings Chrome up for the rest of the session.

**Why not route `fullAuto` through the pump instead**, so the prefix branch would
cover it: `fullAuto → AutoApproveAll` is load-bearing for codex. Its adapter maps
`AutoApproveAll` to `("never", SandboxDangerFull)` at connect, so demoting it to
`AutoApproveOff` would silently re-enable codex approval prompts and lock its
sandbox. The interceptor keeps the claude fast path intact and is
provider-neutral.

**Known gap: codex `fullAuto`.** `SandboxDangerFull` bypasses the runtime
permission callback natively, so no interceptor fires for a codex `fullAuto`
session, and its browser tools still hit a dead port on first use. Tracked in
`docs/tech-debt.md`. The claude path is fully covered.

Opening the panel is the second trigger for the same `EnsureBrowser`, so there is
something to view even if the agent has not browsed yet.

## Host provisioning

The guiding rule for host dependencies:

> Auto-provision anything that lives in userspace. Detect and instruct, never
> silently run, anything that needs root or a system package manager.

Chrome is the lucky case. Playwright self-provisions a Chromium into a user cache
with no `apt` and no root, so `EnsureBrowser` heals a host that has no browser:

1. **Discover.** `findChrome` probes the Playwright-managed Chromium cache as a
   final fallback, after the system-binary lookup list.
2. **Provision if absent.** Run `npx playwright install chromium`, which is
   userspace and idempotent, then point at the cached binary and launch. The
   install is single-flight per host, since the cache is shared across sessions,
   and it broadcasts `session.browser-provisioning` so the UI can explain why the
   first browser call is paused for a one-time ~150MB download.
3. **The privileged gap gets instructions, not sudo.** On a bare Linux host the
   downloaded Chromium may still fail on missing shared libraries (`libnss3`,
   `libatk`, and friends). That is the one step needing root, so `EnsureBrowser`
   returns an error whose message carries the exact remedy,
   `npx playwright install-deps chromium`, surfaced to both the agent and the UI.

So the browser works on a fresh host with no human in the loop, while every
privileged action stays consent-gated.

## What the flag still controls

| | flag off (default) | flag on |
|---|---|---|
| Agent `agentique-playwright` tools | always | always |
| Lazy headless Chrome | on first use | on first use |
| Screenshots into session files | yes | yes |
| Browser toggle and panel in the UI | no | yes |
| Live screencast and human input | no | when the panel is open |

`browserSvc` is always constructed, because it owns Chrome's lifecycle for the
agent. The flag is read only where the panel is exposed: the frontend
`features.browser` gate and the backend screencast start.

## The panel

Opening it calls `EnsureBrowser`, which reuses the agent's Chrome if it is already
up, then starts a screencast and wires human mouse and keyboard input. The human
watches and can intervene in the exact browser the agent drives: same CDP target,
same tab. Closing the panel stops the screencast; Chrome keeps running for the
agent.

No MCP reconnect, no config swap, no second server, which is how the fixed-args
constraint is satisfied for free.

Two CDP clients on one Chrome (the screencast client plus the `--cdp-endpoint`
MCP) is what the old `experimental.browser` path already did, so that coexistence
is exercised by the existing feature rather than being new.

## Preamble

The always-on session-files preamble tells the agent it has headless Playwright
that launches on first use, and to call `browser_take_screenshot` **without** a
`filename` so it auto-saves into the session files directory.

`preambleBrowser`, still gated on the flag, is now only the panel bonus: when the
panel is open, a human can watch and intervene live. The old "do not use until
launched" warning is gone, because the tools are always usable.

## Known cost

Chrome is process-lifetime, so it is gone after a server restart and relaunches on
the next browser tool. Lazy launch already handles that.

The per-session Chrome profile directory lingers in tmp even after the session is
deleted. `agentique prune` reclaims it.
</content>
