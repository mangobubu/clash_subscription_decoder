import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const devProxyTarget = env.VITE_DEV_PROXY_TARGET || 'http://localhost:8080'

  return {
    plugins: [vue()],
    server: {
      proxy: {
        '/api': {
          target: devProxyTarget,
          changeOrigin: true,
        },
        '/sub': {
          target: devProxyTarget,
          changeOrigin: true,
        },
        '/surge.conf': {
          target: devProxyTarget,
          changeOrigin: true,
        },
        '/shadowrocket.conf': {
          target: devProxyTarget,
          changeOrigin: true,
        },
        '/shadowrocket': {
          target: devProxyTarget,
          changeOrigin: true,
        },
      },
    },
    build: {
      outDir: resolve(__dirname, '../backend/dist'),
      emptyOutDir: true,
    },
  }
})
