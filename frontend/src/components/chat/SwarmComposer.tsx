import { Loader2, Minus, Plus, SendHorizonal, Users2 } from "lucide-react";
import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { BehaviorPresets } from "~/lib/generated-types";
import { MODELS, MODEL_LABELS, type ModelId } from "~/lib/session-actions";
import { type SwarmMemberSpec, createSwarm } from "~/lib/team-actions";
import { cn, getErrorMessage } from "~/lib/utils";

type SwarmMode = "goal" | "prompts";

interface AgentEntry {
  id: string;
  name: string;
  prompt: string;
}

let nextId = 1;
function makeEntry(): AgentEntry {
  return { id: `agent-${nextId++}`, name: "", prompt: "" };
}

interface SwarmComposerProps {
  projectId: string;
  model: ModelId;
  onModelChange: (m: ModelId) => void;
  behaviorPresets: BehaviorPresets;
  onCreated: (teamId: string, firstSessionId: string) => void;
}

export function SwarmComposer({
  projectId,
  model,
  onModelChange,
  behaviorPresets,
  onCreated,
}: SwarmComposerProps) {
  const ws = useWebSocket();
  const [mode, setMode] = useState<SwarmMode>("goal");
  const [teamName, setTeamName] = useState("");
  const [sending, setSending] = useState(false);

  // Goal mode
  const [goal, setGoal] = useState("");
  const [agentCount, setAgentCount] = useState(3);

  // Prompts mode — use stable IDs for keys
  const initialEntries = useRef([makeEntry(), makeEntry()]);
  const [entries, setEntries] = useState<AgentEntry[]>(initialEntries.current);

  const updateEntry = useCallback((id: string, field: "name" | "prompt", value: string) => {
    setEntries((prev) => prev.map((e) => (e.id === id ? { ...e, [field]: value } : e)));
  }, []);

  const addEntry = useCallback(() => {
    setEntries((prev) => [...prev, makeEntry()]);
  }, []);

  const removeEntry = useCallback(
    (id: string) => {
      if (entries.length <= 2) return;
      setEntries((prev) => prev.filter((e) => e.id !== id));
    },
    [entries.length],
  );

  const canSubmit =
    !sending &&
    teamName.trim() !== "" &&
    (mode === "goal" ? goal.trim() !== "" : entries.every((e) => e.prompt.trim() !== ""));

  const handleSubmit = useCallback(async () => {
    if (!canSubmit) return;
    setSending(true);

    try {
      let members: SwarmMemberSpec[];

      if (mode === "goal") {
        members = Array.from({ length: agentCount }, (_, i) => ({
          name: `${teamName} #${i + 1}`,
          prompt: goal,
          model,
          behaviorPresets,
        }));
      } else {
        members = entries.map((e) => ({
          name: e.name || e.prompt.slice(0, 40),
          prompt: e.prompt,
          model,
          behaviorPresets,
        }));
      }

      const result = await createSwarm(ws, projectId, teamName, members);

      if (result.errors?.length) {
        toast.warning(`Team created with ${result.errors.length} warning(s)`);
      } else {
        toast.success(`Team "${teamName}" created with ${members.length} sessions`);
      }

      const firstSid = result.sessionIds.find((id) => id !== "");
      if (firstSid) {
        onCreated(result.teamId, firstSid);
      }
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to create team"));
      setSending(false);
    }
  }, [
    canSubmit,
    mode,
    teamName,
    goal,
    agentCount,
    entries,
    model,
    behaviorPresets,
    ws,
    projectId,
    onCreated,
  ]);

  return (
    <div className="border-t border-border bg-background p-4 space-y-3">
      {/* Team name + model */}
      <div className="flex items-center gap-2">
        <Users2 className="h-4 w-4 text-muted-foreground shrink-0" />
        <Input
          value={teamName}
          onChange={(e) => setTeamName(e.target.value)}
          placeholder="Team name"
          className="h-8 text-sm flex-1"
          disabled={sending}
        />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="xs" className="shrink-0">
              {MODEL_LABELS[model]}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {MODELS.map((m) => (
              <DropdownMenuItem key={m} onClick={() => onModelChange(m)} className="text-xs">
                {MODEL_LABELS[m]}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Mode toggle */}
      <div className="flex gap-1">
        <button
          type="button"
          onClick={() => setMode("goal")}
          className={cn(
            "text-xs px-2.5 py-1 rounded-md transition-colors",
            mode === "goal"
              ? "bg-primary text-primary-foreground"
              : "text-muted-foreground hover:text-foreground hover:bg-muted",
          )}
        >
          Single goal
        </button>
        <button
          type="button"
          onClick={() => setMode("prompts")}
          className={cn(
            "text-xs px-2.5 py-1 rounded-md transition-colors",
            mode === "prompts"
              ? "bg-primary text-primary-foreground"
              : "text-muted-foreground hover:text-foreground hover:bg-muted",
          )}
        >
          Per-agent prompts
        </button>
      </div>

      {/* Goal mode */}
      {mode === "goal" && (
        <div className="space-y-2">
          <Textarea
            value={goal}
            onChange={(e) => setGoal(e.target.value)}
            placeholder="Describe the goal — all agents receive this prompt and coordinate via their team."
            className="min-h-[80px] text-sm resize-none"
            disabled={sending}
          />
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground">Agents:</span>
            <Button
              variant="outline"
              size="xs"
              disabled={agentCount <= 2 || sending}
              onClick={() => setAgentCount((c) => c - 1)}
            >
              <Minus className="h-3 w-3" />
            </Button>
            <span className="text-sm font-medium w-4 text-center">{agentCount}</span>
            <Button
              variant="outline"
              size="xs"
              disabled={agentCount >= 6 || sending}
              onClick={() => setAgentCount((c) => c + 1)}
            >
              <Plus className="h-3 w-3" />
            </Button>
          </div>
        </div>
      )}

      {/* Per-agent prompts mode */}
      {mode === "prompts" && (
        <div className="space-y-2">
          {entries.map((entry, idx) => (
            <div key={entry.id} className="flex gap-2 items-start">
              <div className="flex-1 space-y-1">
                <Input
                  value={entry.name}
                  onChange={(e) => updateEntry(entry.id, "name", e.target.value)}
                  placeholder={`Agent ${idx + 1} name (optional)`}
                  className="h-7 text-xs"
                  disabled={sending}
                />
                <Textarea
                  value={entry.prompt}
                  onChange={(e) => updateEntry(entry.id, "prompt", e.target.value)}
                  placeholder="Prompt for this agent"
                  className="min-h-[60px] text-sm resize-none"
                  disabled={sending}
                />
              </div>
              <Button
                variant="ghost"
                size="xs"
                disabled={entries.length <= 2 || sending}
                onClick={() => removeEntry(entry.id)}
                className="mt-1"
              >
                <Minus className="h-3 w-3" />
              </Button>
            </div>
          ))}
          <Button variant="outline" size="xs" onClick={addEntry} disabled={sending}>
            <Plus className="h-3 w-3" />
            Add agent
          </Button>
        </div>
      )}

      {/* Submit */}
      <div className="flex justify-end">
        <Button size="sm" disabled={!canSubmit} onClick={handleSubmit}>
          {sending ? (
            <>
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              Creating...
            </>
          ) : (
            <>
              <SendHorizonal className="h-3.5 w-3.5" />
              Create Team
            </>
          )}
        </Button>
      </div>
    </div>
  );
}
