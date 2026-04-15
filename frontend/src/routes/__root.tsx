import { createRootRoute, Outlet } from "@tanstack/react-router";
import { useCallback, useEffect, useRef } from "react";
import { Toaster } from "sonner";
import { LoginPage } from "~/components/auth/LoginPage";
import { BrowserPanel } from "~/components/browser/BrowserPanel";
import { ErrorBoundary } from "~/components/ErrorBoundary";
import { AppSidebar } from "~/components/layout/AppSidebar";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { TooltipProvider } from "~/components/ui/tooltip";
import { useProjectGitPolling } from "~/hooks/git/useProjectGitPolling";
import { useBrowserStatusSync } from "~/hooks/useBrowserStatusSync";
import { useGlobalSubscriptions } from "~/hooks/useGlobalSubscriptions";
import { useIsMobile } from "~/hooks/useIsMobile";
import { usePreventViewportScroll } from "~/hooks/usePreventViewportScroll";
import { useProjects } from "~/hooks/useProjects";
import { useTheme } from "~/hooks/useTheme";
import { useAppStore } from "~/stores/app-store";
import { useAuthStore } from "~/stores/auth-store";
import { useChatStore } from "~/stores/chat-store";
import { useFeatureStore } from "~/stores/feature-store";
import { useUIStore } from "~/stores/ui-store";

export const Route = createRootRoute({
  component: RootLayout,
});

function RootLayout() {
  useTheme();
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
  usePreventViewportScroll();

  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const browserEnabled = useFeatureStore((s) => s.features.browser);
  useBrowserStatusSync(browserEnabled ? activeSessionId : null);

  const { resolvedTheme } = useTheme();
  const isMobile = useIsMobile();
  const sidebarOpen = useAppStore((s) => s.sidebarOpen);
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);
  const rightPanelCollapsed = useUIStore((s) => s.rightPanelCollapsed);
  const browserPanelWidth = useUIStore((s) => s.browserPanelWidth);
  const setBrowserPanelWidth = useUIStore((s) => s.setBrowserPanelWidth);
  const loadFeatures = useFeatureStore((s) => s.load);

  const dragRef = useRef<{ startX: number; startWidth: number } | null>(null);

  const handleDragStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      const currentWidth = useUIStore.getState().browserPanelWidth;
      dragRef.current = { startX: e.clientX, startWidth: currentWidth };

      const onMove = (ev: MouseEvent) => {
        if (!dragRef.current) return;
        const delta = dragRef.current.startX - ev.clientX;
        setBrowserPanelWidth(dragRef.current.startWidth + delta);
      };
      const onUp = () => {
        dragRef.current = null;
        document.removeEventListener("mousemove", onMove);
        document.removeEventListener("mouseup", onUp);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.addEventListener("mousemove", onMove);
      document.addEventListener("mouseup", onUp);
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
    },
    [setBrowserPanelWidth],
  );

  useEffect(() => {
    loadFeatures();
  }, [loadFeatures]);

  return (
    <ErrorBoundary>
      <TooltipProvider>
        <div className="flex h-dvh">
          {isMobile ? (
            <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
              <SheetContent side="left" className="w-[85vw] p-0" showCloseButton={false}>
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
          <main className="flex-1 flex flex-col overflow-hidden min-w-0">
            <ErrorBoundary>
              <Outlet />
            </ErrorBoundary>
          </main>
          {browserEnabled && !isMobile && !rightPanelCollapsed && activeSessionId && (
            <div
              className="border-l flex flex-col shrink-0 relative"
              style={{ width: browserPanelWidth }}
            >
              <div
                className="absolute left-0 top-0 bottom-0 w-1 cursor-col-resize z-10 hover:bg-primary/20 active:bg-primary/30"
                onMouseDown={handleDragStart}
              />
              <ErrorBoundary>
                <BrowserPanel sessionId={activeSessionId} />
              </ErrorBoundary>
            </div>
          )}
          <Toaster
            theme={resolvedTheme}
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
    </ErrorBoundary>
  );
}
