import fs from "node:fs";
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { VitePWA } from "vite-plugin-pwa";

const certPath = path.resolve(__dirname, "../certs");
const useTls = process.env.VITE_TLS !== "false" && fs.existsSync(path.join(certPath, "server.crt"));

const backendOrigin = useTls ? "https://localhost:9201" : "http://localhost:9201";
const backendWs = useTls ? "wss://localhost:9201" : "ws://localhost:9201";

export default defineConfig({
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
    port: 9200,
    allowedHosts: true,
    ...(useTls && {
      https: {
        cert: path.join(certPath, "server.crt"),
        key: path.join(certPath, "server.key"),
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
    chunkSizeWarningLimit: 800,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ["react", "react-dom"],
          markdown: ["react-markdown", "remark-gfm", "remark-breaks", "react-syntax-highlighter"],
          mermaid: ["mermaid"],
        },
      },
    },
  },
});
