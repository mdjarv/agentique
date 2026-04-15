/**
 * Displays a file path with dimmed directory + bright filename.
 * Truncates naturally from the right; full path in title for hover.
 */
export function FilePath({ path, className }: { path: string; className?: string }) {
  const lastSlash = path.lastIndexOf("/");
  const dir = lastSlash >= 0 ? path.slice(0, lastSlash + 1) : "";
  const filename = lastSlash >= 0 ? path.slice(lastSlash + 1) : path;

  return (
    <span className={className} title={path}>
      {dir && <span className="text-muted-foreground-dim">{dir}</span>}
      <span className="shrink-0">{filename}</span>
    </span>
  );
}
