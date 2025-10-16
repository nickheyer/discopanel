import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	// Consult https://svelte.dev/docs/kit/integrations
	// for more information about preprocessors
	preprocess: [vitePreprocess()],
	onwarn: (warning, handler) => {
		if (warning.code.startsWith('a11y-')) {
			return;
		}
		handler(warning);
	},
	kit: { 
		adapter: adapter({
			fallback: 'index.html',
			strict: false
		}),
		paths: {
			base: ''
		}
	},
	compilerOptions: {  // THE FACT THAT THIS TOOK SO FUCKING LONG TO FIND... I VOW TO NEVER MAKE ACCESSIBLE APPS AGAIN
						// THIS IS WHAT YOU DID SVELTE TEAM. THIS ONE IS ENTIRELY ON YOU, STUPID IDIOTS
		warningFilter: (warning) => !warning.code.startsWith('a11y')
	}
};

export default config;
