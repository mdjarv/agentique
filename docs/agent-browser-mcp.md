# Agent browser MCP — decoupling Playwright from the UI panel

## Problem

Today a single flag, `experimental.browser`, gates **two different things that got
conflated**:

- **Agent browser automation** — agents navigating, testing, and taking
  screenshots. Common, headless is fine, no human needs to watch.
- **The integrated browser panel** — a human watching/driving a live Chrome in a
  UI panel. Rare.

The first is welded inside the second. The `agentique-playwright` MCP connects via
`--cdp-endpoint` to the *same* Chrome the panel launches and screencasts, so an
agent can't take a screenshot unless `experimental.browser` is on **and** someone
has launched the panel's Chrome. With the flag off (the default), the agent has
no browser at all — yet the always-on Session Files preamble still advertises
`mcp__agentique-playwright__browser_take_screenshot`, a fossil of the original
intent that the screenshot capability be a baseline. That dangling reference is
what misleads agents into reporting "the Playwright MCP should be here but isn't
connected."

## Goal

Make agent browser automation a **baseline capability available in every
session**, independent of the rarely-used panel. Keep the panel as a pure
*bonus*: when enabled, it's a **live view of the agent's own browser**, not a
separate one.

### Non-goals

- A second, separately-named browser MCP. The agent always has exactly one
  browser toolset (`mcp__agentique-playwright__*`); the panel only changes
  whether that one browser is *visible*, never which tools exist.
- Eagerly launching Chrome for every session. Most sessions never browse;
  performance-first means Chrome stays down until first use.

## Constraints that shape the design (all verified)

1. **An MCP server's launch args are fixed for the session lifetime.**
   `Session.ReconnectMCP` → `claudecli.ReconnectMCPServer(serverName)` sends only
   `mcp_reconnect {serverName}`; it re-dials an *existing* config and cannot
   change args. So we cannot morph one `agentique-playwright` between
   "headless self-managed" and "`--cdp-endpoint`" at runtime. → The agent's
   browser MCP must use **one fixed config** for the whole session.

2. **`@playwright/mcp` advertises its tools even when the CDP endpoint is down.**
   Probed empirically: `npx @playwright/mcp --headless --cdp-endpoint <dead-port>`
   completes the `initialize` handshake and returns all 23 `browser_*` tools from
   `tools/list`. The CDP connection is attempted lazily on first tool use
   (governed by `--cdp-timeout`, default 30s). → The agent can *see* the browser
   tools from turn 1 with Chrome still down; we launch Chrome just-in-time on the
   first call, well within the connect timeout.

3. **A pre-execution interception point exists.** `handlePendingChange`
   (`runtime_bridge.go:136`) runs on every tool approval, on a goroutine, with
   `rtA.ToolName` available, *before* the call is allowed to execute. It already
   does async work before `SubmitApproval`. → We can ensure Chrome is up before
   approving the first `mcp__agentique-playwright__*` call.

4. **The existing Chrome launch is already headless + debug-port +
   screencast-ready.** `browser.Manager.launchOnPort` (`manager.go:124`) launches
   `--headless=new --remote-debugging-port=<port>`, and `Page.startScreencast`
   works on headless. → The agent's lazy Chrome *is* the same instance the panel
   screencasts. The panel adds no new browser, only a screencast toggle.

Constraints (1)+(2) together force the shape: the agent's MCP is **always**
`--cdp-endpoint` pointed at an agentique-managed Chrome (so the panel can view
it), and the Chrome is launched **lazily** (so idle sessions pay nothing).

## Design

### The agent's browser MCP is always present, always CDP, lazily backed

Every session — regardless of `experimental.browser` — is created with the
`agentique-playwright` MCP configured as:

```
npx @playwright/mcp --cdp-endpoint http://127.0.0.1:<port> \
    --output-dir <session-files-dir>
```

- `<port>` is **pre-allocated** at session create (as today via
  `browserSvc.AllocatePort`), but **Chrome is not launched**.
