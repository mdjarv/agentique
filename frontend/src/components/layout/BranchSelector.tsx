import { Check, ChevronDown, GitBranch, Globe, Loader2 } from "lucide-react";
import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { ScrollArea } from "~/components/ui/scroll-area";
import { useWebSocket } from "~/hooks/useWebSocket";
import { checkoutProjectBranch, listProjectBranches } from "~/lib/project-actions";
import { getErrorMessage } from "~/lib/utils";
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

  const handleCheckout = useCallback(
    async (branch: string) => {
      setCheckingOut(true);
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

  const lowerFilter = filter.toLowerCase();
  const filteredLocal = branches?.local.filter((b) => b.toLowerCase().includes(lowerFilter)) ?? [];
  const filteredRemote =
    branches?.remote.filter((b) => b.toLowerCase().includes(lowerFilter)) ?? [];

  return (
    <DropdownMenu
      onOpenChange={(open) => {
        if (open) fetchBranches();
      }}
    >
      <DropdownMenuTrigger asChild disabled={checkingOut}>
        <button
          type="button"
          className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors w-full cursor-pointer"
        >
          <GitBranch className="size-3 shrink-0" />
          <span className="truncate font-mono">{checkingOut ? "Switching..." : currentBranch}</span>
          <ChevronDown className="size-3 shrink-0 ml-auto" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        {/* Filter input */}
        <div className="px-2 py-1.5">
          <input
            ref={inputRef}
            type="text"
            placeholder="Filter branches..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            onKeyDown={(e) => e.stopPropagation()}
            className="w-full bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
          />
        </div>
        <DropdownMenuSeparator />

        {isDirty && (
          <div className="px-2 py-1.5 text-xs text-warning/80">Commit or stash changes first</div>
        )}

        {loading && (
          <div className="flex items-center justify-center py-3">
            <Loader2 className="size-4 animate-spin text-muted-foreground" />
          </div>
        )}

        {!loading && branches && (
          <ScrollArea className="max-h-48">
            {/* Local branches */}
            {filteredLocal.map((branch) => {
              const isCurrent = branch === currentBranch;
              return (
                <DropdownMenuItem
                  key={`local-${branch}`}
                  disabled={isDirty || isCurrent || checkingOut}
                  onSelect={() => {
                    if (!isCurrent && !isDirty) handleCheckout(branch);
                  }}
                  className="text-xs font-mono gap-2"
                >
                  {isCurrent ? (
                    <Check className="size-3 shrink-0" />
                  ) : (
                    <span className="size-3 shrink-0" />
                  )}
                  {branch}
                </DropdownMenuItem>
              );
            })}

            {/* Remote-only branches */}
            {filteredRemote.length > 0 && filteredLocal.length > 0 && <DropdownMenuSeparator />}
            {filteredRemote.map((branch) => (
              <DropdownMenuItem
                key={`remote-${branch}`}
                disabled={isDirty || checkingOut}
                onSelect={() => {
                  if (!isDirty) handleCheckout(branch);
                }}
                className="text-xs font-mono gap-2 text-muted-foreground"
              >
                <Globe className="size-3 shrink-0" />
                {branch}
              </DropdownMenuItem>
            ))}

            {filteredLocal.length === 0 && filteredRemote.length === 0 && (
              <div className="px-2 py-1.5 text-xs text-muted-foreground">No matching branches</div>
            )}
          </ScrollArea>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
