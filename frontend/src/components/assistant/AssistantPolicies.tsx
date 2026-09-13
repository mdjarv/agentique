/**
 * Policies — `/assistant/policies`, the standing instructions the heartbeat may
 * act under.
 *
 * A policy is the only thing in this app that lets the assistant start work
 * nobody asked for in the moment, so the page is deliberately plain: the words
 * the operator wrote, a switch, and the two numbers that bound what those words
 * can spend. Nothing here is a dial on confidence or autonomy — a budget is a
 * count, which is checkable, where a percentage is a feeling.
 *
 * The rows are the server's (`policies()`, seeded once per connection and kept
 * current by the `assistant.policy` push); what is local is only the unsaved
 * edit. Save sends the draft and renders **what comes back**, because the server
 * mints the id, caps the text and clamps the budgets — a page holding its own
 * copy would show a row that does not exist.
 */
import { Plus, Save, Trash2 } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { AssistantHeader } from "~/components/assistant/AssistantHeader";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type AssistantPolicySave, deletePolicy, savePolicy } from "~/lib/assistant/rpc";
import type { AssistantPolicy } from "~/lib/assistant/wire";
import { getErrorMessage } from "~/lib/utils";
import { selectAssistantPolicies, useAssistantStore } from "~/stores/assistant-store";
import { useFeatureStore } from "~/stores/feature-store";

/** The blank row's identity. Never an id, because it has none until it is saved. */
const NEW_ROW = "new";

/** What the server defaults an unset budget to — the blank row says the same. */
const DEFAULT_IN_FLIGHT = 1;
const DEFAULT_PER_DAY = 3;

/**
 * The smallest budget that can be asked for, and it is one rather than zero.
 *
 * Zero is not spellable: every wire field here is optional, so `budgetPerDay: 0`
 * is indistinguishable from an omitted field, and the server reads an absent
 * budget as its default rather than as "none allowed" — a client that left a
 * field out must not silently write a policy that can do nothing. Which means a
 * field offering 0 would offer a value that comes back as 3. The control for
 * "never act under this" is the switch beside it.
 */
const MIN_BUDGET = 1;

/** The editable half of a row. `id` is the row's identity, not an edit. */
interface Draft {
  name: string;
  text: string;
  enabled: boolean;
  budgetInFlight: number;
  budgetPerDay: number;
}

function draftOf(policy: AssistantPolicy): Draft {
  return {
    name: policy.name ?? "",
    text: policy.text ?? "",
    // Absent is not false on the wire, but for `enabled` the server writes the
    // field whenever it is true, so an absent one is a policy that is off.
    enabled: policy.enabled === true,
    budgetInFlight: policy.budgetInFlight ?? DEFAULT_IN_FLIGHT,
    budgetPerDay: policy.budgetPerDay ?? DEFAULT_PER_DAY,
  };
}

const BLANK: Draft = {
  name: "",
  text: "",
  enabled: false,
  budgetInFlight: DEFAULT_IN_FLIGHT,
  budgetPerDay: DEFAULT_PER_DAY,
};

