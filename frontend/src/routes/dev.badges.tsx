import { createFileRoute } from "@tanstack/react-router";
import {
  BellDot,
  Check,
  Circle,
  CircleCheck,
  CircleDot,
  ClipboardList,
  GitMerge,
  Loader,
  MessageCircle,
  MessageSquare,
  Minus,
  Moon,
  Pause,
  PenLine,
  RefreshCw,
  XCircle,
  Zap,
} from "lucide-react";
import type { ComponentType } from "react";
import { cn } from "~/lib/utils";

// @ts-expect-error dev-only route, not in generated route tree
export const Route = createFileRoute("/dev/badges")({
  component: BadgesDevPage,
});

function Badge({
  bg,
  text,
  pulse,
  dim,
  children,
}: {
  bg: string;
  text: string;
  pulse?: boolean;
  dim?: boolean;
  children: React.ReactNode;
}) {
  return (
    <span
      className={cn(
        "flex size-5 shrink-0 items-center justify-center rounded-full",
        bg,
        text,
        pulse && "animate-pulse",
        dim && "opacity-40",
      )}
    >
      {children}
    </span>
  );
}

interface BadgeVariant {
  label: string;
  bg: string;
  text: string;
  pulse?: boolean;
  icon: ComponentType<{ className?: string }>;
  iconClass?: string;
}

function BadgeRow({ variant }: { variant: BadgeVariant }) {
  return (
    <div className="flex items-center gap-3">
      <Badge bg={variant.bg} text={variant.text} pulse={variant.pulse}>
        {variant.iconClass ? (
          <variant.icon className={variant.iconClass} />
        ) : (
          <variant.icon className="size-3" />
        )}
      </Badge>
      <span className="text-sm text-sidebar-foreground">{variant.label}</span>
      {variant.pulse && <span className="text-xs text-muted-foreground/60 italic">pulse</span>}
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-3">
      <h3 className="text-xs font-semibold tracking-widest text-muted-foreground uppercase">
        {title}
      </h3>
      <div className="space-y-2 pl-1">{children}</div>
    </div>
  );
}

