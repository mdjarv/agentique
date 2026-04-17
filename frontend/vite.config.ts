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
        // Only precache the app shell — lazy-loaded chunks (mermaid, katex,
        // diagram renderers ≈ 1500 files) are fetched on demand via the network.
        globPatterns: [
          "index.html",
          "assets/index-*.js",
          "assets/vendor-*.js",
          "assets/index-*.css",
          "*.png",
          "*.svg",
          "*.ico",
        ],
        navigateFallback: "index.html",
        navigateFallbackDenylist: [/^\/api\//, /^\/ws$/],
        runtimeCaching: [
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
