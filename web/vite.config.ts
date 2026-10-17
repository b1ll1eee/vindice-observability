import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// In dev, the API runs on the Go side (vindice -mode=query or all).
export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: { '/api': 'http://localhost:8080' },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: false, // keep dist/.keep, which go:embed needs on a fresh clone
  },
})
