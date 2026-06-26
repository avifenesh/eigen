import { defineConfig } from "vite";
import { svelte, vitePreprocess } from "@sveltejs/vite-plugin-svelte";
import { resolve } from "node:path";

// No Wails vite plugin: we use untyped Events.On with raw string event names
// (the typed-events plugin hard-errors when no Go events are RegisterEvent'd).
// Bindings are generated separately via `wails3 generate bindings`.
export default defineConfig({
  base: "./",
  plugins: [svelte({ preprocess: vitePreprocess({ script: true }), compilerOptions: { runes: true } })],
  resolve: {
    alias: {
      $lib: resolve(__dirname, "src/lib"),
      $bindings: resolve(__dirname, "bindings"),
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "es2022",
    sourcemap: false,
  },
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
});
