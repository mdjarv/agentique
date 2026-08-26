import { createRootRoute, Outlet } from "@tanstack/react-router";
import { useEffect } from "react";
import { Toaster } from "sonner";
import { LoginPage } from "~/components/auth/LoginPage";
import { ErrorBoundary } from "~/components/ErrorBoundary";
import { useWireCapture } from "~/components/home/use-wire-capture";
import { AppSidebar } from "~/components/layout/AppSidebar";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "~/components/ui/sheet";
import { TooltipProvider } from "~/components/ui/tooltip";
import { VoiceStrip } from "~/components/voice/VoiceStrip";
import { useActiveProjectFetch } from "~/hooks/git/useActiveProjectFetch";
import { useSyncSweep } from "~/hooks/git/useSyncSweep";
import { useBrowserStatusSync } from "~/hooks/useBrowserStatusSync";
import { useGlobalSubscriptions } from "~/hooks/useGlobalSubscriptions";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useMachineConnections } from "~/hooks/useMachineConnections";
import { usePreventViewportScroll } from "~/hooks/usePreventViewportScroll";
import { useProjects } from "~/hooks/useProjects";
import { useTheme } from "~/hooks/useTheme";
import { useUpdateChecks } from "~/hooks/useUpdateChecks";
import { useVoiceFocusNavigation } from "~/hooks/useVoiceFocusNavigation";
import { useAppStore } from "~/stores/app-store";
import { useAuthStore } from "~/stores/auth-store";
import { useChatStore } from "~/stores/chat-store";
import { useFeatureStore } from "~/stores/feature-store";

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
  useWireCapture();
  useMachineConnections();
  useSyncSweep(projects);
  useActiveProjectFetch();
  useUpdateChecks();
  usePreventViewportScroll();
  // The screen follows the voice: a `focus` frame navigates, wherever the
  // operator happens to be.
  useVoiceFocusNavigation();

  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const browserEnabled = useFeatureStore((s) => s.features.browser);
  useBrowserStatusSync(browserEnabled ? activeSessionId : null);

  const { resolvedTheme } = useTheme();
  const isMobile = useIsMobile();
  const sidebarOpen = useAppStore((s) => s.sidebarOpen);
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);
  const loadFeatures = useFeatureStore((s) => s.load);

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
          {/* The session dock mounts inside ChatPanel, beside the transcript:
              it is session chrome, and it needs the session state that panel
              already holds. The call is the opposite — mounted here, not in the
              chat tree, because it outlives every route it navigates to and
              hanging up must be reachable from all of them. */}
          <VoiceStrip />
          <Toaster
            theme={resolvedTheme}
            position={isMobile ? "top-center" : "bottom-right"}
            swipeDirections={isMobile ? ["top", "left", "right"] : ["right", "bottom"]}
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
