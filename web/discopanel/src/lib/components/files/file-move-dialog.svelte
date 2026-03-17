<script lang="ts">
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import { DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { SvelteSet } from 'svelte/reactivity';
	import { Folder, FolderOpen, ChevronRight, ChevronDown } from '@lucide/svelte';
	import type { FileInfo } from '$lib/proto/discopanel/v1/file_pb';

	interface Props {
		open: boolean;
		title: string;
		files: FileInfo[];
		onConfirm: (destinationPath: string) => void;
		onClose: () => void;
	}

	let { open, title, files, onConfirm, onClose }: Props = $props();

	let selectedPath = $state('');
	let expanded = new SvelteSet<string>();

	// Reset when dialog opens
	$effect(() => {
		if (open) {
			selectedPath = '';
			expanded.clear();
		}
	});

	function getDirs(items: FileInfo[]): FileInfo[] {
		return items.filter(f => f.isDir);
	}

	function toggleExpand(path: string) {
		if (expanded.has(path)) {
			expanded.delete(path);
		} else {
			expanded.add(path);
		}
	}
</script>

<DialogPrimitive.Root bind:open>
	<DialogContent class="!max-w-md">
		<DialogHeader>
			<DialogTitle>{title}</DialogTitle>
			<DialogDescription>Select a destination folder</DialogDescription>
		</DialogHeader>
		<div class="border rounded-md max-h-60 overflow-auto py-1">
			<!-- Root option -->
			<button
				class="flex items-center gap-2 w-full px-3 py-1.5 text-sm hover:bg-muted/50 text-left
					{selectedPath === '' ? 'bg-primary/10 font-medium' : ''}"
				onclick={() => selectedPath = ''}
			>
				<FolderOpen class="h-4 w-4 text-blue-400" />
				/ (Root)
			</button>
			{#snippet dirTree(dirs: FileInfo[], depth: number)}
				{#each getDirs(dirs) as dir (dir.path)}
					<div>
						<button
							class="flex items-center gap-1 w-full py-1.5 text-sm hover:bg-muted/50 text-left
								{selectedPath === dir.path ? 'bg-primary/10 font-medium' : ''}"
							style="padding-left: {depth * 16 + 12}px"
							onclick={() => selectedPath = dir.path}
						>
							{#if dir.children && getDirs(dir.children).length > 0}
								<span role="button" tabindex="-1" class="p-0 shrink-0 cursor-pointer" onclick={(e) => { e.stopPropagation(); toggleExpand(dir.path); }}>
									{#if expanded.has(dir.path)}
										<ChevronDown class="h-3 w-3" />
									{:else}
										<ChevronRight class="h-3 w-3" />
									{/if}
								</span>
							{:else}
								<span class="w-3"></span>
							{/if}
							<Folder class="h-4 w-4 text-blue-400 shrink-0" />
							<span class="truncate">{dir.name}</span>
						</button>
						{#if expanded.has(dir.path) && dir.children}
							{@render dirTree(getDirs(dir.children), depth + 1)}
						{/if}
					</div>
				{/each}
			{/snippet}
			{@render dirTree(files, 1)}
		</div>
		<DialogFooter>
			<Button variant="outline" onclick={onClose}>Cancel</Button>
			<Button onclick={() => onConfirm(selectedPath)}>
				{title.startsWith('Move') ? 'Move Here' : 'Copy Here'}
			</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>
