import { Outlet, createRootRoute } from "@tanstack/react-router";
import { Menu } from "lucide-react";
import { Toaster } from "sonner";
import { AppSidebar } from "~/components/layout/AppSidebar";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { TooltipProvider } from "~/components/ui/tooltip";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useAppStore } from "~/stores/app-store";

export const Route = createRootRoute({
  component: RootLayout,
});

function RootLayout() {
  const isMobile = useIsMobile();
  const sidebarOpen = useAppStore((s) => s.sidebarOpen);
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);

  return (
    <TooltipProvider>
      <div className="flex h-screen">
        {isMobile ? (
          <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
            <SheetContent side="left" className="w-72 p-0" showCloseButton={false}>
              <SheetTitle className="sr-only">Navigation</SheetTitle>
              <SheetDescription className="sr-only">
                Project and session navigation
              </SheetDescription>
              <AppSidebar />
            </SheetContent>
          </Sheet>
        ) : (
          <AppSidebar className="w-72 border-r" />
        )}
        <main className="flex-1 flex flex-col overflow-hidden">
          {isMobile && (
            <div className="h-11 border-b px-3 flex items-center gap-2 shrink-0">
              <button
                type="button"
                onClick={() => setSidebarOpen(true)}
                className="h-11 w-11 flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors -ml-3"
                aria-label="Open sidebar"
              >
                <Menu className="h-5 w-5" />
              </button>
              <span className="text-sm font-semibold">Agentique</span>
            </div>
          )}
          <Outlet />
        </main>
        <Toaster
          theme="dark"
          position="bottom-right"
          toastOptions={{
            style: {
              background: "var(--muted)",
              color: "var(--foreground)",
              border: "1px solid var(--border)",
            },
          }}
        />
      </div>
    </TooltipProvider>
  );
}
