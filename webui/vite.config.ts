import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  build: {
    outDir: "dist",
    assetsDir: "assets",
    emptyOutDir: true,
    target: "esnext",
  },
  server: {
    port: 5173,
    proxy: { "/api": "http://localhost:8081", "/v1": "http://localhost:8080", "/healthz": "http://localhost:8080" },
  },
});
