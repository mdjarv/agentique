/**
 * Rename a machine and pick its face.
 *
 * Presentation is host-local by design (docs/multi-machine.md): this is what
 * *this* host calls that machine. Nothing is written to the machine itself, so
 * the dialog works whether or not the machine is currently reachable.
 */
import { useEffect, useState } from "react";
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
import { DEFAULT_MACHINE_ICON, MACHINE_ICONS } from "~/lib/machines/icons";
import { cn } from "~/lib/utils";

export interface MachineIdentityDraft {
  label: string;
  icon: string;
}

export function MachineIdentityDialog({
  open,
  onOpenChange,
  title,
  initial,
  /** Set when the name is fixed by AGENTIQUE_MACHINE_LABEL — icon still applies. */
  labelPinnedNote,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  initial: MachineIdentityDraft;
  labelPinnedNote?: string;
  onSave: (draft: MachineIdentityDraft) => Promise<void>;
}) {
  const [label, setLabel] = useState(initial.label);
  const [icon, setIcon] = useState(initial.icon);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    setLabel(initial.label);
    setIcon(initial.icon);
  }, [open, initial.label, initial.icon]);

  const save = async () => {
    setSaving(true);
    try {
      await onSave({ label: label.trim(), icon });
      onOpenChange(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            How this device shows the machine. Nothing is sent to the machine itself.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="machine-label">Name</Label>
            <Input
              id="machine-label"
              value={label}
              disabled={!!labelPinnedNote}
              onChange={(e) => setLabel(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !saving) void save();
              }}
              placeholder="review"
              maxLength={64}
              autoFocus
            />
            {labelPinnedNote && (
              <span className="text-[11.5px] text-warning">{labelPinnedNote}</span>
            )}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="machine-icon">Icon</Label>
            <div id="machine-icon" className="flex flex-wrap gap-1.5">
              {MACHINE_ICONS.map(({ id, icon: Icon, label: name }) => (
                <button
                  key={id}
                  type="button"
                  title={name}
                  aria-label={name}
                  aria-pressed={icon === id}
                  onClick={() => setIcon(icon === id ? "" : id)}
                  className={cn(
                    "flex size-9 cursor-pointer items-center justify-center rounded-md border transition-colors",
                    icon === id
                      ? "border-primary/60 bg-primary/15 text-primary"
                      : "border-border/60 text-muted-foreground hover:bg-muted/50 hover:text-foreground",
                  )}
                >
                  <Icon className="size-4" />
                </button>
              ))}
            </div>
            <span className="text-[11.5px] text-muted-foreground-faint">
              Unset falls back to the generic <DEFAULT_MACHINE_ICON className="inline size-3" />{" "}
              glyph.
            </span>
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={save} disabled={saving}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
