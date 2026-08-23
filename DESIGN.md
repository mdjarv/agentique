---
name: Agentique
description: A dark, dense control surface for supervising concurrent coding agents — lit glass over flat surfaces, where saturated color only ever means state.
colors:
  signal-blue: "#5e9eff"
  agent-violet: "#c990f0"
  live-teal: "#73daca"
  attention-amber: "#ff9e64"
  warning-gold: "#e0af68"
  alert-rose: "#f7768e"
  ready-green: "#9ece6a"
  info-cyan: "#7dcfff"
  void: "#0c0d14"
  panel: "#16161e"
  sidebar-slate: "#1e2030"
  raised-slate: "#24283b"
  popover-slate: "#1f2335"
  hairline: "#3b4261"
  ink: "#1a1b26"
  text-primary: "#a9b1d6"
  text-bright: "#c0caf5"
  text-dim: "#8891b5"
  text-muted: "#6b7394"
  text-faint: "#565f89"
typography:
  display:
    fontFamily: "Space Grotesk, Inter, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "-0.015em"
  headline:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.025em"
  title:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "normal"
  body:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: "normal"
  label:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.625rem"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "0.05em"
  mono:
    fontFamily: "JetBrains Mono, ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "0.8125rem"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "normal"
rounded:
  sm: "6px"
  md: "8px"
  lg: "10px"
  xl: "14px"
  full: "9999px"
spacing:
  hairline: "2px"
  xs: "4px"
  sm: "6px"
  md: "8px"
  lg: "12px"
  xl: "16px"
  section: "24px"
components:
  button-primary:
    backgroundColor: "{colors.signal-blue}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "0 16px"
    height: "36px"
    typography: "{typography.title}"
  button-primary-hover:
    backgroundColor: "#5e9effe6"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.md}"
    padding: "0 8px"
    height: "24px"
  button-ghost-hover:
    backgroundColor: "{colors.raised-slate}"
    textColor: "{colors.text-bright}"
  button-approve:
    backgroundColor: "{colors.ready-green}"
    textColor: "{colors.void}"
    rounded: "{rounded.md}"
    padding: "0 8px"
    height: "24px"
  input-field:
    backgroundColor: "#3b426133"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.md}"
    padding: "4px 12px"
    height: "36px"
  status-pill-running:
    backgroundColor: "#73daca26"
    textColor: "{colors.live-teal}"
    rounded: "{rounded.full}"
    padding: "2px 8px"
    typography: "{typography.label}"
  status-pill-approval:
    backgroundColor: "#ff9e6426"
    textColor: "{colors.attention-amber}"
    rounded: "{rounded.full}"
    padding: "2px 8px"
    typography: "{typography.label}"
  status-pill-idle:
    backgroundColor: "#9ece6a26"
    textColor: "{colors.ready-green}"
    rounded: "{rounded.full}"
    padding: "2px 8px"
    typography: "{typography.label}"
  card-panel:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.lg}"
    padding: "16px"
  session-row-active:
    backgroundColor: "#292e42"
    textColor: "{colors.text-primary}"
    rounded: "0 8px 8px 0"
    padding: "6px 8px"
  approval-banner:
    backgroundColor: "#e0af681a"
    textColor: "{colors.warning-gold}"
    rounded: "{rounded.md}"
    padding: "10px 12px"
---

# Design System: Agentique

## Overview

**Creative North Star: "The Aurora Workshop"**

Agentique is a near-black workshop lit from behind by colored light. The chrome — sidebar, headers, rows, panels — is a flat blue-black field with a fixed film of grain over it; the life comes from what the agents are doing to it. A project's color tints the frosted glass behind its chat. A running session glows teal. A session that needs a human turns amber and pulses. Todo bars throw sparks when a task lands, and the brain flares when it learns something. The atmosphere is alive because the agents are; nothing decorative moves on its own.

The voice is a **warm terminal**, not a dashboard. JetBrains Mono carries every path, command, diff, and tool name. Inter carries prose at 14px and drops to 10px for the structural labels that organize a dense sidebar. Surfaces are tinted-transparent rather than solid, controls are 24–36px tall, and numbers are tabular so a value changing in place doesn't shift the row. Craft here is meant to be noticed up close — the grain, the analogous frost blobs, the 2px project rail on a session row — never from across the room.

