# Subscription usage

How much of each AI subscription's rate-limit window is spent, when it resets,
and how much disk is left — as one instrument in the sidebar footer, and one
panel behind it.

**Status: shipped — Claude, Codex, the storage gauge, the indicator and the
panel.**

The interesting half is not the UI. It is getting trustworthy numbers out of
vendors that expose them completely differently, and degrading honestly when one
will not answer. Every rule below exists because the naive version was wrong
against the live endpoint.

## The split

A collector per vendor produces one normalized record; the client renders
records without ever learning that Claude is HTTP and Codex is a JSON-RPC
subprocess. Adding a third vendor is one collector and no UI change.

Polling is **server-side**, on the precedent the update checker set: constructed
in `server.New`, started from serve's production block, never from a constructor
a test might call. One probe serves every client, so five tabs cost one request
and a closed browser does not stop the numbers being current.

```
GET /api/usage            (authenticated; every client shows the indicator)
GET /api/usage?refresh=1  (bypasses the reuse window)
```

`percent` is a **fraction 0..1**, never 0..100. It may exceed 1 — clamp when
drawing, never when reporting. **`percent < 0` means unknown, not zero**, and
such a window is filtered from every surface: `usableLimits` is the one filter,
shared by the indicator and the panel, so the two can never disagree about which
windows exist.

**The set of limits is not fixed.** Model-scoped allowances come and go as the
account spends against them. Never hardcode the count or the labels. An unknown
`id` still renders, with a generic mark and a neutral colour.

## Claude, and its four traps

Credentials come from the CLI's own store at `~/.claude/.credentials.json`. The
token goes into an `Authorization` header and **nowhere else** — not the cache,
not the response, not a log line. Only the derived plan label
(`default_claude_max_20x` → "Max 20x") ever leaves `claude.go`, and a test
asserts the token does not survive into the marshalled record.

Reading that file is not "running a provider CLI": it is a file read and an
HTTPS request. Keeping it here rather than in `claudecli-go` also preserves that
library's documented network-free property (decision C2).

The traps, all four verified against the live payload:

1. **`limits[]` is the source of truth; the flat buckets are only a fallback.**
   The model-scoped weekly window exists *only* in `limits[]` — the matching
   legacy buckets (`seven_day_opus`, `seven_day_sonnet`) sit at `null`, so a
   collector reading buckets alone silently drops a limit the account is
   actively spending against.
2. **Never iterate the buckets.** The payload carries codenamed top-level
   buckets — `amber_ladder`, `nimbus_quill`, `tangelo` and others. Most are
   null. `nimbus_quill` is not: it reads `{utilization: 0.0}` with no `limits[]`
   entry, so a thorough collector renders a meter for something with no name a
   user would recognise. The fallback reads the two well-known keys and nothing
   else.
3. **Scale is decided once per payload, from the whole payload.** The endpoint
   reports percentages (`37.0`); older payloads used fractions (`0.37`).
   Deciding per-value means a genuine `1.0` renders as 100% when it meant 1%.
4. **The window comes from `kind`, never from free text.** A model called
   "Opus 5 (1M context)" parsed for a window yields "1M" → a one-minute window.
   The live kinds are `session`, `weekly_all`, `weekly_scoped`. A model can hold
   more than one scoped window, so the dedup key is the pair (model, window).

`resets_at` arrives as an ISO string, epoch seconds or epoch millis; all three
normalize to RFC3339 UTC, with 1e12 separating seconds from milliseconds.

Each entry also carries its own `severity`. **The vendor's verdict wins** — the
server knows what counts as a warning for its own limit, and a client-side
threshold is a guess about somebody else's allowance. Thresholds are the
fallback.

## Gauges and allowances

`kind: "gauge"` marks a record whose levels are **conditions rather than
allowances**, and disk is the only one today.

An allowance is spent and then resets, so it is news: it may escalate to a
warning colour and it shows a countdown. A gauge is a level that is simply where
it is. A 75 GB disk sitting at 88% is the normal state of a small machine, not
something that happened — so a gauge **never** escalates, **never** shows a
countdown, and is drawn neutral at any height. It shows its absolute figure
instead ("9.2 GB free"), because that says more than a percentage does.

Getting this wrong makes the footer show a permanent warning, which teaches the
reader to ignore warnings. One field keeps the two kinds of thing from
pretending to be the same kind of thing.

`UsedPercent` comes from `storage.Stats()` rather than being recomputed, so the
footer and the Storage page cannot disagree — that one is df-style
(`used / (used + available)`), which excludes root-reserved blocks.

## Today's spend

`todayTokens` and `todayPrompts` come from **agentique's own turn results**
(`session_events` where `type = 'result'`), not from a scan of the CLI's JSONL
transcripts.

agentique is the thing that ran those turns, so it already knows — no directory
walk, no dedup by message id, no disk cache. It also answers for *every*
provider, where a Claude-transcript scan can only ever answer for one. The trade
is that work done outside agentique is not counted, which the panel's tooltip
says in those words.

`date(created_at) = date('now', 'localtime')` — "today" means the operator's
day, not UTC's.

## Failing honestly

Every failure becomes a **state on the record**, never an error that blanks the
component. The last good numbers stay on screen with a line saying why they are
old, and that rule lives on both sides: the collector keeps the previous limits
when a probe fails, and the store keeps the previous document when a fetch
throws.

