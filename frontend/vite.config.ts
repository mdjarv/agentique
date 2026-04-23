import fs from "node:fs";
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { VitePWA } from "vite-plugin-pwa";

const certPath = path.resolve(__dirname, "../certs");
const useTls = process.env.VITE_TLS !== "false" && fs.existsSync(path.join(certPath, "server.crt"));

// Backend port: default 9201 (local dev). Override via VITE_BACKEND_PORT (e.g.
// 19201 when targeting the installed agentique service from a remote dev slot).
const backendPort = process.env.VITE_BACKEND_PORT ?? "9201";
const backendOrigin = useTls ? `https://localhost:${backendPort}` : `http://localhost:${backendPort}`;
const backendWs = useTls ? `wss://localhost:${backendPort}` : `ws://localhost:${backendPort}`;

// Public host for remote dev slots. When set, Vite HMR client connects via
// wss://<host>:443 (through the reverse proxy) instead of the local port.
const publicHost = process.env.VITE_PUBLIC_HOST ?? "";
const frontendPort = Number(process.env.VITE_PORT ?? 9200);

export default defineConfig({
  logLevel: "warn",
  plugins: [
    tanstackRouter({ quoteStyle: "double", semicolons: true }),
    react(),
    tailwindcss(),
    VitePWA({
      registerType: "autoUpdate",
      workbox: {
        // Precache hashed assets only. index.html is intentionally excluded —
        // a precached index.html points at hashed js bundles that get evicted
        // on the next deploy, so a stale SW would serve HTML referencing 404s
        // (or the wrong-version bundle). NetworkFirst on navigations below
        // keeps HTML fresh while still allowing offline fallback.
        globPatterns: [
          "assets/index-*.js",
          "assets/vendor-*.js",
          "assets/index-*.css",
          "*.png",
          "*.svg",
          "*.ico",
        ],
        // Activate a new SW as soon as it finishes installing, without
        // waiting for every tab to close. Combined with the reload-on-update
        // logic in main.tsx, this eliminates the "Ctrl+R still shows the old
        // build" trap where the old SW keeps serving a stale precache.
        skipWaiting: true,
        clientsClaim: true,
        cleanupOutdatedCaches: true,
        // Disable the default navigateFallback (which registers a precache-bound
        // NavigationRoute that would claim navigations before our NetworkFirst
        // handler sees them, and index.html is no longer in the precache anyway).
        // NetworkFirst below handles all navigations and caches the response for
        // offline use.
        navigateFallback: null,
        runtimeCaching: [
          {
            // Always try network for navigations so fresh index.html (with
            // current asset hashes) wins over any cached copy. Falls back to
            // cache after 3s — preserves offline behavior. API and WS are
            // never navigations, so no denylist needed here.
            urlPattern: ({ request }) => request.mode === "navigate",
            handler: "NetworkFirst",
            options: {
              cacheName: "html",
              networkTimeoutSeconds: 3,
              expiration: { maxEntries: 4 },
            },
          },
          {
            urlPattern: /^https:\/\/fonts\.(googleapis|gstatic)\.com\/.*/i,
            handler: "CacheFirst",
            options: {
              cacheName: "google-fonts",
              expiration: { maxEntries: 20, maxAgeSeconds: 60 * 60 * 24 * 365 },
              cacheableResponse: { statuses: [0, 200] },
            },
          },
        ],
      },
      manifest: {
        name: "Agentique",
        short_name: "Agentique",
        description: "Manage parallel Claude Code sessions",
        theme_color: "#1a1b26",
        background_color: "#1a1b26",
        display: "standalone",
        scope: "/",
        start_url: "/",
        icons: [
          { src: "/icon-192.png", sizes: "192x192", type: "image/png" },
          { src: "/icon-512.png", sizes: "512x512", type: "image/png" },
          { src: "/icon-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
          { src: "/icon.svg", sizes: "any", type: "image/svg+xml" },
        ],
      },
    }),
  ],
  resolve: {
    alias: {
      "~": path.resolve(__dirname, "src"),
    },
  },
  server: {
    host: "0.0.0.0",
    port: frontendPort,
    allowedHosts: true,
    ...(useTls && {
      https: {
        cert: path.join(certPath, "server.crt"),
        key: path.join(certPath, "server.key"),
      },
    }),
    ...(publicHost && {
      hmr: {
        protocol: "wss",
        clientPort: 443,
        host: publicHost,
      },
    }),
    proxy: {
      "/api": {
        target: backendOrigin,
        secure: false,
      },
      "/ws": {
        target: backendWs,
        ws: true,
        secure: false,
        configure: (proxy) => {
          proxy.on("proxyReqWs", (_proxyReq, _req, socket) => {
            socket.on("error", () => {});
          });
        },
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // Mermaid (lazy-loaded via dynamic import) is ~2.7MB and will trigger this warning;
    // limit set so the warning fires only for that genuinely-large chunk.
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("/node_modules/")) return;
          if (/\/node_modules\/(react|react-dom|scheduler)\//.test(id)) return "vendor";
          if (/\/node_modules\/(react-markdown|remark-gfm|remark-breaks|react-syntax-highlighter)\//.test(id)) {
            return "markdown";
          }
          if (id.includes("/node_modules/mermaid/")) return "mermaid";
        },
      },
    },
  },
});
