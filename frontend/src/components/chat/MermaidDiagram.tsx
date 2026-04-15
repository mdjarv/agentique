import { useEffect, useRef, useState } from "react";
import { CODE_STYLE } from "~/components/chat/Markdown";
import { useTheme } from "~/hooks/useTheme";

type ResolvedTheme = "light" | "dark";

let mermaidPromise: Promise<typeof import("mermaid")> | null = null;
let currentTheme: ResolvedTheme | null = null;

const THEME_VARS: Record<ResolvedTheme, Record<string, unknown>> = {
  dark: {
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
  light: {
    darkMode: false,
    background: "#f4f1ed",
    primaryColor: "#8839ef",
    primaryTextColor: "#554f47",
    primaryBorderColor: "#c9c3ba",
    secondaryColor: "#d9d4cc",
    tertiaryColor: "#ece8e2",
    lineColor: "#7a756d",
    textColor: "#554f47",
    mainBkg: "#d9d4cc",
    nodeBorder: "#c9c3ba",
    clusterBkg: "#ece8e2",
    clusterBorder: "#c9c3ba",
    titleColor: "#1a1816",
    edgeLabelBackground: "#f4f1ed",
    nodeTextColor: "#554f47",
  },
};

function loadMermaid(resolved: ResolvedTheme) {
  if (mermaidPromise && currentTheme === resolved) return mermaidPromise;
  currentTheme = resolved;
  mermaidPromise = import("mermaid").then((mod) => {
    mod.default.initialize({
      startOnLoad: false,
      theme: resolved === "dark" ? "dark" : "default",
      themeVariables: THEME_VARS[resolved],
      securityLevel: "strict",
    });
    return mod;
  });
  return mermaidPromise;
}

export function MermaidDiagram({ code }: { code: string }) {
  const [svg, setSvg] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const { resolvedTheme } = useTheme();

  useEffect(() => {
    let cancelled = false;

    const timeoutId = setTimeout(() => {
      // Generate a fresh ID for each render to avoid mermaid ID collision
      const renderId = `mermaid-${crypto.randomUUID().slice(0, 8)}`;
      loadMermaid(resolvedTheme)
        .then((mod) => mod.default.render(renderId, code))
        .then(({ svg: rendered }) => {
          if (!cancelled) setSvg(rendered);
        })
        .catch((err) => {
          // Keep last successful SVG — don't clear on parse failure
          console.warn("Mermaid render failed", err);
        });
    }, 150);

    return () => {
      cancelled = true;
      clearTimeout(timeoutId);
    };
  }, [code, resolvedTheme]);

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
