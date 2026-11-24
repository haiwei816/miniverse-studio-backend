import { defineConfig } from "vite";

const API = process.env.MINIVERSE_API || "http://127.0.0.1:18080";

export default defineConfig({
  // The web console is served from the site root; the desktop build passes
  // --base=./ because Tauri serves assets from its own protocol.
  base: "/",
  resolve: { alias: { vue: "vue/dist/vue.esm-bundler.js" } },
  server: {
    port: 5173,
    watch: {
      ignored: ["**/src-tauri/**"],
    },
    proxy: {
      "/gm": API,
      "/v1": API,
      "/health": API,
      "/ready": API,
    },
  },
  build: { chunkSizeWarningLimit: 700 },
});
