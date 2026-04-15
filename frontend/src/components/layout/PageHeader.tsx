import { Link } from "@tanstack/react-router";
import { Menu } from "lucide-react";
import type { ReactNode } from "react";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useAppStore } from "~/stores/app-store";

interface PageHeaderProps {
  children?: ReactNode;
  /** Optional accent color for a top border (e.g. project color). */
  accentColor?: string;
}

export function PageHeader({ children, accentColor }: PageHeaderProps) {
  const isMobile = useIsMobile();
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);

  return (
    <header
      className="border-b bg-sidebar px-4 flex items-center gap-2 text-sm shrink-0 h-12"
      style={accentColor ? { borderBottomColor: `${accentColor}40` } : undefined}
    >
      {isMobile && (
        <button
          type="button"
          onClick={() => setSidebarOpen(true)}
          className="h-8 w-8 flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors -ml-2 shrink-0"
          aria-label="Open sidebar"
        >
          <Menu className="h-5 w-5" />
        </button>
      )}
      {children}
    </header>
  );
}

/** Full-page shell for loading/error/empty states — PageHeader + centered message. */
export function StatusPage({ header, message }: { header?: ReactNode; message: string }) {
  return (
    <div className="flex flex-col h-full">
      <PageHeader>{header}</PageHeader>
      <div className="flex-1 flex flex-col items-center justify-center gap-3 text-muted-foreground">
        <p className="text-sm">{message}</p>
        <Link to="/" className="text-sm text-primary hover:underline">
          Go to home
        </Link>
      </div>
    </div>
  );
}
