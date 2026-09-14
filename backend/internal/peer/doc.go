// Package peer is the owner's half of acting across machines (docs/peers.md):
// the /api/peer/* routes a paired server calls with a peer credential, and the
// guard that decides what may happen to this machine's sessions whatever it is
// asked.
//
// The line it holds is "one assistant decides what to ask for; the machine that
// owns a session decides what may happen to it". So nothing in here trusts the
// request for who is acting: every create and send is assistant-origin because
// the credential is a peer's, and a policy id in the body is recorded, never
// read as permission. The guard is pure ([JudgeSend], [JudgeCreate]) so its
// rules are tested without a server; the handler only gathers facts and
// applies the verdict.
//
// It serves this machine's own sessions and nothing else. It never dials
// another machine — the acting side is a different package.
package peer
