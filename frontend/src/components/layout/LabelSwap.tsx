import { cn } from "~/lib/utils";

/**
 * Two labels in the space of one, the second rolling up over the first.
 *
 * A rail row's idiom: at rest it says what the row is, and under the pointer
 * it says what the click will do. A tooltip would say the same thing later and
 * elsewhere; this says it in place, at the moment the pointer arrives. With
 * reduced motion it is still two labels and still swaps — it just does not
 * travel. The parent `group` drives it, and the button carries the spoken
 * name, so both halves are decoration to a screen reader.
 */
export function LabelSwap({ resting, hovered }: { resting: string; hovered: string }) {
  return (
    <span aria-hidden className="block h-4 min-w-0 overflow-hidden">
      <span
        className={cn(
          "flex flex-col transition-transform duration-[220ms] ease-out",
          "group-hover:-translate-y-4 motion-reduce:transition-none",
        )}
      >
        <span className="block h-4 truncate leading-4">{resting}</span>
        <span className="block h-4 truncate leading-4 text-success">{hovered}</span>
      </span>
    </span>
  );
}
