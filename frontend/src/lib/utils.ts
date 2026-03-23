import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

export function uuid(): string {
	if (
		typeof crypto !== "undefined" &&
		typeof crypto.randomUUID === "function"
	) {
		return crypto.randomUUID();
	}
	const bytes = new Uint8Array(16);
	(globalThis.crypto ?? window.crypto).getRandomValues(bytes);
	bytes.set([((bytes[6] ?? 0) & 0x0f) | 0x40], 6);
	bytes.set([((bytes[8] ?? 0) & 0x3f) | 0x80], 8);
	const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join(
		"",
	);
	return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export function readFileAsDataUrl(file: File): Promise<string> {
	return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.addEventListener("load", () => {
			if (typeof reader.result === "string") {
				resolve(reader.result);
				return;
			}
			reject(new Error("Could not read image data."));
		});
		reader.addEventListener("error", () => {
			reject(reader.error ?? new Error("Failed to read image."));
		});
		reader.readAsDataURL(file);
	});
}

export function relativeTime(iso: string): string {
	const diff = Date.now() - new Date(iso).getTime();
	const mins = Math.floor(diff / 60000);
	if (mins < 1) return "now";
	if (mins < 60) return `${mins}m`;
	const hours = Math.floor(mins / 60);
	if (hours < 24) return `${hours}h`;
	return `${Math.floor(hours / 24)}d`;
}

export function copyToClipboard(text: string): Promise<void> {
	if (navigator.clipboard?.writeText) {
		return navigator.clipboard.writeText(text).catch(() => fallbackCopy(text));
	}
	return fallbackCopy(text);
}

function fallbackCopy(text: string): Promise<void> {
	const ta = document.createElement("textarea");
	ta.value = text;
	ta.style.position = "fixed";
	ta.style.opacity = "0";
	document.body.appendChild(ta);
	ta.select();
	document.execCommand("copy");
	document.body.removeChild(ta);
	return Promise.resolve();
}
