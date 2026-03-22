import { Terminal } from "lucide-react";

interface ToolUseBlockProps {
  name: string;
  input: unknown;
}

export function ToolUseBlock({ name, input }: ToolUseBlockProps) {
  return (
    <div className="border rounded-md bg-muted/50 text-xs">
      <div className="flex items-center gap-2 p-2 text-muted-foreground border-b">
        <Terminal className="h-3 w-3" />
        <span className="font-medium">{name}</span>
      </div>
      <pre className="p-2 overflow-x-auto text-muted-foreground">
        {typeof input === "string" ? input : JSON.stringify(input, null, 2)}
      </pre>
    </div>
  );
}
