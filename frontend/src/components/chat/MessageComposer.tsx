import {
	FileText,
	GitBranch,
	Paperclip,
	SendHorizonal,
	Square,
	X,
} from "lucide-react";
import { useRef, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
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

interface MessageComposerProps {
	onSend: (prompt: string, attachments?: Attachment[]) => void;
	disabled: boolean;
	isRunning?: boolean;
	onInterrupt?: () => void;
	isDraft?: boolean;
	worktree?: boolean;
	onWorktreeChange?: (value: boolean) => void;
}

export function MessageComposer({
	onSend,
	disabled,
	isRunning,
	onInterrupt,
	isDraft,
	worktree,
	onWorktreeChange,
}: MessageComposerProps) {
	const [text, setText] = useState("");
	const [attachments, setAttachments] = useState<Attachment[]>([]);
	const textareaRef = useRef<HTMLTextAreaElement>(null);
	const fileInputRef = useRef<HTMLInputElement>(null);

	const handleSend = () => {
		const trimmed = text.trim();
		if ((!trimmed && attachments.length === 0) || disabled) return;
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
					previewUrl: isImage(file.type)
						? URL.createObjectURL(file)
						: undefined,
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

	const removeAttachment = (id: string) => {
		setAttachments((prev) => {
			const a = prev.find((i) => i.id === id);
			if (a?.previewUrl) URL.revokeObjectURL(a.previewUrl);
			return prev.filter((i) => i.id !== id);
		});
	};

	return (
		<div className="border-t p-4 space-y-2">
			{isDraft && (
				<div className="flex items-center gap-2">
					<button
						type="button"
						onClick={() => onWorktreeChange?.(!worktree)}
						className={cn(
							"flex items-center gap-1.5 text-xs rounded-full px-2.5 py-1 border transition-colors",
							worktree
								? "bg-primary/10 border-primary/30 text-primary"
								: "bg-muted border-transparent text-muted-foreground hover:border-border",
						)}
					>
						<GitBranch className="h-3 w-3" />
						{worktree ? "Worktree" : "Local"}
					</button>
				</div>
			)}
			{attachments.length > 0 && (
				<div className="flex gap-2 flex-wrap">
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
			<div className="flex gap-3 items-end">
				<input
					ref={fileInputRef}
					type="file"
					accept={ACCEPTED_TYPES}
					multiple
					className="hidden"
					onChange={handleFileInput}
				/>
				<Button
					variant="ghost"
					size="icon"
					className="shrink-0"
					onClick={() => fileInputRef.current?.click()}
					disabled={disabled}
					aria-label="Attach files"
				>
					<Paperclip className="h-4 w-4" />
				</Button>
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
					placeholder="Send a message..."
					className="flex-1 resize-none rounded-md border bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 overflow-y-auto"
					rows={1}
					style={{ maxHeight: "200px" }}
					disabled={disabled}
				/>
				{isRunning ? (
					<Button size="icon" variant="destructive" onClick={onInterrupt}>
						<Square className="h-4 w-4" />
					</Button>
				) : (
					<Button
						size="icon"
						onClick={handleSend}
						disabled={disabled || (!text.trim() && attachments.length === 0)}
					>
						<SendHorizonal className="h-4 w-4" />
					</Button>
				)}
			</div>
		</div>
	);
}
