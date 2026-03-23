import {
	Bot,
	Brain,
	Check,
	ChevronDown,
	ChevronRight,
	Copy,
	FileText,
	Loader2,
	Terminal,
	User,
} from "lucide-react";
import { useCallback, useState } from "react";
import { Markdown } from "~/components/chat/Markdown";
import { ThinkingBlock } from "~/components/chat/ThinkingBlock";
import { ToolResultBlock } from "~/components/chat/ToolResultBlock";
import { ToolUseBlock } from "~/components/chat/ToolUseBlock";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { copyToClipboard } from "~/lib/utils";
import type { ChatEvent, Turn } from "~/stores/chat-store";

interface TurnBlockProps {
	turn: Turn;
	isLast: boolean;
	currentAssistantText: string;
	sessionState: string;
	projectPath?: string;
	worktreePath?: string;
}

function CollapsibleGroup({
	label,
	icon,
	count,
	defaultExpanded,
	children,
}: {
	label: string;
	icon: React.ReactNode;
	count: number;
	defaultExpanded: boolean;
	children: React.ReactNode;
}) {
	const [expanded, setExpanded] = useState(defaultExpanded);
	return (
		<div className="border rounded-md bg-muted/30 overflow-hidden">
			<button
				type="button"
				onClick={() => setExpanded(!expanded)}
				className="flex items-center gap-2 px-2 py-1.5 text-xs text-muted-foreground w-full text-left hover:bg-muted/50 cursor-pointer transition-colors"
			>
				{expanded ? (
					<ChevronDown className="h-3 w-3 shrink-0" />
				) : (
					<ChevronRight className="h-3 w-3 shrink-0" />
				)}
				{icon}
				<span>
					{count} {label}
				</span>
			</button>
			{expanded && <div className="space-y-1 p-1 pt-0">{children}</div>}
		</div>
	);
}

