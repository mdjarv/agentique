/**
 * This machine's findings at popover density, above the meters: what is wrong
 * and what fixes it. No button, because every kind shown here is a fix only a
 * person at the machine can make — a row offering one would be a button that
 * can only fail.
 */
import { useMemo } from "react";
import { FINDING_GLYPH, findingRow, footerFindings } from "~/lib/steward";
import { useStewardStore } from "~/stores/steward-store";

export function FindingsPopoverRows() {
  const findings = useStewardStore((s) => s.findings);
  const shown = useMemo(() => footerFindings(findings), [findings]);
  if (shown.length === 0) return null;
  const Icon = FINDING_GLYPH;

  return (
    <div className="flex flex-col">
      <div className="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground-faint">
        Needs a look
      </div>
      {shown.map((f) => {
        const row = findingRow(f);
        return (
          <div key={`${f.kind}:${f.subject ?? ""}`} className="flex items-start gap-2 px-3 py-1.5">
            <Icon aria-hidden className="mt-0.5 size-3.5 shrink-0 text-warning" />
            <div className="min-w-0">
              <div className="text-xs text-foreground">{row.label}</div>
              {row.detail && (
                <div className="text-[11px] leading-snug text-muted-foreground">{row.detail}</div>
              )}
            </div>
          </div>
        );
      })}
      <div className="mx-2 my-1 h-px bg-border/60" />
    </div>
  );
}
