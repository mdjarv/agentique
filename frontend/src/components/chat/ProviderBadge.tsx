import { cn } from "~/lib/utils";

interface ProviderBadgeProps {
  provider?: string;
  className?: string;
  size?: "xs" | "sm";
}

export function ProviderBadge({ provider, className, size = "xs" }: ProviderBadgeProps) {
  if (!provider || provider === "claude") return null;
  return (
    <span
      className={cn(
        "inline-flex items-center rounded uppercase tracking-wider font-semibold leading-none",
        "bg-warning/15 text-warning",
        size === "xs" ? "text-[9px] px-1 py-0.5" : "text-[10px] px-1.5 py-0.5",
        className,
      )}
      title={`Provider: ${provider}`}
    >
      {provider}
    </span>
  );
}
