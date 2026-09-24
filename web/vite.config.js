import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'node:path'

// Vite config emits to ../internal/interface/webui/embed/dist so Go can embed without
// any extra copy step. base: './' is required because the binary serves from
// arbitrary paths.
export default defineConfig({
  plugins: [vue()],
  base: './',
  build: {
    outDir: path.resolve(__dirname, '../internal/interface/webui/embed/dist'),
    emptyOutDir: true
  },
  server: {
    port: 5173
  }
})
