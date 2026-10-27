import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

// go:embed dist in ../webui.go expects the build output at ../dist (unchanged
// since the vanilla-JS era) — outDir points there so no Go changes are needed.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  build: {
    outDir: '../dist',
    emptyOutDir: true,
  },
})
