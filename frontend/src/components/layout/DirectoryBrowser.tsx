import { ChevronRight, Folder, GitBranch, Home, Loader2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ScrollArea } from "~/components/ui/scroll-area";
import { type BrowseResult, browseDirectory } from "~/lib/api";

interface DirectoryBrowserProps {
  initialPath?: string;
  onSelect: (path: string) => void;
}

export function DirectoryBrowser({ initialPath, onSelect }: DirectoryBrowserProps) {
  const [browsePath, setBrowsePath] = useState<string | undefined>(initialPath || undefined);
  const [result, setResult] = useState<BrowseResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const navigate = useCallback(
    (path: string | undefined) => {
      setBrowsePath(path);
      if (path) {
        onSelect(path);
      }
    },
    [onSelect],
  );

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");

    browseDirectory(browsePath)
      .then((data) => {
        if (!cancelled) {
          setResult(data);
          setLoading(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err.message);
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [browsePath]);

  const pathSegments = result?.path.split("/").filter(Boolean) ?? [];

  return (
    <div className="space-y-2">
      {/* Breadcrumbs */}
      <div className="flex items-center gap-0.5 overflow-x-auto text-sm">
        <button
          type="button"
          onClick={() => navigate(undefined)}
          className="shrink-0 rounded p-1 text-muted-foreground hover:text-foreground"
          title="Home"
        >
          <Home className="h-3.5 w-3.5" />
        </button>
        <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground-faint" />
        <button
          type="button"
          onClick={() => navigate("/")}
          className="shrink-0 rounded px-1 py-0.5 text-muted-foreground hover:text-foreground"
        >
          /
        </button>
        {pathSegments.map((segment, i) => {
          const segmentPath = `/${pathSegments.slice(0, i + 1).join("/")}`;
          const isLast = i === pathSegments.length - 1;
          return (
            <span key={segmentPath} className="flex items-center gap-0.5">
              <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground-faint" />
              <button
                type="button"
                onClick={() => navigate(segmentPath)}
                className={`shrink-0 rounded px-1 py-0.5 ${isLast ? "text-foreground font-medium" : "text-muted-foreground hover:text-foreground"}`}
              >
                {segment}
              </button>
            </span>
          );
        })}
      </div>

      {/* Directory list */}
      <ScrollArea className="h-60 rounded-md border">
        {loading && (
          <div className="flex h-full items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        )}
        {error && <p className="p-3 text-sm text-destructive">{error}</p>}
        {!loading && !error && result && (
          <div className="p-1">
            {result.parent && (
              <button
                type="button"
                onClick={() => navigate(result.parent)}
                className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              >
                <Folder className="h-4 w-4 shrink-0" />
                <span>..</span>
              </button>
            )}
            {(result.entries ?? []).length === 0 && !result.parent && (
              <p className="p-3 text-center text-sm text-muted-foreground">No subdirectories</p>
            )}
            {(result.entries ?? []).map((entry) => (
              <button
                key={entry.path}
                type="button"
                onClick={() => navigate(entry.path)}
                className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              >
                <Folder className="h-4 w-4 shrink-0 text-muted-foreground" />
                <span className="flex-1 truncate">{entry.name}</span>
                {entry.isGitRepo && (
                  <GitBranch className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                )}
              </button>
            ))}
            {(result.entries ?? []).length === 0 && result.parent && (
              <p className="p-3 text-center text-sm text-muted-foreground">No subdirectories</p>
            )}
          </div>
        )}
      </ScrollArea>
    </div>
  );
}