Two rejections are binding. This is **not a pastel SaaS dashboard**: no light-first surfaces, no generous whitespace, no muted brand tints or illustration spots — the operator watches many sessions at once and density is the product. And it is **not AI-startup marketing chrome**: the one gradient in the entire system is the wordmark, glow is a state signal rather than an aesthetic, and there are no mesh backgrounds, hero treatments, or purple-to-pink flourishes.

**Key Characteristics:**
- Deep blue-black (`#0c0d14`) ground with a fixed grain + gradient film at 7% opacity
- Saturated color reserved for state and project identity; chrome is blue-grey
- Status color always appears as a ~15% tint behind full-strength text — never a solid fill
- Two live themes: Tokyo Night (dark, default) and Catppuccin Latte (light)
- Compact controls (24 / 32 / 36px), 10px uppercase section labels, tabular numerals everywhere
- Depth from blur, grain, and colored light — not from shadows (in dark mode)
- Every project owns a hue; it enters the UI as a 2px rail and a text pill, never as a flooded surface

## Colors

A Tokyo Night–derived palette: one cool blue-black family for every surface, and eight saturated hues that are spent only on meaning.

### Primary
- **Signal Blue** (`#5e9eff`): the product's one action color. Primary buttons, focus rings, links, active nav, selection, the merging state, and the left half of the wordmark gradient. Nothing decorative uses it.

### Secondary
- **Agent Violet** (`#c990f0`): agent identity — the default frost base behind a chat, subagent and persona accents, and the right half of the wordmark gradient. It marks "a machine is speaking here."

### Tertiary
- **Live Teal** (`#73daca`): work in progress. The running and planning states, and the calm end of the project palette.
- **Attention Amber** (`#ff9e64`): a human is blocking the agent — pending tool approval, an open question, a plan awaiting review. The only color allowed to pulse.
- **Warning Gold** (`#e0af68`): the approval banner surface, unread channel messages, and sessions carrying attention in the sidebar.
- **Alert Rose** (`#f7768e`): failure and destruction — failed sessions, removed diff lines, deny actions, destructive confirms.
- **Ready Green** (`#9ece6a`): a session at rest or finished — idle, done, unseen completion, added diff lines, the Allow button.
- **Info Cyan** (`#7dcfff`): neutral informational callouts. The quietest of the signal hues; use it when something is worth noticing but nothing is required.

### Neutral
- **Void** (`#0c0d14`): the app ground. Deeper than stock Tokyo Night so lit panels read as lifted without shadows.
- **Panel** (`#16161e`): cards, code surfaces, and the chat ground.
- **Sidebar Slate** (`#1e2030`): the sidebar rail (at 80% with `backdrop-blur-md`) and page headers (solid, hairline-bottomed).
- **Raised Slate** (`#24283b`): the one step up — secondary buttons, muted fills, inline code, hover surfaces.
- **Popover Slate** (`#1f2335`): floating layers — menus, popovers, dialogs, tooltips.
- **Hairline** (`#3b4261`): every border, divider, input stroke, and scrollbar thumb.
- **Ink** (`#1a1b26`): text placed on a full-strength saturated fill.
- **Text Bright** (`#c0caf5`): headings, emphasis, and the one line in a row that must win.
- **Text Primary** (`#a9b1d6`): body and default UI text.
- **Text Dim** (`#8891b5`) → **Text Muted** (`#6b7394`) → **Text Faint** (`#565f89`): the three-step recession used for secondary metadata, section labels, and timestamps.

### Light Theme
The light theme is Catppuccin Latte, not a tint-inverted copy: base `#eff1f5`, text `#4c4f69`, primary `#1e66f5`, and jewel-tone versions of every signal hue (deeper and more saturated, since dark-mode neon dies on a pale ground). Project colors resolve through a separate `fgLight` ramp for the same reason. Both themes are real and maintained; the app defaults to dark and remembers the choice.

### Named Rules
**The Meaning-Only Color Rule.** Saturated hue is reserved for two things: session state and project identity. Chrome, structure, and typography are blue-grey. If a color can't answer "what does this tell the operator," it doesn't ship.

