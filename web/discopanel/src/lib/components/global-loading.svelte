<script lang="ts">
	import { loadingStore } from '$lib/stores/loading.svelte';
	import { Loader2 } from '@lucide/svelte';
	import { fade } from 'svelte/transition';

	let isLoading = $derived($loadingStore.global);
</script>

{#if isLoading}
	<div
		class="fixed top-4 right-4 z-50 flex items-center gap-2 rounded-lg border bg-background/95 p-3 shadow-lg backdrop-blur supports-[backdrop-filter]:bg-background/60"
		transition:fade={{ duration: 200 }}
	>
		<Loader2 class="h-4 w-4 animate-spin text-primary" />
		<span class="text-sm text-muted-foreground">Loading...</span>
	</div>
{/if}
