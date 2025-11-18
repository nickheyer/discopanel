<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { ResizablePaneGroup, ResizablePane } from '$lib/components/ui/resizable';
	import { Loader2, Upload, Download, Trash2, FolderOpen, Folder, File, FileText, FileCode, Image, Archive, FileEdit, RefreshCw, Plus, FolderPlus, FilePlus, Pencil, Package } from '@lucide/svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { FileInfo } from '$lib/proto/discopanel/v1/file_pb';
	import { formatBytes } from '$lib/utils';
	import FileEditorDialog from './file-editor-dialog.svelte';
	import { DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { Dialog as DialogPrimitive } from "bits-ui";
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';

	interface Props {
		server: Server;
		active?: boolean;
	}

	let { server, active = false }: Props = $props();

	let files = $state<FileInfo[]>([]);
	let loading = $state(true);
	let uploading = $state(false);
	let selectedPath = $state('');
	let expandedDirs = $state<Set<string>>(new Set());
	let editingFile = $state<FileInfo | null>(null);
	let showEditor = $state(false);
	let showNewFileDialog = $state(false);
	let showNewFolderDialog = $state(false);
	let showRenameDialog = $state(false);
	let targetPath = $state('');
	let newItemName = $state('');
	let renamingItem = $state<FileInfo | null>(null);

	let hasLoaded = false;
	let previousServerId = $state(server.id);
	
	// Reset state when server changes
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			// Reset state variables
			files = [];
			loading = true;
			uploading = false;
			selectedPath = '';
			expandedDirs = new Set();
			editingFile = null;
			showEditor = false;
			hasLoaded = false;
			
			// If currently active, load files immediately
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

	async function loadFiles() {
		try {
			loading = true;
			const response = await rpcClient.file.listFiles({
				serverId: server.id,
				path: '',
				tree: true
			});
			files = response.files;
		} catch (error) {
			toast.error('Failed to load files');
		} finally {
			loading = false;
		}
	}

	function getFileIcon(file: FileInfo) {
		if (file.isDir) {
			return expandedDirs.has(file.path) ? FolderOpen : Folder;
		}
		
		const ext = file.name.toLowerCase().split('.').pop() || '';
		const textExts = ['txt', 'md', 'json', 'yml', 'yaml', 'toml', 'properties', 'conf', 'cfg', 'log'];
		const codeExts = ['js', 'ts', 'jsx', 'tsx', 'py', 'java', 'cpp', 'c', 'h', 'cs', 'go', 'rs', 'php', 'rb', 'lua'];
		const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'svg', 'webp'];
		const archiveExts = ['zip', 'tar', 'gz', 'tgz', 'rar', '7z', 'bz2', 'xz', 'lz', 'zst', 'tbz2', 'txz'];
		
		if (textExts.includes(ext)) return FileText;
		if (codeExts.includes(ext)) return FileCode;
		if (imageExts.includes(ext)) return Image;
		if (archiveExts.includes(ext)) return Archive;
		return File;
	}

	function toggleDir(path: string) {
		if (expandedDirs.has(path)) {
			expandedDirs.delete(path);
		} else {
			expandedDirs.add(path);
		}
		expandedDirs = new Set(expandedDirs);
	}

	async function handleFileSelect(event: Event, path?: string) {
		const input = event.target as HTMLInputElement;
		const files = input.files;
		if (!files || files.length === 0) return;

		const uploadPath = path || selectedPath || '';
		uploading = true;
		try {
			for (const file of Array.from(files)) {
				const arrayBuffer = await file.arrayBuffer();
				await rpcClient.file.uploadFile({
					serverId: server.id,
					path: uploadPath,
					filename: file.name,
					content: new Uint8Array(arrayBuffer)
				});
			}
			toast.success(`Uploaded ${files.length} file(s) to ${uploadPath || 'root'}`);
			await loadFiles();
		} catch (error) {
			toast.error('Failed to upload files');
		} finally {
			uploading = false;
			input.value = '';
		}
	}

	function triggerUpload(path: string = '') {
		targetPath = path;
		const input = document.createElement('input');
		input.type = 'file';
		input.multiple = true;
		input.onchange = (e) => handleFileSelect(e, path);
		input.click();
	}

	async function downloadFile(file: FileInfo) {
		try {
			const response = await rpcClient.file.getFile({
				serverId: server.id,
				path: file.path
			});
			const blob = new Blob([response.content]);
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = file.name;
			a.click();
			URL.revokeObjectURL(url);
		} catch (error) {
			toast.error('Failed to download file');
		}
	}

	async function deleteFile(file: FileInfo) {
		const confirmed = confirm(`Are you sure you want to delete "${file.name}"${file.isDir ? ' and all its contents' : ''}?`);
		if (!confirmed) return;

		try {
			await rpcClient.file.deleteFile({
				serverId: server.id,
				path: file.path
			});
			toast.success(`${file.isDir ? 'Directory' : 'File'} deleted`);
			await loadFiles();
		} catch (error) {
			toast.error(`Failed to delete ${file.isDir ? 'directory' : 'file'}`);
		}
	}

	function renderFileTree(items: FileInfo[], level = 0): FileInfo[] {
		const result: FileInfo[] = [];
		
		for (const item of items) {
			result.push(item);
			if (item.isDir && item.children && expandedDirs.has(item.path)) {
				result.push(...renderFileTree(item.children, level + 1));
			}
		}
		
		return result;
	}

	function getIndentLevel(file: FileInfo): number {
		return file.path.split('/').length - 1;
	}

	function editFile(file: FileInfo) {
		if (file.isDir || !file.isEditable) {
			toast.error('This file cannot be edited');
			return;
		}
		editingFile = file;
		showEditor = true;
	}

	async function createNewFile(path: string, fileName: string) {
		if (!fileName.trim()) {
			toast.error('File name cannot be empty');
			return;
		}
		
		const fullPath = path ? `${path}/${fileName}` : fileName;
		try {
			await rpcClient.file.updateFile({
				serverId: server.id,
				path: fullPath,
				content: new Uint8Array()
			});
			toast.success(`Created file: ${fileName}`);
			await loadFiles();
		} catch (error) {
			toast.error('Failed to create file');
		}
	}

	async function createNewFolder(path: string, folderName: string) {
		if (!folderName.trim()) {
			toast.error('Folder name cannot be empty');
			return;
		}
		
		const fullPath = path ? `${path}/${folderName}` : folderName;
		try {
			// Create a dummy file in the folder to ensure it exists
			await rpcClient.file.updateFile({
				serverId: server.id,
				path: `${fullPath}/.gitkeep`,
				content: new Uint8Array()
			});
			toast.success(`Created folder: ${folderName}`);
			await rpcClient.file.deleteFile({
				serverId: server.id,
				path: `${fullPath}/.gitkeep`
			});
			await loadFiles();
		} catch (error) {
			toast.error('Failed to create folder');
		}
	}

	async function renameItem(item: FileInfo, newName: string) {
		if (!newName.trim()) {
			toast.error('Name cannot be empty');
			return;
		}
		
		if (newName === item.name) {
			return;
		}
		
		try {
			await rpcClient.file.renameFile({
				serverId: server.id,
				path: item.path,
				newName: newName
			});
			toast.success(`Renamed ${item.name} to ${newName}`);
			await loadFiles();
		} catch (error: any) {
			toast.error(error.message || 'Failed to rename item');
		}
	}

	async function extractArchive(file: FileInfo) {
		try {
			const result = await rpcClient.file.extractArchive({
				serverId: server.id,
				path: file.path
			});
			toast.success(`Archive extracted successfully`);
			await loadFiles();
		} catch (error: any) {
			toast.error(error.message || 'Failed to extract archive');
		}
	}

	let flatFiles = $derived(renderFileTree(files));
