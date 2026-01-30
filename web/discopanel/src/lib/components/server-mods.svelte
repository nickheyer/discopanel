<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { ResizablePaneGroup, ResizablePane } from '$lib/components/ui/resizable';
	import { Badge } from '$lib/components/ui/badge';
	import { Progress } from '$lib/components/ui/progress';
	import { Loader2, Upload, Download, Trash2, ToggleLeft, ToggleRight, Package, FileText, X } from '@lucide/svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { ModLoader, type Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { Mod } from '$lib/proto/discopanel/v1/mod_pb';
	import { formatBytes } from '$lib/utils';
	import { uploadFile, cancelUpload, type UploadProgress } from '$lib/utils/chunked-upload';

	interface Props {
		server: Server;
		active?: boolean;
	}

	let { server, active = false }: Props = $props();

	let mods = $state<Mod[]>([]);
	let loading = $state(true);
	let uploading = $state(false);
	let uploadProgress = $state<UploadProgress | null>(null);
	let currentUploadFilename = $state('');
	let uploadAbortController = $state<AbortController | null>(null);
	let fileInput = $state<HTMLInputElement | null>(null);

	let hasLoaded = false;
	let previousServerId = $state(server.id);
	
	// Reset state when server changes
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			// Reset state variables
			mods = [];
			loading = true;
			uploading = false;
			hasLoaded = false;
		}
	});
	
	$effect(() => {
		if (active && !hasLoaded) {
			hasLoaded = true;
			loadMods();
		}
	});

	async function loadMods() {
		try {
			loading = true;
			const response = await rpcClient.mod.listMods({ serverId: server.id });
			mods = response.mods;
		} catch (error) {
			if (server.modLoader !== ModLoader.VANILLA) {
				toast.error('Failed to load mods');
			}
		} finally {
			loading = false;
		}
	}

	async function handleFileSelect(event: Event) {
		const input = event.target as HTMLInputElement;
		const fileList = input.files;
		if (!fileList || fileList.length === 0) return;

		uploading = true;
		uploadAbortController = new AbortController();

		try {
			for (const file of Array.from(fileList)) {
				currentUploadFilename = file.name;
				uploadProgress = null;

				// Use chunked upload
				const result = await uploadFile(file, {
					onProgress: (progress) => {
						uploadProgress = progress;
					},
					signal: uploadAbortController.signal
				});

				// Import the uploaded mod
				await rpcClient.mod.importUploadedMod({
					serverId: server.id,
					uploadSessionId: result.sessionId,
					displayName: file.name,
					description: ''
				});
			}
			toast.success(`Uploaded ${fileList.length} mod(s)`);
			await loadMods();
		} catch (error: any) {
			if (error.message === 'Upload cancelled') {
				toast.info('Upload cancelled');
			} else {
				toast.error('Failed to upload mod');
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
		if (uploadAbortController) {
			uploadAbortController.abort();
		}
		if (uploadProgress?.sessionId) {
			cancelUpload(uploadProgress.sessionId).catch(() => {});
		}
	}

	async function toggleMod(mod: Mod) {
		try {
			await rpcClient.mod.updateMod({
				serverId: server.id,
				modId: mod.id,
				enabled: !mod.enabled,
				displayName: mod.displayName,
				description: mod.description
			});
			toast.success(`Mod ${!mod.enabled ? 'enabled' : 'disabled'}`);
			await loadMods();
		} catch (error) {
			toast.error('Failed to toggle mod');
		}
	}

	async function deleteMod(mod: Mod) {
		const confirmed = confirm(`Are you sure you want to delete "${mod.displayName}"?`);
		if (!confirmed) return;

		try {
			await rpcClient.mod.deleteMod({
				serverId: server.id,
				modId: mod.id
			});
			toast.success('Mod deleted');
			await loadMods();
		} catch (error) {
			toast.error('Failed to delete mod');
		}
	}

	async function downloadMod(mod: Mod) {
		try {
			const response = await rpcClient.file.getFile({
				serverId: server.id,
				path: `${getModsDirectory()}/${mod.fileName}`
			});
			const blob = new Blob([new Uint8Array(response.content)]);
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = mod.fileName;
			a.click();
			URL.revokeObjectURL(url);
		} catch (error) {
			toast.error('Failed to download mod');
		}
	}

	function getModsDirectory(): string {
		const modLoaderInfo: Record<ModLoader, string> = {
			[ModLoader.UNSPECIFIED]: 'mods',
			[ModLoader.VANILLA]: 'mods',
			[ModLoader.FORGE]: 'mods',
			[ModLoader.NEOFORGE]: 'mods',
			[ModLoader.FABRIC]: 'mods',
			[ModLoader.QUILT]: 'mods',
			[ModLoader.BUKKIT]: 'plugins',
			[ModLoader.SPIGOT]: 'plugins',
			[ModLoader.PAPER]: 'plugins',
			[ModLoader.PURPUR]: 'plugins',
			[ModLoader.SPONGE_VANILLA]: 'mods',
			[ModLoader.SPONGE_FORGE]: 'mods',
			[ModLoader.MOHIST]: 'mods',
			[ModLoader.CATSERVER]: 'mods',
			[ModLoader.ARCLIGHT]: 'mods',
			[ModLoader.AUTO_CURSEFORGE]: 'mods',
			[ModLoader.MODRINTH]: 'mods',
			[ModLoader.FOLIA]: 'plugins'
		};

		return modLoaderInfo[server.modLoader] || 'mods';
	}

	function canHaveMods(): boolean {
		const noModLoaders = [ModLoader.VANILLA, ModLoader.UNSPECIFIED];
		return !noModLoaders.includes(server.modLoader);
	}
</script>

<ResizablePaneGroup direction="vertical" class="h-full max-h-[800px] min-h-[400px] rounded-lg border">
<ResizablePane defaultSize={100}>
<Card class="h-full flex flex-col">
	<CardHeader>
		<div class="flex items-center justify-between">
			<div>
				<CardTitle>Mod Management</CardTitle>
				<p class="text-sm text-muted-foreground mt-1">
					{#if canHaveMods()}
						Manage mods in the {getModsDirectory()} directory
					{:else}
						This server type does not support mods
					{/if}
				</p>
			</div>
			{#if canHaveMods()}
				<Button onclick={() => fileInput?.click()} disabled={uploading}>
					{#if uploading}
						<Loader2 class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<Upload class="h-4 w-4 mr-2" />
					{/if}
					Upload Mods
				</Button>
				<input
					bind:this={fileInput}
					type="file"
					multiple
					accept=".jar,.zip"
					onchange={handleFileSelect}
					class="hidden"
				/>
			{/if}
		</div>
	</CardHeader>
	{#if uploading && uploadProgress}
		<div class="px-6 pb-4">
			<div class="flex items-center justify-between mb-2">
				<span class="text-sm text-muted-foreground">
					Uploading: {currentUploadFilename}
				</span>
				<div class="flex items-center gap-2">
					<span class="text-sm text-muted-foreground">
						{uploadProgress.percentComplete.toFixed(0)}%
					</span>
					<Button size="icon" variant="ghost" class="h-6 w-6" onclick={cancelCurrentUpload} title="Cancel upload">
						<X class="h-4 w-4" />
					</Button>
				</div>
			</div>
			<Progress value={uploadProgress.percentComplete} class="h-2" />
			<p class="text-xs text-muted-foreground mt-1">
				{formatBytes(uploadProgress.bytesUploaded)} / {formatBytes(uploadProgress.totalBytes)}
			</p>
		</div>
	{/if}
	<CardContent class="flex-1 overflow-auto">
		{#if !canHaveMods()}
			<div class="flex flex-col items-center justify-center py-12 text-muted-foreground">
				<Package class="h-12 w-12 mb-4" />
				<p>This server type does not support mods</p>
			</div>
		{:else if loading}
			<div class="flex items-center justify-center py-12">
				<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
			</div>
		{:else if mods.length === 0}
			<div class="flex flex-col items-center justify-center py-12 text-muted-foreground">
				<Package class="h-12 w-12 mb-4" />
				<p>No mods installed</p>
				<p class="text-sm mt-2">Upload mods to get started</p>
			</div>
		{:else}
			<div class="space-y-2">
				{#each mods as mod}
					<div class="flex items-center justify-between p-4 rounded-lg border">
						<div class="flex items-center gap-4">
							<button
								onclick={() => toggleMod(mod)}
								class="text-muted-foreground hover:text-foreground transition-colors"
								title={mod.enabled ? 'Disable mod' : 'Enable mod'}
							>
								{#if mod.enabled}
									<ToggleRight class="h-6 w-6 text-green-500" />
								{:else}
									<ToggleLeft class="h-6 w-6" />
								{/if}
							</button>
							
							<div>
								<div class="flex items-center gap-2">
									<h4 class="font-medium">{mod.displayName}</h4>
									{#if mod.version}
										<Badge variant="secondary" class="text-xs">{mod.version}</Badge>
									{/if}
									{#if !mod.enabled}
										<Badge variant="outline" class="text-xs">Disabled</Badge>
									{/if}
								</div>
								<div class="flex items-center gap-4 text-sm text-muted-foreground mt-1">
									<span class="flex items-center gap-1">
										<FileText class="h-3 w-3" />
										{mod.fileName}
									</span>
									<span>{formatBytes(Number(mod.fileSize))}</span>
									<span>{mod.uploadedAt ? new Date(Number(mod.uploadedAt.seconds) * 1000).toLocaleDateString() : ''}</span>
								</div>
								{#if mod.description}
									<p class="text-sm text-muted-foreground mt-2">{mod.description}</p>
								{/if}
							</div>
						</div>
						
						<div class="flex items-center gap-2">
							<Button
								size="icon"
								variant="ghost"
								onclick={() => downloadMod(mod)}
								title="Download mod"
							>
								<Download class="h-4 w-4" />
							</Button>
							<Button
								size="icon"
								variant="ghost"
								onclick={() => deleteMod(mod)}
								title="Delete mod"
							>
								<Trash2 class="h-4 w-4" />
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