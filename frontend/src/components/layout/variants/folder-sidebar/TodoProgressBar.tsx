import { memo, useEffect, useRef, useState } from "react";
import type { TodoProgress } from "./types";
import { INDENT_PX } from "./types";

// --- Particle system ---

interface Particle {
  id: number;
  x: number;
  dx: number;
  dy: number;
  size: number;
  duration: number;
}

let particleId = 0;

function spawnConfetti(count: number): Particle[] {
  const particles: Particle[] = [];
  for (let i = 0; i < count; i++) {
    particles.push({
      id: particleId++,
      x: Math.random() * 100,
      dx: (Math.random() - 0.5) * 24,
      dy: -(Math.random() * 18 + 6),
      size: Math.random() * 2.5 + 1,
      duration: Math.random() * 400 + 400,
    });
  }
  return particles;
}

const SparkParticles = memo(function SparkParticles({ particles }: { particles: Particle[] }) {
  return (
    <>
      {particles.map((p) => (
        <span
          key={p.id}
          className="absolute pointer-events-none todo-particle"
          style={
            {
              left: `${p.x}%`,
              top: "50%",
              "--dx": `${p.dx}px`,
              "--dy": `${p.dy}px`,
              width: p.size,
              height: p.size,
              animationDuration: `${p.duration}ms`,
            } as React.CSSProperties
          }
        />
      ))}
    </>
  );
});

// --- TodoProgressBar ---

interface TodoProgressBarProps {
  indent: number;
  progress: TodoProgress;
}

export const TodoProgressBar = memo(function TodoProgressBar({
  indent,
  progress,
}: TodoProgressBarProps) {
  const pct = progress.total > 0 ? (progress.done / progress.total) * 100 : 0;
  const isComplete = progress.done >= progress.total;
  const leftPx = `${indent * INDENT_PX}px`;

  const prevPctRef = useRef(pct);
  const [showFlash, setShowFlash] = useState(false);
  const [particles, setParticles] = useState<Particle[]>([]);

  useEffect(() => {
    const prev = prevPctRef.current;
    prevPctRef.current = pct;
    if (prev === pct) return;

    if (pct === 100 && prev > 0) {
      setParticles((p) => [...p, ...spawnConfetti(12)]);
      setShowFlash(true);
      const timer = setTimeout(() => setShowFlash(false), 800);
      return () => clearTimeout(timer);
    }
  }, [pct]);

  useEffect(() => {
    if (particles.length === 0) return;
    const maxDuration = Math.max(...particles.map((p) => p.duration));
    const timer = setTimeout(() => setParticles([]), maxDuration + 50);
    return () => clearTimeout(timer);
  }, [particles]);

  return (
    <>
      {/* Bottom progress bar */}
      {!isComplete && (
        <div className="absolute bottom-0 right-2 h-[3px]" style={{ left: leftPx }}>
          <div className="absolute inset-0 rounded-full todo-track" />
          <div
            className="h-full todo-bar transition-[width] duration-500 ease-out relative rounded-full"
            style={{ width: `${pct}%` }}
          />
          {pct > 0 && pct < 100 && (
            <div
              className="absolute top-1/2 todo-bar-spark transition-[left] duration-500 ease-out"
              style={{ left: `${pct}%` }}
            />
          )}
        </div>
      )}

      {/* Completion shimmer */}
      {showFlash && (
        <div
          className="absolute inset-y-0 right-0 overflow-hidden rounded-sm pointer-events-none"
          style={{ left: leftPx }}
        >
          <div className="absolute inset-0 todo-complete-flash" />
        </div>
      )}

      {/* Particles */}
      {particles.length > 0 && (
        <div className="absolute bottom-0 right-2 h-0 pointer-events-none" style={{ left: leftPx }}>
          <SparkParticles particles={particles} />
        </div>
      )}
    </>
  );
});
