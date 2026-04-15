import { ArrowLeft, ArrowRight, Globe, Minus, Plus, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { ScreencastMetadata } from "~/stores/browser-store";
import { useBrowserStore } from "~/stores/browser-store";
import { BrowserCanvas } from "./BrowserCanvas";

interface BrowserPanelProps {
  sessionId: string;
}

export function BrowserPanel({ sessionId }: BrowserPanelProps) {
  const ws = useWebSocket();
  const launched = useBrowserStore((s) => s.sessions[sessionId]?.launched ?? false);
  const launching = useBrowserStore((s) => s.sessions[sessionId]?.launching ?? false);
  const zoom = useBrowserStore((s) => s.sessions[sessionId]?.zoom ?? 1.0);
  const storeUrl = useBrowserStore((s) => s.sessions[sessionId]?.url ?? "");
  const [urlInput, setUrlInput] = useState("");
  const prevSessionId = useRef(sessionId);

  // Sync URL bar from store, and reset on session switch
  if (prevSessionId.current !== sessionId) {
    prevSessionId.current = sessionId;
    setUrlInput(storeUrl || "");
  } else if (storeUrl && !urlInput) {
    setUrlInput(storeUrl);
  }

  // Subscribe to push events scoped to this session
  useEffect(() => {
    const unsubFrame = ws.subscribe(
      "browser.frame",
      (payload: { sessionId: string; data: string; metadata: ScreencastMetadata }) => {
        if (payload.sessionId !== sessionId) return;
        useBrowserStore.getState().updateFrame(sessionId, payload.data, payload.metadata);
      },
    );

    const unsubStopped = ws.subscribe(
      "browser.stopped",
      (payload: { sessionId: string; reason: string }) => {
        if (payload.sessionId !== sessionId) return;
        useBrowserStore.getState().setStopped(sessionId);
      },
    );

    return () => {
      unsubFrame();
      unsubStopped();
    };
  }, [ws, sessionId]);

  const handleLaunch = () => {
    useBrowserStore.getState().setLaunching(sessionId);
    ws.request("browser.launch", { sessionId })
      .then(() => useBrowserStore.getState().setLaunched(sessionId))
      .catch((err) => {
        console.error("browser.launch failed", err);
        useBrowserStore.getState().setStopped(sessionId);
      });
  };

  const handleStop = () => {
    ws.request("browser.stop", { sessionId }).catch((err) => {
      console.error("browser.stop failed", err);
      toast.error("Failed to stop browser");
    });
    useBrowserStore.getState().setStopped(sessionId);
  };

  const handleNavigate = (url: string) => {
    const trimmed = url.trim();
    if (!trimmed) return;
    ws.request("browser.navigate", { sessionId, url: trimmed }).catch((err) => {
      console.error("browser.navigate failed", err);
      toast.error("Navigation failed");
    });
  };

  const handleZoomIn = () => useBrowserStore.getState().setZoom(sessionId, zoom + 0.1);
  const handleZoomOut = () => useBrowserStore.getState().setZoom(sessionId, zoom - 0.1);
  const handleZoomReset = () => useBrowserStore.getState().setZoom(sessionId, 1.0);

  if (!launched && !launching) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-2 text-muted-foreground">
        <Globe className="size-10 opacity-30" />
        <Button variant="outline" onClick={handleLaunch}>
          Open Browser
        </Button>
      </div>
    );
  }

  const zoomPercent = Math.round(zoom * 100);

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-1 px-2 py-1.5 border-b shrink-0">
        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0"
          onClick={() =>
            ws.request("browser.navigate", { sessionId, action: "back" }).catch((err) => {
              console.error("browser.navigate back failed", err);
              toast.error("Navigation failed");
            })
          }
        >
          <ArrowLeft className="size-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0"
          onClick={() =>
            ws.request("browser.navigate", { sessionId, action: "forward" }).catch((err) => {
              console.error("browser.navigate forward failed", err);
              toast.error("Navigation failed");
            })
          }
        >
          <ArrowRight className="size-3.5" />
        </Button>
        <Input
          className="flex-1 h-7 text-xs min-w-0"
          placeholder="Enter URL..."
          value={urlInput}
          onChange={(e) => setUrlInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") handleNavigate(urlInput);
          }}
        />
        <div className="flex items-center shrink-0">
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            onClick={handleZoomOut}
            disabled={zoom <= 0.25}
            title="Zoom out"
          >
            <Minus className="size-3" />
          </Button>
          <button
            type="button"
            className="text-[10px] text-muted-foreground w-8 text-center tabular-nums hover:text-foreground"
            onClick={handleZoomReset}
            title="Reset zoom"
          >
            {zoomPercent}%
          </button>
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            onClick={handleZoomIn}
            disabled={zoom >= 3.0}
            title="Zoom in"
          >
            <Plus className="size-3" />
          </Button>
        </div>
        <Button variant="ghost" size="icon" className="size-7 shrink-0" onClick={handleStop}>
          <X className="size-3.5" />
        </Button>
      </div>
      <BrowserCanvas sessionId={sessionId} zoom={zoom} />
    </div>
  );
}
