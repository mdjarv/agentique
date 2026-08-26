import { cn } from "~/lib/utils";

/**
 * A file path with the directory dimmed and the filename bright.
 *
 * **The directory truncates, never the filename.** These render in columns
 * narrower than most repo paths, and clipping from the right left rows reading
 * `frontend/src/components/chat/changes/FileDiffSecti` — every one of which
 * looks the same. The name is the part you were looking for, so it is the part
 * that survives; the full path is in the title.
 */
export function FilePath({ path, className }: { path: string; className?: string }) {
  const lastSlash = path.lastIndexOf("/");
  const dir = lastSlash >= 0 ? path.slice(0, lastSlash + 1) : "";
  const filename = lastSlash >= 0 ? path.slice(lastSlash + 1) : path;

  return (
    <span className={cn("flex min-w-0 overflow-hidden", className)} title={path}>
      {dir && <span className="min-w-0 truncate text-muted-foreground-dim">{dir}</span>}
      <span className="shrink-0">{filename}</span>
    </span>
  );
}
