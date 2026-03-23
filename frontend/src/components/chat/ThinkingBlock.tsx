import { Brain, ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";

interface ThinkingBlockProps {
	content: string;
}

export function ThinkingBlock({ content }: ThinkingBlockProps) {
	const [expanded, setExpanded] = useState(false);

	return (
		<div className="border rounded-md bg-muted/50">
			<button
				type="button"
				onClick={() => setExpanded(!expanded)}
				className="flex items-center gap-2 p-2 text-xs text-muted-foreground w-full text-left hover:bg-muted/80 transition-colors"
			>
				{expanded ? (
					<ChevronDown className="h-3 w-3" />
				) : (
					<ChevronRight className="h-3 w-3" />
				)}
				<Brain className="h-3 w-3" />
				Thinking...
			</button>
			{expanded && (
				<div className="px-3 pb-2 text-xs text-muted-foreground italic whitespace-pre-wrap">
					{content}
				</div>
			)}
		</div>
	);
}
