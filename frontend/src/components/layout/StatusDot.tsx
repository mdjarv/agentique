import { cn } from "~/lib/utils";

interface StatusDotProps {
  bg: string;
  text: string;
  bare?: boolean;
  dim?: boolean;
  ring?: boolean;
  title?: string;
  className?: string;
  style?: React.CSSProperties;
  children: React.ReactNode;
}

export function StatusDot({
  bg,
  text,
  bare,
  dim,
  ring,
  title,
  className,
  style,
  children,
}: StatusDotProps) {
  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center",
        !bare && "rounded-full",
        !bare && bg,
        text,
        dim && "opacity-40",
        ring && "ring-2 ring-current/40",
        className,
      )}
      style={style}
      title={title}
    >
      {children}
    </span>
  );
}
