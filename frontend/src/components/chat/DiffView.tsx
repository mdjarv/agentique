import {
	ChevronDown,
	ChevronRight,
	FileMinus,
	FilePlus,
	FileSymlink,
	FileText,
} from "lucide-react";
import { useState } from "react";
import type { DiffResult } from "~/lib/session-actions";

interface DiffViewProps {
	result: DiffResult;
}

function statusIcon(status: string) {
	switch (status) {
		case "added":
			return <FilePlus className="h-3.5 w-3.5 text-green-500" />;
		case "deleted":
			return <FileMinus className="h-3.5 w-3.5 text-red-500" />;
		case "renamed":
			return <FileSymlink className="h-3.5 w-3.5 text-blue-500" />;
		default:
			return <FileText className="h-3.5 w-3.5 text-yellow-500" />;
	}
}

function totalStats(result: DiffResult) {
	let ins = 0;
	let del = 0;
	for (const f of result.files) {
		ins += f.insertions;
		del += f.deletions;
	}
	return { ins, del };
}

function extractFileDiff(fullDiff: string, path: string): string {
	const marker = `diff --git a/${path} b/${path}`;
	const start = fullDiff.indexOf(marker);
	if (start === -1) return "";
	const nextDiff = fullDiff.indexOf("\ndiff --git ", start + marker.length);
	if (nextDiff === -1) return fullDiff.slice(start);
	return fullDiff.slice(start, nextDiff);
}

function classifyLine(line: string): string {
	if (line.startsWith("+") && !line.startsWith("+++")) {
		return "px-3 bg-green-500/10 text-green-400";
	}
	if (line.startsWith("-") && !line.startsWith("---")) {
		return "px-3 bg-red-500/10 text-red-400";
	}
	if (line.startsWith("@@")) {
		return "px-3 text-blue-400";
	}
	return "px-3 text-muted-foreground";
}

function DiffLines({ text }: { text: string }) {
	const lines = text.split("\n");
	return (
		<pre className="text-xs leading-relaxed overflow-x-auto">
			{lines.map((line, idx) => (
				<div key={`${idx}:${line.slice(0, 20)}`} className={classifyLine(line)}>
					{line}
				</div>
			))}
		</pre>
	);
}

function FileEntry({
	path,
	insertions,
	deletions,
	status,
	diff,
}: {
	path: string;
	insertions: number;
	deletions: number;
	status: string;
	diff: string;
}) {
	const [expanded, setExpanded] = useState(false);
	const hasDiff = diff.length > 0;

	return (
		<div className="border-b last:border-b-0">
			<button
				type="button"
				onClick={() => hasDiff && setExpanded(!expanded)}
				className={`flex items-center gap-2 px-3 py-1.5 text-xs w-full text-left ${hasDiff ? "hover:bg-muted/80 cursor-pointer" : ""} transition-colors`}
			>
				{hasDiff ? (
					expanded ? (
						<ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" />
					) : (
						<ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />
					)
				) : (
					<span className="w-3 shrink-0" />
				)}
				{statusIcon(status)}
				<span className="font-mono truncate min-w-0">{path}</span>
				<span className="ml-auto flex items-center gap-2 shrink-0 text-xs">
					{insertions > 0 && (
						<span className="text-green-500">+{insertions}</span>
					)}
					{deletions > 0 && <span className="text-red-500">-{deletions}</span>}
				</span>
			</button>
			{expanded && (
				<div className="border-t bg-muted/30 max-h-80 overflow-y-auto">
					<DiffLines text={diff} />
				</div>
			)}
		</div>
	);
}

export function DiffView({ result }: DiffViewProps) {
	if (!result.hasDiff) {
		return (
			<div className="px-4 py-3 text-sm text-muted-foreground">
				No changes detected.
			</div>
		);
	}

	const { ins, del } = totalStats(result);

	return (
		<div className="border-t">
			{result.truncated && (
				<div className="px-4 py-2 text-xs text-yellow-500 bg-yellow-500/10 border-b">
					Diff too large, showing summary only.
				</div>
			)}
			<div className="px-4 py-2 text-xs text-muted-foreground border-b">
				{result.files.length} file{result.files.length !== 1 ? "s" : ""} changed
				{ins > 0 && (
					<span className="text-green-500">
						, {ins} insertion{ins !== 1 ? "s" : ""}(+)
					</span>
				)}
				{del > 0 && (
					<span className="text-red-500">
						, {del} deletion{del !== 1 ? "s" : ""}(-)
					</span>
				)}
			</div>
			<div>
				{result.files.map((file) => (
					<FileEntry
						key={file.path}
						path={file.path}
						insertions={file.insertions}
						deletions={file.deletions}
						status={file.status}
						diff={extractFileDiff(result.diff, file.path)}
					/>
				))}
			</div>
		</div>
	);
}
