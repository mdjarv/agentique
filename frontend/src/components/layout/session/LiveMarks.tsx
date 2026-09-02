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
 *
 * It is also the half of the mark that **never yields**. The row's top-right
 * corner belongs to {@link RowActions} on hover and for as long as the row is
 * the focused one, so the orbit standing in the time slot goes with the clock —
 * which is precisely why the mark was drawn at two radii. The comet is at the
 * one x every row shape shares, and it is on the row you are inside, so it has
 * to carry the state alone; drawn in inherited ink it read as chrome, which is
 * how a running session came to look still exactly where it was being watched.
 */
export function ChipComet({ color }: { color?: string }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      className="chip-comet pointer-events-none absolute -inset-[2px] size-[18px]"
      style={color ? { color } : undefined}
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
 *
 * This is the half that *can* be given up: the corner is the row's actions' as
 * soon as the row is hovered or focused, and {@link ChipComet} is still saying
 * the same thing one column over.
 *
 * Its stroke is the thicker of the two in viewBox units and that is not a
 * disagreement with the comet — it is what makes them agree. The two marks
 * render at different sizes from the same 24-unit box, so a shared number is
 * two different widths on screen; matching what the reader actually sees (4 of
 * 24 at 10px, 2 of 24 at 18px — about 1.6px each) is what stops the smaller one
 * washing out to grey while the larger one reads as a second border.
 *
 * The radius comes in to 9.5 to pay for the stroke: at r=10 the outer edge
 * lands exactly on the viewBox wall, and SVG clips there, flattening the arc at
 * the four cardinal points. The dash figures below move with the radius.
 */
export function OrbitArc() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="orbit-arc size-[10px]">
      <circle className="live-arc live-arc-track" cx="12" cy="12" r="9.5" strokeWidth={4} />
      <circle className="live-arc live-arc-head" cx="12" cy="12" r="9.5" strokeWidth={4} />
    </svg>
  );
}
