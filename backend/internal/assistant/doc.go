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
// uncontained is ever performed — asking for one answers
// [ProposalRequiredError] until M3 gives it a card and a yes.
//
// Dependencies point one way. This package imports the store, the event bus and
// internal/usage, and is handed everything else — the directory, the
// dispatcher, the head manager — through narrow interfaces the server
// implements. It never imports internal/session or internal/voice.
package assistant
