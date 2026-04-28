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
import { MarkdownFileLink } from "~/components/chat/MarkdownFileLink";
import { MermaidDiagram } from "~/components/chat/MermaidDiagram";
import { PromptCard, splitByPromptBlocks } from "~/components/chat/PromptCard";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import { useTheme } from "~/hooks/useTheme";
import { getSyntaxTheme } from "~/lib/syntax-theme";
import { cn } from "~/lib/utils";

interface MarkdownProps {
  content: string;
  className?: string;
  /** Convert single newlines to <br> (useful for user-typed messages). */
  preserveNewlines?: boolean;
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

  return (
    <div className="code-block-wrapper">
      <CopyButton text={code} />
      {lang === "mermaid" ? (
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

const COMPONENTS: Components = {
  pre: PreBlock,
  code: InlineCode,
  a: ({ node: _, href, children, ...props }) => {
    if (isInternalMarkdownHref(href)) {
      return <MarkdownFileLink href={href}>{children}</MarkdownFileLink>;
    }
    return (
      <a href={href} {...props} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    );
  },
};

export const Markdown = memo(function Markdown({
  content,
  className,
  preserveNewlines,
}: MarkdownProps) {
  const plugins = preserveNewlines ? BREAKS_PLUGINS : STANDARD_PLUGINS;
  const segments = useMemo(() => splitByPromptBlocks(content), [content]);

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
            />
          );
        }
        if (seg.type === "pending_prompt") {
          return <PendingPromptCard key="pending-prompt" title={seg.title} content={seg.content} />;
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
