import { registerSW } from "virtual:pwa-register";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { router } from "./router";
import "./index.css";

// Auto-update SW: check every 60s, reload immediately when a new version is available.
// This ensures phones always run the latest version without manual refresh.
registerSW({
  immediate: true,
  onRegisteredSW(_url, registration) {
    if (registration) {
      setInterval(() => registration.update(), 60_000);
    }
  },
});

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Root element not found");

async function enableMocking() {
  if (import.meta.env.VITE_MSW !== "true") return;
  const { worker } = await import("./mocks/browser");
  return worker.start({ onUnhandledRequest: "bypass" });
}

enableMocking().then(() => {
  createRoot(rootElement).render(
    <StrictMode>
      <RouterProvider router={router} />
    </StrictMode>,
  );
});
