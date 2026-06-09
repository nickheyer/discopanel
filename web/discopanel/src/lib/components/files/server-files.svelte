<script lang="ts">
	import { Progress } from '$lib/components/ui/progress';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import { Loader2, Folder, X } from '@lucide/svelte';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { authStore } from '$lib/stores/auth';
	import { toast } from 'svelte-sonner';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { FileInfo } from '$lib/proto/discopanel/v1/file_pb';
	import { formatBytes } from '$lib/utils';
	import { uploadFile, cancelUpload, type UploadProgress } from '$lib/utils/chunked-upload';
	import FileEditorDialog from './file-editor-dialog.svelte';
	import FileToolbar from './file-toolbar.svelte';
	import FileBreadcrumb from './file-breadcrumb.svelte';
	import FileBulkActions from './file-bulk-actions.svelte';
	import FileTree from './file-tree.svelte';
	import FileContextMenu from './file-context-menu.svelte';
	import FileMoveDialog from './file-move-dialog.svelte';
	import { SvelteSet } from 'svelte/reactivity';

	interface Props {
		server: Server;
		active?: boolean;
	}

	let { server, active = false }: Props = $props();

	// --- File state ---
	let files = $state<FileInfo[]>([]);
	let loading = $state(true);

	// --- Upload state ---
	let uploading = $state(false);
	let uploadProgress = $state<UploadProgress | null>(null);
	let currentUploadFilename = $state('');
	let uploadAbortController = $state<AbortController | null>(null);

	// --- Extraction state ---
	let extracting = $state(false);
	let extractionFilesExtracted = $state(0);
	let extractionFilename = $state('');

	// --- Tree state ---
	let expandedDirs = $state<Set<string>>(new Set());
	let filterText = $state('');

	// --- Selection state ---
	let selectedPaths = $state<Set<string>>(new Set());
	let focusedPath = $state('');
	let lastClickedPath = $state('');

	// --- Context menu state ---
	let contextMenuVisible = $state(false);
	let contextMenuX = $state(0);
	let contextMenuY = $state(0);
	let contextMenuFile = $state<FileInfo | null>(null);

	// --- Drag and drop state ---
	let dragOverPath = $state('');

	// --- Dialog state ---
	let editingFile = $state<FileInfo | null>(null);
	let showEditor = $state(false);
	let showNewFileDialog = $state(false);
	let showNewFolderDialog = $state(false);
	let showRenameDialog = $state(false);
	let showMoveDialog = $state(false);
	let showCopyDialog = $state(false);
	let dialogTargetPath = $state('');
	let newItemName = $state('');
	let renamingItem = $state<FileInfo | null>(null);

	let hasLoaded = false;
	let previousServerId: string;

	// --- Derived ---
	let flatFiles = $derived.by(() => {
		const result: FileInfo[] = [];
		function walk(items: FileInfo[]) {
			for (const item of items) {
				if (filterText) {
					const match = item.name.toLowerCase().includes(filterText.toLowerCase());
					if (match) result.push(item);
					if (item.isDir && item.children) walk(item.children);
				} else {
					result.push(item);
					if (item.isDir && item.children && expandedDirs.has(item.path)) {
						walk(item.children);
					}
				}
			}
		}
		walk(files);
		return result;
	});

	let currentPath = $derived.by(() => {
		// Derive breadcrumb from the common prefix of expanded dirs
		// For simplicity, track from focused/selected items
		return '';
	});

	let selectedFiles = $derived.by(() => {
		const paths = selectedPaths;
		const result: FileInfo[] = [];
		function walk(items: FileInfo[]) {
			for (const item of items) {
				if (paths.has(item.path)) result.push(item);
				if (item.isDir && item.children) walk(item.children);
			}
		}
		walk(files);
		return result;
	});

	function isArchiveFile(f: FileInfo): boolean {
		if (f.isDir) return false;
		const ext = f.name.toLowerCase().split('.').pop() || '';
		return ['zip', 'tar', 'gz', 'tgz', 'rar', '7z', 'bz2', 'xz', 'lz', 'zst', 'tbz2', 'txz'].includes(ext);
	}

	let canExtractSelection = $derived(
		selectedFiles.length === 1 && isArchiveFile(selectedFiles[0])
	);

	// --- Lifecycle ---
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			files = [];
			loading = true;
			uploading = false;
			expandedDirs = new SvelteSet();
			selectedPaths = new SvelteSet();
			focusedPath = '';
			hasLoaded = false;
			if (active) {
				loadFiles();
				hasLoaded = true;
			}
		}
	});

	$effect(() => {
		if (active && !hasLoaded) {
			loadFiles();
			hasLoaded = true;
		}
	});

	// --- Data loading ---
	async function loadFiles() {
		try {
			loading = true;
			const response = await rpcClient.file.listFiles({
				serverId: server.id,
				path: '',
				tree: true
			});
			files = response.files;
		} catch {
			toast.error('Failed to load files');
		} finally {
			loading = false;
		}
	}

	// --- Selection logic ---
	function getDepth(file: FileInfo): number {
		return file.path.split('/').length - 1;
	}

	function handleSelect(file: FileInfo, event: MouseEvent) {
		if (event.ctrlKey || event.metaKey) {
			// Toggle selection
			const next = new SvelteSet(selectedPaths);
			if (next.has(file.path)) {
				next.delete(file.path);
			} else {
				next.add(file.path);
			}
			selectedPaths = next;
			lastClickedPath = file.path;
		} else if (event.shiftKey && lastClickedPath) {
			// Range select
			const startIdx = flatFiles.findIndex(f => f.path === lastClickedPath);
			const endIdx = flatFiles.findIndex(f => f.path === file.path);
			if (startIdx !== -1 && endIdx !== -1) {
				const [lo, hi] = startIdx < endIdx ? [startIdx, endIdx] : [endIdx, startIdx];
				const next = new SvelteSet(selectedPaths);
				for (let i = lo; i <= hi; i++) {
					next.add(flatFiles[i].path);
				}
				selectedPaths = next;
			}
		} else {
			// Default click: navigate dirs or edit files
			if (file.isDir) {
				toggleExpand(file.path);
			} else if (file.isEditable) {
				editFile(file);
			}
			selectedPaths = new SvelteSet();
			lastClickedPath = file.path;
		}
		focusedPath = file.path;
	}

	function handleCheckboxToggle(file: FileInfo) {
		const next = new SvelteSet(selectedPaths);
		if (next.has(file.path)) {
			next.delete(file.path);
		} else {
			next.add(file.path);
		}
		selectedPaths = next;
		lastClickedPath = file.path;
	}

	function handleSelectAll() {
		if (selectedPaths.size === flatFiles.length) {
			selectedPaths = new SvelteSet();
		} else {
			selectedPaths = new SvelteSet(flatFiles.map(f => f.path));
		}
	}

	function clearSelection() {
		selectedPaths = new SvelteSet();
	}

	// --- Tree navigation ---
	function toggleExpand(path: string) {
		const next = new SvelteSet(expandedDirs);
		if (next.has(path)) {
			next.delete(path);
		} else {
			next.add(path);
		}
		expandedDirs = next;
	}

	function handleBreadcrumbNavigate(path: string) {
		// Collapse everything and re-expand to the target path
		if (!path) {
			expandedDirs = new SvelteSet();
			return;
		}
		const segments = path.split('/');
		const next = new SvelteSet<string>();
		for (let i = 1; i <= segments.length; i++) {
			next.add(segments.slice(0, i).join('/'));
		}
		expandedDirs = next;
	}

	// --- Keyboard navigation ---
	function handleKeydown(event: KeyboardEvent) {
		const idx = flatFiles.findIndex(f => f.path === focusedPath);

		if (event.key === 'ArrowDown') {
			event.preventDefault();
			if (idx < flatFiles.length - 1) {
				focusedPath = flatFiles[idx + 1].path;
			}
		} else if (event.key === 'ArrowUp') {
			event.preventDefault();
			if (idx > 0) {
				focusedPath = flatFiles[idx - 1].path;
			}
		} else if (event.key === 'ArrowRight') {
			event.preventDefault();
			const file = flatFiles[idx];
			if (file?.isDir && !expandedDirs.has(file.path)) {
				toggleExpand(file.path);
			}
		} else if (event.key === 'ArrowLeft') {
			event.preventDefault();
			const file = flatFiles[idx];
			if (file?.isDir && expandedDirs.has(file.path)) {
				toggleExpand(file.path);
			}
		} else if (event.key === 'Enter') {
			event.preventDefault();
			const file = flatFiles[idx];
			if (file?.isDir) toggleExpand(file.path);
			else if (file?.isEditable) editFile(file);
		} else if (event.key === ' ') {
			event.preventDefault();
			const file = flatFiles[idx];
			if (file) handleCheckboxToggle(file);
		} else if (event.key === 'a' && (event.ctrlKey || event.metaKey)) {
			event.preventDefault();
			handleSelectAll();
		} else if (event.key === 'Delete' || event.key === 'Backspace') {
			if (selectedPaths.size > 0) {
				event.preventDefault();
				bulkDelete();
			}
		}
	}

	// --- Context menu ---
	function handleContextMenu(file: FileInfo, event: MouseEvent) {
		contextMenuFile = file;
		contextMenuX = event.clientX;
		contextMenuY = event.clientY;
		contextMenuVisible = true;
	}

	// --- Drag and drop ---
	function handleDragStart(file: FileInfo, event: DragEvent) {
		if (!event.dataTransfer) return;
		// If dragged file is not in selection, drag just that file
		let paths: string[];
		if (selectedPaths.has(file.path)) {
			paths = Array.from(selectedPaths);
		} else {
			paths = [file.path];
		}
		event.dataTransfer.setData('text/plain', JSON.stringify(paths));
		event.dataTransfer.effectAllowed = 'copyMove';
	}

	function handleDragOver(file: FileInfo, event: DragEvent) {
		if (!event.dataTransfer || !file.isDir) return;
		event.preventDefault();
		event.dataTransfer.dropEffect = event.ctrlKey ? 'copy' : 'move';
		dragOverPath = file.path;
	}

	function handleDragLeave() {
		dragOverPath = '';
	}

	async function handleDrop(file: FileInfo, event: DragEvent) {
		event.preventDefault();
		dragOverPath = '';
		if (!event.dataTransfer || !file.isDir) return;

		const data = event.dataTransfer.getData('text/plain');
		let paths: string[];
		try {
			paths = JSON.parse(data);
		} catch {
			return;
		}

		// Prevent dropping into self or children
		for (const p of paths) {
			if (file.path === p || file.path.startsWith(p + '/')) {
				toast.error("Cannot move a folder into itself");
				return;
			}
		}

		const isCopy = event.ctrlKey;

		try {
			for (const sourcePath of paths) {
				const fileName = sourcePath.split('/').pop() || '';
				const destPath = file.path + '/' + fileName;
				if (isCopy) {
					await rpcClient.file.copyFile({
						serverId: server.id,
						sourcePath,
						destinationPath: destPath
					});
				} else {
					await rpcClient.file.moveFile({
						serverId: server.id,
						sourcePath,
						destinationPath: destPath
					});
				}
			}
			toast.success(`${isCopy ? 'Copied' : 'Moved'} ${paths.length} item(s)`);
			await loadFiles();
		} catch {
			toast.error(`Failed to ${isCopy ? 'copy' : 'move'} files`);
		}
	}

	// --- File operations ---
	function editFile(file: FileInfo) {
		if (file.isDir || !file.isEditable) return;
		editingFile = file;
		showEditor = true;
	}

	async function downloadFile(file: FileInfo) {
		try {
			if (file.isDir) {
				await downloadArchive([file.path]);
			} else {
				const response = await rpcClient.file.initFileDownload({
					serverId: server.id,
					path: file.path
				});
				triggerStreamDownload(response.sessionId, response.filename);
			}
		} catch {
			toast.error('Failed to download');
		}
	}

	async function downloadArchive(paths: string[]) {
		const response = await rpcClient.file.downloadArchive({
			serverId: server.id,
			paths
		});
		triggerStreamDownload(response.sessionId, response.filename);
	}

	function triggerStreamDownload(sessionId: string, filename: string) {
		const token = authStore.getToken();
		const url = `/api/v1/download/${sessionId}${token ? `?token=${encodeURIComponent(token)}` : ''}`;
		const a = document.createElement('a');
		a.href = url;
		a.download = filename;
		a.click();
	}

	async function deleteFile(file: FileInfo) {
		const confirmed = confirm(`Delete "${file.name}"${file.isDir ? ' and all its contents' : ''}?`);
		if (!confirmed) return;
		try {
			await rpcClient.file.deleteFile({
				serverId: server.id,
				path: file.path
			});
			toast.success('Deleted');
			await loadFiles();
		} catch {
			toast.error('Failed to delete');
		}
	}

	function renameFile(file: FileInfo) {
		renamingItem = file;
		newItemName = file.name;
		showRenameDialog = true;
	}

	async function confirmRename() {
		if (!renamingItem || !newItemName.trim() || newItemName === renamingItem.name) {
			showRenameDialog = false;
			return;
		}
		try {
			await rpcClient.file.renameFile({
				serverId: server.id,
				path: renamingItem.path,
				newName: newItemName
			});
			toast.success(`Renamed to ${newItemName}`);
			showRenameDialog = false;
			await loadFiles();
		} catch (error) {
			const msg = error instanceof Error ? error.message : 'Failed to rename';
			toast.error(msg);
		}
	}

	async function createNewFile() {
		if (!newItemName.trim()) return;
		const fullPath = dialogTargetPath ? `${dialogTargetPath}/${newItemName}` : newItemName;
		try {
			await rpcClient.file.updateFile({
				serverId: server.id,
				path: fullPath,
				content: new Uint8Array()
			});
			toast.success(`Created file: ${newItemName}`);
			showNewFileDialog = false;
			await loadFiles();
		} catch {
			toast.error('Failed to create file');
		}
	}

	async function createNewFolder() {
		if (!newItemName.trim()) return;
		const fullPath = dialogTargetPath ? `${dialogTargetPath}/${newItemName}` : newItemName;
		try {
			await rpcClient.file.createFolder({
				serverId: server.id,
				path: fullPath
			});
			toast.success(`Created folder: ${newItemName}`);
			showNewFolderDialog = false;
			await loadFiles();
		} catch {
			toast.error('Failed to create folder');
		}
	}

	// --- Bulk operations ---
	async function bulkDelete() {
		const paths = Array.from(selectedPaths);
		if (paths.length === 0) return;
		const confirmed = confirm(`Delete ${paths.length} item(s)?`);
		if (!confirmed) return;
		try {
			await rpcClient.file.deleteFile({
				serverId: server.id,
				path: '',
				paths
			});
			toast.success(`Deleted ${paths.length} item(s)`);
			selectedPaths = new SvelteSet();
			await loadFiles();
		} catch {
			toast.error('Failed to delete items');
		}
	}

	async function bulkDownload() {
		const paths = Array.from(selectedPaths);
		if (paths.length === 0) return;
		// Single non-dir file: direct download
		if (paths.length === 1) {
			const file = selectedFiles[0];
			if (file && !file.isDir) {
				await downloadFile(file);
				return;
			}
		}
		try {
			await downloadArchive(paths);
		} catch {
			toast.error('Failed to download');
		}
	}

	function bulkMove() {
		if (selectedPaths.size === 0) return;
		showMoveDialog = true;
	}

	// function bulkCopy() {
	// 	if (selectedPaths.size === 0) return;
	// 	showCopyDialog = true;
	// }

	async function bulkCompress() {
		const paths = Array.from(selectedPaths);
		if (paths.length === 0) return;
		try {
			const result = await rpcClient.file.createArchive({
				serverId: server.id,
				paths,
				destinationPath: '',
				archiveName: ''
			});
			toast.success(`Archive created: ${result.filesArchived} files`);
			await loadFiles();
		} catch {
			toast.error('Failed to create archive');
		}
	}

	async function confirmMove(destinationPath: string) {
		const paths = Array.from(selectedPaths);
		showMoveDialog = false;
		try {
			for (const sourcePath of paths) {
				const fileName = sourcePath.split('/').pop() || '';
				const dest = destinationPath ? `${destinationPath}/${fileName}` : fileName;
				await rpcClient.file.moveFile({
					serverId: server.id,
					sourcePath,
					destinationPath: dest
				});
			}
			toast.success(`Moved ${paths.length} item(s)`);
			selectedPaths = new SvelteSet();
			await loadFiles();
		} catch {
			toast.error('Failed to move items');
		}
	}

	async function confirmCopy(destinationPath: string) {
		const paths = Array.from(selectedPaths);
		showCopyDialog = false;
		try {
			for (const sourcePath of paths) {
				const fileName = sourcePath.split('/').pop() || '';
				const dest = destinationPath ? `${destinationPath}/${fileName}` : fileName;
				await rpcClient.file.copyFile({
					serverId: server.id,
					sourcePath,
					destinationPath: dest
				});
			}
			toast.success(`Copied ${paths.length} item(s)`);
			await loadFiles();
		} catch {
			toast.error('Failed to copy items');
		}
	}

	async function extractArchive(file?: FileInfo) {
		const target = file || contextMenuFile || (selectedFiles.length === 1 ? selectedFiles[0] : null);
		if (!target || extracting) return;
		try {
			extracting = true;
			extractionFilename = target.name;
			extractionFilesExtracted = 0;

			const { operationId } = await rpcClient.file.extractArchive({
				serverId: server.id,
				path: target.path
			});

			// Poll for progress
			const poll = setInterval(async () => {
				try {
					const status = await rpcClient.file.getExtractionStatus(
						{ operationId },
						silentCallOptions
					);
					extractionFilesExtracted = status.filesExtracted;

					if (status.state === 'completed') {
						clearInterval(poll);
						extracting = false;
						toast.success(`Extracted ${status.filesExtracted} files`);
						await loadFiles();
					} else if (status.state === 'failed') {
						clearInterval(poll);
						extracting = false;
						toast.error(status.error || 'Extraction failed');
					}
				} catch {
					clearInterval(poll);
					extracting = false;
					toast.error('Lost connection to extraction');
				}
			}, 2000);
		} catch (error) {
			extracting = false;
			const msg = error instanceof Error ? error.message : 'Failed to start extraction';
			toast.error(msg);
		}
	}

	// --- Context menu actions ---
	// Returns the effective paths for a context menu action:
	// use the selection if it exists, otherwise fall back to the right-clicked file.
	function ctxPaths(): string[] {
		if (selectedPaths.size > 0) return Array.from(selectedPaths);
		if (contextMenuFile) return [contextMenuFile.path];
		return [];
	}

	function ctxEdit() {
		if (contextMenuFile) editFile(contextMenuFile);
	}
	function ctxRename() {
		if (contextMenuFile) renameFile(contextMenuFile);
	}
	function ctxCopy() {
		const paths = ctxPaths();
		if (paths.length === 0) return;
		// Temporarily set selection so the copy dialog works
		selectedPaths = new SvelteSet(paths);
		showCopyDialog = true;
	}
	function ctxMove() {
		const paths = ctxPaths();
		if (paths.length === 0) return;
		selectedPaths = new SvelteSet(paths);
		showMoveDialog = true;
	}
	function ctxDownload() {
		if (selectedPaths.size > 0) {
			bulkDownload();
		} else if (contextMenuFile) {
			downloadFile(contextMenuFile);
		}
	}
	function ctxNewFile() {
		dialogTargetPath = contextMenuFile?.isDir ? contextMenuFile.path : '';
		newItemName = '';
		showNewFileDialog = true;
	}
	function ctxNewFolder() {
		dialogTargetPath = contextMenuFile?.isDir ? contextMenuFile.path : '';
		newItemName = '';
		showNewFolderDialog = true;
	}
	function ctxUpload() {
		triggerUpload(contextMenuFile?.isDir ? contextMenuFile.path : '');
	}
	function ctxCompress() {
		const paths = ctxPaths();
		if (paths.length === 0) return;
		selectedPaths = new SvelteSet(paths);
		bulkCompress();
	}
	function ctxExtract() {
		extractArchive();
	}
	function ctxDelete() {
		if (selectedPaths.size > 0) {
			bulkDelete();
		} else if (contextMenuFile) {
			deleteFile(contextMenuFile);
		}
	}

	// --- Upload ---
	function triggerUpload(path: string = '') {
		const input = document.createElement('input');
		input.type = 'file';
		input.multiple = true;
		input.onchange = (e) => handleFileSelect(e, path);
		input.click();
	}

	async function handleFileSelect(event: Event, path: string) {
		const input = event.target as HTMLInputElement;
		const fileList = input.files;
		if (!fileList || fileList.length === 0) return;

		uploading = true;
		uploadAbortController = new AbortController();

		try {
			for (const file of Array.from(fileList)) {
				currentUploadFilename = file.name;
				uploadProgress = null;

				const result = await uploadFile(file, {
					onProgress: (progress) => { uploadProgress = progress; },
					signal: uploadAbortController.signal
				});

				await rpcClient.file.saveUploadedFile({
					serverId: server.id,
					uploadSessionId: result.sessionId,
					destinationPath: path,
					filename: file.name
				});
			}
			toast.success(`Uploaded ${fileList.length} file(s)`);
			await loadFiles();
		} catch (error) {
			if (error instanceof Error && error.message === 'Upload cancelled') {
				toast.info('Upload cancelled');
			} else {
				toast.error('Failed to upload files');
			}
		} finally {
			uploading = false;
			uploadProgress = null;
			currentUploadFilename = '';
			uploadAbortController = null;
			input.value = '';
		}
	}

	function cancelCurrentUpload() {
		if (uploadAbortController) uploadAbortController.abort();
		if (uploadProgress?.sessionId) cancelUpload(uploadProgress.sessionId).catch(() => {});
	}

	let containerEl = $state<HTMLDivElement>();
	let heightStyle = $state('max-height: 600px');

	function measure() {
		if (!containerEl) return;
		const rect = containerEl.getBoundingClientRect();
		const available = window.innerHeight - rect.top - 24;
		heightStyle = `height: ${Math.max(200, available)}px`;
	}

	$effect(() => {
		if (containerEl && active) {
			// Measure after layout settles
			const frame = requestAnimationFrame(() => {
				requestAnimationFrame(measure);
			});
			window.addEventListener('resize', measure);
			return () => {
				cancelAnimationFrame(frame);
				window.removeEventListener('resize', measure);
			};
		}
	});
