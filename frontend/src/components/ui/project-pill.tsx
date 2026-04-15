import { useMemo } from "react";
import { useShallow } from "zustand/shallow";
import { useTheme } from "~/hooks/useTheme";
import { getProjectColor } from "~/lib/project-colors";
import { getProjectIcon } from "~/lib/project-icons";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

interface ProjectPillProps {
  /** Project slug to resolve color from. */
  slug: string;
  /** What text to display. Default: "name". */
  display?: "slug" | "name";
  /** Show a tinted background behind the text. Default: true. */
  background?: boolean;
  /** Show project icon before name (colored dot fallback). Default: false. */
  showIcon?: boolean;
  /** Size variant. Default: "sm". */
  size?: "sm" | "md";
  /** Additional class names. */
  className?: string;
}

/**
 * Renders a project identifier as a colored pill.
 * Resolves the project color automatically from the store.
 */
export function ProjectPill({
  slug,
  display = "name",
  background = true,
  showIcon = false,
  size = "sm",
  className,
}: ProjectPillProps) {
  const project = useAppStore((s) => s.projects.find((p) => p.slug === slug));
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const { resolvedTheme } = useTheme();

  const color = useMemo(
    () =>
      project ? getProjectColor(project.color, project.id, projectIds, resolvedTheme) : undefined,
    [project, projectIds, resolvedTheme],
  );

  if (!color || !project) return null;

  const Icon = showIcon ? getProjectIcon(project.icon) : undefined;
  const sm = size === "sm";

  return (
    <span
      title={slug}
      className={cn(
        "font-medium leading-tight",
        showIcon ? "inline-flex items-center" : "rounded-full",
        sm ? "text-[10px]" : "text-sm",
        showIcon && (sm ? "gap-1" : "gap-2"),
        background && (sm ? "rounded-full px-1.5 py-0.5" : "rounded-md px-2 py-1"),
        className,
      )}
      style={{
        backgroundColor: background ? `${color.bg}20` : undefined,
        color: color.fg,
      }}
    >
      {showIcon &&
        (Icon ? (
          <Icon className={cn("shrink-0", sm ? "size-3" : "size-4")} />
        ) : (
          <span
            className={cn("shrink-0 rounded-full", sm ? "size-1.5" : "size-2.5")}
            style={{ backgroundColor: color.bg }}
          />
        ))}
      <span className={showIcon ? "truncate" : undefined}>
        {display === "name" ? project.name : slug}
      </span>
    </span>
  );
}
