// Package assistant is the durable principal under every surface that talks
// to it: the app's thread, a live voice call, and later a messaging gateway.
// See docs/assistant.md, which this package implements; the design decides
// what lives here and what stays in internal/voice, and the "Decided" and
// "The M1 contract" sections name everything.
//
// Two kinds of attachment, and the difference is who thinks. A HEAD brings its
// own model and calls the verbs directly — a voice call is a head, because
// speech has to be native. A TRANSPORT brings no model: it renders the shared
// conversation and forwards the operator's text to this package's own head, a
// Claude persona. The thread is a transport.
//
// Two heads on one body is safe on one condition, which this package is:
// **every rule that matters lives in the verbs, not in either prompt.**
// Refusals, target checks, rate limits and tier gates are enforced here, where
// both heads hit them. The prompts differ per surface on purpose — one tuned
// for speech must never read twelve sessions aloud, one tuned for a screen has
// no read-back to give.
//
// The assistant is an agent and is NOT a trusted principal. It reads untrusted
// text on every turn: reports, summaries, anything a session derived from a
// repository. Containment is the tier table ([Verb.Tier]), the journal that
// records the facts each action was judged on, and the fact that nothing
// uncontained is ever performed by the assistant: asking for one writes a
// [Proposal], and a person accepts it on a surface that shows the card or
// reads the target back ([Service.Decide]).
//
// Dependencies point one way. This package imports the store, the event bus,
// internal/usage and internal/memory — the last for value types only
// (memory.Category, memory.Source, memory.ConfidenceTier), because a closed set
// spelled twice is how one surface files a fact the other cannot read. It is
// handed everything else — the directory, the dispatcher, the head manager, the
// long-term [Memory] — through narrow interfaces the server implements, and it
// never imports internal/session, internal/brain or internal/voice: the brain's
// scope vocabulary is agentique policy and stays on the server's side of the
// seam.
package assistant
