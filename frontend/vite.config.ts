import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";
import fs from "fs";

const backend = process.env.HCDR_API_PROXY ?? "https://127.0.0.1:18080";
const proxyTarget = {
  target: backend,
  changeOrigin: true,
  secure: false,
  ws: true
};

const tlsCert = process.env.HCDR_DEV_TLS_CERT_FILE;
const tlsKey = process.env.HCDR_DEV_TLS_KEY_FILE;
const outDir = process.env.HCDR_FRONTEND_OUT_DIR
  ?? path.resolve(__dirname, "../../hypercdr-runtime/build/community/frontend");
const https = tlsCert && tlsKey
  ? { cert: fs.readFileSync(tlsCert), key: fs.readFileSync(tlsKey) }
  : undefined;

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir,
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined;
          if (id.includes('/react/') || id.includes('/react-dom/') || id.includes('/scheduler/')) return 'vendor-react';
          if (id.includes('/motion/') || id.includes('/framer-motion/')) return 'vendor-motion';
          if (id.includes('/@tanstack/')) return 'vendor-table';
          if (id.includes('/lucide-react/')) return 'vendor-icons';
          return 'vendor';
        }
      }
    }
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, ".")
    }
  },
  server: {
    host: "0.0.0.0",
    port: 3002,
    strictPort: true,
    headers: {
      "Cache-Control": "no-store, no-cache, must-revalidate",
      "Pragma": "no-cache",
      "Expires": "0"
    },
    https,
    hmr: process.env.DISABLE_HMR !== "true",
    watch: process.env.DISABLE_HMR === "true" ? null : {},
    warmup: {
      clientFiles: [
        "./src/main.tsx",
        "./src/App.tsx",
        "./src/styles.css",
        "./src/protect-wizard-modal.tsx"
      ]
    },
    proxy: {
      "/api": proxyTarget,
      "/install.sh": proxyTarget,
      "/prepare-node.sh": proxyTarget,
      "/assets/registry": proxyTarget,
      "/assets/velero": proxyTarget,
      "/healthz": proxyTarget,
      "/readyz": proxyTarget,
      "/ws": proxyTarget
    }
  },
  preview: {
    host: "0.0.0.0",
    port: 3002,
    strictPort: true,
    https,
    proxy: {
      "/api": proxyTarget,
      "/install.sh": proxyTarget,
      "/prepare-node.sh": proxyTarget,
      "/assets/registry": proxyTarget,
      "/assets/velero": proxyTarget,
      "/healthz": proxyTarget,
      "/readyz": proxyTarget,
      "/ws": proxyTarget
    }
  }
});
