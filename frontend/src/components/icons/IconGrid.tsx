/**
 * The icon chooser both pickers share: a search over every lucide icon, and
 * the catalog's categories when the search is empty.
 *
 * Icons are rendered through `DynamicIcon` unless the static registry already
 * holds them, so the grid's few hundred glyphs load when it opens rather than
 * riding the main bundle. A pick is loaded into the shared icon cache before
 * it is reported, so the surfaces that draw it synchronously (the rail, the
 * machine marks) have it by the time they re-render.
 */
import { Search } from "lucide-react";
import { DynamicIcon, dynamicIconImports, type IconName, iconNames } from "lucide-react/dynamic";
import { memo, type ReactNode, useCallback, useMemo, useState } from "react";
import type { IconGroup } from "~/lib/icon-catalog";
import { cacheProjectIcon, getProjectIcon } from "~/lib/project-icons";
import { cn } from "~/lib/utils";

const MAX_RESULTS = 96;

const lucideNames = new Set<string>(iconNames);

export function IconGrid({
  groups,
  value,
  onSelect,
  clearTitle,
  clearGlyph,
  autoFocus,
  className,
}: {
  groups: IconGroup[];
  value: string;
  onSelect: (iconId: string) => void;
  /** Tooltip for the "no icon" cell — what an unset icon falls back to. */
  clearTitle: string;
  /** What the "no icon" cell draws; initials for a project by default. */
  clearGlyph?: ReactNode;
  autoFocus?: boolean;
  className?: string;
}) {
  const [query, setQuery] = useState("");

  const results = useMemo(() => {
    const q = query.toLowerCase().trim();
    if (!q) return null;
    return iconNames.filter((n) => n.includes(q)).slice(0, MAX_RESULTS);
  }, [query]);

  // Only offer what lucide can actually load: a catalog id that a lucide
  // upgrade renamed would otherwise be a blank cell that saves a dead id.
  const featured = useMemo(
    () =>
      groups
        .map((g) => ({ label: g.label, ids: g.ids.filter((id) => lucideNames.has(id)) }))
        .filter((g) => g.ids.length > 0),
    [groups],
  );

  const select = useCallback(
    async (id: string) => {
      if (id && !getProjectIcon(id)) {
        try {
          const mod = await dynamicIconImports[id as IconName]?.();
          if (mod?.default) cacheProjectIcon(id, mod.default);
        } catch {
          // The surfaces that draw it fall back to their default glyph.
        }
      }
      onSelect(id);
    },
    [onSelect],
  );

  return (
    <div className={cn("flex min-h-0 flex-col", className)}>
      <div className="relative mb-2 shrink-0">
        <Search className="absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground-faint" />
        <input
          type="text"
          value={query}
          autoFocus={autoFocus}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={`Search ${iconNames.length.toLocaleString()} icons...`}
          className="w-full rounded border bg-input/50 py-1.5 pr-2 pl-7 text-xs outline-none placeholder:text-muted-foreground-faint focus:ring-1 focus:ring-ring"
        />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <IconCell
          id=""
          active={!value}
          title={clearTitle}
          clearGlyph={clearGlyph}
          onSelect={select}
        />
        {results === null ? (
          featured.map((group) => (
            <div key={group.label} className="mt-2">
              <div className="px-1 pb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground-faint">
                {group.label}
              </div>
              {group.ids.map((id) => (
                <IconCell key={id} id={id} active={value === id} onSelect={select} />
              ))}
            </div>
          ))
        ) : results.length === 0 ? (
          <p className="py-4 text-center text-xs text-muted-foreground">No icons found</p>
        ) : (
          results.map((id) => <IconCell key={id} id={id} active={value === id} onSelect={select} />)
        )}
      </div>
    </div>
  );
}

/** Memoised: a grid of a few hundred cells must not re-render per keystroke. */
const IconCell = memo(function IconCell({
  id,
  active,
  title,
  clearGlyph,
  onSelect,
}: {
  id: string;
  active: boolean;
  title?: string;
  clearGlyph?: ReactNode;
  onSelect: (id: string) => void;
}) {
  const StaticIcon = id ? getProjectIcon(id) : undefined;
  const label = title ?? id.replaceAll("-", " ");
  return (
    <button
      type="button"
      onClick={() => onSelect(id)}
      title={label}
      aria-label={label}
      aria-pressed={active}
      className={cn(
        "inline-flex size-8 cursor-pointer items-center justify-center rounded-md transition-colors",
        active ? "bg-primary/15 text-primary" : "text-muted-foreground hover:bg-muted/50",
      )}
    >
      {!id ? (
        (clearGlyph ?? <span className="text-[10px] font-medium">Aa</span>)
      ) : StaticIcon ? (
        <StaticIcon className="size-4" />
      ) : (
        <DynamicIcon name={id as IconName} className="size-4" />
      )}
    </button>
  );
});
