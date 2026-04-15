import { Variable } from "lucide-react";
import { useCallback, useState } from "react";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { formatVariableName, substituteVariables } from "~/lib/template-utils";

interface VariableDialogProps {
  open: boolean;
  templateName: string;
  variables: string[];
  onSubmit: (substitutedContent: string) => void;
  onCancel: () => void;
  content: string;
}

export function VariableDialog({
  open,
  templateName,
  variables,
  onSubmit,
  onCancel,
  content,
}: VariableDialogProps) {
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(variables.map((v) => [v, ""])),
  );

  const setValue = useCallback((name: string, value: string) => {
    setValues((prev) => ({ ...prev, [name]: value }));
  }, []);

  const allFilled = variables.every((v) => values[v]?.trim());

  const handleSubmit = () => {
    onSubmit(substituteVariables(content, values));
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey && allFilled) {
      e.preventDefault();
      handleSubmit();
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="sm:max-w-md" onKeyDown={handleKeyDown}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Variable className="h-4 w-4" />
            {templateName}
          </DialogTitle>
          <DialogDescription>Fill in the template variables before launching.</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          {variables.map((v) => (
            <div key={v} className="space-y-1.5">
              <Label htmlFor={`var-${v}`}>{formatVariableName(v)}</Label>
              <Input
                id={`var-${v}`}
                value={values[v] ?? ""}
                onChange={(e) => setValue(v, e.target.value)}
                placeholder={`Enter ${formatVariableName(v).toLowerCase()}...`}
                autoFocus={v === variables[0]}
              />
            </div>
          ))}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!allFilled}>
            Use template
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
