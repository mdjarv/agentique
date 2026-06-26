import {
  forceCenter,
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  forceZ,
  type SimNode,
} from "d3-force-3d";
import { useEffect, useMemo, useRef, useState } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import { EffectComposer } from "three/addons/postprocessing/EffectComposer.js";
import { OutputPass } from "three/addons/postprocessing/OutputPass.js";
import { RenderPass } from "three/addons/postprocessing/RenderPass.js";
import { UnrealBloomPass } from "three/addons/postprocessing/UnrealBloomPass.js";
import type { GraphLink } from "~/lib/brain-api";
import {
  type BrainColorBy,
  type BrainNode,
  buildBrainModel,
  type GraphMemory,
} from "~/lib/brain-graph-model";
import { areaColor, communityColor, coreColor, scopeColor } from "~/lib/scope-color";

// BrainGraph3D renders the brain as a true 3D force-directed graph (three.js + d3-force-3d):
// ember nodes (trust-by-heat core + scope-coloured halo) orbiting a central brain model,
// woven by a glowing semantic web with signal particles flowing along it. Mouse: drag to
// orbit, right-drag to pan, scroll to zoom, click a memory to focus it.

type Flags = {
  brain: boolean;
  links: boolean;
  particles: boolean;
  bloom: boolean;
  rotate: boolean;
  labels: boolean;
};

const DEFAULT_FLAGS: Flags = {
  brain: true,
  links: true,
  particles: true,
  bloom: true,
  rotate: false,
  labels: false,
};

type GraphApi = {
  applyColors: (colorBy: BrainColorBy) => void;
  syncFlags: (flags: Flags) => void;
};

export function BrainGraph3D({
  memories,
  links: semanticLinks,
  labelForScope,
}: {
  memories: GraphMemory[];
  links?: GraphLink[] | null;
  labelForScope: (scope: string) => string;
}) {
  const mountRef = useRef<HTMLDivElement>(null);
  const labelsRef = useRef<HTMLDivElement>(null);
  const tipRef = useRef<HTMLDivElement>(null);
  const apiRef = useRef<GraphApi | null>(null);
  const flagsRef = useRef<Flags>(DEFAULT_FLAGS);
  const colorByRef = useRef<BrainColorBy>("scope");

  const [colorBy, setColorBy] = useState<BrainColorBy>("scope");
  const [flags, setFlags] = useState<Flags>(DEFAULT_FLAGS);

  // The layout model is colour-independent (halo colour is a cheap recolor, not a relayout),
  // so colorBy is NOT a dependency — only the data is.
  const model = useMemo(
    () => buildBrainModel(memories, semanticLinks, { colorBy: "scope", labelForScope }),
    [memories, semanticLinks, labelForScope],
  );

  const stats = useMemo(() => {
    const present = new Set<string>();
    for (const l of model.links) {
      present.add(l.source);
      present.add(l.target);
    }
    return {
      nodes: model.nodes.length,
      edges: model.links.length,
      connected: present.size,
      human: model.nodes.filter((nd) => nd.trust === "human").length,
    };
  }, [model]);

  // Build the whole three.js scene whenever the data changes; teardown on cleanup. Toggles
  // and colour changes are applied imperatively via apiRef without rebuilding.
  useEffect(() => {
    const mount = mountRef.current;
    const labelsLayer = labelsRef.current;
    const tip = tipRef.current;
    if (!mount || !labelsLayer || !tip || model.nodes.length === 0) return;

    // colorBy is read from a ref (not a dep) so a colour change recolors in place rather
    // than rebuilding the scene; the data (model) is the only rebuild trigger.
    return buildScene({
      mount,
      labelsLayer,
      tip,
      model,
      flags: flagsRef.current,
      colorByInit: colorByRef.current,
      apiRef,
    });
  }, [model]);

  // Push toggle + colour changes into the live scene.
  useEffect(() => {
    flagsRef.current = flags;
    apiRef.current?.syncFlags(flags);
  }, [flags]);
  useEffect(() => {
    colorByRef.current = colorBy;
    apiRef.current?.applyColors(colorBy);
  }, [colorBy]);

  const toggle = (k: keyof Flags) => setFlags((f) => ({ ...f, [k]: !f[k] }));

  return (
    <div className="relative h-full w-full overflow-hidden bg-[#05060c]">
      <div ref={mountRef} className="absolute inset-0" />
      <div ref={labelsRef} className="pointer-events-none absolute inset-0 overflow-hidden" />
      <div
        ref={tipRef}
        className="pointer-events-none absolute z-20 max-w-[340px] rounded-lg border border-white/15 bg-[#0a0c18]/95 p-2.5 opacity-0 shadow-xl transition-opacity"
      />

      {/* Controls overlay */}
      <div className="absolute left-4 top-4 z-10 w-[230px] rounded-xl border border-white/10 bg-[#0d101e]/75 p-3 text-[#cdd6f4] backdrop-blur">
        <div className="grid grid-cols-2 gap-2">
          <Stat value={stats.nodes} label="nodes" />
          <Stat value={stats.edges} label="edges" />
          <Stat value={stats.connected} label="connected" />
          <Stat value={stats.human} label="human" />
        </div>

        <div className="mt-3 text-[10px] uppercase tracking-wide text-[#7c84a6]">
          Colour halo by
        </div>
        <div className="mt-1 flex rounded-lg bg-white/[0.04] p-0.5">
          {(["scope", "community", "area"] as BrainColorBy[]).map((c) => (
            <button
              type="button"
              key={c}
              onClick={() => setColorBy(c)}
              className={`flex-1 rounded-md px-1.5 py-1 text-xs capitalize transition-colors ${
                colorBy === c ? "bg-sky-400/20 text-sky-100" : "text-[#aab2d5] hover:text-white"
              }`}
            >
              {c}
            </button>
          ))}
        </div>

        <div className="mt-3 text-[10px] uppercase tracking-wide text-[#7c84a6]">Display</div>
        <div className="mt-1 grid grid-cols-2 gap-1.5">
          {(Object.keys(DEFAULT_FLAGS) as (keyof Flags)[]).map((k) => (
            <button
              type="button"
              key={k}
              onClick={() => toggle(k)}
              className="flex items-center gap-2 rounded-md bg-white/[0.03] px-2 py-1.5 text-left hover:bg-white/[0.06]"
            >
              <span
                className={`size-2 rounded-full transition-colors ${
                  flags[k]
                    ? "bg-sky-300 shadow-[0_0_8px_1px_rgba(125,211,252,0.7)]"
                    : "bg-[#39405f]"
                }`}
              />
              <span className="text-[11px] capitalize text-[#b9c0e0]">{LABELS[k]}</span>
            </button>
          ))}
        </div>

        <div className="mt-3 text-[10px] uppercase tracking-wide text-[#7c84a6]">
          Node core = trust
        </div>
        <div className="mt-1 flex flex-col gap-1 text-[11px] text-[#aab2d5]">
          <LegendRow color="#fbfdff" glow>
            Human ground-truth
          </LegendRow>
          <LegendRow color="#fbbf24" glow>
            Needs review
          </LegendRow>
          <LegendRow color="#6d7bbf">Inferred (brightness = confidence)</LegendRow>
        </div>
      </div>

      <div className="absolute bottom-3 left-4 z-10 flex gap-4 rounded-lg border border-white/10 bg-[#0d101e]/75 px-3 py-2 text-[11px] text-[#9aa2c4] backdrop-blur">
        <span>
          <b className="text-[#d6ddf7]">Drag</b> orbit
        </span>
        <span>
          <b className="text-[#d6ddf7]">Right-drag</b> pan
        </span>
        <span>
          <b className="text-[#d6ddf7]">Scroll</b> zoom
        </span>
        <span>
          <b className="text-[#d6ddf7]">Click</b> focus
        </span>
      </div>
    </div>
  );
}

