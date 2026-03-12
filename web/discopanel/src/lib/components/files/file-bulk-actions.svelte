<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Download, Trash2, FolderInput, Archive, Package, X } from '@lucide/svelte';

	interface Props {
		selectedCount: number;
		canExtract: boolean;
		onClear: () => void;
		onDelete: () => void;
		onDownload: () => void;
		onMove: () => void;
		onCompress: () => void;
		onExtract: () => void;
	}

	let { selectedCount, canExtract, onClear, onDelete, onDownload, onMove, onCompress, onExtract }: Props = $props();

	let active = $derived(selectedCount > 0);
</script>

<!-- Always rendered at fixed height to prevent layout shift -->
<div class="flex items-center justify-between px-3 h-[32px] border-b transition-colors {active ? 'bg-primary/5' : 'bg-transparent'}">
	{#if active}
		<div class="flex items-center gap-2">
			<span class="text-xs font-medium">{selectedCount} selected</span>
			<Button size="sm" variant="ghost" class="h-6 px-2 text-xs" onclick={onClear}>
				<X class="h-3 w-3 mr-1" />
				Clear
			</Button>
		</div>
		<div class="flex items-center gap-0.5">
			<Button size="icon" variant="ghost" class="h-7 w-7" onclick={onDownload} title="Download selected">
				<Download class="h-3.5 w-3.5" />
			</Button>
			<Button size="icon" variant="ghost" class="h-7 w-7" onclick={onMove} title="Move selected">
				<FolderInput class="h-3.5 w-3.5" />
			</Button>
			<Button size="icon" variant="ghost" class="h-7 w-7" onclick={onCompress} title="Compress selected">
				<Archive class="h-3.5 w-3.5" />
			</Button>
			{#if canExtract}
				<Button size="icon" variant="ghost" class="h-7 w-7" onclick={onExtract} title="Extract archive">
					<Package class="h-3.5 w-3.5" />
				</Button>
			{/if}
			<Button size="icon" variant="ghost" class="h-7 w-7 text-destructive hover:text-destructive" onclick={onDelete} title="Delete selected">
				<Trash2 class="h-3.5 w-3.5" />
			</Button>
		</div>
	{:else}
		<span class="text-[10px] text-muted-foreground/50">Ctrl+Click to select &middot; Right-click for options</span>
	{/if}
</div>
