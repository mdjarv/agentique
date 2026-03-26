import { Outlet, createRootRoute } from "@tanstack/react-router";
import { Menu } from "lucide-react";
import { useEffect } from "react";
import { Toaster } from "sonner";
import { LoginPage } from "~/components/auth/LoginPage";
import { AppSidebar } from "~/components/layout/AppSidebar";
import { ConnectionIndicator } from "~/components/layout/ConnectionIndicator";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { TooltipProvider } from "~/components/ui/tooltip";
import { useGlobalSubscriptions } from "~/hooks/useGlobalSubscriptions";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useProjectGitPolling } from "~/hooks/useProjectGitPolling";
import { useProjects } from "~/hooks/useProjects";
import { useAppStore } from "~/stores/app-store";
import { useAuthStore } from "~/stores/auth-store";

export const Route = createRootRoute({
  component: RootLayout,
});

function RootLayout() {
  const { authEnabled, authenticated, loading, checkAuth } = useAuthStore();

  useEffect(() => {
    checkAuth();
  }, [checkAuth]);

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <p className="text-muted-foreground">Loading...</p>
      </div>
    );
  }

  if (authEnabled && !authenticated) {
    return <LoginPage />;
  }

  return <AuthenticatedLayout />;
}

function AuthenticatedLayout() {
  const projects = useProjects();
  useGlobalSubscriptions(projects);
  useProjectGitPolling(projects);

  const isMobile = useIsMobile();
  const sidebarOpen = useAppStore((s) => s.sidebarOpen);
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);
  return (
    <TooltipProvider>
      <div className="flex h-dvh">
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
              <div className="ml-auto">
                <ConnectionIndicator />
              </div>
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
