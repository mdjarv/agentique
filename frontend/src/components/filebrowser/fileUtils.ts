import { File, FileCode, FileText, Folder, Image } from "lucide-react";

const IMAGE_EXTS = new Set([".jpg", ".jpeg", ".png", ".gif", ".svg", ".webp", ".ico", ".bmp"]);

const MARKDOWN_EXTS = new Set([".md", ".mdx"]);

const TEXT_EXTS = new Set([
  ".txt",
  ".log",
  ".csv",
  ".tsv",
  ".env",
  ".env.local",
  ".gitignore",
  ".gitattributes",
  ".editorconfig",
  ".dockerignore",
  ".prettierrc",
  ".eslintrc",
  ".npmrc",
]);

const CODE_EXTS: Record<string, string> = {
  ".ts": "typescript",
  ".tsx": "tsx",
  ".js": "javascript",
  ".jsx": "jsx",
  ".go": "go",
  ".py": "python",
  ".rs": "rust",
  ".rb": "ruby",
  ".java": "java",
  ".kt": "kotlin",
  ".c": "c",
  ".cpp": "cpp",
  ".h": "c",
  ".hpp": "cpp",
  ".cs": "csharp",
  ".swift": "swift",
  ".php": "php",
  ".lua": "lua",
  ".sh": "bash",
  ".bash": "bash",
  ".zsh": "bash",
  ".fish": "bash",
  ".sql": "sql",
  ".json": "json",
  ".yaml": "yaml",
  ".yml": "yaml",
  ".toml": "toml",
  ".xml": "xml",
  ".html": "html",
  ".htm": "html",
  ".css": "css",
  ".scss": "scss",
  ".less": "less",
  ".graphql": "graphql",
  ".gql": "graphql",
  ".proto": "protobuf",
  ".dockerfile": "docker",
  ".tf": "hcl",
  ".hcl": "hcl",
  ".zig": "zig",
  ".vim": "vim",
  ".nix": "nix",
};

function ext(name: string): string {
  const i = name.lastIndexOf(".");
  return i >= 0 ? name.slice(i).toLowerCase() : "";
}

export function isImageFile(name: string): boolean {
  return IMAGE_EXTS.has(ext(name));
}

export function isMarkdownFile(name: string): boolean {
  return MARKDOWN_EXTS.has(ext(name));
}

export function isTextFile(name: string): boolean {
  const e = ext(name);
  return TEXT_EXTS.has(e) || MARKDOWN_EXTS.has(e) || e in CODE_EXTS;
}

export function getLanguageFromExtension(name: string): string | undefined {
  return CODE_EXTS[ext(name)];
}

export function getFileIcon(name: string, isDir: boolean) {
  if (isDir) return Folder;
  const e = ext(name);
  if (IMAGE_EXTS.has(e)) return Image;
  if (e in CODE_EXTS) return FileCode;
  if (MARKDOWN_EXTS.has(e) || TEXT_EXTS.has(e)) return FileText;
  return File;
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

/** Special filenames that should be treated as text even without a recognized extension. */
const SPECIAL_TEXT_NAMES = new Set([
  "Dockerfile",
  "Makefile",
  "Justfile",
  "justfile",
  "Vagrantfile",
  "Gemfile",
  "Rakefile",
  "Procfile",
  "LICENSE",
  "CHANGELOG",
  "CLAUDE.md",
]);

export function isPreviewable(name: string): boolean {
  return isTextFile(name) || isImageFile(name) || SPECIAL_TEXT_NAMES.has(name);
}

export function getLanguageForSpecialFile(name: string): string | undefined {
  if (name === "Dockerfile") return "docker";
  if (name === "Makefile" || name === "Justfile" || name === "justfile") return "makefile";
  return undefined;
}
