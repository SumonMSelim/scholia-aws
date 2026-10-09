/// <reference types="vitest/config" />
import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'
import { headTags, llmsTxt, staticBody } from './src/lib/seo.ts'

const apiTarget = process.env.SCHOLIA_API_URL ?? 'http://localhost:8787'

/** Writes the meta tags and a plain copy of the landing page into index.html, and llms.txt beside it. */
function seo(): Plugin {
  return {
    name: 'scholia-seo',
    transformIndexHtml(html) {
      return html.replace('<!--seo-head-->', headTags()).replace('<!--seo-body-->', staticBody())
    },
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'llms.txt', source: llmsTxt() })
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), seo()],
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, './src') },
  },
  server: {
    host: '0.0.0.0',
    port: 5287,
    strictPort: true,
    allowedHosts: ['web', 'localhost'],
    proxy: { '/api': { target: apiTarget, changeOrigin: true } },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    restoreMocks: true,
    exclude: ['e2e/**', 'node_modules/**', 'dist/**'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/main.tsx', 'src/components/ui/**', 'src/test/**', 'src/**/*.d.ts'],
      thresholds: { lines: 85, functions: 85, statements: 85, branches: 80 },
    },
  },
})