export function TurnBlock({
	turn,
	isLast,
	currentAssistantText,
	sessionState,
	projectPath,
	worktreePath,
}: TurnBlockProps) {
	const [copied, setCopied] = useState(false);
	const isStreaming = isLast && !turn.complete;

	const handleCopy = useCallback((text: string) => {
		copyToClipboard(text).then(() => {
			setCopied(true);
			setTimeout(() => setCopied(false), 1500);
		});
	}, []);

	const textContent = isStreaming
		? currentAssistantText
		: turn.events
				.filter((e) => e.type === "text")
				.map((e) => e.content ?? "")
				.join("");

	const thinkingEvents = turn.events.filter((e) => e.type === "thinking");
	const toolUseEvents = turn.events.filter((e) => e.type === "tool_use");
	const toolResultEvents = turn.events.filter((e) => e.type === "tool_result");
	const resultEvent = turn.events.find((e) => e.type === "result");
	const errorEvents = turn.events.filter((e) => e.type === "error");

	const hasAssistantContent =
		textContent ||
		thinkingEvents.length > 0 ||
		toolUseEvents.length > 0 ||
		errorEvents.length > 0 ||
		isStreaming;

	const renderToolPair = (toolUse: ChatEvent) => {
		const result = toolResultEvents.find((r) => r.toolId === toolUse.toolId);
		return (
			<div key={toolUse.id} className="space-y-1">
				<ToolUseBlock
					name={toolUse.toolName ?? "Unknown"}
					input={toolUse.toolInput}
					projectPath={projectPath}
					worktreePath={worktreePath}
				/>
				{result && <ToolResultBlock content={result.content ?? ""} />}
			</div>
		);
	};

	const renderThinkingBlocks = () => {
		if (thinkingEvents.length === 0) return null;
		if (thinkingEvents.length === 1) {
			return <ThinkingBlock content={thinkingEvents[0]?.content ?? ""} />;
		}
		return (
			<CollapsibleGroup
				label="thinking blocks"
				icon={<Brain className="h-3 w-3" />}
				count={thinkingEvents.length}
				defaultExpanded={false}
			>
				{thinkingEvents.map((e) => (
					<ThinkingBlock key={e.id} content={e.content ?? ""} />
				))}
			</CollapsibleGroup>
		);
	};

	const renderToolCalls = () => {
		if (toolUseEvents.length === 0) return null;
		if (toolUseEvents.length <= 3) {
			return <>{toolUseEvents.map(renderToolPair)}</>;
		}
		return (
			<CollapsibleGroup
				label="tool calls"
				icon={<Terminal className="h-3 w-3" />}
				count={toolUseEvents.length}
				defaultExpanded={isStreaming}
			>
				{toolUseEvents.map(renderToolPair)}
			</CollapsibleGroup>
		);
	};

	return (
		<div className="space-y-3">
			{/* User message */}
			<div className="flex gap-3 flex-row-reverse">
				<Avatar className="h-8 w-8 shrink-0">
					<AvatarFallback className="bg-primary text-primary-foreground">
						<User className="h-4 w-4" />
					</AvatarFallback>
				</Avatar>
				<div className="max-w-[75%] rounded-lg px-4 py-2 bg-primary text-primary-foreground">
					{turn.attachments && turn.attachments.length > 0 && (
						<div className="flex gap-1.5 flex-wrap mb-2">
							{turn.attachments.map((a) =>
								a.mimeType.startsWith("image/") ? (
									<img
										key={a.id}
										src={a.previewUrl ?? a.dataUrl}
										alt={a.name}
										className="h-20 max-w-[200px] object-cover rounded"
									/>
								) : (
									<div
										key={a.id}
										className="h-20 w-20 rounded bg-primary-foreground/10 flex flex-col items-center justify-center gap-1 px-1"
									>
										<FileText className="h-5 w-5" />
										<span className="text-[9px] truncate w-full text-center">
											{a.name}
										</span>
									</div>
								),
							)}
						</div>
					)}
					{turn.prompt && (
						<p className="text-sm whitespace-pre-wrap">{turn.prompt}</p>
					)}
				</div>
			</div>

			{/* Assistant response */}
			{hasAssistantContent && (
				<div className="flex gap-3">
					<Avatar className="h-8 w-8 shrink-0">
						<AvatarFallback className="bg-muted">
							<Bot className="h-4 w-4" />
						</AvatarFallback>
					</Avatar>
					<div className="flex-1 space-y-2 max-w-[85%] min-w-0 overflow-hidden">
						{/* Thinking blocks */}
						{renderThinkingBlocks()}

						{/* Tool use/result pairs */}
						{renderToolCalls()}

						{/* Streaming indicator */}
						{isStreaming &&
							!textContent &&
							thinkingEvents.length === 0 &&
							toolUseEvents.length === 0 && (
								<div className="flex items-center gap-2 text-muted-foreground text-sm px-1">
									<Loader2 className="h-3.5 w-3.5 animate-spin" />
									<span>
										{sessionState === "running"
											? "Working..."
											: "Connecting..."}
									</span>
								</div>
							)}

						{isStreaming &&
							(toolUseEvents.length > 0 || thinkingEvents.length > 0) &&
							!textContent && (
								<div className="flex items-center gap-2 text-muted-foreground/60 text-xs px-1">
									<Loader2 className="h-3 w-3 animate-spin" />
								</div>
							)}

						{/* Text content */}
						{textContent && (
							<div className="relative group/msg rounded-lg px-4 py-2 bg-muted">
								<button
									type="button"
									onClick={() => handleCopy(textContent)}
									className="absolute top-2 right-2 p-1 rounded opacity-0 group-hover/msg:opacity-100 hover:bg-background/50 text-muted-foreground transition-opacity"
									aria-label="Copy message"
								>
									{copied ? (
										<Check className="h-3.5 w-3.5" />
									) : (
										<Copy className="h-3.5 w-3.5" />
									)}
								</button>
								<Markdown content={textContent} />
							</div>
						)}

						{/* Streaming indicator after text while still working */}
						{isStreaming && textContent && (
							<div className="flex items-center gap-2 text-muted-foreground/60 text-xs px-1">
								<Loader2 className="h-3 w-3 animate-spin" />
							</div>
						)}

						{/* Error events */}
						{errorEvents.map((e) => (
							<div
								key={e.id}
								className="rounded-lg px-4 py-2 bg-destructive/10 text-destructive text-sm"
							>
								{e.content}
							</div>
						))}

						{/* Result metadata — duration only */}
						{resultEvent &&
							resultEvent.duration != null &&
							resultEvent.duration > 0 && (
								<div className="text-xs text-muted-foreground">
									{(resultEvent.duration / 1000).toFixed(1)}s
								</div>
							)}
					</div>
				</div>
			)}
		</div>
	);
}
