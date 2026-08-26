import { Maximize2, Minimize2, X } from "lucide-react";
import type { ReactNode } from "react";
import { DockTabBar, type DockTabMark } from "~/components/chat/dock/DockTabBar";
import { Button } from "~/components/ui/button";
import type { DockView } from "~/lib/session/dock";

interface SessionDockProps {
  views: readonly DockView[];
  active: DockView;
  marks: Partial<Record<DockView, DockTabMark>>;
  onSelect: (view: DockView) => void;
  onClose: () => void;
  maximized: boolean;
  onMaximizedChange: (maximized: boolean) => void;
  accentColor?: string;
  children: ReactNode;
}

/**
 * The dock's chrome: a tab row, a maximize control, a close, and whatever view
 * is active. It owns no view state of its own — which tabs exist is derived by
 * the caller from what the session actually has.
 *
 * Maximize lives here rather than on each view because every view eventually
 * wants it — a diff, a long agent report, a browser — and one control that
 * serves all three beats three that each serve one.
 */
export function SessionDock({
  views,
  active,
  marks,
  onSelect,
  onClose,
  maximized,
  onMaximizedChange,
  accentColor,
  children,
}: SessionDockProps) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-stretch">
        <div className="min-w-0 flex-1">
          <DockTabBar
            views={views}
            active={active}
            marks={marks}
            onSelect={onSelect}
            accentColor={accentColor}
          />
        </div>
        <div className="flex shrink-0 items-center gap-0.5 border-b px-1">
          <Button
            variant="ghost"
            size="icon"
            className="size-7 text-muted-foreground hover:text-foreground"
            onClick={() => onMaximizedChange(!maximized)}
            aria-label={maximized ? "Restore dock width" : "Maximize dock"}
            title={maximized ? "Restore dock width" : "Maximize dock"}
          >
            {maximized ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="size-7 text-muted-foreground hover:text-foreground"
            onClick={onClose}
            aria-label="Close dock"
            title="Close dock"
          >
            <X className="size-3.5" />
          </Button>
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col">{children}</div>
    </div>
  );
}
