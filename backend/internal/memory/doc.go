// Package memory is an application- and provider-agnostic store of durable facts
// an agent accumulates across sessions — "the brain".
//
// Design goal — liftability. This package depends only on the standard library
// and small, widely-shared primitives (github.com/google/uuid, gopkg.in/yaml.v3).
// It must not import anything from the host application, so that once its
// contracts stabilize it can be lifted verbatim into a shared library (agentkit)
// while the host keeps only the glue: a Store backend, prompt injection, MCP
// tools, and the turn hooks that drive capture and consolidation.
//
// Two tiers, mirroring how memory consolidates over time:
//
//   - episodic captures (Source "capture"): cheap, raw material staged at the end
//     of a turn. Not injected into prompts directly.
//   - durable facts (Source "agent"/"human"/"consolidated"): high-signal records
//     produced by an explicit remember action or by a consolidation pass that
//     merges, abstracts and decays captures. These are what Recall injects.
//
// Text is always the source of truth; embeddings are a derived, rebuildable cache.
package memory