</script>

<ResizablePaneGroup direction="vertical" class="h-full max-h-[800px] min-h-[400px] rounded-lg border">
<ResizablePane defaultSize={100}>
<Card class="h-full flex flex-col">
	<CardHeader>
		<div class="flex items-center justify-between">
			<div>
				<CardTitle>File Manager</CardTitle>
				<p class="text-sm text-muted-foreground mt-1">
					Browse and manage server files
				</p>
			</div>
			<div class="flex items-center gap-2">
				<Button size="sm" variant="outline" onclick={loadFiles} title="Refresh">
					<RefreshCw class="h-4 w-4" />
				</Button>
				<Button size="sm" variant="outline" onclick={() => triggerUpload('')} title="Upload To Server Folder">
					<Upload class="h-4 w-4" />
				</Button>
				<Button size="sm" variant="outline" onclick={() => {
					targetPath = '';
					newItemName = '';
					showNewFileDialog = true;
				}} title="New file in server folder">
					<FilePlus class="h-4 w-4" />
				</Button>
				<Button size="sm" variant="outline" onclick={() => {
					targetPath = '';
					newItemName = '';
					showNewFolderDialog = true;
				}} title="New folder in server folder">
					<FolderPlus class="h-4 w-4" />
				</Button>
			</div>
		</div>
	</CardHeader>
	<CardContent class="flex-1 overflow-auto">
		{#if loading}
			<div class="flex items-center justify-center py-12">
				<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
			</div>
		{:else if files.length === 0}
			<div class="flex flex-col items-center justify-center py-12 text-muted-foreground">
				<Folder class="h-12 w-12 mb-4" />
				<p>No files found</p>
				<p class="text-sm mt-2">Upload files to get started</p>
			</div>
		{:else}
			<div class="space-y-0.5">
				{#each flatFiles as file}
					{@const Icon = getFileIcon(file)}
					{@const indent = getIndentLevel(file)}
					<div 
						class="flex items-center justify-between py-1.5 px-2 rounded hover:bg-muted/50 cursor-pointer"
						style="padding-left: {(indent * 20) + 8}px"
					>
						<button
							class="flex items-center gap-2 flex-1 text-left"
							onclick={() => {
								if (file.isDir) {
									toggleDir(file.path);
								} else if (file.isEditable) {
									editFile(file);
								}
							}}
						>
							<Icon class="h-4 w-4 text-muted-foreground" />
							<span class="text-sm">{file.name}</span>
							{#if !file.isDir}
								<span class="text-xs text-muted-foreground">
									{formatBytes(Number(file.size))}
								</span>
							{/if}
						</button>
						
						<div class="flex items-center gap-1">
							{#if file.isDir}
								<Button
									size="icon"
									variant="ghost"
									class="h-7 w-7"
									onclick={() => triggerUpload(file.path)}
									title="Upload files to this folder"
								>
									<Upload class="h-3 w-3" />
								</Button>
								<Button
									size="icon"
									variant="ghost"
									class="h-7 w-7"
									onclick={() => {
										targetPath = file.path;
										newItemName = '';
										showNewFileDialog = true;
									}}
									title="Create new file in this folder"
								>
									<FilePlus class="h-3 w-3" />
								</Button>
								<Button
									size="icon"
									variant="ghost"
									class="h-7 w-7"
									onclick={() => {
										targetPath = file.path;
										newItemName = '';
										showNewFolderDialog = true;
									}}
									title="Create new folder in this folder"
								>
									<FolderPlus class="h-3 w-3" />
								</Button>
							{/if}
							{#if file.isEditable}
								<Button
									size="icon"
									variant="ghost"
									class="h-7 w-7"
									onclick={() => editFile(file)}
									title="Edit file"
								>
									<FileEdit class="h-3 w-3" />
								</Button>
							{/if}
							{#if !file.isDir && getFileIcon(file) === Archive}
								<Button
									size="icon"
									variant="ghost"
									class="h-7 w-7"
									onclick={() => extractArchive(file)}
									title="Extract archive"
								>
									<Package class="h-3 w-3" />
								</Button>
							{/if}
							{#if !file.isDir}
								<Button
									size="icon"
									variant="ghost"
									class="h-7 w-7"
									onclick={() => downloadFile(file)}
									title="Download file"
								>
									<Download class="h-3 w-3" />
								</Button>
							{/if}
							<Button
								size="icon"
								variant="ghost"
								class="h-7 w-7"
								onclick={() => {
									renamingItem = file;
									newItemName = file.name;
									showRenameDialog = true;
								}}
								title="Rename {file.isDir ? 'folder' : 'file'}"
							>
								<Pencil class="h-3 w-3" />
							</Button>
							<Button
								size="icon"
								variant="ghost"
								class="h-7 w-7"
								onclick={() => deleteFile(file)}
								title="Delete {file.isDir ? 'directory' : 'file'}"
							>
								<Trash2 class="h-3 w-3" />
							</Button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</CardContent>
</Card>
</ResizablePane>
</ResizablePaneGroup>

<FileEditorDialog
	serverId={server.id}
	file={editingFile}
	open={showEditor}
	onClose={() => {
		showEditor = false;
		editingFile = null;
	}}
	onSave={() => {
		// TODO: reload files
	}}
/>

<!-- New File Dialog -->
<DialogPrimitive.Root bind:open={showNewFileDialog}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Create New File</DialogTitle>
			<DialogDescription>
				Enter a name for the new file in {targetPath || 'root'}
			</DialogDescription>
		</DialogHeader>
		<div class="grid gap-4 py-4">
			<div class="grid gap-2">
				<Label for="new-file-name">File Name</Label>
				<Input 
					id="new-file-name" 
					bind:value={newItemName}
					placeholder="example.txt"
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							createNewFile(targetPath, newItemName);
							showNewFileDialog = false;
						}
					}}
				/>
			</div>
		</div>
		<DialogFooter>
			<Button variant="outline" onclick={() => showNewFileDialog = false}>
				Cancel
			</Button>
			<Button onclick={() => {
				createNewFile(targetPath, newItemName);
				showNewFileDialog = false;
			}}>
				Create
			</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>

<!-- New Folder Dialog -->
<DialogPrimitive.Root bind:open={showNewFolderDialog}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Create New Folder</DialogTitle>
			<DialogDescription>
				Enter a name for the new folder in {targetPath || 'root'}
			</DialogDescription>
		</DialogHeader>
		<div class="grid gap-4 py-4">
			<div class="grid gap-2">
				<Label for="new-folder-name">Folder Name</Label>
				<Input 
					id="new-folder-name" 
					bind:value={newItemName}
					placeholder="new-folder"
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							createNewFolder(targetPath, newItemName);
							showNewFolderDialog = false;
						}
					}}
				/>
			</div>
		</div>
		<DialogFooter>
			<Button variant="outline" onclick={() => showNewFolderDialog = false}>
				Cancel
			</Button>
			<Button onclick={() => {
				createNewFolder(targetPath, newItemName);
				showNewFolderDialog = false;
			}}>
				Create
			</Button>
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
					onkeydown={(e) => {
						if (e.key === 'Enter' && renamingItem) {
							renameItem(renamingItem, newItemName);
							showRenameDialog = false;
						}
					}}
				/>
			</div>
		</div>
		<DialogFooter>
			<Button variant="outline" onclick={() => showRenameDialog = false}>
				Cancel
			</Button>
			<Button onclick={() => {
				if (renamingItem) {
					renameItem(renamingItem, newItemName);
					showRenameDialog = false;
				}
			}}>
				Rename
			</Button>
		</DialogFooter>
	</DialogContent>
</DialogPrimitive.Root>