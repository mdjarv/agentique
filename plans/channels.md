# Plan: Channels — First-Class Cross-Agent Communication

> Source PRD: [mdjarv/agentique#6](https://github.com/mdjarv/agentique/issues/6)

## Architectural decisions

Durable decisions that apply across all phases:

- **Route**: `/project/$projectSlug/channel/$channelId` — new TanStack Router route for the dedicated channel view, parallel to the existing `/project/$projectSlug/session/$sessionShortId` route.
- **Schema (Phase 1)**: No schema changes. Reuse existing `teams` table and `sessions.team_id` column. Add `fromUser` flag to agent message events for human broadcasts.
- **Schema (Phase 2)**: Rename `teams` → `channels`, `sessions.team_id` → `sessions.channel_id`, `sessions.team_role` → `sessions.channel_role`. Then replace `sessions.channel_id` FK with a `channel_memberships` join table for M:N.
- **Channel view**: A standalone main panel component (`ChannelPanel`) — not a tab inside `ChatPanel`. Accessed exclusively via the channel route.
- **Human messages**: Broadcast to all channel members. Backend persists as `agent_message` events with `fromUser: true`. No target picker — channel composer always broadcasts.
- **Sidebar grouping**: Sessions with a `teamId`/`channelId` are extracted from the flat session list and rendered under a collapsible channel header. They do NOT appear in both places.
- **Naming**: UI uses "channel" from Phase 1. Backend/DB rename from "team" to "channel" happens in Phase 2a as a dedicated refactor.

## Parallelism

Phases within a milestone can be worked in parallel where noted. Cross-milestone phases are sequential.

```
Milestone 1 (Phase 1):
  1a (sidebar grouping) ──┐
                          ├── 1d (lifecycle + remove Team tab)
  1b (channel view)  ─────┤
  1c (human broadcast) ───┘

Milestone 2 (Phase 2):
  2a (rename) → 2b (independent creation + drag) → 2c (M:N)
```

1a and 1b can be built in parallel (sidebar vs main panel). 1c depends on 1b (needs the channel view composer). 1d depends on all three (removes the old Team tab, wires lifecycle into the channel view).

---

## Phase 1a: Sidebar channel grouping

**User stories**: 1, 5, 7

### What to build

Sessions that belong to a team are pulled out of the flat session list and rendered under a collapsible channel header in the sidebar. The header shows the channel name, member count, and aggregate status indicators (pending approval count, running count, idle count — same pattern as `ActiveSessionIndicators` on collapsed projects). Clicking the channel header navigates to the channel route. Clicking an individual session under it opens that session's chat as usual. Sessions under a channel do NOT also appear in the flat list.

The channel group sits among the active sessions (above the "Completed" section). If all channel members are completed, the channel group moves into the completed section.

### Acceptance criteria

- [ ] Sessions with a `teamId` are grouped under a collapsible channel header with nested indentation
- [ ] Sessions with a `teamId` do not appear in the flat session list
- [ ] Channel header shows: name, member count, aggregate status indicators when collapsed
- [ ] Expanding the channel header shows member sessions as indented rows
- [ ] Clicking the channel header navigates to `/project/$projectSlug/channel/$channelId`
- [ ] Clicking a session under the channel navigates to that session's chat view
- [ ] Multiple teams in the same project each get their own channel group

---

## Phase 1b: Dedicated channel view (read-only)

**User stories**: 2, 3, 6

### What to build

A new TanStack Router route (`/project/$projectSlug/channel/$channelId`) that renders a `ChannelPanel` component in the main panel area. The channel view contains:

1. **Member list** — each member shows `SessionStatusBadge` (with pending approval, planning, git operation awareness), session name, and role. Clicking a member navigates to their session.
2. **Chat timeline** — renders existing `TimelineEvent` data using the same message styling as the current TeamView (agent colors, avatars, session icons, markdown, shadows/gradients). Includes user messages (`fromUser`).
3. **Empty state** — when no messages exist yet.

This phase is read-only: no composer, no lifecycle actions. The channel view subscribes to the team store for live updates (new messages, member state changes).

### Acceptance criteria

- [ ] Route `/project/$projectSlug/channel/$channelId` resolves and renders the channel panel
- [ ] Member list shows all channel members with proper `SessionStatusBadge` indicators
- [ ] Clicking a member in the member list navigates to that session's chat
- [ ] Chat timeline renders existing messages with proper styling (agent colors, avatars, markdown)
- [ ] Timeline updates live when new messages arrive via WebSocket
- [ ] Empty state shown when no messages exist
- [ ] Browser back/forward navigation works correctly between channel and session views

---

## Phase 1c: Human broadcast messages

**User stories**: 4

### What to build

Add a message composer to the channel view that lets the human broadcast a message to all channel members. The composer has no target picker — messages always go to everyone.

**Backend**: New endpoint/RPC that accepts a channel ID and message content, then delivers the message to every session in the channel. The message is persisted as an `agent_message` event with `fromUser: true` on each recipient session. The timeline query returns these alongside agent-to-agent messages.

**Frontend**: Composer at the bottom of the channel view. Human messages appear right-aligned with the user avatar (matching the existing `fromUser` rendering in TeamView). The composer clears after sending.

### Acceptance criteria

- [ ] Channel view has a message composer (text input + send button)
- [ ] Sending a message delivers it to all channel members
- [ ] Human messages appear in the timeline with user avatar, right-aligned
- [ ] All channel members' CLI sessions receive the message
- [ ] Messages persist and appear when reloading the channel view
- [ ] Composer clears after successful send

---

## Phase 1d: Channel lifecycle + remove Team tab

**User stories**: 8, 9, 10, 11

### What to build

Move team management actions into the channel view and remove the per-session Team tab.

**Channel view actions** (in the channel view header area):
- **Dissolve**: Stop all workers, remove worktrees, delete the channel. Channel disappears from sidebar. Lead session returns to flat list.
- **Keep channel**: Stop all workers, remove worktrees, but preserve the channel entity and chat history. Channel stays in sidebar (grayed out or with a "completed" indicator). Lead session remains in the channel.

**Remove Team tab**: The tab bar in `ChatPanel` no longer shows "Team". Sessions that belong to a channel have no team-related UI in their own view — all channel interaction happens through the channel route.

**Unread indicator migration**: The existing `hasUnreadTeamMessage` indicator (orange Users icon on session rows) moves to the channel header in the sidebar instead.

### Acceptance criteria

- [ ] Channel view header has "Dissolve" and "Keep" actions
- [ ] "Dissolve" stops workers, cleans up worktrees, removes channel from sidebar
- [ ] "Keep" stops workers, cleans up worktrees, preserves channel with chat history
- [ ] Kept channels show a visual indicator that workers are stopped (grayed out, completed badge, etc.)
- [ ] The "Team" tab is removed from `ChatPanel` — no team-related tabs on session views
- [ ] Unread message indicator appears on the channel header in the sidebar, not on individual session rows
- [ ] No regressions in session chat functionality after Team tab removal

---

## Phase 2a: Rename team → channel (backend + frontend)

**User stories**: 16

### What to build

Pure rename refactor — no behavior changes. This is a dedicated effort to align the codebase with the "channel" terminology before building new features on top.

**Database migration**: Rename `teams` → `channels`, `sessions.team_id` → `sessions.channel_id`, `sessions.team_role` → `sessions.channel_role`. Update all indexes.

**Backend**: Rename Go structs (`TeamInfo` → `ChannelInfo`, `TeamMember` → `ChannelMember`, etc.), handler functions, RPC event types (`team.create` → `channel.create`, etc.), and sqlc queries.

**Frontend**: Rename stores (`team-store` → `channel-store`), actions (`team-actions` → `channel-actions`), types (`TeamInfo` → `ChannelInfo`), and all references. Update WebSocket event handlers.

### Acceptance criteria

- [ ] Database tables and columns use "channel" naming
- [ ] All Go structs, handlers, and RPC types use "channel" naming
- [ ] All frontend stores, actions, and types use "channel" naming
- [ ] All WebSocket event types use "channel.*" prefix
- [ ] Existing functionality works identically after rename
- [ ] No references to "team" remain in code (except git history)

---

## Phase 2b: Independent channel creation + drag-to-join

**User stories**: 12, 13, 14

### What to build

Channels become independently creatable — not just a side effect of spawning workers.

**Create empty channels**: A "New channel" action in the sidebar (under a project) that creates a channel with just a name. No members initially.

**Add sessions to channels**: Drag a session row onto a channel header to add it to the channel. This wires communication callbacks on the session and injects channel context (peer list, worktree paths). Also available via a context menu on the session row ("Add to channel → [list]").

**Remove sessions from channels**: Context menu on a session within a channel group ("Remove from channel"). This unwires callbacks but does NOT stop the session — it just leaves the channel.

Spawning workers continues to auto-create a channel as a convenience.

### Acceptance criteria

- [ ] "New channel" action in sidebar creates an empty channel under a project
- [ ] Dragging a session onto a channel header adds it to the channel
- [ ] Context menu on session rows offers "Add to channel" with channel list
- [ ] Removing a session from a channel unwires callbacks but does not stop the session
- [ ] Session returns to the flat list after removal
- [ ] Spawning workers still auto-creates a channel and joins members
- [ ] Channel context (peer list) is injected when a session joins

---

## Phase 2c: M:N channel membership

**User stories**: 15

### What to build

Replace the 1:1 `sessions.channel_id` foreign key with a `channel_memberships` join table, allowing a session to participate in multiple channels simultaneously.

**Schema**: New `channel_memberships` table with `(channel_id, session_id, role, joined_at)` composite key. Drop `sessions.channel_id` and `sessions.channel_role` columns. Migrate existing memberships to the join table.

**Backend**: Update all membership queries to use the join table. Message routing validates per-channel membership. Channel context injection lists all channels a session belongs to.

**Frontend**: A session can appear under multiple channel groups in the sidebar. The session's system prompt preamble includes context for all channels.

### Acceptance criteria

- [ ] A session can join multiple channels simultaneously
- [ ] Session appears under each channel group it belongs to in the sidebar
- [ ] Messages in channel A are not visible in channel B
- [ ] Removing a session from one channel does not affect its other channel memberships
- [ ] Existing single-channel memberships are preserved through migration
- [ ] Channel context preamble includes all channels the session belongs to
