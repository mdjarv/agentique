import { Check, Copy } from "lucide-react";
import {
  Children,
  type ComponentPropsWithoutRef,
  type ReactNode,
  isValidElement,
  useCallback,
  useRef,
  useState,
} from "react";
import type { Components } from "react-markdown";
import ReactMarkdown from "react-markdown";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import { PromptCard, parsePromptFromCode } from "~/components/chat/PromptCard";
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
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(null);

  const handleCopy = useCallback(() => {
    void navigator.clipboard.writeText(text).then(() => {
      if (timerRef.current) clearTimeout(timerRef.current);
      setCopied(true);
      timerRef.current = setTimeout(() => setCopied(false), 1200);
    });
  }, [text]);

  return (
    <button
      type="button"
      className="code-copy-btn"
      onClick={handleCopy}
      aria-label={copied ? "Copied" : "Copy code"}
    >
      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
    </button>
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
    (childArray[0] as React.ReactElement<{ className?: string }>).type === "code"
      ? (childArray[0] as React.ReactElement<{ className?: string; children?: ReactNode }>)
      : null;

  if (!codeChild) return <pre {...rest}>{children}</pre>;

  const lang = /language-(\w+)/.exec(codeChild.props.className ?? "")?.[1];
  const code = nodeToPlainText(codeChild.props.children).replace(/\n$/, "");

  if (lang === "prompt") {
    const parsed = parsePromptFromCode(code);
    if (parsed) return <PromptCard title={parsed.title} prompt={parsed.prompt} />;
  }

  return (
    <div className="code-block-wrapper">
      <CopyButton text={code} />
      {lang ? (
        <SyntaxHighlighter
          style={oneDark}
          language={lang}
          customStyle={{ margin: 0, fontSize: "0.75rem", borderRadius: "0.5rem" }}
        >
          {code}
        </SyntaxHighlighter>
      ) : (
        <pre {...rest}>
          <code>{code}</code>
        </pre>
      )}
    </div>
  );
}

const STANDARD_PLUGINS = [remarkGfm];
const BREAKS_PLUGINS = [remarkGfm, remarkBreaks];

const COMPONENTS: Components = { pre: PreBlock };

export function Markdown({ content, className, preserveNewlines }: MarkdownProps) {
  const plugins = preserveNewlines ? BREAKS_PLUGINS : STANDARD_PLUGINS;

  return (
    <div className={cn("prose prose-sm max-w-none", className)}>
      <ReactMarkdown remarkPlugins={plugins} components={COMPONENTS}>
        {content}
      </ReactMarkdown>
    </div>
  );
}
