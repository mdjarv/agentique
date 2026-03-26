import { ArrowDown, ArrowUp } from "lucide-react";

interface GitIndicatorsProps {
  dirty?: boolean;
  uncommittedCount?: number;
  aheadCount?: number;
  behindCount?: number;
}

export function GitIndicators({
  dirty: dirtyProp,
  uncommittedCount,
  aheadCount,
  behindCount,
}: GitIndicatorsProps) {
  const dirty = dirtyProp || (!!uncommittedCount && uncommittedCount > 0);
  const ahead = !!aheadCount && aheadCount > 0;
  const behind = !!behindCount && behindCount > 0;

  if (!dirty && !ahead && !behind) return null;

  return (
    <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
      {dirty && (
        <span className="flex items-center gap-0.5 text-[#e0af68]/80">
          <span className="text-[0.5rem] leading-none">&#9679;</span>
          {uncommittedCount}
        </span>
      )}
      {ahead && (
        <span className="flex items-center gap-0.5">
          <ArrowUp className="size-2.5" />
          {aheadCount}
        </span>
      )}
      {behind && (
        <span className="flex items-center gap-0.5 text-[#7aa2f7]/80">
          <ArrowDown className="size-2.5" />
          {behindCount}
        </span>
      )}
    </span>
  );
}
