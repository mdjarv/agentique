import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { ScrollArea } from "~/components/ui/scroll-area";
import { type FileEntry, type FileListResult, listProjectFiles } from "~/lib/api";
import { formatFileSize, getFileIcon } from "./fileUtils";

interface FileListProps {
  projectId: string;
  path: string;
  selectedFile: string | null;
  onNavigate: (path: string) => void;
  onSelectFile: (path: string) => void;
}

function relativeTime(dateStr: string): string {
  const now = Date.now();
  const then = new Date(dateStr).getTime();
  const diff = now - then;
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  if (months < 12) return `${months}mo ago`;
  return `${Math.floor(months / 12)}y ago`;
}

function FileRow({
  entry,
  parentPath,
  isSelected,
  onNavigate,
  onSelectFile,
}: {
  entry: FileEntry;
  parentPath: string;
  isSelected: boolean;
  onNavigate: (path: string) => void;
  onSelectFile: (path: string) => void;
}) {
  const Icon = getFileIcon(entry.name, entry.isDir);
  const fullPath = parentPath ? `${parentPath}/${entry.name}` : entry.name;

  const handleClick = () => {
    if (entry.isDir) {
      onNavigate(fullPath);
    } else {
      onSelectFile(fullPath);
    }
  };

  return (
    <button
      type="button"
      onClick={handleClick}
      className={`flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-accent ${
        isSelected ? "bg-accent text-accent-foreground" : ""
      }`}
    >
      <Icon className="h-4 w-4 shrink-0 text-muted-foreground" />
      <span className="flex-1 truncate">{entry.name}</span>
      {!entry.isDir && (
        <span className="shrink-0 text-xs text-muted-foreground">{formatFileSize(entry.size)}</span>
      )}
      <span className="shrink-0 text-xs text-muted-foreground w-16 text-right">
        {relativeTime(entry.modTime)}
      </span>
    </button>
  );
}

export function FileList({
  projectId,
  path,
  selectedFile,
  onNavigate,
  onSelectFile,
}: FileListProps) {
  const [result, setResult] = useState<FileListResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");

    listProjectFiles(projectId, path)
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
  }, [projectId, path]);

  if (loading) {
    return (
      <div className="flex h-40 items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error) {
    return <p className="p-4 text-sm text-destructive">{error}</p>;
  }

  if (!result || result.entries.length === 0) {
    return <p className="p-4 text-center text-sm text-muted-foreground">Empty directory</p>;
  }

  return (
    <ScrollArea className="flex-1">
      <div className="p-1">
        {result.entries.map((entry) => (
          <FileRow
            key={entry.name}
            entry={entry}
            parentPath={path}
            isSelected={selectedFile === (path ? `${path}/${entry.name}` : entry.name)}
            onNavigate={onNavigate}
            onSelectFile={onSelectFile}
          />
        ))}
      </div>
    </ScrollArea>
  );
}
