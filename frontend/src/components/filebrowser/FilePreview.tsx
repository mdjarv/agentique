import { Check, Copy, Loader2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import { Markdown } from "~/components/chat/Markdown";
import { Button } from "~/components/ui/button";
import { ScrollArea } from "~/components/ui/scroll-area";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import { fileContentUrl, getFileContent } from "~/lib/api";
import {
  getLanguageForSpecialFile,
  getLanguageFromExtension,
  isImageFile,
  isMarkdownFile,
  isPreviewable,
} from "./fileUtils";

interface FilePreviewProps {
  projectId: string;
  filePath: string;
  onClose: () => void;
  /** Hide the built-in header (used when the parent provides its own). */
  hideHeader?: boolean;
}

function fileName(path: string): string {
  const parts = path.split("/");
  return parts[parts.length - 1] ?? path;
}

export function FilePreview({ projectId, filePath, onClose, hideHeader }: FilePreviewProps) {
  const name = fileName(filePath);
  const isImage = isImageFile(name);
  const isMd = isMarkdownFile(name);
  const canPreview = isPreviewable(name);

  const [content, setContent] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const { copied, copy } = useCopyToClipboard();

  useEffect(() => {
    if (isImage || !canPreview) return;

    let cancelled = false;
    setLoading(true);
    setError("");
    setContent(null);

    getFileContent(projectId, filePath)
      .then((text) => {
        if (!cancelled) {
          setContent(text);
          setLoading(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err.message);
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [projectId, filePath, isImage, canPreview]);

  const lang = getLanguageFromExtension(name) ?? getLanguageForSpecialFile(name);

  return (
    <div className="flex flex-col h-full">
      {/* Header — shown in desktop split view */}
      {!hideHeader && (
        <div className="flex items-center gap-2 px-4 py-2 border-b shrink-0">
          <span className="font-mono text-sm truncate flex-1">{filePath}</span>
          {content !== null && (
            <Button variant="ghost" size="sm" onClick={() => copy(content)} className="shrink-0">
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              <span className="ml-1.5">{copied ? "Copied" : "Copy"}</span>
            </Button>
          )}
          <Button variant="ghost" size="icon-sm" onClick={onClose} className="shrink-0">
            <X className="h-4 w-4" />
          </Button>
        </div>
      )}

      {/* Copy bar for mobile (no close button, just copy) */}
      {hideHeader && content !== null && (
        <div className="flex items-center justify-end px-4 py-1.5 border-b shrink-0">
          <Button variant="ghost" size="sm" onClick={() => copy(content)}>
            {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            <span className="ml-1.5">{copied ? "Copied" : "Copy"}</span>
          </Button>
        </div>
      )}

      {/* Content */}
      <ScrollArea className="flex-1">
        {loading && (
          <div className="flex h-40 items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        )}

        {error && <p className="p-4 text-sm text-destructive">{error}</p>}

        {!canPreview && (
          <div className="p-8 text-center text-sm text-muted-foreground">
            Preview not available for this file type
          </div>
        )}

        {isImage && (
          <div className="p-4 flex items-center justify-center">
            <img
              src={fileContentUrl(projectId, filePath)}
              alt={name}
              className="max-w-full max-h-[70vh] object-contain rounded"
            />
          </div>
        )}

        {isMd && content !== null && (
          <div className="p-4">
            <Markdown content={content} />
          </div>
        )}

        {!isMd && !isImage && content !== null && (
          <SyntaxHighlighter
            style={oneDark}
            language={lang ?? "text"}
            customStyle={{ margin: 0, fontSize: "0.75rem", borderRadius: 0, minHeight: "100%" }}
            showLineNumbers
          >
            {content}
          </SyntaxHighlighter>
        )}
      </ScrollArea>
    </div>
  );
}
