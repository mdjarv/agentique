import { Outlet, createRootRoute } from "@tanstack/react-router";
import { AppSidebar } from "~/components/layout/AppSidebar";

export const Route = createRootRoute({
  component: RootLayout,
});

function RootLayout() {
  return (
    <div className="flex h-screen">
      <AppSidebar />
      <main className="flex-1 flex flex-col overflow-hidden">
        <Outlet />
      </main>
    </div>
  );
}
