import { createFileRoute } from "@tanstack/react-router";
import { ChevronRight, FileText, Pencil, Search, Terminal } from "lucide-react";
import type { ReactNode } from "react";
import { Markdown } from "~/components/chat/Markdown";
import { UserMessage } from "~/components/chat/UserMessage";

export const Route = createFileRoute("/dev/bubbles")({
  component: DevBubbles,
});

const noiseSvg = `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`;

function FakeToolRow({
  icon,
  iconColor,
  label,
  trailing,
}: {
  icon: ReactNode;
  iconColor: string;
  label: string;
  trailing?: string;
}) {
  return (
    <div className="flex items-center gap-2 px-2 py-1.5 text-xs text-muted-foreground w-full min-w-0">
      <span className={iconColor}>{icon}</span>
      <span className="truncate">{label}</span>
      <span className="ml-auto flex items-center gap-1.5 min-w-0">
        {trailing && <span className="text-muted-foreground/60 text-[10px]">{trailing}</span>}
        <ChevronRight className="h-3 w-3 shrink-0 opacity-30" />
      </span>
    </div>
  );
}

function AgentBubble({ children }: { children: ReactNode }) {
  return (
    <div className="flex gap-3">
      <div className="h-8 w-8 shrink-0 rounded-full bg-agent/20 flex items-center justify-center">
        <span className="text-agent text-xs font-bold">A</span>
      </div>
      <div className="max-w-[75%] rounded-lg px-4 py-2 bg-gradient-to-br from-agent/25 to-agent/15 border border-agent/20 shadow-lg shadow-black/30 text-foreground">
        {children}
      </div>
    </div>
  );
}

function AgentActivity({ children }: { children: ReactNode }) {
  return (
    <div className="flex gap-3">
      <div className="w-8 shrink-0 flex justify-center">
        <div className="w-px bg-agent/15 min-h-full" />
      </div>
      <div className="flex-1 min-w-0 rounded-md border border-border/50 bg-muted/30 overflow-hidden">
        {children}
      </div>
    </div>
  );
}

const i3 = "h-3 w-3";

function ChatSample() {
  return (
    <div className="space-y-6 p-4">
      <UserMessage
        prompt="Can you refactor the auth middleware to use the new token validation?"
        deliveryStatus="delivered"
      />

      <AgentBubble>
        <Markdown
          content="I'll refactor the auth middleware. Let me start by reading the current implementation."
          className="prose-agent"
          preserveNewlines
        />
      </AgentBubble>

      <AgentActivity>
        <FakeToolRow
          icon={<FileText className={i3} />}
          iconColor="text-success/70"
          label="backend/internal/auth/middleware.go"
          trailing="142 lines"
        />
        <FakeToolRow
          icon={<Search className={i3} />}
          iconColor="text-success/70"
          label='Grep "validateToken"'
          trailing="3 matches"
        />
        <FakeToolRow
          icon={<Pencil className={i3} />}
          iconColor="text-warning/70"
          label="backend/internal/auth/middleware.go"
          trailing="changed"
        />
        <FakeToolRow
          icon={<Terminal className={i3} />}
          iconColor="text-warning/70"
          label="go test ./internal/auth/..."
          trailing="ok 0.8s"
        />
      </AgentActivity>

      <AgentBubble>
        <Markdown
          content={
            "Done. Replaced the hand-rolled JWT check with `tokenValidator.Validate()` from the new library. All auth tests pass."
          }
          className="prose-agent"
          preserveNewlines
        />
      </AgentBubble>

      <UserMessage
        prompt={`Looks good. Can you also update the \`session.history\` handler? I think there's a race condition when the WebSocket reconnects.`}
        deliveryStatus="delivered"
      />

      <AgentBubble>
        <Markdown
          content="Let me check the reconnection flow."
          className="prose-agent"
          preserveNewlines
        />
      </AgentBubble>

      <AgentActivity>
        <FakeToolRow
          icon={<FileText className={i3} />}
          iconColor="text-success/70"
          label="backend/internal/ws/handler.go"
          trailing="89 lines"
        />
        <FakeToolRow
          icon={<Search className={i3} />}
          iconColor="text-success/70"
          label='Grep "session.history"'
          trailing="2 matches"
        />
      </AgentActivity>

      <AgentBubble>
        <Markdown
          content="Found it — the client re-subscribes before the history response arrives. I'll add a sync barrier."
          className="prose-agent"
          preserveNewlines
        />
      </AgentBubble>

      <UserMessage prompt="any update on the fix?" deliveryStatus="sending" />
    </div>
  );
}

function DevBubbles() {
  return (
    <div className="h-full overflow-y-auto relative">
      {/* Grain overlay */}
      <div
        className="absolute inset-0 opacity-[0.08] pointer-events-none z-10"
        style={{
          backgroundImage: noiseSvg,
          backgroundRepeat: "repeat",
          backgroundSize: "128px 128px",
        }}
      />
      {/* Vertical gradient */}
      <div
        className="absolute inset-0 pointer-events-none z-10"
        style={{
          background:
            "linear-gradient(180deg, hsl(230 19% 15% / .15) 0%, transparent 30%, hsl(230 19% 10% / .2) 100%)",
        }}
      />
      <div className="relative z-20 max-w-3xl mx-auto">
        <ChatSample />
      </div>
    </div>
  );
}
