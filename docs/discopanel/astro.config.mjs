// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://docs.discopanel.app',
	integrations: [
		starlight({
			title: 'DiscoPanel',
			social: [
				{ icon: 'github', label: 'GitHub', href: 'https://github.com/nickheyer/discopanel' },
			],
			sidebar: [
				{ label: 'Introduction', slug: 'introduction' },
				{ label: 'Getting Started', slug: 'getting-started' },
				{ label: 'Configuration', slug: 'configuration' },
				{
					label: 'Guides',
					items: [
						{ label: 'Keycloak Auth Setup', slug: 'guides/keycloak' },
						{ label: 'Authelia Auth Setup', slug: 'guides/authelia' },
					],
				},
				{ label: 'FAQ', slug: 'faq' },
				{ label: 'Troubleshooting', slug: 'troubleshooting' },
				{ label: 'API Reference', slug: 'api' },
				{ label: 'Contributing', slug: 'contributing' },
			],
		}),
	],
});
