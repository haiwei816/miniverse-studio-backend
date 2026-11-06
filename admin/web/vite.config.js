import {defineConfig} from 'vite';export default defineConfig({resolve:{alias:{vue:'vue/dist/vue.esm-bundler.js'}},server:{proxy:{'/gm':'http://127.0.0.1:18080','/v1':'http://127.0.0.1:18080'}}});