- `--output-dir <session-files-dir>` points screenshots straight at the session
  files directory, so `browser_take_screenshot` output is immediately embeddable
  via `/api/sessions/<id>/files/...` with no copy step. **Caveat (spike-verified):**
  this only holds when the agent calls `browser_take_screenshot` *without* a
  `filename` — a bare `filename` resolves cwd-relative instead. So the preamble
  steers the agent to omit it. The default path also returns the image inline as a
  fallback, and drops a small `page-*.yml` snapshot beside each png (minor clutter).
- **No profile/launch flags on the MCP.** In `--cdp-endpoint` mode the MCP
  *connects to* an agentique-launched Chrome rather than launching its own, so
  headless-ness and the browser **profile** are governed by agentique's launch,
  not the MCP. `browser.Manager.launchOnPort` already launches `--headless=new`
  with a **persistent per-session profile**
  (`--user-data-dir=<tmp>/agentique-chrome-<sessionID>` — a stable path keyed by
  session, reused across relaunches within the session). `Stop` kills Chrome but
  leaves the profile dir, so persistence is automatic; the dir is only ever
  removed when the session itself is deleted. (Today it lingers in tmp even after
  delete — a minor cleanup follow-up, not blocking.)
- The MCP process starts and advertises all 23 tools immediately (constraint 2).

### Lazy Chrome launch on first browser tool

In `handlePendingChange`, before the bypass/broadcast decision, detect a
`mcp__agentique-playwright__*` tool name and **ensure Chrome is up**:

```
if isBrowserTool(rtA.ToolName) {
    if err := browserSvc.EnsureBrowser(s.ID); err != nil {
        // deny with a clear message; do not hang the call
    }
}
// ... existing bypass / UI-broadcast / SubmitApproval logic unchanged
```

`EnsureBrowser` is **idempotent and concurrency-safe**: launch Chrome on the
pre-allocated port if not already running, wait for CDP ready, return. First
browser call pays a one-time cold start (a few hundred ms, far under the 30s
`--cdp-timeout`); subsequent calls find Chrome up and are no-ops. The MCP's lazy
CDP connect then succeeds against the live endpoint. Works identically in
auto-approve and manual-approve modes because it runs ahead of that branch.

**`EnsureBrowser` is a purely local op — it never touches the CLI control
channel.** It does *not* send `mcp_reconnect`: the spike (below) proved
`@playwright/mcp --cdp-endpoint` connects lazily at *tool-execution* time, so
having Chrome up before we approve the call is sufficient; the MCP attaches
itself when the approved tool runs. This is the crux simplification — there is no
"reconnect while an approval is pending," so the control-channel re-entrancy risk
is designed out, not merely mitigated.

A second trigger calls the same `EnsureBrowser`: **the user opening the panel**
(below), so there's something to view even if the agent hasn't browsed yet.

### Host provisioning — auto-install the browser binary

A guiding principle for host dependencies:

> **Auto-provision anything that lives in userspace; detect-and-instruct (never
> silently run) anything that needs root or a system package manager.**

Chrome is the lucky case: Playwright self-provisions a Chromium into a user cache
(`~/.cache/ms-playwright/...`) with **no `apt`, no root**. So `EnsureBrowser`
self-heals a host that has no browser:

1. **Discover.** `findChrome` (injectable, cached — `manager.go:41`) gains a final
   fallback that probes the Playwright-managed Chromium cache, after the existing
   system-binary `LookPath` list.
2. **Provision if absent.** If nothing is found, run `npx playwright install
   chromium` (userspace, idempotent), then point `chromePath` at the cached
   binary and launch. The install is **single-flight per host** (the
   `ms-playwright` cache is shared across sessions) and broadcasts a
   `session.browser-provisioning` status so the UI shows *why* the first browser
   call is paused (a one-time ~150MB download).
3. **Privileged gap → instruct, don't sudo.** On a bare Linux host the downloaded
   Chromium may still fail to launch on missing shared libs (`libnss3`, `libatk`,
   …). That's the one step that needs root, so we **don't** run it — `EnsureBrowser`
   returns an error whose message carries the exact remedy
   (`npx playwright install-deps chromium`), surfaced to the agent and UI.

