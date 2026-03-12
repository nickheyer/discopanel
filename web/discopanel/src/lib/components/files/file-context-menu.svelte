<script lang="ts">
	import type { FileInfo } from '$lib/proto/discopanel/v1/file_pb';
	import { FileEdit, Pencil, Copy, FolderInput, Download, FilePlus, FolderPlus, Upload, Archive, Package, Trash2 } from '@lucide/svelte';

	interface Props {
		visible: boolean;
		x: number;
		y: number;
		file: FileInfo | null;
		hasSelection: boolean;
		selectedCount: number;
		onClose: () => void;
		onEdit: () => void;
		onRename: () => void;
		onCopy: () => void;
		onMove: () => void;
		onDownload: () => void;
		onNewFile: () => void;
		onNewFolder: () => void;
		onUpload: () => void;
		onCompress: () => void;
		onExtract: () => void;
		onDelete: () => void;
	}

	let {
		visible, x, y, file, hasSelection, selectedCount,
		onClose, onEdit, onRename, onCopy, onMove, onDownload,
		onNewFile, onNewFolder, onUpload, onCompress, onExtract, onDelete
	}: Props = $props();

	function isArchive(f: FileInfo | null): boolean {
		if (!f || f.isDir) return false;
		const ext = f.name.toLowerCase().split('.').pop() || '';
		return ['zip', 'tar', 'gz', 'tgz', 'rar', '7z', 'bz2', 'xz', 'lz', 'zst', 'tbz2', 'txz'].includes(ext);
	}

	function handleAction(action: () => void) {
		action();
		onClose();
	}

	// Adjust position so menu doesn't go off-screen
	let menuStyle = $derived.by(() => {
		const maxX = typeof window !== 'undefined' ? window.innerWidth - 200 : x;
		const maxY = typeof window !== 'undefined' ? window.innerHeight - 300 : y;
		return `left: ${Math.min(x, maxX)}px; top: ${Math.min(y, maxY)}px;`;
	});

	const itemClass = "flex items-center gap-2 w-full px-3 py-1.5 text-left text-xs cursor-pointer rounded-sm mx-0.5 hover:bg-accent hover:text-accent-foreground transition-colors";
	const dangerClass = "flex items-center gap-2 w-full px-3 py-1.5 text-left text-xs cursor-pointer rounded-sm mx-0.5 text-destructive hover:bg-destructive/10 hover:text-destructive transition-colors";
</script>

{#if visible}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="fixed inset-0 z-50" onclick={onClose} oncontextmenu={(e) => { e.preventDefault(); onClose(); }}>
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="fixed z-50 min-w-[180px] bg-popover border rounded-md shadow-lg py-1 text-popover-foreground"
			style={menuStyle}
			onclick={(e) => e.stopPropagation()}
		>
			{#if file?.isEditable}
				<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onEdit)}>
					<FileEdit class="h-3.5 w-3.5" />
					Edit
				</button>
			{/if}

			{#if file}
				<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onRename)}>
					<Pencil class="h-3.5 w-3.5" />
					Rename
				</button>
			{/if}

			<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onCopy)}>
				<Copy class="h-3.5 w-3.5" />
				Copy{hasSelection && selectedCount > 1 ? ` (${selectedCount})` : ''}
			</button>

			<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onMove)}>
				<FolderInput class="h-3.5 w-3.5" />
				Move to...{hasSelection && selectedCount > 1 ? ` (${selectedCount})` : ''}
			</button>

			<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onDownload)}>
				<Download class="h-3.5 w-3.5" />
				Download{hasSelection && selectedCount > 1 ? ` (${selectedCount})` : ''}
			</button>

			<div class="border-t my-1 mx-2"></div>

			{#if file?.isDir}
				<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onNewFile)}>
					<FilePlus class="h-3.5 w-3.5" />
					New File
				</button>
				<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onNewFolder)}>
					<FolderPlus class="h-3.5 w-3.5" />
					New Folder
				</button>
				<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onUpload)}>
					<Upload class="h-3.5 w-3.5" />
					Upload Here
				</button>
				<div class="border-t my-1 mx-2"></div>
			{/if}

			<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onCompress)}>
				<Archive class="h-3.5 w-3.5" />
				Compress{hasSelection && selectedCount > 1 ? ` (${selectedCount})` : ''}
			</button>

			{#if isArchive(file)}
				<button class={itemClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onExtract)}>
					<Package class="h-3.5 w-3.5" />
					Extract
				</button>
			{/if}

			<div class="border-t my-1 mx-2"></div>

			<button class={dangerClass} style="width: calc(100% - 4px)" onclick={() => handleAction(onDelete)}>
				<Trash2 class="h-3.5 w-3.5" />
				Delete{hasSelection && selectedCount > 1 ? ` (${selectedCount})` : ''}
			</button>
		</div>
	</div>
{/if}
