// Minimal ambient types for d3-force-3d (the package ships none). Covers only the
// subset the 3D brain graph uses: a 3D force simulation with the standard forces.
declare module "d3-force-3d" {
  export interface SimNode {
    index?: number;
    x?: number;
    y?: number;
    z?: number;
    vx?: number;
    vy?: number;
    vz?: number;
    fx?: number | null;
    fy?: number | null;
    fz?: number | null;
  }

  export interface Force<N extends SimNode> {
    (alpha: number): void;
    initialize?(nodes: N[], random?: () => number, numDimensions?: number): void;
  }

  export interface Simulation<N extends SimNode> {
    tick(iterations?: number): this;
    restart(): this;
    stop(): this;
    alpha(): number;
    alpha(value: number): this;
    alphaMin(value: number): this;
    alphaDecay(value: number): this;
    velocityDecay(value: number): this;
    nodes(): N[];
    nodes(nodes: N[]): this;
    // biome-ignore lint/suspicious/noExplicitAny: force accessor is intentionally loose
    force(name: string): any;
    force(name: string, force: Force<N> | null): this;
    on(typenames: string, listener: ((this: Simulation<N>) => void) | null): this;
  }

  export function forceSimulation<N extends SimNode>(
    nodes?: N[],
    numDimensions?: number,
  ): Simulation<N>;

  // biome-ignore lint/suspicious/noExplicitAny: the d3-force builders are chainable and loosely typed here
  type AnyForce = any;
  export function forceManyBody(): AnyForce;
  export function forceLink<L = unknown>(links?: L[]): AnyForce;
  export function forceCenter(x?: number, y?: number, z?: number): AnyForce;
  export function forceX(x?: number): AnyForce;
  export function forceY(y?: number): AnyForce;
  export function forceZ(z?: number): AnyForce;
  export function forceCollide(radius?: number | ((d: unknown) => number)): AnyForce;
}