**A transport failure and an HTTP status are different.** Nothing answered — no
DNS, no route, refused — warrants a fast retry, because the first probe after a
laptop wakes commonly beats DHCP. Any HTTP status, including an error one, means
a server *did* answer, so back off and do not pester it. A 429 surfaces its
`retry-after`.

**Auth has four distinct states and each needs its own words**: no token at all;
a token whose `expiresAt` has passed; a probe rejected; a probe that succeeded
and returned no limits. "Error" for all four is useless — only the CLI can mint
a fresh token, so the expired case names `claude auth login`, and the
no-limits case must not blame auth for something that is not an auth problem.
An expired token is never spent on a request that cannot succeed.

**A cached percentage expires when its window rolls over, not on a timer.** A
stale 78% would misreport an allowance that has since reset to zero. It is
per-limit, because one window can roll over while another has days to run. A
limit with no reset time, or one that will not parse, is **kept** — an
unreadable timestamp is no reason to throw away a real number.

## The two surfaces

**The indicator** is one group per agent: a run of vertical meters, one per
window, then that vendor's mark. No numbers at this level — the point is
reading "how much room is left" without reading anything. Colour does the
warning, the mark does the identification.

A window at 0% still draws a **visible stub**, because an empty track reads as a
*missing* agent rather than an unused one. When no agent has a usable window the
component renders nothing at all: a row of zeros is a worse lie than silence.

A glyph per group is also what makes three meters legible where three anonymous
columns were not — those are something you learn once and then rely on
remembering. `SiClaude` is the official mark; OpenAI's was withdrawn from
simple-icons, so `ProviderMark.tsx` carries a hand-authored one in its own
component, to be swapped for an official asset without touching anything else.

**The panel** is one section per agent: name, plan tier, any status text, then
one row per window — label, countdown, percentage, and a full-width track
beneath it — then the local stats line.

Countdowns are minutes-scale, so the clock ticks every **30 seconds and only
while the panel is open**. A per-second tick repaints sixty times for a label
that changes once.

The client polls every 15 minutes, dropping to 30 seconds while nothing has
answered *at all*. That retry is armed off "do we have a document", never off a
request completing: a fetch that never starts produces no completion to hook,
and a completion-driven retry leaves the indicator dark forever.

## Codex, through the connector

The spec's approach — spawn `codex -s read-only -a never app-server` and speak
JSON-RPC — is not available to agentique, which never runs a provider CLI. That
rule is not fussiness: the binary agentique would spawn is the connector's
business, and a PATH lookup here is right only by coincidence. Anything the
product needs from a CLI is a gap in that library.

Both gaps are now closed. `codexcli-go` v0.3.0 added `Conn.AccountRateLimits`,
and agentkit v0.4.0 added **`runtime.AccountInspectable`**, type-asserted off a
`CLIConnector` — the same seam `InstallInspectable` uses, and for the same
reason: the connector owns its client options, so it is the only thing that
stays correct if a binary path is ever overridden.

That makes `connector.go` **vendor-neutral**. Any provider whose connector
implements the capability becomes a record with no code of its own; the Claude
collector is bespoke only because Anthropic exposes usage over HTTP rather than
through its CLI. Adding a third vendor that speaks through its connector is one
line in `server.New`.

The probe dials its own app-server, asks, and hangs up — measured at 0.7–1.6s,
dominated by the spawn. It is bounded by `probeBudget` and belongs on the poll,
never on a request path. Verified live with no session created: the app-server
process count is unchanged across probes.

### Structural versus transient

The split that decides whether a record survives a failure, and it is not
"failed versus fine":

- **Structural** — `ErrNotSupported`, which is what an uninstalled or
  downgraded CLI looks like. The record is **forgotten entirely**. Keeping a
  meter alive for a provider that is gone is worse than dropping it, because it
  never stops being wrong.
- **Transient** — a dial that failed, a timeout, backend prose. The numbers were
  true a minute ago and are the best answer available, so they stay with a line
  saying why they are old.

Signed-out sits with the transient half deliberately: the operator can fix it,
the last numbers are still meaningful context, and the command that fixes it is
not guessable. It is also the only failure worth a row on a machine that has
never had numbers — everything else stays silent, because a CLI that is not
installed is a normal state rather than a row reporting its absence.

agentkit's contract already says an adapter with nothing to report leaves the
window out rather than emitting a 0% placeholder. `collectConnector` honours the
same rule on the way in, via `RateLimitWindow.Known()`.

### Collectors run in parallel

One dials a subprocess and one makes an HTTPS request. A vendor that hangs must
not hold up the others or take them down, so each is bounded on its own and a
panic is contained to its own goroutine. The provider id rides the result rather
than being read off the record — a collector that says "nothing at all" returns
a zero record, and deleting by its empty id would silently leave the stale one
in place.

## Invariants

- **The token reaches an `Authorization` header and nothing else.**
- **`limits[]` over buckets, and never iterate the buckets.**
- **Scale is decided per payload, not per value.**
- **Windows are named from `kind` or a duration, never from a model name.**
- **Unknown is not zero.** A negative percent is filtered everywhere.
- **A gauge never escalates and never counts down.**
- **A failed refresh never blanks anything.**
- **A transport failure is not an HTTP status.**
- **A cached percentage expires on its window, not on a clock.**
- **agentique never runs a provider CLI** — a missing fact is a gap in the
  provider library.
