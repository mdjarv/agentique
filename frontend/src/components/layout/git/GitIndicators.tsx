import { ArrowDown, ArrowUp, Circle } from "lucide-react";
import { cn } from "~/lib/utils";

interface GitIndicatorsProps {
  dirty?: boolean;
  uncommittedCount?: number;
  aheadCount?: number;
  behindCount?: number;
  className?: string;
}

export function GitIndicators({
  dirty: dirtyProp,
  uncommittedCount,
  aheadCount,
  behindCount,
  className,
}: GitIndicatorsProps) {
  const dirty = dirtyProp || (!!uncommittedCount && uncommittedCount > 0);
  const ahead = !!aheadCount && aheadCount > 0;
  const behind = !!behindCount && behindCount > 0;

  if (!dirty && !ahead && !behind) return null;

  return (
    <span className={cn("flex items-center gap-1 text-[11px]", className)}>
      {dirty && (
        <span className="flex items-center gap-0.5 rounded-full bg-warning/15 px-1.5 py-0.5 text-warning">
          <Circle className="size-2 fill-current" />
          {uncommittedCount}
        </span>
      )}
      {ahead && (
        <span className="flex items-center gap-0.5 rounded-full bg-success/15 px-1.5 py-0.5 text-success">
          <ArrowUp className="size-2.5" />
          {aheadCount}
        </span>
      )}
      {behind && (
        <span className="flex items-center gap-0.5 rounded-full bg-primary/15 px-1.5 py-0.5 text-primary">
          <ArrowDown className="size-2.5" />
          {behindCount}
        </span>
      )}
    </span>
  );
}
