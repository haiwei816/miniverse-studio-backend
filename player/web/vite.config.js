import { defineConfig } from 'vite';
const target = process.env.MINIVERSE_API || 'http://127.0.0.1:18080';
export default defineConfig({
  plugins: [{
    name: 'cauldron-public-wasm',
    configureServer(server) { server.middlewares.use((req, res, next) => { if (req.url?.startsWith('/games/witch-cauldron/runtime/')) res.setHeader('Access-Control-Allow-Origin', '*'); next(); }); },
    configurePreviewServer(server) { server.middlewares.use((req, res, next) => { if (req.url?.startsWith('/games/witch-cauldron/runtime/')) res.setHeader('Access-Control-Allow-Origin', '*'); next(); }); },
  }],
  resolve: { alias: { vue: 'vue/dist/vue.esm-bundler.js' } },
  server: { port: 5174, proxy: { '/v1': target, '/health': target, '/ready': target } },
});
