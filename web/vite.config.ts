import { defineConfig } from 'vite'

export default defineConfig({
  build: {
    manifest: true,
    // Old open tabs can continue to load the previous build's hashed chunks.
    emptyOutDir: false,
    rolldownOptions: {
      onLog(level, log, handler) {
        // Ant Design marks client components for RSC consumers. This app renders entirely in the browser.
        if (log.code === 'MODULE_LEVEL_DIRECTIVE' && log.message.includes('"use client"') && log.message.includes('node_modules/')) {
          return
        }
        handler(level, log)
      },
    },
  },
})
