---
id: schema-full-0001
scope: project:demo
category: project
source: agent
pinned: true
locked: true
uses: 5
helped: 2
corroborations: 4
created: 2026-05-01T09:00:00Z
updated: 2026-05-01T10:00:00Z
last_used: 2026-05-01T11:00:00Z
derived_from:
    - cap-1
    - cap-2
related:
    - rel-1
    - rel-2
evidence: code_verified
volatility: ephemeral
lifecycle: superseded
relations:
    - type: supersedes
      target: rel-x
    - type: duplicates
      target: rel-y
keywords:
    - kw-one
    - kw-two
last_curated: 2026-05-01T12:00:00Z
curator_note: curated during the schema test
community: 7
area: tooling
confidence: inferred
confidence_score: 0.8
review_note: flagged for review during schema test
subsumed:
    - scope: project:other
      text: merged source fact
---

Every frontmatter field is populated for the schema round-trip.
