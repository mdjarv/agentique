import { AlertCircle, AlertTriangle, Ban, ShieldAlert } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import type { ChatEvent } from "~/stores/chat-store";

type ErrorStyle = "warning" | "destructive";

interface ErrorInfo {
  title: ReactNode;
  detail?: string;
  style: ErrorStyle;
  icon: typeof AlertTriangle;
}

function RateLimitCountdown({ seconds }: { seconds: number }) {
  const [remaining, setRemaining] = useState(seconds);

  useEffect(() => {
    setRemaining(seconds);
    const id = setInterval(() => {
      setRemaining((r) => {
        if (r <= 1) {
          clearInterval(id);
          return 0;
        }
        return r - 1;
      });
    }, 1000);
    return () => clearInterval(id);
  }, [seconds]);

  if (remaining <= 0) return <span>Retrying...</span>;
  return <span>Rate limited — retrying in {remaining}s</span>;
}

function classifyError(event: ChatEvent): ErrorInfo {
  const raw = event.content ?? "";

  switch (event.errorType) {
    case "rate_limit":
      return {
        title: event.retryAfterSecs ? (
          <RateLimitCountdown seconds={event.retryAfterSecs} />
        ) : (
          "Rate limited"
        ),
        style: "warning",
        icon: AlertTriangle,
      };
    case "overloaded":
      return {
        title: "API overloaded",
        detail: "Retrying automatically — no action needed.",
        style: "warning",
        icon: AlertTriangle,
      };
    case "auth":
      return {
        title: "Authentication error",
        detail: raw || undefined,
        style: "destructive",
        icon: ShieldAlert,
      };
    case "billing":
      return {
        title: "Billing error",
        detail: raw || undefined,
        style: "destructive",
        icon: Ban,
      };
    case "permission":
      return {
        title: "Permission denied",
        detail: raw || undefined,
        style: "destructive",
        icon: ShieldAlert,
      };
    case "invalid_request":
      return {
        title: "Invalid request",
        detail: raw || undefined,
        style: "destructive",
        icon: AlertCircle,
      };
    case "not_found":
      return {
        title: "Not found",
        detail: raw || undefined,
        style: "destructive",
        icon: AlertCircle,
      };
    case "request_too_large":
      return {
        title: "Request too large",
        detail: raw || "The request exceeded the maximum allowed size.",
        style: "destructive",
        icon: AlertCircle,
      };
    case "api_error":
      return {
        title: "API error",
        detail: raw || undefined,
        style: event.fatal ? "destructive" : "warning",
        icon: event.fatal ? AlertCircle : AlertTriangle,
      };
    default:
      return {
        title: raw || "Unknown error",
        style: event.fatal ? "destructive" : "warning",
        icon: event.fatal ? AlertCircle : AlertTriangle,
      };
  }
}

const styles: Record<ErrorStyle, string> = {
  warning: "bg-warning/10 text-warning",
  destructive: "bg-destructive/10 text-destructive",
};

export function ErrorBlock({ event }: { event: ChatEvent }) {
  const info = classifyError(event);
  const Icon = info.icon;

  return (
    <div className={`rounded-lg px-4 py-2.5 text-sm ${styles[info.style]}`}>
      <div className="flex items-center gap-2">
        <Icon className="h-4 w-4 shrink-0" />
        <span className="font-medium">{info.title}</span>
      </div>
      {info.detail && <p className="mt-1 ml-6 text-xs opacity-80 leading-relaxed">{info.detail}</p>}
    </div>
  );
}
