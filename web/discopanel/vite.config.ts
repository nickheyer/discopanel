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
	} catch (error) {
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
			}
		}
	},
	optimizeDeps: {
		include: ['monaco-editor']
	}
});
