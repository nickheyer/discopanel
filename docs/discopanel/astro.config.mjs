// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://nickheyer.github.io',
	base: '/discopanel',
	integrations: [
		starlight({
			title: 'DiscoPanel',
			social: [
				{ icon: 'github', label: 'GitHub', href: 'https://github.com/nickheyer/discopanel' },
			],
			sidebar: [
				{
					label: 'Getting Started',
					items: [
						{ label: 'Introduction', slug: 'getting-started/introduction' },
						{ label: 'Quick Start', slug: 'getting-started/quick-start' },
					],
				},
				{
					label: 'Deployment',
					autogenerate: { directory: 'deployment' },
				},
				{
					label: 'Guides',
					autogenerate: { directory: 'guides' },
				},
				{
					label: 'Troubleshooting',
					autogenerate: { directory: 'troubleshooting' },
				},
			],
		}),
	],
});
