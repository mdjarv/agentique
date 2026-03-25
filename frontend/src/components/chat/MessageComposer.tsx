import {
  FileText,
  ListChecks,
  ListPlus,
  MessageSquare,
  Paperclip,
  SendHorizonal,
  ShieldCheck,
  Square,
  X,
} from "lucide-react";
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { toast } from "sonner";
import { cn, readFileAsDataUrl, uuid } from "~/lib/utils";
import type { Attachment } from "~/stores/chat-store";

const MAX_ATTACHMENT_BYTES = 10 * 1024 * 1024; // 10 MB
const MAX_ATTACHMENTS = 8;
const ACCEPTED_TYPES = "image/*,application/pdf";

function isAllowedType(mime: string): boolean {
  return mime.startsWith("image/") || mime === "application/pdf";
}

function isImage(mime: string): boolean {
  return mime.startsWith("image/");
}

export interface ComposerHandle {
  setText: (text: string) => void;
}

export type EffortLevel = "" | "low" | "medium" | "high";

interface MessageComposerProps {
  onSend: (prompt: string, attachments?: Attachment[]) => void;
  disabled?: boolean;
  isRunning?: boolean;
  onInterrupt?: () => void;
  initialText?: string;
  onTextPersist?: (text: string) => void;
  placeholder?: string;
  planMode?: boolean;
  onPlanModeChange?: (value: boolean) => void;
  autoApprove?: boolean;
  onAutoApproveChange?: (value: boolean) => void;
}