**The 15% Tint Rule.** Status color reaches the screen as `color/15` (or `/20`) behind full-strength text and icon of the same hue. Solid saturated fills are reserved for exactly two roles: the primary action button and the Allow button. This is what lets a dozen colored states coexist in one sidebar without shouting.

**The Amber Monopoly.** Amber and its pulse mean one thing — an agent is blocked on a human. Never use amber for warnings, tips, or emphasis; spending it elsewhere costs the operator the one signal they actually have to answer.

## Typography

**Display Font:** Space Grotesk (with Inter, sans-serif)
**Body Font:** Inter (with ui-sans-serif, system-ui)
**Label/Mono Font:** JetBrains Mono (with ui-monospace, SFMono-Regular, Menlo)

**Character:** Inter does the invisible work — dense, neutral, legible at 10px. JetBrains Mono is the product's real voice: every path, command, tool name, diff, log line, and composer placeholder is monospace, so the interface reads as adjacent to the terminal rather than a layer above it. Space Grotesk appears twice in the entire app and is effectively the logotype.

### Hierarchy
- **Display** (600, 1.125rem, tracking -0.015em): the "Agentique" wordmark and the login screen only. Rendered as a `signal-blue → agent-violet` gradient clipped to the text.
- **Headline** (600, 1.5rem, tracking -0.025em): page titles ("Sessions") and the big tabular numbers in stat tiles.
- **Title** (600, 0.875rem): session names, panel headings, card titles. Bumps to `text-bright` + 600 when a session has an unseen completion.
- **Body** (400, 0.875rem, 1.55): chat prose, descriptions, form text. Markdown bodies use the same size with restored paragraph rhythm (1em top/bottom).
- **Label** (600, 0.625rem, uppercase, tracking 0.05em): sidebar section headers, table headers, and metadata rows, in `text-muted` or `text-faint`.
- **Mono** (400, 0.8125rem, 1.6): code blocks, tool arguments, file paths, git branches, diffs, and the composer placeholder. Inline code drops to 0.8em on a `raised-slate` chip.

### Named Rules
**The Space Grotesk Is The Logo Rule.** The display face appears in the wordmark and the login screen. Nowhere else. A new heading is Inter.

**The Tabular Numerals Rule.** Any number that updates in place — session counts, elapsed time, todo ratios, diff stats, token bars — is `tabular-nums`. Rows must never twitch as values change.

**The 10px Label Rule.** Structural labels are 10px/600/uppercase/tracking-wider in a dimmed neutral. They organize the densest surfaces without ever competing with content; making them larger or brighter is how this UI starts to look like a dashboard.

## Layout

A three-zone shell: a fixed 288px (`w-72`) sidebar rail, a 48px page header, and one scrolling work surface. `html`, `body`, and `#root` are all `overflow: hidden` at `100dvh` — the page itself never scrolls; individual panes do. Optional right panels (todos, changes, browser) also come in at 288px.

Density is deliberate: session rows are `6px 8px` with a 1.5-unit gap, section labels sit at `4px 8px`, and controls stand at 24 / 32 / 36px. Content columns are capped for reading (chat bubbles at 75% of the pane, prose at typographic measure) while lists run full-bleed to the rail.

Spacing follows a 4px base with 2px available for hairline offsets; the working set is 4 / 6 / 8 / 12 / 16 / 24. Gaps of 1.5 units (6px) dominate inside rows, 12–16px between blocks, 24px between page sections.

**Responsive:** one breakpoint carries the product — `md` (768px) — and it is used as `max-md:` overrides, which is to say the desktop layout is the base and the phone layout is the deliberate exception. Below it, the sidebar becomes a sheet behind a hamburger, chat bubbles go full width, avatars shrink 32→24px, row padding grows (`py-1.5` → `py-2.5`), and every action button in a decision surface grows to 40px (`max-md:h-10`). `viewport-fit=cover` plus `interactive-widget=resizes-content` keep the composer above the keyboard.

**The Two Screens Rule.** Desk and phone are equally weighted (PRODUCT.md). A surface that is only good on a wide screen is unfinished — and the phone version is not a smaller copy: it trades hover affordances for permanently visible ones (`@media (hover: none)` reveals the code-block actions at 70% opacity rather than on hover).

