import type { ReactNode } from "react";
import { useState } from "react";
import { ExpandableRow } from "~/components/chat/ExpandableRow";

export function CollapsibleGroup({
  title,
  icon,
  defaultExpanded,
  activeHeader,
  trailingIcons,
  children,
}: {
  title: string;
  icon: ReactNode;
  defaultExpanded: boolean;
  activeHeader?: ReactNode;
  trailingIcons?: ReactNode;
  children: ReactNode;
}) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const showActiveHeader = !!activeHeader && !expanded;

  return (
    <div className="border rounded-md bg-muted/30 overflow-hidden">
      <ExpandableRow
        expanded={expanded}
        onToggle={() => setExpanded(!expanded)}
        className="hover:bg-muted/50"
        trailing={
          !expanded && trailingIcons ? (
            <span className="flex items-center gap-1.5 text-primary/40">{trailingIcons}</span>
          ) : undefined
        }
      >
        {showActiveHeader ? (
          activeHeader
        ) : (
          <>
            {icon}
            <span className="shrink-0 truncate max-w-[50%]">{title}</span>
          </>
        )}
      </ExpandableRow>
      {expanded && <div className="space-y-2 p-1.5 pt-1">{children}</div>}
    </div>
  );
}
