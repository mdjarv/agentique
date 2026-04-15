import { Loader2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { useBrowserStore } from "~/stores/browser-store";

function cdpModifiers(e: React.MouseEvent | React.KeyboardEvent): number {
  let m = 0;
  if (e.altKey) m |= 1;
  if (e.ctrlKey) m |= 2;
  if (e.metaKey) m |= 4;
  if (e.shiftKey) m |= 8;
  return m;
}

function mouseButton(e: React.MouseEvent, type: string): string {
  if (type === "mouseMoved") return "none";
  if (e.button === 0) return "left";
  if (e.button === 2) return "right";
  return "middle";
}

interface BrowserCanvasProps {
  sessionId: string;
  zoom: number;
}

export function BrowserCanvas({ sessionId, zoom }: BrowserCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const ws = useWebSocket();
  const frameRef = useBrowserStore((s) => s.sessions[sessionId]?.frameRef);
  const metadata = useBrowserStore((s) => s.sessions[sessionId]?.metadata ?? null);
  const [showSpinner, setShowSpinner] = useState(true);
  const firstFrameDrawn = useRef(false);
  const lastMoveTime = useRef(0);

  // rAF loop: poll frameRef, decode JPEG, draw to canvas
  // Also resets spinner on session switch (frameRef changes).
  useEffect(() => {
    firstFrameDrawn.current = false;
    setShowSpinner(true);
    if (!frameRef) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    let lastData: string | null = null;
    let animId: number;

    const render = () => {
      const data = frameRef.current;
      if (data && data !== lastData) {
        lastData = data;
        const img = new Image();
        img.onload = () => {
          if (canvas.width !== img.naturalWidth) canvas.width = img.naturalWidth;
          if (canvas.height !== img.naturalHeight) canvas.height = img.naturalHeight;
          ctx.drawImage(img, 0, 0);
          if (!firstFrameDrawn.current) {
            firstFrameDrawn.current = true;
            setShowSpinner(false);
          }
        };
        img.src = `data:image/jpeg;base64,${data}`;
      }
      animId = requestAnimationFrame(render);
    };

    animId = requestAnimationFrame(render);
    return () => cancelAnimationFrame(animId);
  }, [frameRef]);

  const sendMouse = useCallback(
    (e: React.MouseEvent<HTMLCanvasElement>, type: string) => {
      const canvas = canvasRef.current;
      if (!canvas || !metadata) return;

      if (type === "mouseMoved") {
        const now = performance.now();
        if (now - lastMoveTime.current < 50) return;
        lastMoveTime.current = now;
      }

      const rect = canvas.getBoundingClientRect();
      const scaleX = metadata.deviceWidth / rect.width;
      const scaleY = metadata.deviceHeight / rect.height;

      ws.request("browser.input", {
        sessionId,
        inputType: "mouse",
        type,
        x: (e.clientX - rect.left) * scaleX,
        y: (e.clientY - rect.top) * scaleY,
        button: mouseButton(e, type),
        clickCount: type === "mouseMoved" ? 0 : 1,
        modifiers: cdpModifiers(e),
      }).catch((err) => console.error("browser.input (mouse) failed", err));
    },
    [sessionId, metadata, ws],
  );

  const sendKey = useCallback(
    (e: React.KeyboardEvent<HTMLCanvasElement>, type: string) => {
      e.preventDefault();
      ws.request("browser.input", {
        sessionId,
        inputType: "key",
        type,
        key: e.key,
        code: e.code,
        text: type === "keyDown" && e.key.length === 1 ? e.key : "",
        modifiers: cdpModifiers(e),
      }).catch((err) => console.error("browser.input (key) failed", err));
    },
    [sessionId, ws],
  );

  // Use metadata dimensions for the wrapper so overflow-auto scrollbar covers the
  // scaled canvas. metadata re-renders on dimension change; canvas.width doesn't.
  const scaledW = metadata ? metadata.deviceWidth * zoom : 0;
  const scaledH = metadata ? metadata.deviceHeight * zoom : 0;

  return (
    <div className="relative flex-1 min-h-0 overflow-auto bg-neutral-950">
      <div
        style={{
          width: scaledW || "100%",
          height: scaledH || "100%",
        }}
      >
        <canvas
          ref={canvasRef}
          tabIndex={0}
          className="block outline-none"
          style={{
            transform: `scale(${zoom})`,
            transformOrigin: "top left",
          }}
          onMouseDown={(e) => sendMouse(e, "mousePressed")}
          onMouseUp={(e) => sendMouse(e, "mouseReleased")}
          onMouseMove={(e) => sendMouse(e, "mouseMoved")}
          onKeyDown={(e) => sendKey(e, "keyDown")}
          onKeyUp={(e) => sendKey(e, "keyUp")}
          onContextMenu={(e) => e.preventDefault()}
        />
      </div>
      {showSpinner && (
        <div className="absolute inset-0 flex items-center justify-center">
          <Loader2 className="size-8 animate-spin text-muted-foreground" />
        </div>
      )}
    </div>
  );
}