## Elevation & Depth

Depth is **light, not shadow**. Four layers do the work: a fixed grain + vertical gradient film over the whole app (`#root::before`, SVG fractal noise at 7% opacity); tonal steps between surfaces (`void` → `panel` → `sidebar-slate` → `raised-slate` → `popover-slate`); `backdrop-blur` on the sidebar and floating panels rendered at 80–85% opacity; and colored radial blobs blurred 50px behind the chat pane, derived from the project color with ±25° analogous hue rotation. Shadows exist in dark mode but are nearly invisible against `#0c0d14` and carry no meaning.

Light mode inverts that logic, because glass and glow do not read on a pale ground: the sidebar gets a real two-stop shadow, and a selected row gets a lifted-card shadow plus a 1px ring.

### Shadow Vocabulary
- **Sidebar lift, light only** (`box-shadow: 2px 0 12px hsl(228 20% 50% / 0.12), 4px 0 24px hsl(228 20% 50% / 0.06)`): separates the rail from the work surface when there is no glow to do it.
- **Selected row, light only** (`box-shadow: 0 1px 3px hsl(228 20% 50% / 0.08), 0 0 0 1px hsl(228 15% 85% / 0.5)`): the light-mode equivalent of a brighter fill.
- **Chat bubble** (`shadow-lg shadow-black/30`, dashed + `shadow-md/10` while pending): the one place a dark-mode shadow is intentional, separating the user's words from the frost behind them.

### Named Rules
**The Glass Over Grain Rule.** Panels sit on grain and are lit from behind; they are not lifted off the page by shadow. Reach for `backdrop-blur` + a tint before reaching for elevation.

**The Frost Is Identity Rule.** The colored blobs behind a chat are derived from the *project's* color (`--frost-base`, with warm/cool analogues at ±25° hue and compensated saturation). They are ambience with a job: knowing which project you're in without reading a label.

## Shapes

Corners are gentle and consistent: 6px on small controls and chips, 8px on buttons, inputs, rows, and tool blocks, 10px on cards and panels, 14px on the largest containers, and fully round on every status pill, badge, avatar, and progress track. Nothing in the system is square-cornered by choice.

Borders are single-hairline `#3b4261`, frequently softened to 50–60% with `color-mix` so a dense grid of blocks doesn't turn into a cage. Dashes carry one meaning: *provisional* — a pending message bubble, an unconsolidated brain memory.

**The Rail Rule.** When a row carries a project accent, it becomes a 2px left border with `rounded-r-md` — square on the rail side so the color reads as a rail rather than a stray edge. This shape is the sidebar's primary identity device.

## Components

### Buttons
- **Shape:** gently curved (8px, `rounded-md`); pill only for badge-like toggles.
- **Sizes:** `xs` 24px / `sm` 32px / `default` 36px / `lg` 40px, with matching icon-only squares. Icons are 16px (12px at `xs`) and never optical-center by accident — every variant is `inline-flex` centered with a 8px gap.
- **Primary:** solid Signal Blue on Ink, hover at 90% opacity.
- **Ghost / Outline / Secondary:** the working defaults. Ghost has no resting surface and takes `raised-slate` on hover; outline adds a hairline and an `input/30` fill in dark mode.
- **Focus:** a 3px `signal-blue/50` ring plus a border shift — never an outline removal. Disabled is 50% opacity with pointer events off.
- **Destructive:** Alert Rose text on a `destructive/10` hover in banners; solid only in confirm dialogs.

### Status Pills & Badges
The signature component. One config maps twelve session states (`idle`, `running`, `done`, `stopped`, `failed`, `merging`, `approval`, `question`, `plan`, `planning`, `unseen`, `channel_msg`) to a tint + text color + icon + label, and every surface — sidebar dot, row badge, header pill, hover card — renders from that single source.
- **Style:** `color/15` fill, full-strength text, `rounded-full`, `2px 8px`, 10–12px label, icon 8–12px.
- **Motion is state, not decoration:** `running` and `merging` spin their loader; `planning` breathes; `approval` / `question` / `plan` carry a pulsing `ring-current/30`. Everything else is static.
- **Dimming carries meaning:** a disconnected session drops to 40% opacity — unless it has a pending approval, which always stays at full strength.
- **Compact mode** drops the label and keeps the dot, for the sidebar and mobile headers.

