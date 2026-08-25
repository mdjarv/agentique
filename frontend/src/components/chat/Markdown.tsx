import { Check, Copy } from "lucide-react";
import {
  Children,
  type ComponentPropsWithoutRef,
  isValidElement,
  memo,
  type ReactNode,
  useEffect,
  useMemo,
  useState,
} from "react";
import type { Components } from "react-markdown";
import ReactMarkdown from "react-markdown";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import { BrainCard } from "~/components/chat/BrainCard";
import { ImageLightbox } from "~/components/chat/ImageLightbox";
import { MarkdownFileLink } from "~/components/chat/MarkdownFileLink";
import { MermaidDiagram } from "~/components/chat/MermaidDiagram";
import { PromptCard, splitByPromptBlocks } from "~/components/chat/PromptCard";
import { RunBlockButton } from "~/components/chat/RunBlockButton";
import { useSessionMachineId } from "~/components/chat/SessionMachineContext";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import { useTheme } from "~/hooks/useTheme";
import {
  apiFetch,
  rewriteRemoteLocalhost,
  sessionFileMachineId,
  sessionFilePath,
} from "~/lib/machines/api";
import { getSyntaxTheme } from "~/lib/syntax-theme";
import { cn } from "~/lib/utils";

interface MarkdownProps {
  content: string;
  className?: string;
  /** Convert single newlines to <br> (useful for user-typed messages). */
  preserveNewlines?: boolean;
  /** True while this message is still streaming. Gates prompt-block recovery:
   *  when false (the default), a malformed/missing closer is recovered into a
   *  card; when true, an unclosed block stays a pending placeholder. */
  isStreaming?: boolean;
}

function nodeToPlainText(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(nodeToPlainText).join("");
  if (isValidElement<{ children?: ReactNode }>(node)) return nodeToPlainText(node.props.children);
  return "";
}

function CopyButton({ text }: { text: string }) {
  const { copied, copy } = useCopyToClipboard();

  return (
    <button
      type="button"
      className="code-copy-btn"
      onClick={() => copy(text)}
      aria-label={copied ? "Copied" : "Copy code"}
    >
      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
    </button>
  );
}

export const CODE_STYLE = { margin: 0, fontSize: "0.75rem", borderRadius: "0.5rem" } as const;

function DeferredHighlighter({ code, language }: { code: string; language: string }) {
  const [ready, setReady] = useState(false);
  const { resolvedTheme } = useTheme();

  useEffect(() => {
    const id = setTimeout(() => setReady(true), 0);
    return () => clearTimeout(id);
  }, []);

  if (!ready) {
    return (
      <pre style={{ ...CODE_STYLE, background: "var(--muted)", padding: "1em", overflow: "auto" }}>
        <code>{code}</code>
      </pre>
    );
  }

  return (
    <SyntaxHighlighter
      style={getSyntaxTheme(resolvedTheme)}
      language={language}
      customStyle={CODE_STYLE}
    >
      {code}
    </SyntaxHighlighter>
  );
}

function PreBlock({
  children,
  node: _,
  ...rest
}: ComponentPropsWithoutRef<"pre"> & { node?: unknown }) {
  const childArray = Children.toArray(children);
  const codeChild =
    childArray.length === 1 &&
    isValidElement<{ className?: string; children?: ReactNode }>(childArray[0]) &&
    ((childArray[0] as React.ReactElement<{ className?: string }>).type === "code" ||
      (childArray[0] as React.ReactElement<{ className?: string }>).type === InlineCode)
      ? (childArray[0] as React.ReactElement<{ className?: string; children?: ReactNode }>)
      : null;

  if (!codeChild) return <pre {...rest}>{children}</pre>;

  const lang = /language-(\w+)/.exec(codeChild.props.className ?? "")?.[1];
  const code = nodeToPlainText(codeChild.props.children).replace(/\n$/, "");

  const isMermaid = lang === "mermaid";

  return (
    <div className="code-block-wrapper">
      <div className="code-block-actions">
        {/* Mermaid renders as a diagram, not a runnable block. */}
        {!isMermaid && <RunBlockButton code={code} />}
        <CopyButton text={code} />
      </div>
      {isMermaid ? (
        <MermaidDiagram code={code} />
      ) : lang ? (
        <DeferredHighlighter code={code} language={lang} />
      ) : (
        <pre {...rest}>
          <code>{code}</code>
        </pre>
      )}
    </div>
  );
}

function PendingPromptCard({ title, content }: { title?: string; content: string }) {
  return (
    <div className="not-prose my-3 rounded-lg border border-border/40 border-l-[3px] border-l-primary bg-primary/[0.03]">
      <div className="px-4 py-3 space-y-2">
        <div className="flex items-center gap-2">
          {title ? (
            <div className="font-medium text-sm">{title}</div>
          ) : (
            <div className="h-4 w-32 rounded bg-muted animate-pulse" />
          )}
        </div>
        {content && (
          <div className="text-xs text-muted-foreground/80 leading-relaxed whitespace-pre-wrap">
            {content}
          </div>
        )}
        <div className="flex items-center justify-end gap-2 pt-0.5">
          <div className="h-6 w-24 rounded bg-muted/50 animate-pulse" />
        </div>
      </div>
    </div>
  );
}

const STANDARD_PLUGINS = [remarkGfm];
const BREAKS_PLUGINS = [remarkGfm, remarkBreaks];

