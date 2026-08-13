// Конфигурация Vite: сборка кладётся в panel/web/dist,
// откуда panel встраивает её через go:embed (разд. 2.4 ТЗ).
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import pkg from './package.json'

export default defineConfig({
  plugins: [vue()],
  // Версия фронта из package.json — показываем в углу интерфейса,
  // чтобы визуально видеть, какой билд реально загружен (диагностика кэша).
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  build: {
    // Статика уезжает внутрь Go-бинарника — единственная точка связи с panel.
    outDir: '../panel/web/dist',
    emptyOutDir: true,
  },
  server: {
    // Dev-режим: проксируем API и WebSocket на локально запущенный panel.
    proxy: {
      '/api': {
        target: 'http://localhost:9000',
        ws: true,
      },
    },
  },
})