const LABELS: Record<keyof Flags, string> = {
  brain: "Brain",
  links: "Links",
  particles: "Particles",
  bloom: "Bloom",
  rotate: "Auto-rotate",
  labels: "Hub labels",
};

function Stat({ value, label }: { value: number; label: string }) {
  return (
    <div className="rounded-lg bg-white/[0.03] px-2 py-1.5">
      <div className="text-base font-semibold leading-none text-[#e6ebff]">
        {value.toLocaleString()}
      </div>
      <div className="mt-0.5 text-[10px] uppercase tracking-wide text-[#7c84a6]">{label}</div>
    </div>
  );
}

function LegendRow({
  color,
  glow,
  children,
}: {
  color: string;
  glow?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-2">
      <span
        className="size-2.5 rounded-full"
        style={{ background: color, boxShadow: glow ? `0 0 7px ${color}` : undefined }}
      />
      {children}
    </div>
  );
}

// ---------------------------------------------------------------------------------------
// Imperative three.js scene. Returns a cleanup function.
// ---------------------------------------------------------------------------------------

type SceneNode = BrainNode & SimNode;

function buildScene(args: {
  mount: HTMLDivElement;
  labelsLayer: HTMLDivElement;
  tip: HTMLDivElement;
  model: { nodes: BrainNode[]; links: { source: string; target: string; weight?: number }[] };
  flags: Flags;
  colorByInit: BrainColorBy;
  apiRef: React.MutableRefObject<GraphApi | null>;
}): () => void {
  const { mount, labelsLayer, tip, model, apiRef } = args;
  let flags = args.flags;
  const W = () => mount.clientWidth || 1;
  const H = () => mount.clientHeight || 1;

  // --- renderer / scene / camera / controls ---
  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
  renderer.setSize(W(), H());
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  renderer.setClearColor(0x000000, 0);
  mount.appendChild(renderer.domElement);

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(58, W() / H(), 1, 60000);
  camera.position.set(0, 0, 1600);

  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  controls.dampingFactor = 0.08;
  controls.rotateSpeed = 0.85;
  controls.zoomSpeed = 1.1;
  controls.screenSpacePanning = true;
  controls.autoRotateSpeed = 0.55;
  controls.maxDistance = 40000;
  controls.autoRotate = flags.rotate;

  scene.add(new THREE.AmbientLight(0x5b66a8, 1.1));
  const dl1 = new THREE.DirectionalLight(0xcfe0ff, 1.5);
  dl1.position.set(1, 1.3, 0.8);
  scene.add(dl1);
  const dl2 = new THREE.DirectionalLight(0x8e6bff, 0.85);
  dl2.position.set(-1, -0.5, -0.8);
  scene.add(dl2);

  // starfield
  {
    const N = 2400;
    const g = new THREE.BufferGeometry();
    const pos = new Float32Array(N * 3);
    for (let i = 0; i < N; i++) {
      const r = 9000 + Math.random() * 22000;
      const t = Math.random() * Math.PI * 2;
      const p = Math.acos(2 * Math.random() - 1);
      pos[i * 3] = r * Math.sin(p) * Math.cos(t);
      pos[i * 3 + 1] = r * Math.sin(p) * Math.sin(t);
      pos[i * 3 + 2] = r * Math.cos(p);
    }
    g.setAttribute("position", new THREE.BufferAttribute(pos, 3));
    scene.add(
      new THREE.Points(
        g,
        new THREE.PointsMaterial({
          color: 0x9fb0e0,
          size: 6,
          sizeAttenuation: true,
          transparent: true,
          opacity: 0.55,
          depthWrite: false,
        }),
      ),
    );
  }

  // --- graph data → sim nodes/links ---
  const nodes = model.nodes as SceneNode[];
  const n = nodes.length;
  const idIndex = new Map<string, number>();
  for (let i = 0; i < n; i++) idIndex.set(nodes[i]?.id ?? "", i);
  for (const nd of nodes) {
    const r = 300 + Math.random() * 500;
    const t = Math.random() * Math.PI * 2;
    const p = Math.acos(2 * Math.random() - 1);
    nd.x = r * Math.sin(p) * Math.cos(t);
    nd.y = r * Math.sin(p) * Math.sin(t);
    nd.z = r * Math.cos(p);
  }
  const links = model.links.map((l) => {
    const si = idIndex.get(l.source) ?? 0;
    const ti = idIndex.get(l.target) ?? 0;
    return { si, ti, source: nodes[si] as SceneNode, target: nodes[ti] as SceneNode, w: l.weight };
  });
  const adj: number[][] = Array.from({ length: n }, () => []);
  for (const l of links) {
    adj[l.si]?.push(l.ti);
    adj[l.ti]?.push(l.si);
  }

  const nodeRadius = (val: number) => 3.6 * Math.sqrt(val);

  // --- node meshes (core + halo, instanced) ---
  const dummy = new THREE.Object3D();
  const coreGeo = new THREE.IcosahedronGeometry(1, 2);
  const coreMat = new THREE.MeshBasicMaterial({ toneMapped: false });
  const coreMesh = new THREE.InstancedMesh(coreGeo, coreMat, n);
  coreMesh.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
  const haloGeo = new THREE.IcosahedronGeometry(1, 1);
  const haloMat = new THREE.MeshBasicMaterial({
    transparent: true,
    opacity: 0.085,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
    toneMapped: false,
  });
  const haloMesh = new THREE.InstancedMesh(haloGeo, haloMat, n);
  haloMesh.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
  scene.add(haloMesh, coreMesh);

  const HALO_REACH = 2.0;
  const CORE_FRAC = 0.5;
  const baseCore = new Float32Array(n * 3);
  const baseHalo = new Float32Array(n * 3);
  const tmpColor = new THREE.Color();
  function haloHex(nd: BrainNode, by: BrainColorBy): string {
    if (by === "area") return areaColor(nd.area);
    if (by === "community") return communityColor(nd.scope, nd.community);
    return scopeColor(nd.scope);
  }
  function computeBaseColors(by: BrainColorBy) {
    for (let i = 0; i < n; i++) {
      const nd = nodes[i];
      if (!nd) continue;
      tmpColor.set(coreColor(nd.trust, scopeColor(nd.scope), nd.conf));
      baseCore[i * 3] = tmpColor.r;
      baseCore[i * 3 + 1] = tmpColor.g;
      baseCore[i * 3 + 2] = tmpColor.b;
      tmpColor.set(haloHex(nd, by));
      baseHalo[i * 3] = tmpColor.r;
      baseHalo[i * 3 + 1] = tmpColor.g;
      baseHalo[i * 3 + 2] = tmpColor.b;
    }
  }
  function writeMatrices() {
    for (let i = 0; i < n; i++) {
      const nd = nodes[i];
      if (!nd) continue;
      const r = nodeRadius(nd.val);
      dummy.position.set(nd.x ?? 0, nd.y ?? 0, nd.z ?? 0);
      dummy.scale.setScalar(r * CORE_FRAC);
      dummy.updateMatrix();
      coreMesh.setMatrixAt(i, dummy.matrix);
      dummy.scale.setScalar(r * HALO_REACH);
      dummy.updateMatrix();
      haloMesh.setMatrixAt(i, dummy.matrix);
    }
    coreMesh.instanceMatrix.needsUpdate = true;
    haloMesh.instanceMatrix.needsUpdate = true;
  }

  // --- links (one LineSegments, vertex colours, additive) ---
  const linkGeo = new THREE.BufferGeometry();
  const lpos = new Float32Array(links.length * 6);
  const lcol = new Float32Array(links.length * 6);
  const lposAttr = new THREE.BufferAttribute(lpos, 3);
  const lcolAttr = new THREE.BufferAttribute(lcol, 3);
  linkGeo.setAttribute("position", lposAttr);
  linkGeo.setAttribute("color", lcolAttr);
  const linkMat = new THREE.LineBasicMaterial({
    vertexColors: true,
    transparent: true,
    opacity: 0.5,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
  });
  const linkMesh = new THREE.LineSegments(linkGeo, linkMat);
  scene.add(linkMesh);
  const baseLinkCol = links.map((l) =>
    new THREE.Color(0x57c4ff).multiplyScalar(0.05 + 0.32 * (l.w ?? 0.4)),
  );
  function writeLinkPositions() {
    for (let i = 0; i < links.length; i++) {
      const l = links[i];
      if (!l) continue;
      const o = i * 6;
      lpos[o] = l.source.x ?? 0;
      lpos[o + 1] = l.source.y ?? 0;
      lpos[o + 2] = l.source.z ?? 0;
      lpos[o + 3] = l.target.x ?? 0;
      lpos[o + 4] = l.target.y ?? 0;
      lpos[o + 5] = l.target.z ?? 0;
    }
    lposAttr.needsUpdate = true;
  }
  function writeLinkColors(activeId: number | null) {
    for (let i = 0; i < links.length; i++) {
      const l = links[i];
      const c = baseLinkCol[i];
      if (!l || !c) continue;
      const o = i * 6;
      const f = activeId == null ? 1 : l.si === activeId || l.ti === activeId ? 1.4 : 0.45;
      lcol[o] = c.r * f;
      lcol[o + 1] = c.g * f;
      lcol[o + 2] = c.b * f;
      lcol[o + 3] = c.r * f;
      lcol[o + 4] = c.g * f;
      lcol[o + 5] = c.b * f;
    }
    lcolAttr.needsUpdate = true;
  }

  // --- particles: signal motes along edges + ambient dust ---
  const DOT = softDot();
  const FLOW_N = Math.min(2000, links.length);
  const flowGeo = new THREE.BufferGeometry();
  const flowPos = new Float32Array(FLOW_N * 3);
  const flowCol = new Float32Array(FLOW_N * 3);
  const flowPosAttr = new THREE.BufferAttribute(flowPos, 3);
  const flowColAttr = new THREE.BufferAttribute(flowCol, 3);
  flowGeo.setAttribute("position", flowPosAttr);
  flowGeo.setAttribute("color", flowColAttr);
  const flowMat = new THREE.PointsMaterial({
    size: 11,
    map: DOT,
    vertexColors: true,
    transparent: true,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
    sizeAttenuation: true,
  });
  const flowPts = new THREE.Points(flowGeo, flowMat);
  scene.add(flowPts);
  const flowLink = new Int32Array(FLOW_N);
  const flowT = new Float32Array(FLOW_N);
  const flowSpeed = new Float32Array(FLOW_N);
  const flowBaseCol = new Array<THREE.Color>(FLOW_N);
  for (let i = 0; i < FLOW_N; i++) {
    const li = Math.floor(Math.random() * links.length);
    flowLink[i] = li;
    flowT[i] = Math.random();
    flowSpeed[i] = 0.07 + 0.13 * Math.random();
    flowBaseCol[i] = new THREE.Color(0x9fe9ff).multiplyScalar(0.55 + 0.65 * (links[li]?.w ?? 0.4));
  }
  function updateFlow(dt: number, activeId: number | null) {
    for (let i = 0; i < FLOW_N; i++) {
      let tt = (flowT[i] ?? 0) + (flowSpeed[i] ?? 0) * dt;
      if (tt >= 1) {
        tt -= 1;
        if (Math.random() < 0.3) flowLink[i] = Math.floor(Math.random() * links.length);
      }
      flowT[i] = tt;
      const l = links[flowLink[i] ?? 0];
      const c = flowBaseCol[i];
      if (!l || !c) continue;
      const o = i * 3;
      flowPos[o] = (l.source.x ?? 0) + ((l.target.x ?? 0) - (l.source.x ?? 0)) * tt;
      flowPos[o + 1] = (l.source.y ?? 0) + ((l.target.y ?? 0) - (l.source.y ?? 0)) * tt;
      flowPos[o + 2] = (l.source.z ?? 0) + ((l.target.z ?? 0) - (l.source.z ?? 0)) * tt;
      const f = activeId == null ? 1 : l.si === activeId || l.ti === activeId ? 1.6 : 0.4;
      flowCol[o] = c.r * f;
      flowCol[o + 1] = c.g * f;
      flowCol[o + 2] = c.b * f;
    }
    flowPosAttr.needsUpdate = true;
    flowColAttr.needsUpdate = true;
  }

  let dustPts: THREE.Points | null = null;
  let dustVel: Float32Array | null = null;
  let dustPosAttr: THREE.BufferAttribute | null = null;
  const DUST_N = 1000;
  function buildDust(center: THREE.Vector3, radius: number) {
    const geo = new THREE.BufferGeometry();
    const pos = new Float32Array(DUST_N * 3);
    dustVel = new Float32Array(DUST_N * 3);
    for (let i = 0; i < DUST_N; i++) {
      const r = radius * Math.cbrt(Math.random());
      const th = Math.random() * Math.PI * 2;
      const ph = Math.acos(2 * Math.random() - 1);
      const o = i * 3;
      pos[o] = center.x + r * Math.sin(ph) * Math.cos(th);
      pos[o + 1] = center.y + r * Math.sin(ph) * Math.sin(th);
      pos[o + 2] = center.z + r * Math.cos(ph);
      dustVel[o] = Math.random() - 0.5;
      dustVel[o + 1] = Math.random() - 0.5;
      dustVel[o + 2] = Math.random() - 0.5;
    }
    dustPosAttr = new THREE.BufferAttribute(pos, 3);
    geo.setAttribute("position", dustPosAttr);
    const mat = new THREE.PointsMaterial({
      size: radius * 0.014,
      map: DOT,
      color: 0x9ec2ff,
      transparent: true,
      opacity: 0.32,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
      sizeAttenuation: true,
    });
    dustPts = new THREE.Points(geo, mat);
    dustPts.userData = { center, radius };
    dustPts.visible = flags.particles;
    scene.add(dustPts);
  }
  function updateDust(dt: number) {
    if (!dustPts || !dustVel || !dustPosAttr) return;
    const pos = dustPosAttr.array as Float32Array;
    const vel = dustVel;
    const { center, radius } = dustPts.userData as { center: THREE.Vector3; radius: number };
    const k = radius * 0.03 * dt;
    const r2 = radius * radius;
    for (let i = 0; i < DUST_N; i++) {
      const o = i * 3;
      const x = (pos[o] ?? 0) + (vel[o] ?? 0) * k;
      const y = (pos[o + 1] ?? 0) + (vel[o + 1] ?? 0) * k;
      const z = (pos[o + 2] ?? 0) + (vel[o + 2] ?? 0) * k;
      pos[o] = x;
      pos[o + 1] = y;
      pos[o + 2] = z;
      const dx = x - center.x;
      const dy = y - center.y;
      const dz = z - center.z;
      if (dx * dx + dy * dy + dz * dz > r2) {
        vel[o] = -(vel[o] ?? 0);
        vel[o + 1] = -(vel[o + 1] ?? 0);
        vel[o + 2] = -(vel[o + 2] ?? 0);
      }
    }
    dustPosAttr.needsUpdate = true;
  }

  // --- central brain model + hollow-core force ---
  let brainGroup: THREE.Group | null = null;
  let brainRadius = 300;
  function buildBrainMesh(center: THREE.Vector3, R: number) {
    brainGroup = new THREE.Group();
    brainGroup.position.copy(center);
    const geo = new THREE.IcosahedronGeometry(R, 24);
    const pos = geo.attributes.position as THREE.BufferAttribute;
    const v = new THREE.Vector3();
    const dir = new THREE.Vector3();
    for (let i = 0; i < pos.count; i++) {
      v.fromBufferAttribute(pos, i);
      dir.copy(v).normalize();
      const w = fbmRidge(dir.x * 2.4, dir.y * 2.4, dir.z * 2.4);
      let disp = 0.84 + 0.16 * w;
      const fis = Math.exp(-(dir.x * dir.x) / 0.012) * (0.45 + 0.55 * Math.max(0, dir.y));
      disp -= 0.17 * fis;
      v.copy(dir).multiplyScalar(R * disp);
      v.x *= 1.18;
      v.y *= 0.84;
      v.z *= 1.06;
      pos.setXYZ(i, v.x, v.y, v.z);
    }
    geo.computeVertexNormals();
    const surf = new THREE.Mesh(
      geo,
      new THREE.MeshStandardMaterial({
        color: 0x3a2f6b,
        emissive: 0x5847c4,
        emissiveIntensity: 0.32,
        roughness: 0.55,
        metalness: 0.15,
        transparent: true,
        opacity: 0.34,
        depthWrite: false,
      }),
    );
    const wire = new THREE.Mesh(
      geo,
      new THREE.MeshBasicMaterial({
        color: 0xa493ff,
        wireframe: true,
        transparent: true,
        opacity: 0.09,
        blending: THREE.AdditiveBlending,
        depthWrite: false,
        toneMapped: false,
      }),
    );
    brainGroup.add(surf, wire);
    brainGroup.visible = flags.brain;
    scene.add(brainGroup);
  }

  // --- proximity labels (HTML pool) ---
  const LABEL_POOL = 80;
  const MIN_LABEL_PX = 9;
  const labelDivs: HTMLDivElement[] = [];
  for (let i = 0; i < LABEL_POOL; i++) {
    const d = document.createElement("div");
    d.style.cssText =
      "position:absolute;transform:translate(-50%,-150%);white-space:nowrap;font-size:11.5px;" +
      "font-weight:500;color:#e3e9ff;opacity:0;text-shadow:0 1px 4px #000,0 0 7px rgba(0,0,0,.9);" +
      "max-width:230px;overflow:hidden;text-overflow:ellipsis;";
    labelsLayer.appendChild(d);
    labelDivs.push(d);
  }
  const _lv = new THREE.Vector3();
  const _cand: [number, number][] = [];
  function updateLabels(activeId: number | null) {
    if (!flags.labels && activeId == null) {
      for (const d of labelDivs) d.style.opacity = "0";
      return;
    }
    const fovScale = H() / (2 * Math.tan(THREE.MathUtils.degToRad(camera.fov * 0.5)));
    const cp = camera.position;
    _cand.length = 0;
    for (let i = 0; i < n; i++) {
      const nd = nodes[i];
      if (!nd) continue;
      const dx = (nd.x ?? 0) - cp.x;
      const dy = (nd.y ?? 0) - cp.y;
      const dz = (nd.z ?? 0) - cp.z;
      const d = Math.sqrt(dx * dx + dy * dy + dz * dz) || 1e-6;
      const screenR = (nodeRadius(nd.val) / d) * fovScale;
      if (screenR > MIN_LABEL_PX || i === activeId) _cand.push([screenR, i]);
    }
    _cand.sort((a, b) => b[0] - a[0]);
    const show = Math.min(LABEL_POOL, _cand.length);
    for (let k = 0; k < LABEL_POOL; k++) {
      const div = labelDivs[k];
      if (!div) continue;
      if (k >= show) {
        div.style.opacity = "0";
        continue;
      }
      const entry = _cand[k];
      if (!entry) continue;
      const [sr, i] = entry;
      const nd = nodes[i];
      if (!nd) continue;
      _lv.set(nd.x ?? 0, nd.y ?? 0, nd.z ?? 0).project(camera);
      if (_lv.z > 1 || _lv.x < -1.05 || _lv.x > 1.05 || _lv.y < -1.05 || _lv.y > 1.05) {
        div.style.opacity = "0";
        continue;
      }
      div.textContent = nd.label.replace(/…$/, "");
      div.style.left = `${((_lv.x * 0.5 + 0.5) * W()).toFixed(1)}px`;
      div.style.top = `${((-_lv.y * 0.5 + 0.5) * H()).toFixed(1)}px`;
      let op = Math.max(0, Math.min(0.95, (sr - MIN_LABEL_PX) / 22));
      if (i === activeId) op = 1;
      div.style.opacity = op.toFixed(2);
    }
  }

  // --- postprocessing ---
  const composer = new EffectComposer(renderer);
  composer.addPass(new RenderPass(scene, camera));
  const bloom = new UnrealBloomPass(new THREE.Vector2(W(), H()), 0.5, 0.5, 0.42);
  composer.addPass(bloom);
  composer.addPass(new OutputPass());
  let bloomOn = flags.bloom;

  // --- colours / highlight ---
  const DIM = 0.5;
  const tc = new THREE.Color();
  let curColorBy = args.colorByInit;
  function applyColors(activeId: number | null) {
    let lit: Set<number> | null = null;
    if (activeId != null) {
      lit = new Set(adj[activeId] ?? []);
      lit.add(activeId);
    }
    for (let i = 0; i < n; i++) {
      const f = lit && !lit.has(i) ? DIM : 1;
      tc.setRGB(
        (baseCore[i * 3] ?? 0) * f,
        (baseCore[i * 3 + 1] ?? 0) * f,
        (baseCore[i * 3 + 2] ?? 0) * f,
      );
      coreMesh.setColorAt(i, tc);
      tc.setRGB(
        (baseHalo[i * 3] ?? 0) * f,
        (baseHalo[i * 3 + 1] ?? 0) * f,
        (baseHalo[i * 3 + 2] ?? 0) * f,
      );
      haloMesh.setColorAt(i, tc);
    }
    if (coreMesh.instanceColor) coreMesh.instanceColor.needsUpdate = true;
    if (haloMesh.instanceColor) haloMesh.instanceColor.needsUpdate = true;
    writeLinkColors(activeId);
  }

  // --- interaction state ---
  const raycaster = new THREE.Raycaster();
  const ptr = new THREE.Vector2();
  let ptrActive = false;
  let hoverId: number | null = null;
  let lockId: number | null = null;
  let downXY: [number, number] | null = null;
  let dragMoved = false;
  const TRUST_BADGE: Record<string, [string, string, string]> = {
    human: ["#fbfdff", "#0b0d18", "ground-truth"],
    review: ["#fbbf24", "#231a05", "review"],
    normal: ["#5b6bb5", "#0b0f22", "inferred"],
  };

  const onPointerMove = (e: PointerEvent) => {
    const rect = mount.getBoundingClientRect();
    ptr.x = ((e.clientX - rect.left) / W()) * 2 - 1;
    ptr.y = -((e.clientY - rect.top) / H()) * 2 + 1;
    ptrActive = true;
    tip.style.left = `${Math.min(e.clientX - rect.left + 16, W() - 350)}px`;
    tip.style.top = `${Math.min(e.clientY - rect.top + 16, H() - 150)}px`;
    if (downXY) {
      if (Math.hypot(e.clientX - downXY[0], e.clientY - downXY[1]) > 5) dragMoved = true;
    }
  };
  const onPointerLeave = () => {
    ptrActive = false;
  };
  const onPointerDown = (e: PointerEvent) => {
    downXY = [e.clientX, e.clientY];
    dragMoved = false;
  };
  const onPointerUp = () => {
    downXY = null;
  };
  const onClick = () => {
    if (dragMoved) return;
    const id = pick();
    if (id == null) {
      lockId = null;
      return;
    }
    lockId = lockId === id ? null : id;
    if (lockId != null) focusNode(lockId);
  };
  renderer.domElement.addEventListener("pointermove", onPointerMove);
  renderer.domElement.addEventListener("pointerleave", onPointerLeave);
  renderer.domElement.addEventListener("pointerdown", onPointerDown);
  window.addEventListener("pointerup", onPointerUp);
  renderer.domElement.addEventListener("click", onClick);

  function pick(): number | null {
    raycaster.setFromCamera(ptr, camera);
    const hit = raycaster.intersectObject(haloMesh, false);
    return hit.length && hit[0]?.instanceId != null ? hit[0].instanceId : null;
  }
  const activeId = () => (hoverId != null ? hoverId : lockId);

  let fly: {
    t: number;
    cf: THREE.Vector3;
    ct: THREE.Vector3;
    tf: THREE.Vector3;
    tt: THREE.Vector3;
  } | null = null;
  function focusNode(id: number) {
    const nd = nodes[id];
    if (!nd) return;
    const target = new THREE.Vector3(nd.x ?? 0, nd.y ?? 0, nd.z ?? 0);
    const dist = Math.max(graphRadius * 0.16, nodeRadius(nd.val) * 14);
    const dir = new THREE.Vector3().subVectors(camera.position, controls.target).normalize();
    fly = {
      t: 0,
      cf: camera.position.clone(),
      ct: new THREE.Vector3().copy(target).addScaledVector(dir, dist),
      tf: controls.target.clone(),
      tt: target,
    };
  }
  function showTip(id: number) {
    const nd = nodes[id];
    if (!nd) return;
    const [bg, fg, word] = TRUST_BADGE[nd.trust] ??
      TRUST_BADGE.normal ?? ["#5b6bb5", "#0b0f22", ""];
    const txt = nd.fullText.length > 260 ? `${nd.fullText.slice(0, 260)}…` : nd.fullText;
    tip.innerHTML =
      `<div style="display:flex;gap:8px;align-items:center;margin-bottom:6px;flex-wrap:wrap">` +
      `<span style="font-weight:650;color:#e6ebff;font-size:12px">${esc(nd.scopeLabel)}</span>` +
      `<span style="font-size:10px;padding:1.5px 6px;border-radius:999px;background:${bg};color:${fg}">${word}</span></div>` +
      `<div style="color:#c3cbeb;font-size:12px;line-height:1.5">${esc(txt)}</div>` +
      `<div style="margin-top:7px;color:#767ea0;font-size:10.5px;display:flex;gap:12px">` +
      `<span>${esc(nd.category)}</span><span>${nd.degree} links</span>${nd.uses ? `<span>used ${nd.uses}×</span>` : ""}${nd.pinned ? "<span>📌 pinned</span>" : ""}</div>`;
    tip.style.opacity = "1";
  }
  const hideTip = () => {
    tip.style.opacity = "0";
  };

  // --- camera framing ---
  const graphCenter = new THREE.Vector3();
  let graphRadius = 1000;
  function fitCamera() {
    const box = new THREE.Box3();
    const v = new THREE.Vector3();
    for (const nd of nodes) {
      if ((nd.degree ?? 0) > 0) box.expandByPoint(v.set(nd.x ?? 0, nd.y ?? 0, nd.z ?? 0));
    }
    if (box.isEmpty())
      for (const nd of nodes) box.expandByPoint(v.set(nd.x ?? 0, nd.y ?? 0, nd.z ?? 0));
    const sph = new THREE.Sphere();
    box.getBoundingSphere(sph);
    graphCenter.copy(sph.center);
    graphRadius = sph.radius || 1000;
    controls.target.copy(sph.center);
    const d = graphRadius / Math.sin(THREE.MathUtils.degToRad(camera.fov * 0.5));
    camera.position
      .copy(sph.center)
      .add(new THREE.Vector3(0.2, 0.15, 1).normalize().multiplyScalar(d * 1.05));
    camera.near = Math.max(1, d * 0.001);
    camera.far = d * 12;
    camera.updateProjectionMatrix();
  }
  function hollowForce(center: THREE.Vector3, rmin: number) {
    let nds: SceneNode[] | null = null;
    const f = (alpha: number) => {
      if (!nds) return;
      for (const nd of nds) {
        const dx = (nd.x ?? 0) - center.x;
        const dy = (nd.y ?? 0) - center.y;
        const dz = (nd.z ?? 0) - center.z;
        const d = Math.sqrt(dx * dx + dy * dy + dz * dz) || 1e-6;
        if (d < rmin) {
          const k = ((rmin - d) / d) * 3.4 * alpha;
          nd.vx = (nd.vx ?? 0) + dx * k;
          nd.vy = (nd.vy ?? 0) + dy * k;
          nd.vz = (nd.vz ?? 0) + dz * k;
        }
      }
    };
    f.initialize = (input: SceneNode[]) => {
      nds = input;
    };
    return f;
  }

  // --- force simulation (3D) ---
  const sim = forceSimulation<SceneNode>(nodes, 3)
    .force("charge", forceManyBody().strength(-110).distanceMax(2600).theta(0.9))
    .force(
      "link",
      forceLink(links)
        .distance((l: (typeof links)[number]) => {
          const cross = l.source.scope !== l.target.scope;
          const base = 150 - 80 * (l.w ?? 0.4);
          return cross ? base + 240 : base;
        })
        .strength((l: (typeof links)[number]) => {
          const s = 0.018 + 0.13 * (l.w ?? 0.4);
          return l.source.scope !== l.target.scope ? s * 0.06 : s * 1.6;
        }),
    )
    .force("center", forceCenter(0, 0, 0))
    .force("collide", forceCollide((d) => nodeRadius((d as SceneNode).val) * 1.4).strength(0.85))
    .force(
      "gx",
      forceX(0).strength((d: SceneNode) => ((d.degree ?? 0) > 0 ? 0.008 : 0.05)),
    )
    .force(
      "gy",
      forceY(0).strength((d: SceneNode) => ((d.degree ?? 0) > 0 ? 0.008 : 0.05)),
    )
    .force(
      "gz",
      forceZ(0).strength((d: SceneNode) => ((d.degree ?? 0) > 0 ? 0.008 : 0.05)),
    )
    .stop();
  sim.alpha(1).alphaDecay(0.0175).velocityDecay(0.4);

  // pre-settle (CPU only), carve hollow, build brain + dust
  computeBaseColors(curColorBy);
  let t = 0;
  while (sim.alpha() > 0.04 && t < 420) {
    sim.tick();
    t++;
  }
  fitCamera();
  brainRadius = graphRadius * 0.19;
  sim.force("hollow", hollowForce(graphCenter.clone(), brainRadius * 2.1));
  sim.alpha(0.6).alphaDecay(0.02);
  let t2 = 0;
  while (sim.alpha() > 0.04 && t2 < 300) {
    sim.tick();
    t2++;
  }
  fitCamera();
  writeMatrices();
  writeLinkPositions();
  buildBrainMesh(graphCenter.clone(), brainRadius);
  buildDust(graphCenter, graphRadius * 1.08);
  applyColors(null);

  // expose imperative API to React
  apiRef.current = {
    applyColors: (by: BrainColorBy) => {
      curColorBy = by;
      computeBaseColors(by);
      applyColors(activeId());
    },
    syncFlags: (next: Flags) => {
      flags = next;
      linkMesh.visible = next.links;
      flowPts.visible = next.particles;
      if (dustPts) dustPts.visible = next.particles;
      if (brainGroup) brainGroup.visible = next.brain;
      controls.autoRotate = next.rotate;
      bloomOn = next.bloom;
    },
  };

  // --- resize ---
  const ro = new ResizeObserver(() => {
    camera.aspect = W() / H();
    camera.updateProjectionMatrix();
    renderer.setSize(W(), H());
    composer.setSize(W(), H());
    bloom.setSize(W(), H());
  });
  ro.observe(mount);

  // --- render loop ---
  let raf = 0;
  let prevNow = performance.now();
  let frameN = 0;
  let lastActive: number | null | undefined;
  const loop = () => {
    raf = requestAnimationFrame(loop);
    const now = performance.now();
    const dt = Math.min(0.05, (now - prevNow) / 1000);
    prevNow = now;

    if (ptrActive && !dragMoved) {
      hoverId = pick();
      if (hoverId != null) showTip(hoverId);
      else hideTip();
    } else if (!ptrActive) {
      hoverId = null;
      hideTip();
    }

    const act = activeId();
    if (act !== lastActive) {
      applyColors(act);
      lastActive = act;
    }

    if (flags.particles) {
      updateFlow(dt, act);
      updateDust(dt);
    }
    if (brainGroup?.visible) brainGroup.rotation.y += dt * 0.04;
    if ((frameN++ & 1) === 0) updateLabels(act);

    if (fly) {
      fly.t += 0.045;
      const e = fly.t >= 1 ? 1 : 1 - (1 - fly.t) ** 3;
      camera.position.lerpVectors(fly.cf, fly.ct, e);
      controls.target.lerpVectors(fly.tf, fly.tt, e);
      if (fly.t >= 1) fly = null;
    }

    controls.update();
    if (bloomOn) composer.render();
    else renderer.render(scene, camera);
  };
  loop();

  // --- cleanup ---
  return () => {
    cancelAnimationFrame(raf);
    ro.disconnect();
    sim.stop();
    renderer.domElement.removeEventListener("pointermove", onPointerMove);
    renderer.domElement.removeEventListener("pointerleave", onPointerLeave);
    renderer.domElement.removeEventListener("pointerdown", onPointerDown);
    renderer.domElement.removeEventListener("click", onClick);
    window.removeEventListener("pointerup", onPointerUp);
    controls.dispose();
    composer.dispose();
    renderer.dispose();
    scene.traverse((obj) => {
      const m = obj as THREE.Mesh;
      if (m.geometry) m.geometry.dispose();
      const mat = (m as THREE.Mesh).material;
      if (Array.isArray(mat)) for (const x of mat) x.dispose();
      else if (mat) (mat as THREE.Material).dispose();
    });
    DOT.dispose();
    if (renderer.domElement.parentNode === mount) mount.removeChild(renderer.domElement);
    labelsLayer.replaceChildren();
    tip.innerHTML = "";
    apiRef.current = null;
  };
}

