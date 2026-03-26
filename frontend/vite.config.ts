import fs from "node:fs";
import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";

const certPath = path.resolve(__dirname, "../certs");
const hasCerts = fs.existsSync(path.join(certPath, "server.crt"));

const backendOrigin = hasCerts ? "https://localhost:9201" : "http://localhost:9201";
const backendWs = hasCerts ? "wss://localhost:9201" : "ws://localhost:9201";

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
    ...(hasCerts && {
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
  },
});
