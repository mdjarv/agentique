import {
  Check,
  ChevronDown,
  FileText,
  FolderOpen,
  Gauge,
  GitBranch,
  ListChecks,
  ListPlus,
  MessageSquare,
  Paperclip,
  SendHorizonal,
  ShieldAlert,
  ShieldCheck,
  Square,
  X,
} from "lucide-react";
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { toast } from "sonner";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useAutocomplete } from "~/hooks/useAutocomplete";
import { MODELS, MODEL_LABELS, type ModelId } from "~/lib/session-actions";
import { cn, readFileAsDataUrl, uuid } from "~/lib/utils";
import type { AutoApproveMode } from "~/stores/chat-store";
import type { Attachment } from "~/stores/chat-store";
import { AutocompletePopup } from "./AutocompletePopup";

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

export type EffortLevel = "" | "low" | "medium" | "high" | "max";

const EFFORT_LEVELS: EffortLevel[] = ["max", "high", "medium", "low", ""];
const EFFORT_LABELS: Record<EffortLevel, string> = {
  "": "Default",
  low: "Low",
  medium: "Medium",
  high: "High",
  max: "Max",
};
const EFFORT_COLORS: Record<EffortLevel, string> = {
  "": "text-muted-foreground",
  low: "text-info",
  medium: "text-primary",
  high: "text-orange",
  max: "text-destructive",
};

const PERMISSION_MODES: AutoApproveMode[] = ["manual", "auto", "fullAuto"];
const PERMISSION_LABELS: Record<AutoApproveMode, string> = {
  manual: "Manual",
  auto: "Auto",
  fullAuto: "Full Auto",
};
const PERMISSION_DESCRIPTIONS: Record<AutoApproveMode, string> = {
  manual: "Approve every tool use individually",
  auto: "Auto-approve reads and writes, prompt for shell commands",
  fullAuto: "Auto-approve all operations including shell commands",
};
const PERMISSION_COLORS: Record<AutoApproveMode, string> = {
  manual: "text-muted-foreground",
  auto: "text-success",
  fullAuto: "text-warning",
};
const PERMISSION_BG: Record<AutoApproveMode, string> = {
  manual: "",
  auto: "bg-success/10",
  fullAuto: "bg-warning/10",
};

interface MessageComposerProps {
  projectId: string;
  onSend: (prompt: string, attachments?: Attachment[]) => void;
  disabled?: boolean;
  isRunning?: boolean;
  onInterrupt?: () => void;
  initialText?: string;
  onTextPersist?: (text: string) => void;
  placeholder?: string;
  worktree?: boolean;
  onWorktreeChange?: (value: boolean) => void;
  planMode?: boolean;
  onPlanModeChange?: (value: boolean) => void;
  autoApproveMode?: AutoApproveMode;
  onAutoApproveModeChange?: (value: AutoApproveMode) => void;
  model?: ModelId;
  onModelChange?: (value: ModelId) => void;
  effort?: EffortLevel;
  onEffortChange?: (value: EffortLevel) => void;
  onEmptySubmit?: () => void;
}