// soft round sprite so points read as glowing motes
function softDot(): THREE.CanvasTexture {
  const s = 64;
  const cv = document.createElement("canvas");
  cv.width = cv.height = s;
  const cx = cv.getContext("2d");
  if (cx) {
    const g = cx.createRadialGradient(s / 2, s / 2, 0, s / 2, s / 2, s / 2);
    g.addColorStop(0, "rgba(255,255,255,1)");
    g.addColorStop(0.35, "rgba(255,255,255,0.65)");
    g.addColorStop(1, "rgba(255,255,255,0)");
    cx.fillStyle = g;
    cx.fillRect(0, 0, s, s);
  }
  return new THREE.CanvasTexture(cv);
}

// layered ridged pseudo-noise → gyri/sulci wrinkles on the brain surface
function fbmRidge(x: number, y: number, z: number): number {
  let v = 0;
  let a = 0.5;
  let f = 1.0;
  for (let o = 0; o < 4; o++) {
    const s =
      Math.sin(x * f * 3.1 + y * f * 1.7) * Math.cos(y * f * 2.3 + z * f * 1.3) +
      Math.sin(z * f * 2.7 + x * f * 1.1) * Math.cos(x * f * 1.9 + y * f * 0.7);
    v += a * (1 - Math.abs(s) * 0.55);
    f *= 2.0;
    a *= 0.5;
  }
  return v;
}

function esc(s: string): string {
  return s.replace(/[&<>]/g, (c) => (c === "&" ? "&amp;" : c === "<" ? "&lt;" : "&gt;"));
}
