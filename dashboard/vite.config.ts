import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  base: "/dashboard/",
  build: {
    outDir: "../internal/dashboard/web",
    emptyOutDir: true,
  },
  server: {
    port: 5178,
    strictPort: true,
    proxy: {
      "/v8/management": "http://127.0.0.1:8318",
      "/v1": "http://127.0.0.1:8318",
    },
  },
  test: { environment: "jsdom", setupFiles: ["./src/test-setup.ts"] },
});
