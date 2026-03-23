import { useParams } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ProjectTreeItem } from "~/components/layout/ProjectTreeItem";
import { useProjects } from "~/hooks/useProjects";
import { cn } from "~/lib/utils";
import { useChatStore } from "~/stores/chat-store";

export function ProjectList() {
  const projects = useProjects();
  const params = useParams({ strict: false });
  const activeProjectId = (params as { projectId?: string }).projectId;
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());

  // Auto-expand the active project when it changes.
  useEffect(() => {
    if (activeProjectId) {
      setExpandedIds((prev) => {
        if (prev.has(activeProjectId)) return prev;
        return new Set(prev).add(activeProjectId);
      });
    }
  }, [activeProjectId]);

  const toggleExpand = useCallback((id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const activeSessionId = useChatStore((s) => s.activeSessionId);

  if (projects.length === 0) {
    return <div className="p-4 text-sm text-muted-foreground">No projects yet</div>;
  }

  const handleNewChat = () => {
    if (activeProjectId) {
      useChatStore.getState().createDraft(activeProjectId);
    }
  };

  return (
    <div className="p-2 space-y-1">
      {projects.map((project) => (
        <ProjectTreeItem
          key={project.id}
          project={project}
          isActive={project.id === activeProjectId}
          isExpanded={expandedIds.has(project.id)}
          onToggleExpand={() => toggleExpand(project.id)}
        />
      ))}
      {activeProjectId && (
        <button
          type="button"
          onClick={handleNewChat}
          className={cn(
            "flex items-center gap-1.5 w-full rounded-md px-2 py-1 text-xs transition-colors",
            activeSessionId?.startsWith("draft-")
              ? "text-foreground bg-accent/70"
              : "text-muted-foreground hover:text-foreground hover:bg-accent",
          )}
        >
          <Plus className="h-3 w-3" />
          <span>New chat</span>
        </button>
      )}
    </div>
  );
}
