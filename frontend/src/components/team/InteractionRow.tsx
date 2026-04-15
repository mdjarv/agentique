import { useMemo, useState } from "react";
import { Badge } from "~/components/ui/badge";
import type { AgentProfileInfo, PersonaInteraction } from "~/lib/team-actions";

export const ACTION_VARIANT: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  answer: "secondary",
  spawn: "default",
  queue: "outline",
  reject: "destructive",
  redirect: "outline",
};

export function InteractionRow({
  interaction,
  profiles,
}: {
  interaction: PersonaInteraction;
  profiles: Record<string, AgentProfileInfo>;
}) {
  const [expanded, setExpanded] = useState(false);
  const target = profiles[interaction.profileId];
  const asker = interaction.askerId ? profiles[interaction.askerId] : null;
  const askerLabel = asker ? asker.name : "User";

  const time = useMemo(() => {
    const d = new Date(interaction.createdAt);
    return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  }, [interaction.createdAt]);

  return (
    <button
      type="button"
      className="w-full text-left rounded border bg-card/30 px-2 py-1.5 space-y-1 hover:bg-muted/30 transition-colors"
      onClick={() => setExpanded((v) => !v)}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs truncate">
          <span className="text-muted-foreground">{askerLabel}</span>
          <span className="text-muted-foreground/60 mx-1">&rarr;</span>
          <span className="font-medium">{target?.name ?? "Unknown"}</span>
        </span>
        <div className="flex items-center gap-1.5 shrink-0">
          <Badge
            variant={ACTION_VARIANT[interaction.action] ?? "secondary"}
            className="text-[10px] px-1 py-0"
          >
            {interaction.action}
          </Badge>
          <span className="text-[10px] text-muted-foreground/60">{time}</span>
        </div>
      </div>
      <p className="text-[11px] text-muted-foreground truncate">{interaction.question}</p>
      {expanded && (
        <div className="space-y-1 mt-1">
          {interaction.response && (
            <div className="rounded bg-muted/50 p-1.5 text-[11px] whitespace-pre-wrap">
              {interaction.response}
            </div>
          )}
          <div className="flex items-center gap-2 text-[10px] text-muted-foreground/50">
            {interaction.responseTimeMs > 0 && <span>{interaction.responseTimeMs}ms</span>}
            {interaction.confidence > 0 && (
              <span>confidence: {(interaction.confidence * 100).toFixed(0)}%</span>
            )}
            {interaction.redirectTo && <span>redirect: {interaction.redirectTo}</span>}
          </div>
        </div>
      )}
    </button>
  );
}
