<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Loader2, Upload, Download, Trash2, FolderOpen, Folder, File, FileText, FileCode, Image, Archive, Plus, Edit2, RefreshCw } from '@lucide/svelte';
	import { api } from '$lib/api/client';
	import { toast } from 'svelte-sonner';
	import type { Server, FileInfo } from '$lib/api/types';
	import { formatBytes } from '$lib/utils';
	import FileEditorDialog from './file-editor-dialog.svelte';

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
	let fileInput = $state<HTMLInputElement | null>(null);
	let editingFile = $state<FileInfo | null>(null);
	let showEditor = $state(false);

	let hasLoaded = false;
	
	$effect(() => {
		if (active && !hasLoaded) {
			hasLoaded = true;
			loadFiles();
		}
	});

	async function loadFiles() {
		try {
			loading = true;
			files = await api.listFiles(server.id, '', true); // Get tree view
		} catch (error) {
			toast.error('Failed to load files');
		} finally {
			loading = false;
		}
	}

	function getFileIcon(file: FileInfo) {
		if (file.is_dir) {
			return expandedDirs.has(file.path) ? FolderOpen : Folder;
		}
		
		const ext = file.name.toLowerCase().split('.').pop() || '';
		const textExts = ['txt', 'md', 'json', 'yml', 'yaml', 'toml', 'properties', 'conf', 'cfg', 'log'];
		const codeExts = ['js', 'ts', 'jsx', 'tsx', 'py', 'java', 'cpp', 'c', 'h', 'cs', 'go', 'rs', 'php', 'rb', 'lua'];
		const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'svg', 'webp'];
		const archiveExts = ['zip', 'tar', 'gz', 'rar', '7z', 'bz2'];
		
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

	async function handleFileSelect(event: Event) {
		const input = event.target as HTMLInputElement;
		const files = input.files;
		if (!files || files.length === 0) return;

		uploading = true;
		try {
			for (const file of Array.from(files)) {
				await api.uploadFile(server.id, file, selectedPath);
			}
			toast.success(`Uploaded ${files.length} file(s)`);
			await loadFiles();
		} catch (error) {
			toast.error('Failed to upload files');
		} finally {
			uploading = false;
			input.value = '';
		}
	}

	async function downloadFile(file: FileInfo) {
		try {
			const blob = await api.downloadFile(server.id, file.path);
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
		const confirmed = confirm(`Are you sure you want to delete "${file.name}"${file.is_dir ? ' and all its contents' : ''}?`);
		if (!confirmed) return;

		try {
			await api.deleteFile(server.id, file.path);
			toast.success(`${file.is_dir ? 'Directory' : 'File'} deleted`);
			await loadFiles();
		} catch (error) {
			toast.error(`Failed to delete ${file.is_dir ? 'directory' : 'file'}`);
		}
	}

	function renderFileTree(items: FileInfo[], level = 0): FileInfo[] {
		const result: FileInfo[] = [];
		
		for (const item of items) {
			result.push(item);
			if (item.is_dir && item.children && expandedDirs.has(item.path)) {
				result.push(...renderFileTree(item.children, level + 1));
			}
		}
		
		return result;
	}

	function getIndentLevel(file: FileInfo): number {
		return file.path.split('/').length - 1;
	}

	function isTextFile(file: FileInfo): boolean {
		if (file.is_dir) return false;
		const ext = file.name.toLowerCase().split('.').pop() || '';
		const textExts = [
			'txt', 'md', 'json', 'yml', 'yaml', 'toml', 'properties', 'conf', 'cfg', 'log',
			'js', 'ts', 'jsx', 'tsx', 'py', 'java', 'cpp', 'c', 'h', 'cs', 'go', 'rs', 
			'php', 'rb', 'lua', 'sh', 'bash', 'zsh', 'fish', 'ps1', 'bat', 'cmd',
			'html', 'css', 'scss', 'sass', 'less', 'xml', 'ini', 'env'
		];
		return textExts.includes(ext) || file.name.startsWith('.');
	}

	function editFile(file: FileInfo) {
		if (!isTextFile(file)) {
			toast.error('This file type cannot be edited');
			return;
		}
		editingFile = file;
		showEditor = true;
	}

	let flatFiles = $derived(renderFileTree(files));
</script>

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
				<Button size="sm" variant="outline" onclick={loadFiles}>
					<RefreshCw class="h-4 w-4" />
				</Button>
				<Button onclick={() => fileInput?.click()} disabled={uploading}>
					{#if uploading}
						<Loader2 class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<Upload class="h-4 w-4 mr-2" />
					{/if}
					Upload Files
				</Button>
				<input
					bind:this={fileInput}
					type="file"
					multiple
					onchange={handleFileSelect}
					class="hidden"
				/>
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
								if (file.is_dir) {
									toggleDir(file.path);
								} else if (isTextFile(file)) {
									editFile(file);
								}
							}}
						>
							<Icon class="h-4 w-4 text-muted-foreground" />
							<span class="text-sm">{file.name}</span>
							{#if !file.is_dir}
								<span class="text-xs text-muted-foreground">
									{formatBytes(file.size)}
								</span>
							{/if}
						</button>
						
						<div class="flex items-center gap-1">
							{#if !file.is_dir && isTextFile(file)}
								<Button
									size="icon"
									variant="ghost"
									class="h-7 w-7"
									onclick={() => editFile(file)}
									title="Edit file"
								>
									<Edit2 class="h-3 w-3" />
								</Button>
							{/if}
							{#if !file.is_dir}
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
								onclick={() => deleteFile(file)}
								title="Delete {file.is_dir ? 'directory' : 'file'}"
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

<FileEditorDialog
	serverId={server.id}
	file={editingFile}
	open={showEditor}
	onClose={() => {
		showEditor = false;
		editingFile = null;
	}}
	onSave={() => {
		// Optionally reload files if needed
	}}
/>