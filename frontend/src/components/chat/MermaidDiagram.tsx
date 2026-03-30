import { useEffect, useRef, useState } from "react";
import { CODE_STYLE } from "~/components/chat/Markdown";

let mermaidPromise: Promise<typeof import("mermaid")> | null = null;

function loadMermaid() {
  if (!mermaidPromise) {
    mermaidPromise = import("mermaid").then((mod) => {
      mod.default.initialize({
        startOnLoad: false,
        theme: "dark",
        themeVariables: {
          darkMode: true,
          background: "#1a1b26",
          primaryColor: "#7aa2f7",
          primaryTextColor: "#c0caf5",
          primaryBorderColor: "#3b4261",
          secondaryColor: "#24283b",
          tertiaryColor: "#24283b",
          lineColor: "#565f89",
          textColor: "#a9b1d6",
          mainBkg: "#24283b",
          nodeBorder: "#3b4261",
          clusterBkg: "#1f2335",
          clusterBorder: "#3b4261",
          titleColor: "#c0caf5",
          edgeLabelBackground: "#1a1b26",
          nodeTextColor: "#a9b1d6",
        },
        securityLevel: "strict",
      });
      return mod;
    });
  }
  return mermaidPromise;
}

export function MermaidDiagram({ code }: { code: string }) {
  const [svg, setSvg] = useState<string | null>(null);
  const idRef = useRef(`mermaid-${crypto.randomUUID().slice(0, 8)}`);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let cancelled = false;

    const timeoutId = setTimeout(() => {
      loadMermaid()
        .then((mod) => mod.default.render(idRef.current, code))
        .then(({ svg: rendered }) => {
          if (!cancelled) setSvg(rendered);
        })
        .catch(() => {
          // Keep last successful SVG — don't clear on parse failure
        });
    }, 150);

    return () => {
      cancelled = true;
      clearTimeout(timeoutId);
    };
  }, [code]);

  useEffect(() => {
    if (containerRef.current && svg) {
      containerRef.current.innerHTML = svg;
    }
  }, [svg]);

  if (!svg) {
    return (
      <pre style={{ ...CODE_STYLE, background: "var(--muted)", padding: "1em", overflow: "auto" }}>
        <code>{code}</code>
      </pre>
    );
  }

  return <div ref={containerRef} className="mermaid-diagram overflow-x-auto" />;
}