</script>

<div bind:this={containerEl} class="flex flex-col border rounded-lg overflow-hidden bg-background">
	<!-- Toolbar -->
	<FileToolbar
		{filterText}
		onRefresh={loadFiles}
		onNewFile={() => { dialogTargetPath = ''; newItemName = ''; showNewFileDialog = true; }}
		onNewFolder={() => { dialogTargetPath = ''; newItemName = ''; showNewFolderDialog = true; }}
		onUpload={() => triggerUpload('')}
		onFilterChange={(v) => filterText = v}
	/>

	<!-- Breadcrumb -->
	<FileBreadcrumb
		currentPath={currentPath}
		onNavigate={handleBreadcrumbNavigate}
	/>

	<!-- Upload progress -->
	{#if uploading && uploadProgress}
		<div class="px-3 py-2 border-b">
			<div class="flex items-center justify-between mb-1">
				<span class="text-xs text-muted-foreground truncate">
					Uploading: {currentUploadFilename}
				</span>
				<div class="flex items-center gap-2">
					<span class="text-xs text-muted-foreground">
						{uploadProgress.percentComplete.toFixed(0)}%
					</span>
					<Button size="icon" variant="ghost" class="h-5 w-5" onclick={cancelCurrentUpload} title="Cancel">
						<X class="h-3 w-3" />
					</Button>
				</div>
			</div>
			<Progress value={uploadProgress.percentComplete} class="h-1.5" />
			<p class="text-[10px] text-muted-foreground mt-0.5">
				{formatBytes(uploadProgress.bytesUploaded)} / {formatBytes(uploadProgress.totalBytes)}
			</p>
		</div>
	{/if}

	<!-- Extraction progress -->
	{#if extracting}
		<div class="px-3 py-2 border-b">
			<div class="flex items-center justify-between mb-1">
				<span class="text-xs text-muted-foreground truncate">
					Extracting: {extractionFilename}
				</span>
				<span class="text-xs text-muted-foreground">
					{extractionFilesExtracted} files
				</span>
			</div>
			<Progress value={100} class="h-1.5 animate-pulse" />
		</div>
	{/if}

	<!-- Bulk actions bar -->
	<FileBulkActions
		selectedCount={selectedPaths.size}
		canExtract={canExtractSelection}
		onClear={clearSelection}
		onDelete={bulkDelete}
		onDownload={bulkDownload}
		onMove={bulkMove}
		onCompress={bulkCompress}
		onExtract={() => extractArchive()}
	/>

	<!-- Loading state -->
	{#if loading}
		<div class="flex-1 flex items-center justify-center">
			<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
		</div>
	{:else if files.length === 0}
		<div class="flex-1 flex flex-col items-center justify-center text-muted-foreground">
			<Folder class="h-12 w-12 mb-4" />
			<p>No files found</p>
			<p class="text-sm mt-2">Upload files to get started</p>
		</div>
	{:else}
		<!-- File tree -->
		<FileTree
			{flatFiles}
			{expandedDirs}
			{selectedPaths}
			{focusedPath}
			{dragOverPath}
			onToggleExpand={toggleExpand}
			onSelect={handleSelect}
			onCheckboxToggle={handleCheckboxToggle}
			onSelectAll={handleSelectAll}
			onContextMenu={handleContextMenu}
			onDragStart={handleDragStart}
			onDragOver={handleDragOver}
			onDragLeave={handleDragLeave}
			onDrop={handleDrop}
			onKeydown={handleKeydown}
			{getDepth}
		/>
	{/if}

	<!-- Status bar -->
	<div class="flex items-center justify-between px-3 py-1 border-t text-[10px] text-muted-foreground bg-muted/20">
		<span>{flatFiles.length} items</span>
		{#if selectedPaths.size > 0}
			<span>{selectedPaths.size} selected</span>
		{/if}
	</div>
</div>

<!-- Context menu -->
<FileContextMenu
	visible={contextMenuVisible}
	x={contextMenuX}
	y={contextMenuY}
	file={contextMenuFile}
	hasSelection={selectedPaths.size > 0}
	selectedCount={selectedPaths.size}
	onClose={() => contextMenuVisible = false}
	onEdit={ctxEdit}
	onRename={ctxRename}
	onCopy={ctxCopy}
	onMove={ctxMove}
	onDownload={ctxDownload}
	onNewFile={ctxNewFile}
	onNewFolder={ctxNewFolder}
	onUpload={ctxUpload}
	onCompress={ctxCompress}
	onExtract={ctxExtract}
	onDelete={ctxDelete}
/>

<!-- File editor -->
<FileEditorDialog
	serverId={server.id}
	file={editingFile}
	open={showEditor}
	onClose={() => { showEditor = false; editingFile = null; }}
	onSave={async () => { await loadFiles(); }}
/>

<!-- New File Dialog -->
<DialogPrimitive.Root bind:open={showNewFileDialog}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Create New File</DialogTitle>
			<DialogDescription>
				Enter a name for the new file in {dialogTargetPath || 'root'}
			</DialogDescription>
		</DialogHeader>
		<div class="grid gap-4 py-4">
			<div class="grid gap-2">
				<Label for="new-file-name">File Name</Label>
				<Input
					id="new-file-name"
					bind:value={newItemName}
					placeholder="example.txt"
					onkeydown={(e) => { if (e.key === 'Enter') createNewFile(); }}
				/>
			</div>
		</div>
		<DialogFooter>
			<Button variant="outline" onclick={() => showNewFileDialog = false}>Cancel</Button>
			<Button onclick={createNewFile}>Create</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>

<!-- New Folder Dialog -->
<DialogPrimitive.Root bind:open={showNewFolderDialog}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Create New Folder</DialogTitle>
			<DialogDescription>
				Enter a name for the new folder in {dialogTargetPath || 'root'}
			</DialogDescription>
		</DialogHeader>
		<div class="grid gap-4 py-4">
			<div class="grid gap-2">
				<Label for="new-folder-name">Folder Name</Label>
				<Input
					id="new-folder-name"
					bind:value={newItemName}
					placeholder="new-folder"
					onkeydown={(e) => { if (e.key === 'Enter') createNewFolder(); }}
				/>
			</div>
		</div>
		<DialogFooter>
			<Button variant="outline" onclick={() => showNewFolderDialog = false}>Cancel</Button>
			<Button onclick={createNewFolder}>Create</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>

<!-- Rename Dialog -->
<DialogPrimitive.Root bind:open={showRenameDialog}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Rename {renamingItem?.isDir ? 'Folder' : 'File'}</DialogTitle>
			<DialogDescription>
				Enter a new name for {renamingItem?.name}
			</DialogDescription>
		</DialogHeader>
		<div class="grid gap-4 py-4">
			<div class="grid gap-2">
				<Label for="rename-item">New Name</Label>
				<Input
					id="rename-item"
					bind:value={newItemName}
					placeholder={renamingItem?.name}
					onkeydown={(e) => { if (e.key === 'Enter') confirmRename(); }}
				/>
			</div>
		</div>
		<DialogFooter>
			<Button variant="outline" onclick={() => showRenameDialog = false}>Cancel</Button>
			<Button onclick={confirmRename}>Rename</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>

<!-- Move Dialog -->
<FileMoveDialog
	open={showMoveDialog}
	title="Move {selectedPaths.size} item(s)"
	{files}
	onConfirm={confirmMove}
	onClose={() => showMoveDialog = false}
/>

<!-- Copy Dialog -->
<FileMoveDialog
	open={showCopyDialog}
	title="Copy {selectedPaths.size} item(s)"
	{files}
	onConfirm={confirmCopy}
	onClose={() => showCopyDialog = false}
/>
