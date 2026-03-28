import { Outlet, createRootRoute, useRouterState } from "@tanstack/react-router";
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
import { usePreventViewportScroll } from "~/hooks/usePreventViewportScroll";
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

function MobileHeaderTitle() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const projects = useAppStore((s) => s.projects);

  // /project/:slug/settings
  if (pathname.includes("/settings")) {
    const slug = pathname.split("/")[2];
    const name = projects.find((p) => p.slug === slug)?.name;
    return <span className="text-sm font-semibold truncate">{name ?? slug} — Settings</span>;
  }

  // /project/:slug/session/... or /project/:slug
  const slugMatch = pathname.match(/^\/project\/([^/]+)/);
  if (slugMatch) {
    const name = projects.find((p) => p.slug === slugMatch[1])?.name;
    return <span className="text-sm font-semibold truncate">{name ?? slugMatch[1]}</span>;
  }

  return <span className="text-sm font-semibold">Agentique</span>;
}

function AuthenticatedLayout() {
  const projects = useProjects();
  useGlobalSubscriptions(projects);
  useProjectGitPolling(projects);
  usePreventViewportScroll();

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
              <MobileHeaderTitle />
              <div className="ml-auto">
                <ConnectionIndicator />
              </div>
            </div>
          )}
          <Outlet />
        </main>
        <Toaster
          theme="dark"
          position={isMobile ? "top-center" : "bottom-right"}
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
