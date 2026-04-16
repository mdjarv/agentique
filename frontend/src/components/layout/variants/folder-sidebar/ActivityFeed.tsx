import { useNavigate } from "@tanstack/react-router";
import { Activity } from "lucide-react";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { ActivityItem } from "~/lib/generated-types";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

const CATEGORY_LABELS: Record<string, string> = {
  command: "ran command",
  file_write: "edited",
  file_read: "read",
  web: "searched web",
  agent: "delegated",
  task: "managed tasks",
  plan: "planned",
  meta: "configured",
  question: "asked",
  mcp: "used tool",
  other: "worked",
};

function basename(path: string): string {
  const i = path.lastIndexOf("/");
  return i >= 0 ? path.slice(i + 1) : path;
}

function formatTimestamp(iso: string): string {
  try {
    const d = new Date(iso.endsWith("Z") ? iso : `${iso}Z`);
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  } catch {
    return "";
  }
}

function formatItemPreview(item: ActivityItem): string {
  if (item.kind === "message") {
    return item.content;
  }
  switch (item.eventType) {
    case "tool_use": {
      const label = CATEGORY_LABELS[item.category ?? ""] ?? "used";
      const file = item.filePath ? ` ${basename(item.filePath)}` : "";
      return `${label}${file}${!file && item.content ? ` ${item.content}` : ""}`;
    }
    case "result":
      return "turn complete";
    case "error":
      return item.content || "error";
    default:
      return item.eventType;
  }
}

const EMPTY_ITEMS: ActivityItem[] = [];

const ActivityItemRow = memo(function ActivityItemRow({
  item,
  onClick,
}: {
  item: ActivityItem;
  onClick: () => void;
}) {
  const preview = formatItemPreview(item);
  const isMessage = item.kind === "message";
  const isError = item.eventType === "error";

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-start gap-1.5 w-full px-2 py-1 rounded text-left hover:bg-sidebar-accent/40 transition-colors cursor-pointer text-[11px] min-w-0"
    >
      <span className="text-muted-foreground-faint shrink-0 pt-0.5 w-10 text-right tabular-nums">
        {formatTimestamp(item.createdAt)}
      </span>
      <span className="min-w-0 flex-1">
        <span className={cn("font-medium", isMessage && "text-primary/80")}>
          {item.sourceName || "unknown"}
        </span>
        <span className={cn("text-muted-foreground ml-1", isError && "text-destructive")}>
          {preview}
        </span>
      </span>
    </button>
  );
});

export function ActivityFeed({ projectId }: { projectId: string }) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const projects = useAppStore((s) => s.projects);
  const [items, setItems] = useState<ActivityItem[]>(EMPTY_ITEMS);
  const [loading, setLoading] = useState(true);
  const loadedRef = useRef(false);

  const projectSlug =
    projects.find((p) => p.id === projectId)?.slug ?? projectId.split("-")[0] ?? "";

  // Fetch initial activity.
  useEffect(() => {
    if (loadedRef.current) return;
    loadedRef.current = true;
    ws.request<ActivityItem[]>("project.activity", { projectId })
      .then((data) => setItems(data))
      .catch((err) => console.error("project.activity failed", err))
      .finally(() => setLoading(false));
  }, [ws, projectId]);

  // Subscribe to real-time activity items.
  useEffect(() => {
    const unsub = ws.subscribe("project.activity-item", (item: ActivityItem) => {
      setItems((prev) => [item, ...prev].slice(0, 100));
    });
    return unsub;
  }, [ws]);

  const handleClick = useCallback(
    (item: ActivityItem) => {
      useAppStore.getState().setSidebarOpen(false);
      if (item.kind === "message") {
        // Navigate to channel
        navigate({
          to: "/project/$projectSlug/channel/$channelId",
          params: { projectSlug, channelId: item.sourceId },
        });
      } else {
        // Navigate to session
        navigate({
          to: "/project/$projectSlug/session/$sessionShortId",
          params: { projectSlug, sessionShortId: item.sourceId.split("-")[0] ?? "" },
        });
      }
    },
    [navigate, projectSlug],
  );

  if (loading) return null;
  if (items.length === 0) return null;

  return (
    <div className="space-y-0.5">
      <h3 className="flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground-faint px-2">
        <Activity className="size-3" />
        Recent Activity
      </h3>
      <div className="max-h-48 overflow-y-auto">
        {items.map((item) => (
          <ActivityItemRow key={item.itemId} item={item} onClick={() => handleClick(item)} />
        ))}
      </div>
    </div>
  );
}
