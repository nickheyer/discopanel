<script lang="ts">
	import { icons } from '@lucide/svelte';

	interface Props {
		name: string | undefined;
		class?: string;
		fallback?: string;
	}

	let { name, class: className = '', fallback = 'Box' }: Props = $props();

	// Convert kebab-case to PascalCase for icon lookup
	function kebabToPascal(str: string): string {
		return str
			.split('-')
			.map((word) => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
			.join('');
	}

	// Get the icon component by name
	let IconComponent = $derived.by(() => {
		if (!name) {
			return icons[fallback as keyof typeof icons] || null;
		}

		// Try exact match first (already PascalCase)
		if (name in icons) {
			return icons[name as keyof typeof icons];
		}

		// Try converting from kebab-case
		const pascalName = kebabToPascal(name);
		if (pascalName in icons) {
			return icons[pascalName as keyof typeof icons];
		}

		// Try lowercase match
		const lowerName = name.toLowerCase();
		for (const key of Object.keys(icons)) {
			if (key.toLowerCase() === lowerName) {
				return icons[key as keyof typeof icons];
			}
		}

		// Fallback
		return icons[fallback as keyof typeof icons] || null;
	});
</script>

{#if IconComponent}
	{@const Icon = IconComponent}
	<Icon class={className} />
{/if}
