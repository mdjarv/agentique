import { AlertTriangle, MessageSquare, X } from "lucide-react";
import { Button } from "~/components/ui/button";

interface ConflictPanelProps {
  files: string[];
  onDismiss: () => void;
  onAskResolve?: () => void;
}

export function ConflictPanel({ files, onDismiss, onAskResolve }: ConflictPanelProps) {
  return (
    <div className="border-t border-amber-500/30 bg-amber-500/10 px-4 py-3">
      <div className="flex items-start gap-2">
        <AlertTriangle className="h-4 w-4 text-amber-500 shrink-0 mt-0.5" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium text-amber-500">Merge conflict</p>
          <ul className="mt-1 text-xs text-amber-400/80 space-y-0.5">
            {files.map((file) => (
              <li key={file} className="font-mono truncate">
                {file}
              </li>
            ))}
          </ul>
          {onAskResolve && (
            <Button
              variant="ghost"
              size="sm"
              className="mt-1.5 h-6 px-2 text-xs text-amber-500 hover:text-amber-400 hover:bg-amber-500/10"
              onClick={onAskResolve}
            >
              <MessageSquare className="h-3 w-3" />
              Ask Claude to resolve
            </Button>
          )}
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0 text-amber-500 hover:text-amber-400 shrink-0"
          onClick={onDismiss}
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  );
}