function BadgesDevPage() {
  // --- CURRENT ---
  const currentHighAttention: BadgeVariant[] = [
    {
      label: "Pending approval",
      bg: "bg-[#bb9af7]/15",
      text: "text-[#bb9af7]",
      pulse: true,
      icon: MessageSquare,
    },
    {
      label: "Plan review",
      bg: "bg-[#bb9af7]/15",
      text: "text-[#bb9af7]",
      pulse: true,
      icon: ClipboardList,
    },
    {
      label: "Unseen completion (Zap)",
      bg: "bg-[#73daca]/15",
      text: "text-[#73daca]",
      icon: Zap,
    },
  ];

  const currentInProgress: BadgeVariant[] = [
    {
      label: "Running",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      pulse: true,
      icon: Loader,
      iconClass: "size-3 animate-spin",
    },
    {
      label: "Planning (running)",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      pulse: true,
      icon: PenLine,
    },
    {
      label: "Merging",
      bg: "bg-[#7aa2f7]/15",
      text: "text-[#7aa2f7]",
      pulse: true,
      icon: Loader,
      iconClass: "size-3 animate-spin",
    },
    {
      label: "Rebasing",
      bg: "bg-[#7aa2f7]/15",
      text: "text-[#7aa2f7]",
      pulse: true,
      icon: RefreshCw,
      iconClass: "size-3 animate-spin",
    },
  ];

  const currentPassive: BadgeVariant[] = [
    {
      label: "Idle",
      bg: "bg-[#9ece6a]/15",
      text: "text-[#9ece6a]",
      icon: Circle,
      iconClass: "size-2.5",
    },
    { label: "Done", bg: "bg-emerald-500/15", text: "text-emerald-500", icon: Check },
    {
      label: "Stopped",
      bg: "bg-[#a9b1d6]/10",
      text: "text-[#a9b1d6]/80",
      icon: Pause,
    },
    { label: "Failed", bg: "bg-[#f7768e]/15", text: "text-[#f7768e]", icon: XCircle },
  ];

  // --- PROPOSED ---
  const proposedHighAttention: BadgeVariant[] = [
    {
      label: "Pending approval",
      bg: "bg-[#bb9af7]/15",
      text: "text-[#bb9af7]",
      pulse: true,
      icon: MessageSquare,
    },
    {
      label: "Plan review",
      bg: "bg-[#bb9af7]/15",
      text: "text-[#bb9af7]",
      pulse: true,
      icon: ClipboardList,
    },
    {
      label: "Unseen completion (BellDot, amber)",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      pulse: true,
      icon: BellDot,
    },
    {
      label: "Unseen completion (BellDot, teal)",
      bg: "bg-[#73daca]/15",
      text: "text-[#73daca]",
      pulse: true,
      icon: BellDot,
    },
    {
      label: "Unseen completion (CircleCheck, amber)",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      pulse: true,
      icon: CircleCheck,
    },
    {
      label: "Unseen completion (MessageCircle, amber)",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      pulse: true,
      icon: MessageCircle,
    },
    {
      label: "Unseen completion (MessageCircle, teal)",
      bg: "bg-[#73daca]/15",
      text: "text-[#73daca]",
      pulse: true,
      icon: MessageCircle,
    },
  ];

  const proposedInProgress: BadgeVariant[] = [
    {
      label: "Running (no pulse)",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      icon: Loader,
      iconClass: "size-3 animate-spin",
    },
    {
      label: "Planning (no pulse)",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      icon: PenLine,
    },
    {
      label: "Merging (no pulse)",
      bg: "bg-[#7aa2f7]/15",
      text: "text-[#7aa2f7]",
      icon: Loader,
      iconClass: "size-3 animate-spin",
    },
    {
      label: "Merging — GitMerge (no pulse)",
      bg: "bg-[#7aa2f7]/15",
      text: "text-[#7aa2f7]",
      icon: GitMerge,
    },
  ];

  const proposedIdleVariants: BadgeVariant[] = [
    {
      label: "Idle — CircleDot (green)",
      bg: "bg-[#9ece6a]/15",
      text: "text-[#9ece6a]",
      icon: CircleDot,
      iconClass: "size-2.5",
    },
    {
      label: "Idle — CircleDot (muted)",
      bg: "bg-[#a9b1d6]/10",
      text: "text-[#a9b1d6]/60",
      icon: CircleDot,
      iconClass: "size-2.5",
    },
    {
      label: "Idle — Minus (muted)",
      bg: "bg-[#565f89]/15",
      text: "text-[#565f89]",
      icon: Minus,
    },
    {
      label: "Idle — Moon (muted blue)",
      bg: "bg-[#7aa2f7]/10",
      text: "text-[#7aa2f7]/40",
      icon: Moon,
    },
    {
      label: "Idle — Circle filled (green, size-2)",
      bg: "bg-[#9ece6a]/15",
      text: "text-[#9ece6a]",
      icon: Circle,
      iconClass: "size-2 fill-current",
    },
  ];

  // --- SIDE-BY-SIDE COMPARISON ---
  // Simulates a mini session list to see how they look in context
  const contextCurrent: BadgeVariant[] = [
    {
      label: "Fix Permission Persistence",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      pulse: true,
      icon: Loader,
      iconClass: "size-3 animate-spin",
    },
    {
      label: "Improve Commit UX",
      bg: "bg-[#73daca]/15",
      text: "text-[#73daca]",
      icon: Zap,
    },
    {
      label: "MSW Mock Backend",
      bg: "bg-[#9ece6a]/15",
      text: "text-[#9ece6a]",
      icon: Circle,
      iconClass: "size-2.5",
    },
    {
      label: "OAuth user whitelist",
      bg: "bg-[#bb9af7]/15",
      text: "text-[#bb9af7]",
      pulse: true,
      icon: MessageSquare,
    },
  ];

  const contextProposed: BadgeVariant[] = [
    {
      label: "Fix Permission Persistence",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      icon: Loader,
      iconClass: "size-3 animate-spin",
    },
    {
      label: "Improve Commit UX",
      bg: "bg-[#e0af68]/15",
      text: "text-[#e0af68]",
      pulse: true,
      icon: BellDot,
    },
    {
      label: "MSW Mock Backend",
      bg: "bg-[#9ece6a]/15",
      text: "text-[#9ece6a]",
      icon: CircleDot,
      iconClass: "size-2.5",
    },
    {
      label: "OAuth user whitelist",
      bg: "bg-[#bb9af7]/15",
      text: "text-[#bb9af7]",
      pulse: true,
      icon: MessageSquare,
    },
  ];

  return (
    <div className="flex-1 overflow-y-auto p-8">
      <div className="mx-auto max-w-4xl space-y-10">
        <h1 className="text-xl font-semibold text-foreground-bright">Badge Variants</h1>

        {/* Side-by-side context comparison */}
        <div className="grid grid-cols-2 gap-8">
          <div className="rounded-lg bg-sidebar p-4 space-y-1.5">
            <h3 className="text-xs font-semibold tracking-widest text-muted-foreground uppercase mb-3">
              Current — Session List
            </h3>
            {contextCurrent.map((v) => (
              <div
                key={v.label}
                className="flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm"
              >
                <Badge bg={v.bg} text={v.text} pulse={v.pulse}>
                  {v.iconClass ? <v.icon className={v.iconClass} /> : <v.icon className="size-3" />}
                </Badge>
                <span className="text-sidebar-foreground truncate">{v.label}</span>
              </div>
            ))}
          </div>
          <div className="rounded-lg bg-sidebar p-4 space-y-1.5">
            <h3 className="text-xs font-semibold tracking-widest text-muted-foreground uppercase mb-3">
              Proposed — Session List
            </h3>
            {contextProposed.map((v) => (
              <div
                key={v.label}
                className="flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm"
              >
                <Badge bg={v.bg} text={v.text} pulse={v.pulse}>
                  {v.iconClass ? <v.icon className={v.iconClass} /> : <v.icon className="size-3" />}
                </Badge>
                <span className="text-sidebar-foreground truncate">{v.label}</span>
              </div>
            ))}
          </div>
        </div>

        <div className="border-t border-border pt-8" />

        {/* Full catalog */}
        <div className="grid grid-cols-2 gap-12">
          {/* Current column */}
          <div className="space-y-8">
            <h2 className="text-lg font-semibold text-foreground-bright">Current</h2>
            <Section title="Needs Attention">
              {currentHighAttention.map((v) => (
                <BadgeRow key={v.label} variant={v} />
              ))}
            </Section>
            <Section title="In Progress">
              {currentInProgress.map((v) => (
                <BadgeRow key={v.label} variant={v} />
              ))}
            </Section>
            <Section title="Passive">
              {currentPassive.map((v) => (
                <BadgeRow key={v.label} variant={v} />
              ))}
            </Section>
          </div>

          {/* Proposed column */}
          <div className="space-y-8">
            <h2 className="text-lg font-semibold text-foreground-bright">Proposed</h2>
            <Section title="Needs Attention (pulse)">
              {proposedHighAttention.map((v) => (
                <BadgeRow key={v.label} variant={v} />
              ))}
            </Section>
            <Section title="In Progress (spin only, no pulse)">
              {proposedInProgress.map((v) => (
                <BadgeRow key={v.label} variant={v} />
              ))}
            </Section>
            <Section title="Idle Variants">
              {proposedIdleVariants.map((v) => (
                <BadgeRow key={v.label} variant={v} />
              ))}
            </Section>
          </div>
        </div>
      </div>
    </div>
  );
}
