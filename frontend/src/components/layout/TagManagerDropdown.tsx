import { Pencil, Plus, Trash2 } from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";
import { toast } from "sonner";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { Tag } from "~/lib/generated-types";
import { createTag, deleteTag, updateTag } from "~/lib/project-actions";
import { TAG_COLORS, getTagColor } from "~/lib/tag-colors";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { TagEditForm } from "./TagEditForm";

interface TagManagerDropdownProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}

export function TagManagerDropdown({ open, onOpenChange, children }: TagManagerDropdownProps) {
  const tags = useAppStore((s) => s.tags);
  const ws = useWebSocket();
  const [editingTag, setEditingTag] = useState<Tag | null>(null);
  const [showNewForm, setShowNewForm] = useState(false);
  const [creating, setCreating] = useState(false);

  const handleCreate = useCallback(
    async (name: string, color: string) => {
      const trimmed = name.trim();
      if (!trimmed) return;
      setCreating(true);
      try {
        const tag = await createTag(ws, trimmed, color);
        useAppStore.getState().addTag(tag);
        setShowNewForm(false);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to create tag"));
      } finally {
        setCreating(false);
      }
    },
    [ws],
  );

  const handleUpdate = useCallback(
    async (tag: Tag, name: string, color: string) => {
      try {
        const updated = await updateTag(ws, tag.id, name, color);
        useAppStore.getState().updateTag(updated);
        setEditingTag(null);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to update tag"));
      }
    },
    [ws],
  );

  const handleDelete = useCallback(
    async (id: string) => {
      try {
        await deleteTag(ws, id);
        useAppStore.getState().removeTag(id);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to delete tag"));
      }
    },
    [ws],
  );

  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent side="right" align="start" sideOffset={8} className="w-56">
        <div className="px-2 py-1.5 text-xs font-semibold text-muted-foreground">Tags</div>
        {tags.map((tag) => {
          const color = getTagColor(tag.color);
          if (editingTag?.id === tag.id) {
            return (
              <TagEditForm
                key={tag.id}
                initialName={tag.name}
                initialColor={tag.color}
                onSave={(name, colorId) => handleUpdate(tag, name, colorId)}
                onCancel={() => setEditingTag(null)}
              />
            );
          }
          return (
            <div
              key={tag.id}
              className="flex items-center gap-2 px-2 py-1.5 rounded-sm hover:bg-accent group"
            >
              <span
                className="inline-block size-2.5 rounded-full shrink-0"
                style={{ backgroundColor: color.bg }}
              />
              <span className="text-sm flex-1 truncate">{tag.name}</span>
              <button
                type="button"
                onClick={() => setEditingTag(tag)}
                className="opacity-0 group-hover:opacity-100 p-0.5 text-muted-foreground hover:text-foreground transition-opacity cursor-pointer"
              >
                <Pencil className="size-3" />
              </button>
              <button
                type="button"
                onClick={() => handleDelete(tag.id)}
                className="opacity-0 group-hover:opacity-100 p-0.5 text-muted-foreground hover:text-destructive transition-opacity cursor-pointer"
              >
                <Trash2 className="size-3" />
              </button>
            </div>
          );
        })}
        {tags.length > 0 && <DropdownMenuSeparator />}
        {showNewForm ? (
          <TagEditForm
            initialName=""
            initialColor={TAG_COLORS[0].id}
            onSave={(name, color) => handleCreate(name, color)}
            onCancel={() => setShowNewForm(false)}
            disabled={creating}
            autoFocus
          />
        ) : (
          <button
            type="button"
            onClick={() => setShowNewForm(true)}
            className="flex w-full items-center gap-2 px-2 py-1.5 text-sm text-muted-foreground hover:text-foreground hover:bg-accent rounded-sm transition-colors cursor-pointer"
          >
            <Plus className="size-3.5" />
            New tag
          </button>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
