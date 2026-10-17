import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// In dev, the API runs on the Go side (vindice -mode=query or all).
export default defineConfig({
  plugins: [vue()],
  server: {
    // Keep the browser's Host header: the API refuses writes whose Origin
    // differs from Host (CSRF check), and Origin here is localhost:5173.
    proxy: { '/api': { target: 'http://localhost:8080', changeOrigin: false } },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: false, // keep dist/.keep, which go:embed needs on a fresh clone
  },
})
