import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// dev server 与构建/预览的 base 取值不同：
//   - 构建态 base='/admin'：资产引用 /admin/assets/...，与后端 mountUI 挂载的 /admin、/login 两前缀一致。
//   - 开发态 base='/'：vite baseMiddleware 在 base='/admin' 时只放行以 /admin 开头的路径，
//     顶层 /login 会被它 404。改回 / 后 /login、/admin、深层路由都能回退 index.html 交给 React Router。
// isPreview 亦为 command='serve'，故需一并排除，否则 vite preview 会错误回退成 '/'。
export default defineConfig(({ command, isPreview }) => ({
  plugins: [react(), tailwindcss()],
  base: command === 'serve' && !isPreview ? '/' : '/admin',
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
}));
