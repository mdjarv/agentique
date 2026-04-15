import { ChevronRight, FolderRoot } from "lucide-react";

interface FileBreadcrumbsProps {
  projectName: string;
  path: string;
  onNavigate: (path: string) => void;
}

export function FileBreadcrumbs({ projectName, path, onNavigate }: FileBreadcrumbsProps) {
  const segments = path ? path.split("/").filter(Boolean) : [];

  return (
    <div className="flex items-center gap-0.5 overflow-x-auto text-sm font-mono px-4 py-2 border-b">
      <button
        type="button"
        onClick={() => onNavigate("")}
        className="shrink-0 flex items-center gap-1.5 rounded px-1.5 py-0.5 text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
      >
        <FolderRoot className="h-3.5 w-3.5" />
        <span>{projectName}</span>
      </button>
      {segments.map((segment, i) => {
        const segmentPath = segments.slice(0, i + 1).join("/");
        const isLast = i === segments.length - 1;
        return (
          <span key={segmentPath} className="flex items-center gap-0.5">
            <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground-faint" />
            <button
              type="button"
              onClick={() => onNavigate(segmentPath)}
              className={`shrink-0 rounded px-1.5 py-0.5 transition-colors ${
                isLast
                  ? "text-foreground font-medium"
                  : "text-muted-foreground hover:text-foreground hover:bg-accent"
              }`}
            >
              {segment}
            </button>
          </span>
        );
      })}
    </div>
  );
}