export const MessageComposer = forwardRef<ComposerHandle, MessageComposerProps>(
  function MessageComposer(
    {
      projectId,
      onSend,
      disabled,
      isRunning,
      placeholder,
      onInterrupt,
      initialText,
      onTextPersist,
      worktree,
      onWorktreeChange,
      planMode,
      onPlanModeChange,
      autoApproveMode,
      onAutoApproveModeChange,
      model,
      onModelChange,
      effort,
      onEffortChange,
      onEmptySubmit,
    },
    ref,
  ) {
    const [text, setText] = useState(initialText ?? "");
    const [attachments, setAttachments] = useState<Attachment[]>([]);
    const [isDragging, setIsDragging] = useState(false);
    const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const fileInputRef = useRef<HTMLInputElement>(null);
    const submittingRef = useRef(false);
    const textRef = useRef(text);
    textRef.current = text;
    const onTextPersistRef = useRef(onTextPersist);
    onTextPersistRef.current = onTextPersist;

    const attachmentsRef = useRef(attachments);
    attachmentsRef.current = attachments;

    const autocomplete = useAutocomplete({ projectId, textareaRef, text, onTextChange: setText });

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
        el.selectionStart = el.selectionEnd = el.value.length;
      }
      return () => {
        onTextPersistRef.current?.(textRef.current);
        for (const a of attachmentsRef.current) {
          if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
        }
      };
    }, []);

    // Debounced draft persistence — save 500ms after typing stops
    useEffect(() => {
      const timer = setTimeout(() => {
        onTextPersistRef.current?.(text);
      }, 500);
      return () => clearTimeout(timer);
    }, [text]);

    const handleSend = () => {
      const trimmed = text.trim();
      if ((!trimmed && attachments.length === 0) || disabled || submittingRef.current) return;
      submittingRef.current = true;
      autocomplete.close();
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

    const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      autocomplete.onKeyDown(e);
      if (e.defaultPrevented) return;
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        if (!text.trim() && attachments.length === 0 && onEmptySubmit) {
          onEmptySubmit();
          return;
        }
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

    const hasToggles = worktree !== undefined || onPlanModeChange || autoApproveMode !== undefined;

    return (
      <div className="border-t p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] shrink-0">
        {/* Attachment previews */}
        {attachments.length > 0 && (
          <div className="flex gap-2 flex-wrap mb-2">
            {attachments.map((a) => (
              <div key={a.id} className="relative group">
                {isImage(a.mimeType) ? (
                  <button
                    type="button"
                    className="p-0 border-none bg-transparent cursor-pointer"
                    onClick={() => setLightboxSrc(a.previewUrl ?? a.dataUrl)}
                  >
                    <img
                      src={a.previewUrl ?? a.dataUrl}
                      alt={a.name}
                      className="h-16 w-16 object-cover rounded-md border"
                    />
                  </button>
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
                  className="absolute -top-1.5 -right-1.5 h-4 w-4 rounded-full bg-destructive text-destructive-foreground flex items-center justify-center max-md:opacity-100 opacity-0 group-hover:opacity-100 transition-opacity"
                >
                  <X className="h-2.5 w-2.5" />
                </button>
              </div>
            ))}
          </div>
        )}

        {/* Unified composer container */}
        <div className="relative">
          {autocomplete.isOpen && autocomplete.triggerType && (
            <AutocompletePopup
              items={autocomplete.items}
              selectedIndex={autocomplete.selectedIndex}
              triggerType={autocomplete.triggerType}
              onSelect={autocomplete.accept}
            />
          )}
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
              placeholder={
                placeholder ?? (isRunning ? "Queue a follow-up..." : "Send a message...")
              }
              className="w-full resize-none bg-transparent px-3 pt-3 pb-1 text-sm placeholder:text-muted-foreground focus:outline-none overflow-y-auto"
              rows={1}
              style={{ maxHeight: "200px" }}
              disabled={disabled}
            />

            {/* Bottom bar */}
            <div className="flex items-center justify-between px-2 pb-2">
              <div className="flex items-center gap-0.5 max-md:gap-1 max-md:overflow-x-auto max-md:flex-nowrap min-w-0">
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={disabled}
                  className="h-7 w-7 max-md:h-10 max-md:w-10 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/80 flex items-center justify-center transition-colors disabled:opacity-40 cursor-pointer"
                  aria-label="Attach files"
                >
                  <Paperclip className="h-3.5 w-3.5" />
                </button>

                {hasToggles && <div className="w-px h-4 bg-border mx-1 shrink-0" />}

                {worktree !== undefined &&
                  (onWorktreeChange ? (
                    <button
                      type="button"
                      onClick={() => onWorktreeChange(!worktree)}
                      className={cn(
                        "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 transition-colors shrink-0 cursor-pointer",
                        worktree ? "bg-primary/10 text-primary" : "bg-orange/10 text-orange",
                      )}
                    >
                      {worktree ? (
                        <GitBranch className="h-3 w-3" />
                      ) : (
                        <FolderOpen className="h-3 w-3" />
                      )}
                      {worktree ? "Worktree" : "Local"}
                    </button>
                  ) : (
                    <span
                      className={cn(
                        "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 shrink-0",
                        worktree ? "text-primary" : "text-orange",
                      )}
                    >
                      {worktree ? (
                        <GitBranch className="h-3 w-3" />
                      ) : (
                        <FolderOpen className="h-3 w-3" />
                      )}
                      {worktree ? "Worktree" : "Local"}
                    </span>
                  ))}
                {onPlanModeChange && (
                  <button
                    type="button"
                    onClick={() => onPlanModeChange(!planMode)}
                    disabled={isRunning}
                    className={cn(
                      "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 transition-colors shrink-0 cursor-pointer",
                      planMode ? "bg-warning/10 text-warning" : "bg-primary/10 text-primary",
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
                {autoApproveMode !== undefined &&
                  (() => {
                    const mode = autoApproveMode ?? "manual";
                    const icon =
                      mode === "fullAuto" ? (
                        <ShieldAlert className="h-3 w-3" />
                      ) : (
                        <ShieldCheck className="h-3 w-3" />
                      );
                    return onAutoApproveModeChange ? (
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          className={cn(
                            "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 transition-colors shrink-0 cursor-pointer",
                            "hover:text-foreground hover:bg-muted/80 focus-visible:outline-none",
                            PERMISSION_COLORS[mode],
                            PERMISSION_BG[mode],
                          )}
                        >
                          {icon}
                          {PERMISSION_LABELS[mode]}
                          <ChevronDown className="h-3 w-3" />
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="start" className="min-w-[16rem]">
                          {PERMISSION_MODES.map((m) => (
                            <DropdownMenuItem
                              key={m}
                              onClick={() => onAutoApproveModeChange(m)}
                              className="text-xs gap-2 items-start"
                            >
                              <Check
                                className={cn(
                                  "h-3 w-3 mt-0.5",
                                  m === mode ? "opacity-100" : "opacity-0",
                                )}
                              />
                              <div className="flex flex-col gap-0.5">
                                <span
                                  className={cn("flex items-center gap-1", PERMISSION_COLORS[m])}
                                >
                                  {m === "fullAuto" ? (
                                    <ShieldAlert className="h-3 w-3" />
                                  ) : (
                                    <ShieldCheck className="h-3 w-3" />
                                  )}
                                  {PERMISSION_LABELS[m]}
                                </span>
                                <span className="text-[10px] text-muted-foreground">
                                  {PERMISSION_DESCRIPTIONS[m]}
                                </span>
                              </div>
                            </DropdownMenuItem>
                          ))}
                        </DropdownMenuContent>
                      </DropdownMenu>
                    ) : (
                      <span
                        className={cn(
                          "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 shrink-0",
                          PERMISSION_COLORS[mode],
                        )}
                      >
                        {icon}
                        {PERMISSION_LABELS[mode]}
                      </span>
                    );
                  })()}

                {(effort !== undefined || model) && (
                  <div className="w-px h-4 bg-border mx-1 shrink-0" />
                )}

                {model &&
                  (onModelChange ? (
                    <DropdownMenu>
                      <DropdownMenuTrigger
                        className={cn(
                          "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 transition-colors shrink-0 cursor-pointer",
                          "text-muted-foreground hover:text-foreground hover:bg-muted/80",
                          "focus-visible:outline-none",
                        )}
                      >
                        {MODEL_LABELS[model]}
                        <ChevronDown className="h-3 w-3" />
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="start">
                        {MODELS.map((m) => (
                          <DropdownMenuItem
                            key={m}
                            onClick={() => onModelChange(m)}
                            className="text-xs gap-2"
                          >
                            <Check
                              className={cn("h-3 w-3", m === model ? "opacity-100" : "opacity-0")}
                            />
                            {MODEL_LABELS[m]}
                          </DropdownMenuItem>
                        ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  ) : (
                    <span className="flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 text-muted-foreground shrink-0">
                      {MODEL_LABELS[model]}
                    </span>
                  ))}

                {effort !== undefined &&
                  (onEffortChange ? (
                    <DropdownMenu>
                      <DropdownMenuTrigger
                        className={cn(
                          "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 transition-colors shrink-0 cursor-pointer",
                          "hover:text-foreground hover:bg-muted/80 focus-visible:outline-none",
                          EFFORT_COLORS[effort],
                        )}
                      >
                        <Gauge className="h-3 w-3" />
                        {EFFORT_LABELS[effort]}
                        <ChevronDown className="h-3 w-3" />
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="start">
                        {EFFORT_LEVELS.map((lvl) => (
                          <DropdownMenuItem
                            key={lvl || "auto"}
                            onClick={() => onEffortChange(lvl)}
                            className="text-xs gap-2"
                          >
                            <Check
                              className={cn(
                                "h-3 w-3",
                                lvl === effort ? "opacity-100" : "opacity-0",
                              )}
                            />
                            <span className={EFFORT_COLORS[lvl]}>{EFFORT_LABELS[lvl]}</span>
                          </DropdownMenuItem>
                        ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  ) : (
                    <span
                      className={cn(
                        "flex items-center gap-1 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 shrink-0",
                        EFFORT_COLORS[effort],
                      )}
                    >
                      <Gauge className="h-3 w-3" />
                      {EFFORT_LABELS[effort]}
                    </span>
                  ))}
              </div>

              <div className="flex items-center gap-1">
                {isRunning && (
                  <button
                    type="button"
                    onClick={onInterrupt}
                    className="h-8 w-8 max-md:h-10 max-md:w-10 rounded-lg text-destructive hover:bg-destructive/10 flex items-center justify-center transition-colors cursor-pointer"
                    aria-label="Stop"
                  >
                    <Square className="h-3.5 w-3.5" />
                  </button>
                )}
                <button
                  type="button"
                  onClick={handleSend}
                  disabled={disabled || (!text.trim() && attachments.length === 0)}
                  className="h-8 w-8 max-md:h-10 max-md:w-10 rounded-lg bg-primary text-primary-foreground flex items-center justify-center transition-colors hover:bg-primary/90 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
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
        </div>

        <input
          ref={fileInputRef}
          type="file"
          accept={ACCEPTED_TYPES}
          multiple
          className="hidden"
          onChange={handleFileInput}
        />

        {lightboxSrc &&
          createPortal(
            <dialog
              open
              className="fixed inset-0 z-50 bg-black/80 flex items-center justify-center cursor-pointer m-0 p-0 border-none max-w-none max-h-none w-screen h-screen"
              onClick={() => setLightboxSrc(null)}
              onKeyDown={(e) => {
                if (e.key === "Escape") setLightboxSrc(null);
              }}
              aria-label="Image preview"
            >
              <img
                src={lightboxSrc}
                alt="Full-size preview"
                className="max-h-[90vh] max-w-[90vw] object-contain rounded-lg"
              />
            </dialog>,
            document.body,
          )}
      </div>
    );
  },
);
