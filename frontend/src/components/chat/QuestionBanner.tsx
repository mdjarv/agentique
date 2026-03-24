import { MessageSquare, Send } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import { resolveQuestion } from "~/lib/session-actions";
import type { PendingQuestion, Question } from "~/stores/chat-store";

interface QuestionBannerProps {
  sessionId: string;
  pending: PendingQuestion;
}

function OptionsInput({
  question,
  value,
  onChange,
}: {
  question: Question;
  value: string;
  onChange: (value: string) => void;
}) {
  if (question.multiSelect) {
    const selected = new Set(value ? value.split("\n") : []);
    return (
      <div className="flex flex-col gap-1">
        {question.options?.map((opt) => (
          <label
            key={opt.label}
            className="flex items-start gap-2 cursor-pointer rounded px-1.5 py-1 hover:bg-muted/50"
          >
            <input
              type="checkbox"
              className="mt-0.5 accent-purple-500"
              checked={selected.has(opt.label)}
              onChange={(e) => {
                const next = new Set(selected);
                if (e.target.checked) {
                  next.add(opt.label);
                } else {
                  next.delete(opt.label);
                }
                onChange([...next].join("\n"));
              }}
            />
            <span className="text-sm">
              <span className="font-medium">{opt.label}</span>
              {opt.description && (
                <span className="text-muted-foreground ml-1">{opt.description}</span>
              )}
            </span>
          </label>
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1">
      {question.options?.map((opt) => (
        <label
          key={opt.label}
          className="flex items-start gap-2 cursor-pointer rounded px-1.5 py-1 hover:bg-muted/50"
        >
          <input
            type="radio"
            className="mt-0.5 accent-purple-500"
            name={question.question}
            checked={value === opt.label}
            onChange={() => onChange(opt.label)}
          />
          <span className="text-sm">
            <span className="font-medium">{opt.label}</span>
            {opt.description && (
              <span className="text-muted-foreground ml-1">{opt.description}</span>
            )}
          </span>
        </label>
      ))}
    </div>
  );
}

function QuestionInput({
  question,
  value,
  onChange,
}: {
  question: Question;
  value: string;
  onChange: (value: string) => void;
}) {
  const hasOptions = question.options && question.options.length > 0;
  const [customMode, setCustomMode] = useState(false);

  if (!hasOptions) {
    return (
      <textarea
        className="w-full rounded border border-border bg-background px-2 py-1.5 text-sm resize-none"
        placeholder="Type your response..."
        rows={2}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  if (customMode) {
    return (
      <div className="flex flex-col gap-1.5">
        <textarea
          ref={(el) => el?.focus()}
          className="w-full rounded border border-border bg-background px-2 py-1.5 text-sm resize-none"
          placeholder="Type your response..."
          rows={3}
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
        <button
          type="button"
          className="self-start text-xs text-purple-400 hover:text-purple-300"
          onClick={() => {
            onChange("");
            setCustomMode(false);
          }}
        >
          Back to options
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      <OptionsInput question={question} value={value} onChange={onChange} />
      <button
        type="button"
        className="self-start text-xs text-purple-400 hover:text-purple-300 flex items-center gap-1"
        onClick={() => {
          onChange("");
          setCustomMode(true);
        }}
      >
        <MessageSquare className="h-3 w-3" />
        Type a custom response
      </button>
    </div>
  );
}

export function QuestionBanner({ sessionId, pending }: QuestionBannerProps) {
  const ws = useWebSocket();
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);

  const setAnswer = useCallback((questionText: string, value: string) => {
    setAnswers((prev) => ({ ...prev, [questionText]: value }));
  }, []);

  const handleSubmit = useCallback(() => {
    setSubmitting(true);
    resolveQuestion(ws, sessionId, pending.questionId, answers).catch((err) => {
      setSubmitting(false);
      toast.error(err instanceof Error ? err.message : "Failed to submit answer");
    });
  }, [ws, sessionId, pending.questionId, answers]);

  const allAnswered = pending.questions.every((q) => (answers[q.question] ?? "").trim() !== "");

  return (
    <div className="mx-4 mb-2 rounded-md border border-purple-500/40 bg-purple-500/10 px-3 py-2">
      <div className="flex flex-col gap-3">
        {pending.questions.map((q) => (
          <div key={q.question} className="flex flex-col gap-1.5">
            {q.header && (
              <span className="text-xs font-semibold uppercase tracking-wide text-purple-400">
                {q.header}
              </span>
            )}
            <span className="text-sm font-medium">{q.question}</span>
            <QuestionInput
              question={q}
              value={answers[q.question] ?? ""}
              onChange={(v) => setAnswer(q.question, v)}
            />
          </div>
        ))}
        <div className="flex justify-end">
          <Button
            size="sm"
            className="h-7 px-3 bg-purple-600 hover:bg-purple-700 text-white"
            disabled={!allAnswered || submitting}
            onClick={handleSubmit}
          >
            <Send className="h-3.5 w-3.5 mr-1" />
            Submit
          </Button>
        </div>
      </div>
    </div>
  );
}
