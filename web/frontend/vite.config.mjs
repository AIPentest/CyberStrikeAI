import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { readFile } from 'node:fs/promises';

export default defineConfig({
    plugins: [react(), {
        name: 'local-static-assets',
        configureServer(server) {
            server.middlewares.use(async (req, res, next) => {
                const path = req.url?.split('?')[0];
                if (path?.startsWith('/api/') && !process.env.CSAI_API_ORIGIN) {
                    res.writeHead(503, { 'Content-Type': 'application/json' });
                    res.end(JSON.stringify({ error: '开发服务器未连接后端，请设置 CSAI_API_ORIGIN。' }));
                    return;
                }
                if (!/^\/static\/(?:logo\.png|favicon\.ico|site\.webmanifest|i18n\/[a-z]{2}-[A-Z]{2}\.json)$/.test(path || '')) return next();
                try {
                    const data = await readFile(new URL(`..${path}`, import.meta.url));
                    res.setHeader('Content-Type', path.endsWith('.png') ? 'image/png' : path.endsWith('.ico') ? 'image/x-icon' : 'application/json');
                    res.end(data);
                } catch (error) {
                    next(error);
                }
            });
        }
    }],
    base: '/static/app/',
    build: {
        outDir: '../static/app',
        emptyOutDir: true,
        target: 'es2020',
        chunkSizeWarningLimit: 4000,
        rollupOptions: {
            output: {
                manualChunks(id) {
                    if (id.includes('node_modules/@douyinfe')) return 'semi';
                    if (id.includes('node_modules/react')) return 'react';
                    return undefined;
                }
            }
        }
    },
    server: {
        port: 5199,
        proxy: process.env.CSAI_API_ORIGIN ? {
            '/api': { target: process.env.CSAI_API_ORIGIN, changeOrigin: true }
        } : undefined
    }
});