export function AssistantPoliciesPage() {
  const ws = useWebSocket();
  const policies = useAssistantStore(selectAssistantPolicies);
  const enabled = useFeatureStore((s) => s.features.assistant);
  const featuresLoaded = useFeatureStore((s) => s.loaded);
  // Unsaved edits, by row id — kept out of the store because they are this
  // browser's and this moment's, and a push must not overwrite a half-typed
  // rule. A row with no entry here is showing exactly what the server holds.
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [busy, setBusy] = useState<string | null>(null);

  const edit = useCallback((key: string, patch: Partial<Draft>, base: Draft) => {
    setDrafts((held) => ({ ...held, [key]: { ...base, ...(held[key] ?? {}), ...patch } }));
  }, []);

  const save = useCallback(
    async (key: string, draft: Draft) => {
      const payload: AssistantPolicySave = {
        ...(key === NEW_ROW ? {} : { id: key }),
        name: draft.name.trim(),
        text: draft.text,
        enabled: draft.enabled,
        budgetInFlight: draft.budgetInFlight,
        budgetPerDay: draft.budgetPerDay,
      };
      setBusy(key);
      try {
        const saved = await savePolicy(ws, payload);
        useAssistantStore.getState().applyPolicy(saved);
        // The draft goes, so the row renders the server's copy from here on —
        // including whatever it clamped. The blank row empties for the next one.
        setDrafts((held) => {
          const { [key]: _gone, ...rest } = held;
          return rest;
        });
      } catch (err) {
        toast.error(getErrorMessage(err, "The policy was not saved"));
      } finally {
        setBusy(null);
      }
    },
    [ws],
  );

  const remove = useCallback(
    async (id: string) => {
      setBusy(id);
      try {
        await deletePolicy(ws, id);
        // The push removes the row; this covers the peer that sends none.
        useAssistantStore.getState().applyPolicy({ id, deleted: true });
      } catch (err) {
        toast.error(getErrorMessage(err, "The policy was not deleted"));
      } finally {
        setBusy(null);
      }
    },
    [ws],
  );

  if (featuresLoaded && !enabled) {
    return (
      <div className="flex flex-col h-full">
        <AssistantHeader title="Policies" />
        <div className="flex-1 flex items-center justify-center px-6 text-center">
          <p className="text-sm text-muted-foreground max-w-sm">
            The assistant is off on this machine, so it acts under nothing. Set{" "}
            <code>[experimental] assistant</code> in the server config to turn it on.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      <AssistantHeader title="Policies" />
      <div className="flex-1 overflow-y-auto px-4 py-4 md:px-6">
        <div className="mx-auto flex max-w-2xl flex-col gap-4">
          <p className="text-xs text-muted-foreground">
            A policy is a standing instruction: what the assistant may start on its own when its
            heartbeat finds something worth acting on. The budgets are the whole of the limit — how
            many sessions it may have running under this policy at once, and how many it may start
            in a day. Anything that would need your yes is still a proposal.
          </p>
          {policies.length === 0 && (
            <p className="text-xs text-muted-foreground-faint">
              No policies yet. The assistant acts on its own only under one of these.
            </p>
          )}
          {policies.map((policy) => (
            <PolicyRow
              // The id is the identity: a row must not lose its draft because a
              // neighbour was renamed and the list re-sorted.
              key={policy.id}
              rowKey={policy.id ?? ""}
              saved={policy}
              draft={drafts[policy.id ?? ""]}
              busy={busy === policy.id}
              onEdit={edit}
              onSave={save}
              onDelete={remove}
            />
          ))}
          <PolicyRow
            key={NEW_ROW}
            rowKey={NEW_ROW}
            draft={drafts[NEW_ROW]}
            busy={busy === NEW_ROW}
            onEdit={edit}
            onSave={save}
          />
        </div>
      </div>
    </div>
  );
}

interface PolicyRowProps {
  /** The policy id, or `new` for the blank row. Every control's id ends in it. */
  rowKey: string;
  /** What the server holds, when it holds anything. Absent on the blank row. */
  saved?: AssistantPolicy;
  /** The unsaved edit, when there is one. */
  draft?: Draft;
  busy: boolean;
  onEdit: (key: string, patch: Partial<Draft>, base: Draft) => void;
  onSave: (key: string, draft: Draft) => void;
  /** Absent on the blank row: there is nothing yet to delete. */
  onDelete?: (id: string) => void;
}

/**
 * One policy, and the blank row is the same component.
 *
 * The two are one row type on purpose: the save is one upsert op, so a separate
 * "add" form would be a second arrangement of the same five controls, drifting
 * from this one the first time a field is added.
 *
 * Every control carries a stable id ending in the row's own — `policy-name-<id>`
 * and so on — so a label points at its field and a test can reach the row it
 * means without counting siblings.
 */
function PolicyRow({ rowKey, saved, draft, busy, onEdit, onSave, onDelete }: PolicyRowProps) {
  const base = saved ? draftOf(saved) : BLANK;
  const value = draft ?? base;
  const isNew = rowKey === NEW_ROW;
  const dirty = draft !== undefined;
  // A policy with no name is one the head cannot name when it acts, so the
  // server refuses it. The button says so by being unpressable.
  const nameable = value.name.trim().length > 0;

  return (
    <section className="rounded-lg border border-border/70 bg-card/40 p-3">
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-[12rem] flex-1">
          <label
            htmlFor={`policy-name-${rowKey}`}
            className="mb-1 block text-[11px] font-medium text-muted-foreground"
          >
            Name
          </label>
          <Input
            id={`policy-name-${rowKey}`}
            value={value.name}
            placeholder={isNew ? "keep-the-tests-green" : undefined}
            onChange={(e) => onEdit(rowKey, { name: e.target.value }, base)}
            className="h-8"
          />
        </div>
        <div className="w-24">
          <label
            htmlFor={`policy-in-flight-${rowKey}`}
            className="mb-1 block text-[11px] font-medium text-muted-foreground"
          >
            At once
          </label>
          <Input
            id={`policy-in-flight-${rowKey}`}
            type="number"
            min={MIN_BUDGET}
            value={value.budgetInFlight}
            onChange={(e) =>
              onEdit(
                rowKey,
                { budgetInFlight: numberOf(e.target.value, base.budgetInFlight) },
                base,
              )
            }
            className="h-8"
          />
        </div>
        <div className="w-24">
          <label
            htmlFor={`policy-per-day-${rowKey}`}
            className="mb-1 block text-[11px] font-medium text-muted-foreground"
          >
            Per day
          </label>
          <Input
            id={`policy-per-day-${rowKey}`}
            type="number"
            min={MIN_BUDGET}
            value={value.budgetPerDay}
            onChange={(e) =>
              onEdit(rowKey, { budgetPerDay: numberOf(e.target.value, base.budgetPerDay) }, base)
            }
            className="h-8"
          />
        </div>
        {/* A real checkbox, `role="switch"`: this is the control that decides
            whether the words below are in force, and the native element already
            knows how to be reached and toggled. */}
        <label
          htmlFor={`policy-enabled-${rowKey}`}
          className="flex h-8 cursor-pointer select-none items-center gap-1.5 text-xs text-muted-foreground"
        >
          <input
            id={`policy-enabled-${rowKey}`}
            type="checkbox"
            role="switch"
            // `role="switch"` needs its own state spelled out: the role stops a
            // reader calling it a checkbox, and then `checked` alone is not what
            // it reads.
            aria-checked={value.enabled}
            checked={value.enabled}
            onChange={(e) => onEdit(rowKey, { enabled: e.target.checked }, base)}
            className="size-3.5 cursor-pointer accent-primary"
          />
          Enabled
        </label>
      </div>
      <div className="mt-3">
        <label
          htmlFor={`policy-text-${rowKey}`}
          className="mb-1 block text-[11px] font-medium text-muted-foreground"
        >
          Instruction
        </label>
        <Textarea
          id={`policy-text-${rowKey}`}
          value={value.text}
          rows={3}
          placeholder={
            isNew ? "When a session's tests go red overnight, start a session to fix them." : ""
          }
          onChange={(e) => onEdit(rowKey, { text: e.target.value }, base)}
          className="min-h-[68px] font-mono text-xs"
        />
      </div>
      <div className="mt-3 flex items-center gap-2">
        <Button
          size="xs"
          id={`policy-save-${rowKey}`}
          disabled={busy || !nameable || (!isNew && !dirty)}
          onClick={() => onSave(rowKey, value)}
        >
          {isNew ? <Plus className="size-3" /> : <Save className="size-3" />}
          {isNew ? "Add" : "Save"}
        </Button>
        {onDelete && saved?.id && (
          <Button
            size="xs"
            variant="outline"
            id={`policy-delete-${rowKey}`}
            disabled={busy}
            onClick={() => onDelete(saved.id as string)}
          >
            <Trash2 className="size-3" />
            Delete
          </Button>
        )}
        {saved?.lastFiredAt && (
          <span className="ml-auto font-mono text-[10px] text-muted-foreground-faint">
            last acted {saved.lastFiredAt}
          </span>
        )}
      </div>
    </section>
  );
}

/**
 * A budget as a number, or the value it had.
 *
 * An empty field, a half-typed minus sign and a zero all keep the previous
 * value: a blank field is somebody retyping a limit, not a policy they just
 * switched off, and zero is a number the wire cannot carry ([MIN_BUDGET]) — the
 * server would answer 3 to it, which is the one thing worse than refusing it.
 */
function numberOf(raw: string, fallback: number): number {
  const n = Number.parseInt(raw, 10);
  if (Number.isNaN(n) || n < MIN_BUDGET) return fallback;
  return n;
}
