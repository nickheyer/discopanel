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
				{
					label: 'Getting Started',
					items: [
						{ label: 'Docker Compose', slug: 'getting-started/docker-compose' },
						{ label: 'Proxmox LXC', slug: 'getting-started/proxmox' },
						{ label: 'Prebuilt Binaries', slug: 'getting-started/prebuilt-binaries' },
						{ label: 'Building from Source', slug: 'getting-started/build-from-source' },
					],
				},
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
