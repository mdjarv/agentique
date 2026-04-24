import { useEffect, useState } from "react";

const LARGE_BREAKPOINT = 1024;

export function useIsLarge(): boolean {
  const [isLarge, setIsLarge] = useState(() => window.innerWidth >= LARGE_BREAKPOINT);

  useEffect(() => {
    const mql = window.matchMedia(`(min-width: ${LARGE_BREAKPOINT}px)`);
    const onChange = (e: MediaQueryListEvent) => setIsLarge(e.matches);
    mql.addEventListener("change", onChange);
    setIsLarge(mql.matches);
    return () => mql.removeEventListener("change", onChange);
  }, []);

  return isLarge;
}
