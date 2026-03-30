import fs from "node:fs";
import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";

const certPath = path.resolve(__dirname, "../certs");
const useTls =
  process.env.VITE_TLS !== "false" && fs.existsSync(path.join(certPath, "server.crt"));

const backendOrigin = useTls ? "https://localhost:9201" : "http://localhost:9201";
const backendWs = useTls ? "wss://localhost:9201" : "ws://localhost:9201";

export default defineConfig({
  plugins: [
    TanStackRouterVite({ quoteStyle: "double" }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      "~": path.resolve(__dirname, "src"),
    },
  },
  server: {
    host: "0.0.0.0",
    port: 9200,
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