export const MessageComposer = forwardRef<ComposerHandle, MessageComposerProps>(
  function MessageComposer(
    {
      onSend,
      disabled,
      isRunning,
      placeholder,
      onInterrupt,
      initialText,
      onTextPersist,
      planMode,
      onPlanModeChange,
      autoApprove,
      onAutoApproveChange,
    },
    ref,
  ) {
    const [text, setText] = useState(initialText ?? "");
    const [attachments, setAttachments] = useState<Attachment[]>([]);
    const [isDragging, setIsDragging] = useState(false);
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const fileInputRef = useRef<HTMLInputElement>(null);
    const submittingRef = useRef(false);
    const textRef = useRef(text);
    textRef.current = text;
    const onTextPersistRef = useRef(onTextPersist);
    onTextPersistRef.current = onTextPersist;

    const attachmentsRef = useRef(attachments);
    attachmentsRef.current = attachments;

    useImperativeHandle(ref, () => ({
      setText: (value: string) => {
        setText(value);
        requestAnimationFrame(() => {
          const el = textareaRef.current;
          if (el) {
            el.style.height = "auto";
            el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
            el.focus();
          }
        });
      },
    }));

    useEffect(() => {
      const el = textareaRef.current;
      if (el?.value) {
        el.style.height = "auto";
        el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
      }
      return () => {
        onTextPersistRef.current?.(textRef.current);
        for (const a of attachmentsRef.current) {
          if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
        }
      };
    }, []);

    const handleSend = () => {
      const trimmed = text.trim();
      if ((!trimmed && attachments.length === 0) || disabled || submittingRef.current) return;
      submittingRef.current = true;
      onSend(trimmed, attachments.length > 0 ? attachments : undefined);
      setText("");
      setAttachments((prev) => {
        for (const a of prev) {
          if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
        }
        return [];
      });
      if (textareaRef.current) {
        textareaRef.current.style.height = "auto";
      }
      queueMicrotask(() => {
        submittingRef.current = false;
      });
    };

    const handleKeyDown = (e: React.KeyboardEvent) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    };

    const handleInput = () => {
      const el = textareaRef.current;
      if (el) {
        el.style.height = "auto";
        el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
      }
    };

    const addFiles = async (files: File[]) => {
      const allowed = files.filter((f) => isAllowedType(f.type));
      if (allowed.length === 0) return;

      const remaining = MAX_ATTACHMENTS - attachments.length;
      if (remaining <= 0) {
        toast.error(`Maximum ${MAX_ATTACHMENTS} attachments per message`);
        return;
      }
      const batch = allowed.slice(0, remaining);
      if (batch.length < allowed.length) {
        toast.warning(`Only ${remaining} more attachment(s) allowed`);
      }

      const added: Attachment[] = [];
      for (const file of batch) {
        if (file.size > MAX_ATTACHMENT_BYTES) {
          toast.error(`${file.name} exceeds 10 MB limit`);
          continue;
        }
        try {
          const dataUrl = await readFileAsDataUrl(file);
          added.push({
            id: uuid(),
            name: file.name,
            mimeType: file.type,
            dataUrl,
            previewUrl: isImage(file.type) ? URL.createObjectURL(file) : undefined,
          });
        } catch {
          toast.error(`Failed to read ${file.name}`);
        }
      }
      if (added.length > 0) {
        setAttachments((prev) => [...prev, ...added]);
      }
    };

    const handlePaste = (e: React.ClipboardEvent) => {
      const files = Array.from(e.clipboardData.files);
      if (files.length === 0) return;
      const hasAllowed = files.some((f) => isAllowedType(f.type));
      if (!hasAllowed) return;
      e.preventDefault();
      addFiles(files);
    };

    const handleFileInput = (e: React.ChangeEvent<HTMLInputElement>) => {
      const files = Array.from(e.target.files ?? []);
      if (files.length > 0) addFiles(files);
      e.target.value = "";
    };

    const handleDrop = (e: React.DragEvent) => {
      e.preventDefault();
      setIsDragging(false);
      const files = Array.from(e.dataTransfer.files);
      if (files.length > 0) addFiles(files);
    };

    const handleDragOver = (e: React.DragEvent) => {
      e.preventDefault();
      setIsDragging(true);
    };

    const handleDragLeave = (e: React.DragEvent) => {
      if (e.currentTarget.contains(e.relatedTarget as Node)) return;
      setIsDragging(false);
    };

    const removeAttachment = (id: string) => {
      setAttachments((prev) => {
        const a = prev.find((i) => i.id === id);
        if (a?.previewUrl) URL.revokeObjectURL(a.previewUrl);
        return prev.filter((i) => i.id !== id);
      });
    };

    const hasToggles = onPlanModeChange || onAutoApproveChange;

    return (
      <div className="border-t p-3">
        {/* Attachment previews */}
        {attachments.length > 0 && (
          <div className="flex gap-2 flex-wrap mb-2">
            {attachments.map((a) => (
              <div key={a.id} className="relative group">
                {isImage(a.mimeType) ? (
                  <img
                    src={a.previewUrl ?? a.dataUrl}
                    alt={a.name}
                    className="h-16 w-16 object-cover rounded-md border"
                  />
                ) : (
                  <div className="h-16 w-16 rounded-md border bg-muted flex flex-col items-center justify-center gap-1 px-1">
                    <FileText className="h-5 w-5 text-muted-foreground" />
                    <span className="text-[9px] text-muted-foreground truncate w-full text-center">
                      {a.name}
                    </span>
                  </div>
                )}
                <button
                  type="button"
                  onClick={() => removeAttachment(a.id)}
                  className="absolute -top-1.5 -right-1.5 h-4 w-4 rounded-full bg-destructive text-destructive-foreground flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity"
                >
                  <X className="h-2.5 w-2.5" />
                </button>
              </div>
            ))}
          </div>
        )}

        {/* Unified composer container */}
        <div
          className={cn(
            "rounded-xl border bg-secondary/50 transition-all",
            isDragging
              ? "border-primary ring-2 ring-primary/30"
              : "focus-within:border-ring/50 focus-within:ring-1 focus-within:ring-ring/30",
          )}
          onDrop={handleDrop}
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
        >
          <textarea
            ref={textareaRef}
            autoFocus
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              handleInput();
            }}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            placeholder={placeholder ?? (isRunning ? "Queue a follow-up..." : "Send a message...")}
            className="w-full resize-none bg-transparent px-3 pt-3 pb-1 text-sm placeholder:text-muted-foreground focus:outline-none overflow-y-auto"
            rows={1}
            style={{ maxHeight: "200px" }}
            disabled={disabled}
          />

          {/* Bottom bar */}
          <div className="flex items-center justify-between px-2 pb-2">
            <div className="flex items-center gap-0.5">
              <button
                type="button"
                onClick={() => fileInputRef.current?.click()}
                disabled={disabled}
                className="h-7 w-7 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/80 flex items-center justify-center transition-colors disabled:opacity-40"
                aria-label="Attach files"
              >
                <Paperclip className="h-3.5 w-3.5" />
              </button>

              {hasToggles && <div className="w-px h-4 bg-border mx-1" />}

              {onPlanModeChange && (
                <button
                  type="button"
                  onClick={() => onPlanModeChange(!planMode)}
                  disabled={isRunning}
                  className={cn(
                    "flex items-center gap-1 text-[11px] rounded-md px-2 py-1 transition-colors",
                    planMode
                      ? "bg-yellow-500/10 text-yellow-500"
                      : "text-muted-foreground hover:text-foreground hover:bg-muted/80",
                    isRunning && "opacity-40 cursor-not-allowed",
                  )}
                >
                  {planMode ? (
                    <ListChecks className="h-3 w-3" />
                  ) : (
                    <MessageSquare className="h-3 w-3" />
                  )}
                  {planMode ? "Plan" : "Chat"}
                </button>
              )}
              {onAutoApproveChange && (
                <button
                  type="button"
                  onClick={() => onAutoApproveChange(!autoApprove)}
                  className={cn(
                    "flex items-center gap-1 text-[11px] rounded-md px-2 py-1 transition-colors",
                    autoApprove
                      ? "bg-green-500/10 text-green-500"
                      : "text-muted-foreground hover:text-foreground hover:bg-muted/80",
                  )}
                >
                  <ShieldCheck className="h-3 w-3" />
                  {autoApprove ? "Auto" : "Manual"}
                </button>
              )}
            </div>

            <div className="flex items-center gap-1">
              {isRunning && (
                <button
                  type="button"
                  onClick={onInterrupt}
                  className="h-7 w-7 rounded-lg text-destructive hover:bg-destructive/10 flex items-center justify-center transition-colors"
                  aria-label="Stop"
                >
                  <Square className="h-3.5 w-3.5" />
                </button>
              )}
              <button
                type="button"
                onClick={handleSend}
                disabled={disabled || (!text.trim() && attachments.length === 0)}
                className="h-7 w-7 rounded-lg bg-primary text-primary-foreground flex items-center justify-center transition-colors hover:bg-primary/90 disabled:opacity-30 disabled:cursor-not-allowed"
                aria-label={isRunning ? "Queue message" : "Send message"}
              >
                {isRunning ? (
                  <ListPlus className="h-3.5 w-3.5" />
                ) : (
                  <SendHorizonal className="h-3.5 w-3.5" />
                )}
              </button>
            </div>
          </div>
        </div>

        <input
          ref={fileInputRef}
          type="file"
          accept={ACCEPTED_TYPES}
          multiple
          className="hidden"
          onChange={handleFileInput}
        />
      </div>
    );
  },
);