const HEX_COLOR_RE = /^#(?:[0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/i;

function InlineCode({
  node: _,
  children,
  ...props
}: ComponentPropsWithoutRef<"code"> & { node?: unknown }) {
  const text = typeof children === "string" ? children : "";
  if (HEX_COLOR_RE.test(text)) {
    return (
      <code {...props}>
        <span
          className="inline-block size-2.5 rounded-full align-middle mr-1 ring-1 ring-foreground/10"
          style={{ backgroundColor: text }}
        />
        {children}
      </code>
    );
  }
  return <code {...props}>{children}</code>;
}

const MARKDOWN_HREF_RE = /\.(?:md|mdx)(?:[?#]|$)/i;

function isInternalMarkdownHref(href: string | undefined): href is string {
  if (!href) return false;
  if (!MARKDOWN_HREF_RE.test(href)) return false;
  if (href.startsWith("/")) return true;
  if (typeof window !== "undefined" && href.startsWith(window.location.origin)) return true;
  return false;
}

function MarkdownLink({
  href,
  children,
  ...props
}: ComponentPropsWithoutRef<"a"> & { node?: unknown }) {
  const { node: _, ...rest } = props;
  const machineId = useSessionMachineId();
  if (isInternalMarkdownHref(href)) {
    return <MarkdownFileLink href={href}>{children}</MarkdownFileLink>;
  }
  const resolved = href ? rewriteRemoteLocalhost(href, machineId) : href;
  return (
    <a
      href={resolved}
      {...rest}
      target="_blank"
      rel="noopener noreferrer"
      title={resolved !== href ? `${href} on the session's machine` : undefined}
    >
      {children}
    </a>
  );
}

/** Inline images: a session-file src (screenshots agents embed) belonging to
 *  a remote machine loads as a blob object URL — an <img src> cannot carry
 *  the bearer header that machine requires. Everything else renders as-is. */
function MarkdownImage({
  src,
  alt,
  ...props
}: ComponentPropsWithoutRef<"img"> & { node?: unknown }) {
  const { node: _, ...rest } = props;
  const machineId = typeof src === "string" ? sessionFileMachineId(src) : undefined;
  const filePath = typeof src === "string" ? sessionFilePath(src) : undefined;
  const [blobUrl, setBlobUrl] = useState<string | null>(null);

  useEffect(() => {
    if (!machineId || !filePath) return;
    let cancelled = false;
    let objectUrl: string | null = null;
    apiFetch(machineId, filePath)
      .then((res) => (res.ok ? res.blob() : Promise.reject(new Error(`${res.status}`))))
      .then((blob) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setBlobUrl(objectUrl);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [machineId, filePath]);

  if (!machineId) {
    // A primary session's file: normalize an absolute-localhost variant to
    // the relative form so it loads from any device (cookie auth applies).
    return <ZoomableImage src={filePath ?? src} alt={alt ?? ""} {...rest} />;
  }
  if (!blobUrl) return <span className="text-xs text-muted-foreground">{alt || "image"}…</span>;
  return <ZoomableImage src={blobUrl} alt={alt ?? ""} {...rest} />;
}

/** An image in the transcript opens full-screen, where it can be zoomed and
 *  panned. A screenshot an agent embedded is often the whole point of the
 *  message and is rendered at column width, which on a phone is unreadable —
 *  so the picture is a control, not decoration. */
function ZoomableImage({
  src,
  alt,
  className,
  ...rest
}: ComponentPropsWithoutRef<"img"> & { src?: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={alt ? `View ${alt} full screen` : "View image full screen"}
        className="block cursor-zoom-in border-none bg-transparent p-0"
      >
        <img src={src} alt={alt ?? ""} className={cn("max-w-full", className)} {...rest} />
      </button>
      <ImageLightbox src={open ? (src ?? null) : null} onClose={() => setOpen(false)} />
    </>
  );
}

const COMPONENTS: Components = {
  pre: PreBlock,
  code: InlineCode,
  a: MarkdownLink,
  img: MarkdownImage,
};

export const Markdown = memo(function Markdown({
  content,
  className,
  preserveNewlines,
  isStreaming,
}: MarkdownProps) {
  const plugins = preserveNewlines ? BREAKS_PLUGINS : STANDARD_PLUGINS;
  const segments = useMemo(
    () => splitByPromptBlocks(content, { isFinal: !isStreaming }),
    [content, isStreaming],
  );

  return (
    <div className={cn("prose prose-sm max-w-none", className)}>
      {segments.map((seg) => {
        if (seg.type === "prompt") {
          return (
            <PromptCard
              key={`prompt-${seg.block.title}`}
              title={seg.block.title}
              prompt={seg.block.prompt}
              projectSlug={seg.block.projectSlug}
              warning={seg.block.warning}
            />
          );
        }
        if (seg.type === "pending_prompt") {
          return <PendingPromptCard key="pending-prompt" title={seg.title} content={seg.content} />;
        }
        if (seg.type === "brain") {
          return <BrainCard key="brain-recall" facts={seg.facts} />;
        }
        return (
          <ReactMarkdown
            key={`md-${seg.content.slice(0, 80)}`}
            remarkPlugins={plugins}
            components={COMPONENTS}
          >
            {seg.content}
          </ReactMarkdown>
        );
      })}
    </div>
  );
});
