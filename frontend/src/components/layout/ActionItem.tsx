import { cn } from "~/lib/utils";

interface ActionItemProps {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  onClick: () => void;
  destructive?: boolean;
  disabled?: boolean;
}

export function ActionItem({ icon: Icon, label, onClick, destructive, disabled }: ActionItemProps) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      className={cn(
        "flex w-full items-center gap-2 px-3 py-1.5 text-sm transition-colors cursor-pointer",
        destructive
          ? "text-destructive hover:bg-destructive/10"
          : "text-popover-foreground hover:bg-accent",
        disabled && "opacity-50 cursor-default",
      )}
    >
      <Icon className="size-3.5" />
      {label}
    </button>
  );
}
