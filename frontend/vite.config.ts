import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  // base 与后端托管一致：后台挂在 /admin 下，构建资产引用 /admin/assets/...。
  // 用无尾斜杠 /admin：vite baseMiddleware 按 startsWith 前缀匹配，
  // 这样访问裸 /admin（不带 /）也能命中并进入 dashboard，而非被 404 提示加斜杠。
  base: '/admin',
  resolve: {
    alias: { '@': '/src' },
  },
  server: {
    // host: true 监听所有网卡，供局域网/容器内其它机器访问（等价 vite --host）。
    host: true,
    port: 8080,
    proxy: {
      '/api': { target: 'http://localhost:8090', changeOrigin: true },
    },
  },
});
