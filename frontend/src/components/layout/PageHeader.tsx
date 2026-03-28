import { Menu } from "lucide-react";
import type { ReactNode } from "react";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useAppStore } from "~/stores/app-store";

interface PageHeaderProps {
  children?: ReactNode;
}

export function PageHeader({ children }: PageHeaderProps) {
  const isMobile = useIsMobile();
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);

  return (
    <header className="border-b px-4 py-2 flex items-center gap-2 text-sm shrink-0">
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
