<script lang="ts">
	import type { FileInfo } from '$lib/proto/discopanel/v1/file_pb';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import FileTreeRow from './file-tree-row.svelte';

	interface Props {
		flatFiles: FileInfo[];
		expandedDirs: Set<string>;
		selectedPaths: Set<string>;
		focusedPath: string;
		dragOverPath: string;
		onToggleExpand: (path: string) => void;
		onSelect: (file: FileInfo, event: MouseEvent) => void;
		onCheckboxToggle: (file: FileInfo) => void;
		onSelectAll: () => void;
		onContextMenu: (file: FileInfo, event: MouseEvent) => void;
		onDragStart: (file: FileInfo, event: DragEvent) => void;
		onDragOver: (file: FileInfo, event: DragEvent) => void;
		onDragLeave: () => void;
		onDrop: (file: FileInfo, event: DragEvent) => void;
		onKeydown: (event: KeyboardEvent) => void;
		getDepth: (file: FileInfo) => number;
	}

	let {
		flatFiles, expandedDirs, selectedPaths, focusedPath, dragOverPath,
		onToggleExpand, onSelect, onCheckboxToggle, onSelectAll, onContextMenu,
		onDragStart, onDragOver, onDragLeave, onDrop, onKeydown, getDepth
	}: Props = $props();

	let hasSelection = $derived(selectedPaths.size > 0);
	let allSelected = $derived(flatFiles.length > 0 && selectedPaths.size === flatFiles.length);
</script>

<div
	class="flex-1 min-h-0 overflow-auto focus:outline-none"
	tabindex="0"
	role="tree"
	onkeydown={onKeydown}
>
	<!-- Column header - matches row layout exactly -->
	<div class="flex items-center h-[26px] text-[11px] text-muted-foreground border-b bg-background sticky top-0 z-10 pr-3">
		<!-- Checkbox column -->
		<div class="flex items-center justify-center w-6 shrink-0 {hasSelection ? 'visible' : 'invisible'}">
			<Checkbox
				checked={allSelected}
				onCheckedChange={onSelectAll}
				class="h-3.5 w-3.5"
			/>
		</div>
		<!-- Indent placeholder + chevron column -->
		<div class="w-4 shrink-0"></div>
		<!-- Name -->
		<div class="flex-1 pl-1 font-medium uppercase tracking-wider">Name</div>
		<!-- Size -->
		<span class="w-16 text-right font-medium uppercase tracking-wider shrink-0">Size</span>
		<!-- Modified -->
		<span class="w-20 text-right font-medium uppercase tracking-wider shrink-0 hidden sm:inline-block">Modified</span>
	</div>

	<!-- File rows -->
	{#each flatFiles as file (file.path)}
		<FileTreeRow
			{file}
			depth={getDepth(file)}
			isExpanded={expandedDirs.has(file.path)}
			isSelected={selectedPaths.has(file.path)}
			isFocused={focusedPath === file.path}
			isDragOver={dragOverPath === file.path}
			{hasSelection}
			{onToggleExpand}
			{onSelect}
			{onCheckboxToggle}
			{onContextMenu}
			{onDragStart}
			{onDragOver}
			{onDragLeave}
			{onDrop}
		/>
	{/each}

	{#if flatFiles.length === 0}
		<div class="flex flex-col items-center justify-center py-12 text-muted-foreground text-sm">
			<p>No files found</p>
		</div>
	{/if}
</div>
