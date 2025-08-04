<script lang="ts">
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Loader2, Upload, Download, Trash2, ToggleLeft, ToggleRight, Package, FileText } from '@lucide/svelte';
	import { api } from '$lib/api/client';
	import { toast } from 'svelte-sonner';
	import type { Server, Mod } from '$lib/api/types';
	import { formatBytes } from '$lib/utils';

	interface Props {
		server: Server;
		active?: boolean;
	}

	let { server, active = false }: Props = $props();

	let mods = $state<Mod[]>([]);
	let loading = $state(true);
	let uploading = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);

	let hasLoaded = false;
	
	$effect(() => {
		if (active && !hasLoaded) {
			hasLoaded = true;
			loadMods();
		}
	});

	async function loadMods() {
		try {
			loading = true;
			mods = await api.getMods(server.id);
		} catch (error) {
			toast.error('Failed to load mods');
		} finally {
			loading = false;
		}
	}

	async function handleFileSelect(event: Event) {
		const input = event.target as HTMLInputElement;
		const files = input.files;
		if (!files || files.length === 0) return;

		uploading = true;
		try {
			for (const file of Array.from(files)) {
				await api.uploadMod(server.id, file);
			}
			toast.success(`Uploaded ${files.length} mod(s)`);
			await loadMods();
		} catch (error) {
			toast.error('Failed to upload mod');
		} finally {
			uploading = false;
			input.value = '';
		}
	}

	async function toggleMod(mod: Mod) {
		try {
			await api.updateMod(server.id, mod.id, {
				enabled: !mod.enabled,
				name: mod.name,
				version: mod.version || '',
				description: mod.description || ''
			});
			toast.success(`Mod ${!mod.enabled ? 'enabled' : 'disabled'}`);
			await loadMods();
		} catch (error) {
			toast.error('Failed to toggle mod');
		}
	}

	async function deleteMod(mod: Mod) {
		const confirmed = confirm(`Are you sure you want to delete "${mod.name}"?`);
		if (!confirmed) return;

		try {
			await api.deleteMod(server.id, mod.id);
			toast.success('Mod deleted');
			await loadMods();
		} catch (error) {
			toast.error('Failed to delete mod');
		}
	}

	async function downloadMod(mod: Mod) {
		try {
			const blob = await api.downloadFile(server.id, `mods/${mod.file_name}`);
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = mod.file_name;
			a.click();
			URL.revokeObjectURL(url);
		} catch (error) {
			toast.error('Failed to download mod');
		}
	}

	function getModsDirectory(): string {
		const modLoaderInfo: Record<string, string> = {
			forge: 'mods',
			neoforge: 'mods',
			fabric: 'mods',
			quilt: 'mods',
			bukkit: 'plugins',
			spigot: 'plugins',
			paper: 'plugins',
			pufferfish: 'plugins',
			magma: 'mods',
			magma_maintained: 'mods',
			ketting: 'mods',
			mohist: 'mods',
			youer: 'mods',
			banner: 'mods',
			catserver: 'mods',
			arclight: 'mods',
			spongevanilla: 'mods'
		};
		
		return modLoaderInfo[server.mod_loader] || 'mods';
	}

	function canHaveMods(): boolean {
		const noModLoaders = ['vanilla', 'limbo', 'nanolimbo', 'glowstone', 'custom'];
		return !noModLoaders.includes(server.mod_loader);
	}
</script>

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
									<h4 class="font-medium">{mod.name}</h4>
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
										{mod.file_name}
									</span>
									<span>{formatBytes(mod.file_size)}</span>
									<span>{new Date(mod.uploaded_at).toLocaleDateString()}</span>
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