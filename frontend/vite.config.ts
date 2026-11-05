import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: { '/api': process.env.AIQ_API_TARGET || 'http://127.0.0.1:8080', '/health': process.env.AIQ_API_TARGET || 'http://127.0.0.1:8080' },
  },
});
