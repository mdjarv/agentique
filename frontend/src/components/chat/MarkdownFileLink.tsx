import { Check, Copy, ExternalLink, Loader2 } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { Markdown } from "~/components/chat/Markdown";
import { Button } from "~/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "~/components/ui/dialog";
import { ScrollArea } from "~/components/ui/scroll-area";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";

interface MarkdownFileLinkProps {
  href: string;
  children: ReactNode;
}

function fileNameFromHref(href: string): string {
  try {
    const path = new URL(href, window.location.origin).pathname;
    const parts = path.split("/").filter(Boolean);
    return parts[parts.length - 1] ?? href;
  } catch {
    return href;
  }
}

export function MarkdownFileLink({ href, children }: MarkdownFileLinkProps) {
  const [open, setOpen] = useState(false);
  const [content, setContent] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const { copied, copy } = useCopyToClipboard();
  const name = fileNameFromHref(href);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    setContent(null);

    fetch(href)
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
        return res.text();
      })
      .then((text) => {
        if (!cancelled) {
          setContent(text);
          setLoading(false);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load file");
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [open, href]);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="text-primary underline underline-offset-2 hover:no-underline cursor-pointer bg-transparent border-0 p-0 font-inherit"
      >
        {children}
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          className="sm:max-w-4xl p-0 gap-0 max-h-[85vh] flex flex-col"
          showCloseButton={false}
        >
          <div className="flex items-center gap-2 px-4 py-2 border-b shrink-0">
            <DialogTitle className="font-mono text-sm truncate flex-1">{name}</DialogTitle>
            <DialogDescription className="sr-only">
              Rendered preview of {name}. Use the Raw button to view source.
            </DialogDescription>
            {content !== null && (
              <Button variant="ghost" size="sm" onClick={() => copy(content)} className="shrink-0">
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                <span className="ml-1.5">{copied ? "Copied" : "Copy"}</span>
              </Button>
            )}
            <Button variant="ghost" size="sm" asChild className="shrink-0">
              <a href={href} target="_blank" rel="noopener noreferrer">
                <ExternalLink className="h-3.5 w-3.5" />
                <span className="ml-1.5">Raw</span>
              </a>
            </Button>
          </div>

          <ScrollArea className="flex-1">
            {loading && (
              <div className="flex h-40 items-center justify-center">
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              </div>
            )}
            {error && <p className="p-4 text-sm text-destructive">{error}</p>}
            {content !== null && (
              <div className="p-4">
                <Markdown content={content} />
              </div>
            )}
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </>
  );
}
