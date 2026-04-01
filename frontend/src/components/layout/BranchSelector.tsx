import { Check, ChevronDown, GitBranch, Globe, Loader2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { checkoutProjectBranch, listProjectBranches } from "~/lib/project-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import type { ProjectGitStatus } from "~/stores/app-store";

interface BranchSelectorProps {
  projectId: string;
  currentBranch: string;
  isDirty: boolean;
  onBranchChanged: (status: ProjectGitStatus) => void;
}

interface BranchList {
  local: string[];
  remote: string[];
}

export function BranchSelector({
  projectId,
  currentBranch,
  isDirty,
  onBranchChanged,
}: BranchSelectorProps) {
  const ws = useWebSocket();
  const [branches, setBranches] = useState<BranchList | null>(null);
  const [loading, setLoading] = useState(false);
  const [checkingOut, setCheckingOut] = useState(false);
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  const fetchBranches = useCallback(async () => {
    setLoading(true);
    setFilter("");
    try {
      const result = await listProjectBranches(ws, projectId);
      setBranches({ local: result.local ?? [], remote: result.remote ?? [] });
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to list branches"));
    } finally {
      setLoading(false);
    }
  }, [ws, projectId]);

  const handleToggle = useCallback(() => {
    if (checkingOut) return;
    const next = !open;
    setOpen(next);
    if (next) fetchBranches();
  }, [open, checkingOut, fetchBranches]);

  const handleCheckout = useCallback(
    async (branch: string) => {
      setCheckingOut(true);
      setOpen(false);
      try {
        const status = await checkoutProjectBranch(ws, projectId, branch);
        onBranchChanged(status);
        setBranches(null);
        toast.success(`Switched to ${branch}`);
      } catch (err) {
        toast.error(getErrorMessage(err, "Checkout failed"));
      } finally {
        setCheckingOut(false);
      }
    },
    [ws, projectId, onBranchChanged],
  );

  // Focus filter input when dropdown opens
  useEffect(() => {
    if (open && !loading) inputRef.current?.focus();
  }, [open, loading]);

  const lowerFilter = filter.toLowerCase();
  const filteredLocal = branches?.local.filter((b) => b.toLowerCase().includes(lowerFilter)) ?? [];
  const filteredRemote =
    branches?.remote.filter((b) => b.toLowerCase().includes(lowerFilter)) ?? [];

  return (
    <div className="relative">
      {/* Trigger */}
      <button
        type="button"
        onClick={handleToggle}
        className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors w-full cursor-pointer"
      >
        <GitBranch className="size-3 shrink-0" />
        <span className="truncate font-mono">{checkingOut ? "Switching..." : currentBranch}</span>
        <ChevronDown className="size-3 shrink-0 ml-auto" />
      </button>

      {/* Inline dropdown */}
      {open && (
        <div className="absolute left-0 right-0 top-full z-50 mt-1 rounded-md border bg-popover text-popover-foreground shadow-md">
          {/* Filter input */}
          <div className="px-2 py-1.5 border-b">
            <input
              ref={inputRef}
              type="text"
              placeholder="Filter branches..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.preventDefault();
                  setOpen(false);
                }
              }}
              onBlur={() => setTimeout(() => setOpen(false), 150)}
              className="w-full bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
            />
          </div>

          {isDirty && (
            <div className="px-2 py-1.5 text-xs text-warning/80">Commit or stash changes first</div>
          )}

          {loading && (
            <div className="flex items-center justify-center py-3">
              <Loader2 className="size-4 animate-spin text-muted-foreground" />
            </div>
          )}

          {!loading && branches && (
            <div className="max-h-48 overflow-y-auto">
              {/* Local branches */}
              {filteredLocal.map((branch) => {
                const isCurrent = branch === currentBranch;
                return (
                  <button
                    key={`local-${branch}`}
                    type="button"
                    disabled={isDirty || isCurrent || checkingOut}
                    onMouseDown={(e) => {
                      e.preventDefault();
                      if (!isCurrent && !isDirty) handleCheckout(branch);
                    }}
                    className={cn(
                      "flex items-center gap-2 w-full px-2 py-1.5 text-xs font-mono",
                      isCurrent
                        ? "text-foreground"
                        : "hover:bg-accent cursor-pointer text-popover-foreground",
                      (isDirty || isCurrent) && "opacity-50 cursor-default",
                    )}
                  >
                    {isCurrent ? (
                      <Check className="size-3 shrink-0" />
                    ) : (
                      <span className="size-3 shrink-0" />
                    )}
                    {branch}
                  </button>
                );
              })}

              {/* Separator */}
              {filteredRemote.length > 0 && filteredLocal.length > 0 && (
                <div className="-mx-1 my-1 h-px bg-muted" />
              )}

              {/* Remote-only branches */}
              {filteredRemote.map((branch) => (
                <button
                  key={`remote-${branch}`}
                  type="button"
                  disabled={isDirty || checkingOut}
                  onMouseDown={(e) => {
                    e.preventDefault();
                    if (!isDirty) handleCheckout(branch);
                  }}
                  className={cn(
                    "flex items-center gap-2 w-full px-2 py-1.5 text-xs font-mono text-muted-foreground",
                    !isDirty && "hover:bg-accent cursor-pointer",
                    isDirty && "opacity-50 cursor-default",
                  )}
                >
                  <Globe className="size-3 shrink-0" />
                  {branch}
                </button>
              ))}

              {filteredLocal.length === 0 && filteredRemote.length === 0 && (
                <div className="px-2 py-1.5 text-xs text-muted-foreground">
                  No matching branches
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