This makes the browser feature **just work on a fresh host with no human in the
loop** (the autonomy north star), while keeping every privileged action
consent-gated. The general cross-feature version of step 3 — a read-only
`agentique doctor` preflight for *all* optional deps (browser binary, the brain's
docker/chroma/ollama, node/npx) — is noted as a follow-up below, not part of this
work.

### `experimental.browser` shrinks to a UI-only gate

The flag no longer controls *whether the agent has a browser*. It controls only
the **panel**:

| | flag off (default) | flag on |
|---|---|---|
| Agent `agentique-playwright` tools | ✅ always | ✅ always |
| Lazy headless Chrome | ✅ on first use | ✅ on first use |
| Screenshots → session files | ✅ | ✅ |
| UI `BrowserToggle` button / panel | ❌ | ✅ |
| Live screencast + human input | ❌ | ✅ when panel opened |

Backend `browserSvc` is **always constructed** (it owns Chrome lifecycle for the
agent). The flag is read only where the panel is exposed: the frontend
`features.browser` gate (`SessionHeader.tsx:52`) and the backend screencast start.

### Panel = live view, not a second browser

When the panel is enabled and the user opens it:

1. `browser.launch` WS handler calls `EnsureBrowser` (idempotent — reuses the
   agent's Chrome if already up).
2. Start `Page.startScreencast` → `browser.frame` to the panel; wire
   `browser.input` for human mouse/keyboard.
3. The human now watches and can intervene in the **exact** browser the agent
   drives — same CDP target, same tab.

Closing the panel stops the screencast; Chrome keeps running for the agent.
No MCP reconnect, no config swap, no second server — this is why constraint (1)
is satisfied for free.

### Preamble

- The line-180 screenshot note in the always-on `preambleSessionFiles` is now
  **correct unconditionally** (the tool always exists). Keep it, and add a short
  always-on line: *"You have headless Playwright via `agentique-playwright`
  (`mcp__agentique-playwright__*`); it launches on first use. Call
  `browser_take_screenshot` **without** a `filename` so it auto-saves into your
  session files dir, then embed it via `/api/sessions/.../files/<name>`."*
- `preambleBrowser` (gated on `browserEnabled`) slims to just the **panel** bonus:
  *"When the browser panel is open, a human can watch and intervene in your
  browser live."* The "do NOT use until launched" warning is **removed** — the
  tools are always usable now.

## Validation (spike, 2026-06-28)

A standalone harness exercised the load-bearing mechanics end-to-end (no live
Claude turn needed — the parts that needed one were settled by code-reading
instead). Results:

- **Lazy connect, no reconnect (the crux).** Started `@playwright/mcp
  --cdp-endpoint :P` with Chrome **down**; `tools/list` returned all 23 tools.
  Brought Chrome up on `:P` afterward, then issued the **first** `browser_navigate`
  with **no reconnect signal** → **OK**. Confirms `EnsureBrowser` only needs to
  launch Chrome before approving; no `mcp_reconnect` in the hot path.
- **Control channel can't deadlock (code-read).** `claudecli` `sendControlRequestRaw`
  is async + id-correlated, with the stdin mutex held only per-write, and
  `handlePendingChange` is already goroutine-offloaded. Combined with "no reconnect
  needed," the re-entrancy risk is fully designed out.
- **Userspace provisioning works.** `npx playwright install chromium` (no root)
  installed to `~/.cache/ms-playwright/chromium-*/chrome-linux*/chrome`, discoverable
  by glob — the `findChrome` fallback + `ProvisionChromium` path is real.
- **Screenshots → output-dir (default path).** A no-`filename`
  `browser_take_screenshot` saved `page-<ts>.png` into `--output-dir` and also
  returned the image inline; a bare `filename` instead writes cwd-relative (hence
  the preamble steer).

Residual unknown is now only the live two-CDP-clients screencast coexistence — but
that is **already** what today's `experimental.browser` path does (screencast CDP
client + `--cdp-endpoint` MCP on one Chrome), so it's exercised by the existing
feature, not new. Confidence: ~95%.

## Implementation steps