### Session Row
- **Structure:** status badge, then a truncating title, then an optional 10px meta line (project pill · agent name · elapsed time, right-aligned), then an optional 4px todo bar with a `n/m` counter.
- **Accent:** the project color as a 2px left rail with `rounded-r-md`; active adds `sidebar-accent`, inactive-but-accented sits at 30%.
- **Title states:** untitled → italic muted; draft → italic; finished/merged → struck through and muted; needs attention → Warning Gold; unseen completion → semibold Text Bright.
- **Live sub-line:** while running, the row narrates itself in 10px faint text ("editing ws-client.ts · 3 commits · 12 tool calls").

### Inputs & Composer
- **Style:** transparent (dark: `input/30`) with a hairline border, 8px radius, 36px tall, 14px text that stays 16px below `md` so iOS doesn't zoom.
- **Focus:** border shifts to Signal Blue and a 3px `ring/50` appears; selection is `primary` on `primary-foreground`.
- **Invalid:** `aria-invalid` drives a rose border and a rose ring — never a color-only cue without the attribute.
- **Composer:** monospace placeholder, an attachment/tool strip beneath, and a right-side send control; on mobile the rarely-changed controls collapse behind a `+` tray.

### Cards & Stat Tiles
- **Corner style:** 10px. **Background:** `panel`, or `card/85` with backdrop blur when floating. **Border:** hairline, often at 50–60%. **Padding:** 16px, dropping to 8–12px in dense grids.
- **Stat tiles** pair a 10px uppercase label with a 1.5rem tabular number, colored only when the number means attention (Attention Amber for "needs attention", Signal Blue for "running").

### Navigation
- **Sidebar:** `sidebar/80` with `backdrop-blur-md`, a 48px brand header (wordmark, primary New-session launcher, More menu), a search bar, then a flat session list: Pinned (drag-orderable) / Open (attention-first) / Archived (collapsed count). Rows are icon-anchored: a 26px project-color icon with an 11px corner state dot, name + tabular time, and a mono machine line. Selection is a raised surface plus a brighter name — never an accent bar.
- **Row state line:** an awake row's third line is `glyph + specifics` — the glyph names the state (terminal = working, diamond = planning, triangle = approval, `?` = open question, check = unread, git-merge = merging, `×` = failed, dashed square = draft), so the words never repeat it and carry only what the glyph can't say: the file, the command, the question. One marker per row: the amber pulse lives *on* the blocked glyph, never beside it as a second dot. A resting row draws no glyph — absence is the rest state.
- **New-session palette:** the launcher's popover is also the product's only per-project surface, so its rows carry the project affordances the folder sidebar used to own — remote-sync pills (`↑N` push / `↓N` pull), settings, favorite — beside the project pill. It is a collision-aware popover, not a hand-placed panel: it is wider than the space to its left inside the 288px rail.
- **A broken machine is not an away machine.** Away is unprovable and ordinary, so it stays grey and silent. A *proven* fault — the address answers as a different machine, refuses this device's credential, or isn't agentique at all — never resolves itself, so it takes Alert Rose on the machine tag wherever that tag already appears, carries its sentence in the tooltip, and gets the full sentence plus a Re-pair action in Settings › Machines. No toast: a fault that will still be true tomorrow doesn't need to interrupt today.
- **An away machine is a state, not a failure.** A laptop suspends daily; agentique treats that as ordinary. Background sync never raises a toast (its dominant failure is a machine that is simply asleep), a remote machine's projects are driven only by that machine's own socket, and everything it owns stays visible from cache — readable, navigable, dimmed. What it cannot do is *pretend*: its rows lose their action buttons, its option in the run-on picker greys out with "offline", its sessions' composer says which machine is away instead of spinning on a send, and bulk actions leave it out. The only place that spells it out in words is Settings › Machines ("last seen 3h ago"). Nothing about an away machine blocks work on a machine that is here.
- **Sync dock:** the rail's last band, under the session list and above the footer — sessions, then settled work, then repos, then system, so its growth pushes into the footer and never into the session list. One line at rest (amber dot, the overlapped project chips of the drifted repos, then the action count); expanded, one row per drifted *checkout*, so a repo out of sync on two machines is two rows but one chip — the line answers "which repos", the list answers "what to do". Expansion is a remembered preference. The dock runs only the two mechanical operations (push, fast-forward pull) and hands a diverged checkout to a session with a rebase prompt; it never runs anything that can conflict. Uncommitted work is not a sync problem and never docks.
- **Freshness is stated, not implied:** ahead/behind is only as true as the last `git fetch`, so the dock carries its age, says "sync unknown" rather than showing a stale count, and fetches when you expand it — asking for the list is asking for the truth. Status (local, cheap) and fetch (remote, expensive) run on separate beats; never restore a poll-everything-every-few-seconds loop.
- **Remote-sync pills:** `↑N` in Ready Green pushes on click; `↓N` in Signal Blue fast-forwards, or turns Attention Amber and opens a local session with a rebase prompt when the pull is non-FF. Same component wherever a project is identifiable — palette row, session header, landing deck — so the gesture never changes meaning.
- **Section labels:** chevron, optional icon, faint count, then the 10px uppercase label — count *before* label, so the numbers form a scannable column.
- **Row actions** appear on `group-hover` at full opacity and are permanently visible on touch.

