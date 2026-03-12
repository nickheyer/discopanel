<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { FilePlus, FolderPlus, Upload, RefreshCw, Search, X } from '@lucide/svelte';

	interface Props {
		filterText: string;
		onRefresh: () => void;
		onNewFile: () => void;
		onNewFolder: () => void;
		onUpload: () => void;
		onFilterChange: (value: string) => void;
	}

	let { filterText, onRefresh, onNewFile, onNewFolder, onUpload, onFilterChange }: Props = $props();

	let showSearch = $state(false);
</script>

<div class="flex items-center justify-between px-3 py-1.5 border-b bg-muted/30">
	<div class="flex items-center gap-0.5">
		<Button size="icon" variant="ghost" class="h-7 w-7" onclick={onNewFile} title="New File">
			<FilePlus class="h-3.5 w-3.5" />
		</Button>
		<Button size="icon" variant="ghost" class="h-7 w-7" onclick={onNewFolder} title="New Folder">
			<FolderPlus class="h-3.5 w-3.5" />
		</Button>
		<Button size="icon" variant="ghost" class="h-7 w-7" onclick={onUpload} title="Upload Files">
			<Upload class="h-3.5 w-3.5" />
		</Button>
	</div>
	<div class="flex items-center gap-0.5">
		{#if showSearch}
			<div class="flex items-center gap-1">
				<Input
					class="h-7 w-40 text-xs"
					placeholder="Filter files..."
					value={filterText}
					oninput={(e) => onFilterChange((e.target as HTMLInputElement).value)}
					autofocus
				/>
				<Button size="icon" variant="ghost" class="h-7 w-7" onclick={() => { showSearch = false; onFilterChange(''); }}>
					<X class="h-3.5 w-3.5" />
				</Button>
			</div>
		{:else}
			<Button size="icon" variant="ghost" class="h-7 w-7" onclick={() => showSearch = true} title="Filter">
				<Search class="h-3.5 w-3.5" />
			</Button>
		{/if}
		<Button size="icon" variant="ghost" class="h-7 w-7" onclick={onRefresh} title="Refresh">
			<RefreshCw class="h-3.5 w-3.5" />
		</Button>
	</div>
</div>