1. **`browser_service.go`**
   - `StandalonePlaywrightMCPConfig(port, outputDir string) string` — the always-on
     config above (cdp-endpoint + output-dir only; no launch/profile flags).
   - `EnsureBrowser(sessionID) error` — idempotent launch+CDP-ready, factored out
     of `LaunchBrowser`. `LaunchBrowser` (panel open) becomes `EnsureBrowser` +
     start screencast. On a missing binary, runs the provisioning step before
     launch and broadcasts `session.browser-provisioning`.
2. **`browser/manager.go` + `chrome_unix.go`/`chrome_windows.go`** — extend
   `findChrome` with a Playwright-cache fallback; add `ProvisionChromium()`
   (`npx playwright install chromium`, single-flight per host) and map a
   launch-time missing-shared-lib failure to an error carrying the
   `install-deps` remedy.
3. **`service.go`** — always emit the browser MCP config in create (`:331`) and
   resume (`:1283`), gated on `browserSvc != nil` *being constructed* (always),
   not on the experimental flag. Thread the session-files dir into the config.
   Rename `allocateBrowserPort` → `browserMCPConfig`.
4. **`server.go`** — construct `browserSvc` unconditionally; pass
   `cfg.ExperimentalBrowser` separately as the **panel** gate only (features map
   `:188`, frontend).
5. **`runtime_bridge.go`** — add the `isBrowserTool` → `EnsureBrowser` guard at
   the top of `handlePendingChange`.
6. **`preamble.go`** — add always-on standalone line; slim `preambleBrowser`.
7. **Frontend** — `features.browser` keeps gating the `BrowserToggle`/panel only
   (already does), plus a small `session.browser-provisioning` status indicator.

## Edge cases & risks

- **Chrome / Chromium not installed.** `EnsureBrowser` auto-provisions the
  Playwright Chromium (userspace, see *Host provisioning*) on first use — the
  feature self-heals with no human in the loop. The only non-recoverable case is
  the privileged shared-lib gap, which is surfaced as an actionable
  `install-deps` instruction rather than denied opaquely. **No up-front feature
  knob** — provisioning + graceful instruct covers it.
- **Per-session node MCP process.** Already the pattern (channel/agentique MCP
  servers spawn per session). Only the *node* process is per-session; **Chrome
  stays lazy**, so idle sessions cost nothing beyond what they already do.
- **Concurrency.** `handlePendingChange` runs on a goroutine; multiple browser
  tool calls can race `EnsureBrowser`. Guard with the per-session browser mutex
  so only one launch happens.
- **Resume / reconnect.** Same config re-applied; Chrome (process-lifetime) is
  gone after a restart and re-launches on the next browser tool. Already handled
  by making launch lazy.
- **Deny / cold-start latency.** First browser call blocks on a few-hundred-ms
  Chrome start inside the approval pause. Acceptable and one-time per session.

## Testing

- Config-string test: `StandalonePlaywrightMCPConfig` includes the cdp-endpoint
  port and output-dir (and no launch/profile flags).
- Wiring test: browser MCP config present regardless of `ExperimentalBrowser`;
  panel/screencast paths present only when on.
- Preamble tests: always-on standalone line present unconditionally; panel block
  present only when `browserEnabled`.
- `EnsureBrowser` idempotency / concurrency test (fake `execCommand`).
- Provisioning test: with `findChrome` returning "not found", `EnsureBrowser`
  invokes the provisioning command (injected/faked) and a launch-time
  missing-lib failure maps to the `install-deps` remedy message.
- `go test ./... -count=1 -short`, then `just check`.

## Follow-up (out of scope here): `agentique doctor`

The provisioning logic above is the first instance of a general pattern: optional
features depend on host resources that may be absent. A separate, read-only
`agentique doctor` preflight would check them all and report present/missing with
exact install hints, applying the same boundary (userspace = offer to provision,
privileged = instruct only):

- **browser:** Chrome/Chromium binary + shared libs.
- **semantic brain:** docker, Chroma heartbeat, Ollama + the embed model.
- **MCP servers:** node/npx availability.

Worth its own short design doc; this feature ships without it (browser
self-provisions; everything else already degrades cleanly).
