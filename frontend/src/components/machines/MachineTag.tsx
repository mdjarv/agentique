import type { NamedMachine } from "~/hooks/useMachineNaming";
import { resolveMachineGlyph } from "~/lib/machines/platform";
import { cn } from "~/lib/utils";

/**
 * A machine's glyph and name, inline. The one rendering of "this project lives
 * on that machine" in project lists, so a picker and the settings page cannot
 * draw the same fact two ways.
 */
export function MachineTag({
  machine,
  offline,
  className,
}: {
  machine: NamedMachine;
  offline?: boolean;
  className?: string;
}) {
  const Icon = resolveMachineGlyph(machine.icon, machine.platform);
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1", className)}>
      <Icon className="size-3 shrink-0" />
      <span className="truncate">{machine.label}</span>
      {offline && <span className="shrink-0 text-muted-foreground-faint">offline</span>}
    </span>
  );
}
