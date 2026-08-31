/**
 * Whether an agent asks before it acts — a mark, not a sentence.
 *
 * The setting is almost always Full Auto, which is the argument for demoting
 * the labelled dropdown, not for deleting the fact: it is the one control on
 * this screen that decides whether an agent stops to ask, and the codebase
 * deliberately does not carry it between sessions for that reason
 * (`ui-store.lastUsed` holds model and effort and stops there). A row that
 * never mentions it is that default with an extra step. So the words move into
 * the menu — where the descriptions that actually distinguish the modes already
 * live — and the row keeps a glyph.
 *
 * The glyphs are transport controls, and the silhouette carries the meaning so
 * nothing depends on an interior detail: a **hand** stops, **play** runs,
 * **fast-forward** does not stop. That is why they are not the shield family
 * they replace — the whole distinction there lived inside the shield, in a 4px
 * tick or exclamation mark, which is the first thing to die at 12px.
 *
 * It is deliberately *not* a warning triangle, which is the obvious glyph for
 * "skip all approvals" and is already taken: `TriangleAlert` means "someone is
 * waiting on you" in `ThreadRow`, `DockToggle` and `DockTabBar`, and one mark
 * has to mean one thing across surfaces.
 */
import { Check, FastForward, Hand, Play } from "lucide-react";
import { memo } from "react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import {
  PERMISSION_COLORS,
  PERMISSION_DESCRIPTIONS,
  PERMISSION_MODES,
  PERMISSION_VERBS,
} from "~/lib/composer-constants";
import { cn } from "~/lib/utils";
import type { AutoApproveMode } from "~/stores/chat-store";

const MODE_GLYPH = {
  manual: Hand,
  auto: Play,
  fullAuto: FastForward,
} as const;

/**
 * Only the riskiest mode gets a ground. The mark is the one safety fact on the
 * row, and the disc is what earns it a size the other glyphs do not get —
 * everything else here is 12–14px, this is 16.
 */
const MODE_GROUND: Record<AutoApproveMode, string> = {
  manual: "",
  auto: "",
  fullAuto: "bg-warning/15 rounded-full",
};

export const PermissionMark = memo(function PermissionMark({
  mode,
  onChange,
}: {
  mode: AutoApproveMode;
  onChange?: (value: AutoApproveMode) => void;
}) {
  const Glyph = MODE_GLYPH[mode];
  const title = `${PERMISSION_VERBS[mode]} — ${PERMISSION_DESCRIPTIONS[mode]}`;

  if (!onChange) {
    return (
      <span
        className={cn("flex items-center p-1 shrink-0", PERMISSION_COLORS[mode], MODE_GROUND[mode])}
        title={title}
        aria-label={title}
      >
        <Glyph className="size-4" />
      </span>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        title={title}
        aria-label={title}
        className={cn(
          "flex items-center p-1 shrink-0 cursor-pointer transition-[filter] hover:brightness-125 focus-visible:outline-none",
          PERMISSION_COLORS[mode],
          MODE_GROUND[mode],
        )}
      >
        <Glyph className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-[16rem]">
        {PERMISSION_MODES.map((m) => {
          const ItemGlyph = MODE_GLYPH[m];
          return (
            <DropdownMenuItem
              key={m}
              onClick={() => onChange(m)}
              className="text-xs gap-2 items-start"
            >
              {/* A check, not a tinted row: the menu is read against a
                  translucent popover where a background tint is the same
                  weight as hover, and hover is not selection. */}
              <Check
                className={cn("h-3 w-3 mt-0.5 shrink-0", m === mode ? "opacity-100" : "opacity-0")}
              />
              <ItemGlyph className={cn("h-3.5 w-3.5 mt-0.5 shrink-0", PERMISSION_COLORS[m])} />
              <div className="flex flex-col gap-0.5">
                <span className={cn(m === mode && "font-medium text-foreground")}>
                  {PERMISSION_VERBS[m]}
                </span>
                <span className="text-[10px] text-muted-foreground">
                  {PERMISSION_DESCRIPTIONS[m]}
                </span>
              </div>
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
});
