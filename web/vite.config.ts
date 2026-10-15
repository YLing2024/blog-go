import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: process.env.BUILD_OUT_DIR || '/var/www/homepage',
    emptyOutDir: true
  }
})