### Tool-Use Block
- **Collapsed:** one hairline-bordered `muted/50` row at 12px — icon, tool name in Inter medium, target path in mono faint, a one-line summary, and a success check in Ready Green at 70%.
- **Expanded:** a bordered body with `max-h-64` scroll, diff lines tinted `success/15` and `destructive/15` at 70% text, raw output in 11px mono.
- **Principle:** a turn with forty tool calls must still read as a paragraph of prose with quiet machinery under it.

### Approval Banner
The one surface allowed to interrupt. A `warning/10` field with a `warning/40` border, 8px radius, docked above the composer with a 16px inset; a shield icon, the tool name as a mono chip on `warning/15`, the command in mono, and three actions — ghost Deny in Alert Rose, solid Ready Green Allow, outline "Allow all". On mobile it restacks vertically and every button grows to 40px.

### Todo Bar
A 4px `muted/50` track with a Signal Blue fill (Ready Green when complete), a flickering 2px spark at the leading edge, a sweep flash on completion, and particle burst on the final task. Entirely disabled under `prefers-reduced-motion`.

## Do's and Don'ts

### Do:
- **Do** render every session state through the shared badge config — one state, one tint, one icon, one label, everywhere it appears.
- **Do** use `color/15` tints behind full-strength text for status, and reserve solid saturated fills for the primary action and Allow.
- **Do** keep saturated hue tied to state or project identity; structure stays blue-grey.
- **Do** set `tabular-nums` on every number that updates in place.
- **Do** use JetBrains Mono for anything the machine produced — paths, commands, branches, diffs, tool names, IDs.
- **Do** build depth from `backdrop-blur`, tonal steps, and the grain film first; add a shadow only where light mode needs it.
- **Do** grow decision controls to 40px under `max-md:` and reveal hover-only affordances under `@media (hover: none)`.
- **Do** gate every ambient animation behind `prefers-reduced-motion: reduce` — the spark, the flash, the particles, the shimmer, the mic pulse, and the brain flare all already are.
- **Do** honor both themes when adding a color: dark gets the neon value, light gets the jewel-tone one at lower lightness and higher saturation.

### Don't:
- **Don't** spend Attention Amber or its pulse on anything but "an agent is blocked on a human."
- **Don't** introduce a fourth typeface, or use Space Grotesk anywhere except the wordmark and login.
- **Don't** enlarge or brighten the 10px section labels to make them "readable" — their recession is what makes the sidebar scannable.
- **Don't** add a decorative gradient. The wordmark is the system's only one.
- **Don't** use a solid saturated background for an informational surface; it reads as an action.
- **Don't** rely on hover to reveal anything essential — the same product runs on a phone.
- **Don't** animate anything that isn't reporting a state change.
- **Don't** add whitespace to "let the design breathe" in list, row, or tree surfaces; density is the product, and the operator is watching many sessions at once.
- **Don't** load a font, icon, or asset from a CDN — the app ships as one self-contained binary (PRODUCT.md).
