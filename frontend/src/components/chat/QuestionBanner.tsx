import { ChevronLeft, ChevronRight, MessageSquare, Send } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import { resolveQuestion } from "~/lib/session-actions";
import { cn } from "~/lib/utils";
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

function StepDots({
  questions,
  current,
  answeredSteps,
  onNavigate,
}: {
  questions: Question[];
  current: number;
  answeredSteps: Set<number>;
  onNavigate: (step: number) => void;
}) {
  return (
    <div className="flex items-center gap-1.5" role="tablist" aria-label="Question steps">
      {questions.map((q, i) => (
        <button
          key={q.question}
          type="button"
          role="tab"
          aria-selected={i === current}
          aria-label={`Question ${i + 1}`}
          className={cn(
            "h-2 w-2 rounded-full transition-colors",
            "p-0 before:content-[''] before:absolute before:inset-[-6px] relative",
            i === current
              ? "bg-purple-400"
              : answeredSteps.has(i)
                ? "bg-purple-400/50"
                : "bg-muted-foreground/30",
          )}
          onClick={() => onNavigate(i)}
        />
      ))}
    </div>
  );
}

function SingleQuestionBanner({
  question,
  value,
  onChange,
  onSubmit,
  submitting,
  answered,
}: {
  question: Question;
  value: string;
  onChange: (value: string) => void;
  onSubmit: () => void;
  submitting: boolean;
  answered: boolean;
}) {
  return (
    <div className="mx-4 mb-2 rounded-md border border-purple-500/40 bg-purple-500/10 px-3 py-2">
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1.5">
          {question.header && (
            <span className="text-xs font-semibold uppercase tracking-wide text-purple-400">
              {question.header}
            </span>
          )}
          <span className="text-sm font-medium">{question.question}</span>
          <QuestionInput question={question} value={value} onChange={onChange} />
        </div>
        <div className="flex justify-end">
          <Button
            size="sm"
            className="h-7 px-3 bg-purple-600 hover:bg-purple-700 text-white"
            disabled={!answered || submitting}
            onClick={onSubmit}
          >
            <Send className="h-3.5 w-3.5 mr-1" />
            Submit
          </Button>
        </div>
      </div>
    </div>
  );
}

function WizardQuestionBanner({
  questions,
  answers,
  setAnswer,
  onSubmit,
  submitting,
  allAnswered,
}: {
  questions: Question[];
  answers: Record<string, string>;
  setAnswer: (questionText: string, value: string) => void;
  onSubmit: () => void;
  submitting: boolean;
  allAnswered: boolean;
}) {
  const [step, setStep] = useState(0);
  const total = questions.length;
  const q = questions[step];
  if (!q) return null;
  const currentValue = answers[q.question] ?? "";
  const currentAnswered = currentValue.trim() !== "";
  const isLast = step === total - 1;

  const answeredSteps = new Set(
    questions.reduce<number[]>((acc, question, i) => {
      if ((answers[question.question] ?? "").trim() !== "") acc.push(i);
      return acc;
    }, []),
  );

  return (
    <div className="mx-4 mb-2 rounded-md border border-purple-500/40 bg-purple-500/10 px-3 py-2">
      <div className="flex flex-col gap-2">
        {/* Header: step label + dots */}
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted-foreground">
            {step + 1} / {total}
          </span>
          <StepDots
            questions={questions}
            current={step}
            answeredSteps={answeredSteps}
            onNavigate={setStep}
          />
        </div>

        {/* Current question */}
        <div className="flex flex-col gap-1.5">
          {q.header && (
            <span className="text-xs font-semibold uppercase tracking-wide text-purple-400">
              {q.header}
            </span>
          )}
          <span className="text-sm font-medium">{q.question}</span>
          <QuestionInput
            key={q.question}
            question={q}
            value={currentValue}
            onChange={(v) => setAnswer(q.question, v)}
          />
        </div>

        {/* Navigation */}
        <div className="flex items-center justify-between">
          <Button
            variant="ghost"
            size="sm"
            className="h-8 px-2 text-muted-foreground hover:text-foreground"
            disabled={step === 0}
            onClick={() => setStep(step - 1)}
          >
            <ChevronLeft className="h-4 w-4 mr-0.5" />
            Back
          </Button>

          {isLast ? (
            <Button
              size="sm"
              className="h-8 px-3 bg-purple-600 hover:bg-purple-700 text-white"
              disabled={!allAnswered || submitting}
              onClick={onSubmit}
            >
              <Send className="h-3.5 w-3.5 mr-1" />
              Submit
            </Button>
          ) : (
            <Button
              size="sm"
              className="h-8 px-3 bg-purple-600 hover:bg-purple-700 text-white"
              disabled={!currentAnswered}
              onClick={() => setStep(step + 1)}
            >
              Next
              <ChevronRight className="h-4 w-4 ml-0.5" />
            </Button>
          )}
        </div>
      </div>
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

  if (pending.questions.length === 1) {
    const q = pending.questions[0];
    if (!q) return null;
    return (
      <SingleQuestionBanner
        question={q}
        value={answers[q.question] ?? ""}
        onChange={(v) => setAnswer(q.question, v)}
        onSubmit={handleSubmit}
        submitting={submitting}
        answered={(answers[q.question] ?? "").trim() !== ""}
      />
    );
  }

  return (
    <WizardQuestionBanner
      questions={pending.questions}
      answers={answers}
      setAnswer={setAnswer}
      onSubmit={handleSubmit}
      submitting={submitting}
      allAnswered={allAnswered}
    />
  );
}
