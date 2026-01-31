import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import { execSync } from 'child_process';

// Get version from env for CI builds || git tag for local
function getVersion() {
	if (process.env.APP_VERSION) {
		return process.env.APP_VERSION;
	}

	try {
		return execSync('git describe --tags --always').toString().trim();
	} catch {
		console.warn('Failed to get git version, using default');
		return 'dev';
	}
}

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	define: {
		__APP_VERSION__: JSON.stringify(getVersion())
	},
	server: {
		proxy: {
			'/api': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			// Proxy Connect RPC service paths
			'/discopanel.v1': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			// Proxy gRPC reflection service
			'/grpc.reflection': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			// Proxy Connect service paths
			'/connect': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			// Proxy WebSocket connections
			'/ws': {
				target: 'ws://localhost:8080',
				ws: true,
				changeOrigin: true
			}
		}
	},
	optimizeDeps: {
		include: ['monaco-editor']
	}
});
