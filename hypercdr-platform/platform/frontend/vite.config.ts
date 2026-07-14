import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

const backend = process.env.HCDR_API_PROXY ?? "https://127.0.0.1:18080";
const proxyTarget = {
  target: backend,
  changeOrigin: true,
  secure: false
};

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, ".")
    }
  },
  server: {
    host: "0.0.0.0",
    port: 3002,
    strictPort: true,
    hmr: process.env.DISABLE_HMR !== "true",
    watch: process.env.DISABLE_HMR === "true" ? null : {},
    proxy: {
      "/api": proxyTarget,
      "/install.sh": proxyTarget,
      "/healthz": proxyTarget,
      "/readyz": proxyTarget
    }
  }
});
