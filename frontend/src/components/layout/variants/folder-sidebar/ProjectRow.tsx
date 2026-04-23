import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, Pin, Plus, Settings } from "lucide-react";
import { memo, useCallback } from "react";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import type { ProjectColor } from "~/lib/project-colors";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { type BadgeState, SessionBadge } from "../../session/SessionBadge";
import { IconSlot } from "./IconSlot";

/** Project row content — chevron, icon, name, badge, new-session button. */
export const ProjectContent = memo(function ProjectContent({
  slug,
  name,
  icon,
  color,
  expanded,
  isPinned,
  onToggle,
  onExpand,
  onTogglePin,
  worstState,
}: {
  slug: string;
  name: string;
  icon: string;
  color: ProjectColor;
  expanded: boolean;
  isPinned?: boolean;
  onToggle: () => void;
  onExpand: () => void;
  onTogglePin?: () => void;
  worstState: BadgeState | null;
}) {
  const navigate = useNavigate();
  const Icon = useProjectIcon(icon);
  const initials = slug
    .split("-")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  const handleNewSession = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      onExpand();
      useAppStore.getState().setSidebarOpen(false);
      navigate({ to: "/project/$projectSlug/session/new", params: { projectSlug: slug } });
    },
    [navigate, slug, onExpand],
  );

  const handleSettings = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      useAppStore.getState().setSidebarOpen(false);
      navigate({ to: "/project/$projectSlug/settings", params: { projectSlug: slug } });
    },
    [navigate, slug],
  );

  return (
    <>
      {/* Chevron */}
      <button type="button" onClick={onToggle} className="cursor-pointer">
        <IconSlot>
          {expanded ? (
            <ChevronDown className="size-3 text-muted-foreground" />
          ) : (
            <ChevronRight className="size-3 text-muted-foreground" />
          )}
        </IconSlot>
      </button>

      {/* Project icon */}
      <IconSlot>
        <span
          className="size-5 rounded flex items-center justify-center text-[8px] font-bold"
          style={{ backgroundColor: `${color.bg}20`, color: color.fg }}
        >
          {Icon ? <Icon className="size-3" /> : initials}
        </span>
      </IconSlot>

      {/* Name */}
      <button
        type="button"
        onClick={onToggle}
        className="flex items-center gap-1.5 min-w-0 flex-1 py-1.5 ml-1 cursor-pointer"
      >
        <span className="text-sm font-semibold truncate" style={{ color: color.fg }}>
          {name}
        </span>
        {!expanded && worstState && <SessionBadge state={worstState} size="md" pulse />}
      </button>

      {/* Actions: settings (hover) + pin (visible when pinned, hover otherwise) + new session (always) */}
      <span className="flex items-center gap-0.5 shrink-0">
        <button
          type="button"
          onClick={handleSettings}
          className="size-5 rounded-md flex items-center justify-center cursor-pointer text-muted-foreground hover:text-foreground transition-colors opacity-0 group-hover/proj:opacity-100 transition-opacity"
        >
          <Settings className="size-3" />
        </button>
        {onTogglePin && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onTogglePin();
            }}
            title={isPinned ? "Unpin from focus" : "Pin to focus"}
            className={cn(
              "size-5 rounded-md flex items-center justify-center cursor-pointer transition-all",
              isPinned
                ? "opacity-100 hover:scale-110"
                : "opacity-0 group-hover/proj:opacity-60 hover:!opacity-100 text-muted-foreground hover:text-foreground",
            )}
            style={isPinned ? { color: color.fg } : undefined}
          >
            <Pin className={cn("size-3", isPinned && "fill-current")} />
          </button>
        )}
        <button
          type="button"
          onClick={handleNewSession}
          className="size-5 rounded-md flex items-center justify-center cursor-pointer transition-all hover:scale-110 active:scale-95"
          style={{ color: color.fg, backgroundColor: `${color.bg}25` }}
        >
          <Plus className="size-3" />
        </button>
      </span>
    </>
  );
});
