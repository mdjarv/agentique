/**
 * The two marks that say a session is producing something right now.
 *
 * Both are the same idea at two radii: **a bright arc travelling a faint
 * track**. That shape was picked over every mark that animates in place — a
 * pulsing dot, a breathing ring, a scaling bar — for one reason. At 10px an
 * in-place mark has to signal through a single property at a single point, and
 * peripheral vision does not resolve that; it resolves *travel*. Giving the
 * mark a path 60px long is legibility that a 10px element cannot buy any other
 * way.
 *
 * **The track is not decoration.** It is what the mark looks like when nothing
 * is moving, and it earns its place three times over: a live row reads as "a
 * ring with a bright spot on it" rather than as an intermittent flicker; a
 * glance that lands on the arc's empty side still sees something; and
 * `prefers-reduced-motion` gets a resting state for free — the head simply
 * holds still on its track, so calming the animation never removes the state.
 *
 * **Colour is the caller's, and it is the project's.** Neither mark picks a
 * colour, because `docs`-level rule in CLAUDE.md holds: hue means *whose repo
 * this is*, and motion alone means live. A blue-for-running mark would be a
 * second colour system competing with the filing one.
 *
 * Both are `aria-hidden`: the row's own `aria-label` already speaks the state,
 * and a screen reader gains nothing from a decorative arc.
 */

/**
 * The comet that traces the 14px project chip.
 *
 * Sized to sit 2px outside the chip, so the arc rides just off the square
 * rather than on its edge. The rounded rectangle's ~76 units of perimeter is
 * what makes this work at all — and its four corners give the arc a rhythm a
 * circle does not have.
 *
 * Renders as a *sibling* of the chip square, never a child: the notch masks cut
 * their element's children away, and an arc nested inside would be sliced by
 * the very notch it passes.
 */
export function ChipComet() {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      className="chip-comet pointer-events-none absolute -inset-[2px] size-[18px]"
    >
      <rect
        className="live-arc live-arc-track"
        x="1.2"
        y="1.2"
        width="21.6"
        height="21.6"
        rx="6.2"
        strokeWidth={2}
      />
      <rect
        className="live-arc live-arc-head"
        x="1.2"
        y="1.2"
        width="21.6"
        height="21.6"
        rx="6.2"
        strokeWidth={2}
      />
    </svg>
  );
}

/**
 * The 10px orbit that stands in the time slot while a session runs.
 *
 * It replaces the age rather than crowding it, and only while running — which
 * costs nothing, because a running session's recency is "now". The clock keeps
 * the slot on every row where the number still answers something, and yields it
 * on the rows where it does not.
 */
export function OrbitArc() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="orbit-arc size-[10px]">
      <circle className="live-arc live-arc-track" cx="12" cy="12" r="10" strokeWidth={3} />
      <circle className="live-arc live-arc-head" cx="12" cy="12" r="10" strokeWidth={3} />
    </svg>
  );
}
