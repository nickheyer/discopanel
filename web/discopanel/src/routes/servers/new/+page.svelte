<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Label } from '$lib/components/ui/label';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { Separator } from '$lib/components/ui/separator';
	import { api } from '$lib/api/client';
	import { serversStore } from '$lib/stores/servers';
	import { toast } from 'svelte-sonner';
	import { ArrowLeft, Loader2, Package, Heart } from '@lucide/svelte';
	import type { CreateServerRequest, ModLoader, MinecraftVersion, ModLoaderInfo, DockerImageInfo, IndexedModpack } from '$lib/api/types';
	import { Badge } from '$lib/components/ui/badge';
	import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';

	let loading = $state(false);
	let loadingVersions = $state(true);
	let minecraftVersions = $state<string[]>([]);
	let modLoaders = $state<ModLoaderInfo[]>([]);
	let dockerImages = $state<DockerImageInfo[]>([]);
	let latestVersion = $state('');
	
	// Modpack selection
	let showModpackDialog = $state(false);
	let selectedModpack = $state<IndexedModpack | null>(null);
	let favoriteModpacks = $state<IndexedModpack[]>([]);
	let loadingModpacks = $state(false);

	let formData = $state<CreateServerRequest>({
		name: '',
		description: '',
		mod_loader: 'vanilla',
		mc_version: '',
		port: 25565,
		max_players: 20,
		memory: 2048,
		docker_image: '',
		auto_start: false
	});

	onMount(async () => {
		try {
			const [versionsData, loadersData, imagesData] = await Promise.all([
				api.getMinecraftVersions(),
				api.getModLoaders(),
				api.getDockerImages()
			]);
			
			minecraftVersions = versionsData.versions;
			latestVersion = versionsData.latest;
			modLoaders = loadersData.modloaders;
			dockerImages = imagesData.images;
			
			if (!formData.mc_version && latestVersion) {
				formData.mc_version = latestVersion;
			}
			
			// Load favorite modpacks
			await loadFavoriteModpacks();
			
			// Check if modpack was passed in URL
			const modpackId = $page.url.searchParams.get('modpack');
			if (modpackId) {
				// Load and select the modpack
				try {
					const response = await fetch(`/api/v1/modpacks/${modpackId}`);
					if (response.ok) {
						const data = await response.json();
						await selectModpack(data.modpack);
					}
				} catch (error) {
					console.error('Failed to load modpack from URL:', error);
				}
			}
		} catch (error) {
			toast.error('Failed to load server configuration options');
			console.error(error);
		} finally {
			loadingVersions = false;
		}
	});
	
	async function loadFavoriteModpacks() {
		try {
			const response = await fetch('/api/v1/modpacks/favorites');
			if (!response.ok) throw new Error('Failed to load favorites');
			
			const result = await response.json();
			favoriteModpacks = result.modpacks;
		} catch (error) {
			console.error('Failed to load favorite modpacks:', error);
		}
	}
	
	async function selectModpack(modpack: IndexedModpack) {
		selectedModpack = modpack;
		showModpackDialog = false;
		
		try {
			// Get configuration from the server
			const response = await fetch(`/api/v1/modpacks/${modpack.id}/config`);
			if (!response.ok) throw new Error('Failed to get modpack config');
			
			const config = await response.json();
			
			// Populate ALL form fields from server response
			formData.name = config.name;
			formData.description = config.description;
			formData.mod_loader = config.mod_loader;
			formData.mc_version = config.mc_version;
			formData.memory = config.memory;
			formData.docker_image = config.docker_image;
		} catch (error) {
			toast.error('Failed to load modpack configuration');
			console.error(error);
			selectedModpack = null;
		}
	}
	
	function removeModpack() {
		selectedModpack = null;
		// Reset fields that were set by modpack
		formData.mod_loader = 'vanilla';
		formData.mc_version = latestVersion || '';
		formData.docker_image = '';
		formData.memory = 2048;
	}
	
	function parseJsonArray(jsonStr: string): string[] {
		try {
			return JSON.parse(jsonStr);
		} catch {
			return [];
		}
	}

	async function handleSubmit(e: Event) {
		e.preventDefault();
		
		if (!formData.name.trim()) {
			toast.error('Server name is required');
			return;
		}

		loading = true;
		try {
			// Add modpack ID to the request if selected
			const createRequest = {
				...formData,
				modpack_id: selectedModpack?.id || ''
			};
			
			// Create the server
			const server = await api.createServer(createRequest);
			serversStore.addServer(server);
			
			toast.success(`Server "${server.name}" created successfully!`);
			goto(`/servers/${server.id}`);
		} catch (error) {
			toast.error(`Failed to create server: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			loading = false;
		}
	}

	function getRecommendedMemory(): number {
		switch (formData.mod_loader) {
			case 'vanilla':
				return 2048;
			case 'paper':
			case 'spigot':
				return 2048;
			case 'fabric':
				return 3072;
			case 'forge':
			case 'neoforge':
				return 4096;
			default:
				return 2048;
		}
	}

	function setRecommendedMemory() {
		formData.memory = getRecommendedMemory();
	}

	$effect(() => {
		// Auto-adjust memory when mod loader changes
		if (formData.memory === getRecommendedMemory()) {
			setRecommendedMemory();
		}
	});

	function getDockerImageDisplayName(tagOrImage: string | DockerImageInfo): string {
		const image = typeof tagOrImage === 'string' 
			? dockerImages.find(img => img.tag === tagOrImage)
			: tagOrImage;
		
		if (!image) return tagOrImage as string;
		
		let displayName = `Java ${image.javaVersion} (${image.tag})`;
		if (image.linux !== 'Ubuntu') {
			displayName = `Java ${image.javaVersion} ${image.linux} (${image.tag})`;
		}
		if (image.jvmType !== 'Hotspot') {
			displayName = `Java ${image.javaVersion} ${image.jvmType} (${image.tag})`;
		}
		return displayName;
	}
</script>

<div class="flex-1 space-y-4 p-8 pt-6">
	<div class="flex items-center space-x-2">
		<Button variant="ghost" size="icon" href="/servers">
			<ArrowLeft class="h-4 w-4" />
		</Button>
		<div>
			<h2 class="text-3xl font-bold tracking-tight">Create Server</h2>
			<p class="text-muted-foreground">Set up a new Minecraft server instance</p>
		</div>
	</div>

	<form onsubmit={handleSubmit}>
		<div class="grid gap-4 md:grid-cols-2">
			<Card>
				<CardHeader>
					<CardTitle>Basic Information</CardTitle>
					<CardDescription>Configure your server's basic settings</CardDescription>
				</CardHeader>
				<CardContent class="space-y-4">
					<div class="space-y-2">
						<Label>Configure From</Label>
						<div class="grid grid-cols-2 gap-2">
							<Button
								type="button"
								variant={selectedModpack ? "outline" : "default"}
								onclick={() => selectedModpack = null}
								class="justify-start"
							>
								Manual Configuration
							</Button>
							<Button
								type="button"
								variant={selectedModpack ? "default" : "outline"}
								onclick={() => showModpackDialog = true}
								disabled={loading || favoriteModpacks.length === 0}
								class="justify-start"
							>
								<Package class="h-4 w-4 mr-2" />
								{favoriteModpacks.length === 0 ? 'No Favorites' : 'Modpack'}
							</Button>
						</div>
						
						{#if selectedModpack}
							<Card>
								<CardContent class="p-4">
									<div class="flex items-start gap-3">
										{#if selectedModpack.logo_url}
											<img 
												src={selectedModpack.logo_url} 
												alt={selectedModpack.name}
												class="w-12 h-12 rounded-md object-cover"
											/>
										{/if}
										<div class="flex-1 min-w-0">
											<h4 class="font-semibold">{selectedModpack.name}</h4>
											<p class="text-sm text-muted-foreground line-clamp-2">
												{selectedModpack.summary}
											</p>
											<div class="flex gap-2 mt-2">
												{#if parseJsonArray(selectedModpack.game_versions).length > 0}
													<Badge variant="secondary" class="text-xs">
														MC {parseJsonArray(selectedModpack.game_versions)[0]}
													</Badge>
												{/if}
												{#if parseJsonArray(selectedModpack.mod_loaders).length > 0}
													<Badge variant="secondary" class="text-xs">
														{parseJsonArray(selectedModpack.mod_loaders)[0]}
													</Badge>
												{/if}
											</div>
										</div>
										<Button
											type="button"
											variant="ghost"
											size="sm"
											onclick={removeModpack}
											disabled={loading}
										>
											Remove
										</Button>
									</div>
								</CardContent>
							</Card>
						{:else if favoriteModpacks.length === 0}
							<p class="text-sm text-muted-foreground">
								Visit the <a href="/modpacks" class="underline">Modpacks</a> page to browse and favorite modpacks
							</p>
						{/if}
					</div>

					<Separator />

					<div class="space-y-2">
						<Label for="name">Server Name</Label>
						<Input
							id="name"
							placeholder="My Awesome Server"
							bind:value={formData.name}
							required
							disabled={loading}
						/>
					</div>

					<div class="space-y-2">
						<Label for="description">Description</Label>
						<Textarea
							id="description"
							placeholder="A fun server for friends..."
							bind:value={formData.description}
							disabled={loading}
						/>
					</div>

					<Separator />

					<div class="space-y-2">
						<Label for="mc_version">Minecraft Version</Label>
						{#if loadingVersions}
							<div class="flex items-center justify-center p-4">
								<Loader2 class="h-4 w-4 animate-spin" />
							</div>
						{:else}
							<Select type="single" value={formData.mc_version} onValueChange={(v: string | undefined) => formData.mc_version = v ?? ''} disabled={loading}>
								<SelectTrigger id="mc_version">
									<span>{formData.mc_version || 'Select a version'}</span>
								</SelectTrigger>
								<SelectContent>
									{#each minecraftVersions as version}
										<SelectItem value={version}>
											{version} {version === latestVersion ? '(Latest)' : ''}
										</SelectItem>
									{/each}
								</SelectContent>
							</Select>
						{/if}
					</div>

					<div class="space-y-2">
						<Label for="mod_loader">Mod Loader</Label>
						<Select type="single" value={formData.mod_loader} onValueChange={(v: string | undefined) => formData.mod_loader = (v as ModLoader) ?? 'vanilla'} disabled={loading}>
							<SelectTrigger id="mod_loader">
								<span>{modLoaders.find(l => l.Name === formData.mod_loader)?.DisplayName || 'Select a mod loader'}</span>
							</SelectTrigger>
							<SelectContent>
								{#each modLoaders as loader}
									<SelectItem value={loader.Name}>
										{loader.DisplayName}
									</SelectItem>
								{/each}
							</SelectContent>
						</Select>
						{#if formData.mod_loader === 'vanilla'}
							<p class="text-sm text-muted-foreground">
								No mod support - vanilla Minecraft server
							</p>
						{:else if modLoaders.find(l => l.Name === formData.mod_loader)?.ModsDirectory}
							<p class="text-sm text-muted-foreground">
								Mods will be stored in: {modLoaders.find(l => l.Name === formData.mod_loader)?.ModsDirectory}/
							</p>
						{/if}
					</div>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Server Configuration</CardTitle>
					<CardDescription>Fine-tune your server's performance and network settings</CardDescription>
				</CardHeader>
				<CardContent class="space-y-4">
					<div class="space-y-2">
						<Label for="port">Server Port</Label>
						<Input
							id="port"
							type="number"
							min="1"
							max="65535"
							bind:value={formData.port}
							disabled={loading}
						/>
						<p class="text-sm text-muted-foreground">
							Default Minecraft port is 25565
						</p>
					</div>

					<div class="space-y-2">
						<Label for="max_players">Max Players</Label>
						<Input
							id="max_players"
							type="number"
							min="1"
							max="1000"
							bind:value={formData.max_players}
							disabled={loading}
						/>
					</div>

					<div class="space-y-2">
						<Label for="memory">Memory Allocation (MB)</Label>
						<div class="flex space-x-2">
							<Input
								id="memory"
								type="number"
								min="512"
								max="32768"
								step="512"
								bind:value={formData.memory}
								disabled={loading}
							/>
							<Button
								type="button"
								variant="outline"
								onclick={setRecommendedMemory}
								disabled={loading}
							>
								Recommended
							</Button>
						</div>
						<p class="text-sm text-muted-foreground">
							Recommended: {getRecommendedMemory()} MB for {formData.mod_loader}
						</p>
					</div>

					<Separator />

					<div class="space-y-2">
						<Label for="docker_image">Docker Image (Advanced)</Label>
						<Select type="single" value={formData.docker_image} onValueChange={(v: string | undefined) => formData.docker_image = v ?? ''} disabled={loading || loadingVersions}>
							<SelectTrigger id="docker_image">
								<span>{formData.docker_image ? getDockerImageDisplayName(formData.docker_image) : 'Auto-select (Recommended)'}</span>
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="">Auto-select (Recommended)</SelectItem>
								{#each dockerImages.filter(img => !img.deprecated) as image}
									<SelectItem value={image.tag}>
										{getDockerImageDisplayName(image)}
										{#if image.note}
											<span class="text-xs text-muted-foreground ml-2">({image.note})</span>
										{/if}
									</SelectItem>
								{/each}
							</SelectContent>
						</Select>
						<p class="text-sm text-muted-foreground">
							Leave as auto-select unless you have specific requirements
						</p>
					</div>

					<Separator />

					<div class="flex items-center justify-between">
						<div class="space-y-0.5">
							<Label for="auto_start">Auto Start</Label>
							<p class="text-sm text-muted-foreground">
								Automatically start the server when DiscoPanel starts
							</p>
						</div>
						<Switch
							id="auto_start"
							bind:checked={formData.auto_start}
							disabled={loading}
						/>
					</div>
				</CardContent>
			</Card>
		</div>

		<div class="flex justify-end space-x-2 mt-6">
			<Button variant="outline" href="/servers" disabled={loading}>
				Cancel
			</Button>
			<Button type="submit" disabled={loading || loadingVersions}>
				{#if loading}
					<Loader2 class="h-4 w-4 mr-2 animate-spin" />
					Creating...
				{:else}
					Create Server
				{/if}
			</Button>
		</div>
	</form>
</div>

<Dialog bind:open={showModpackDialog}>
	<DialogContent class="max-w-2xl max-h-[80vh] overflow-hidden flex flex-col">
		<DialogHeader>
			<DialogTitle>Select Modpack</DialogTitle>
			<DialogDescription>
				Choose from your favorite modpacks
			</DialogDescription>
		</DialogHeader>
		
		<div class="overflow-y-auto flex-1 -mx-6 px-6">
			<div class="grid gap-4">
				{#each favoriteModpacks as modpack}
					<Card 
						class="cursor-pointer hover:shadow-md transition-shadow"
						onclick={() => selectModpack(modpack)}
					>
						<CardContent class="p-4">
							<div class="flex items-start gap-4">
								{#if modpack.logo_url}
									<img 
										src={modpack.logo_url} 
										alt={modpack.name}
										class="w-16 h-16 rounded-md object-cover"
									/>
								{/if}
								<div class="flex-1 min-w-0">
									<h4 class="font-semibold">{modpack.name}</h4>
									<p class="text-sm text-muted-foreground line-clamp-2 mb-2">
										{modpack.summary}
									</p>
									<div class="flex items-center gap-2">
										<Badge variant="secondary" class="text-xs">
											{modpack.indexer}
										</Badge>
										{#if parseJsonArray(modpack.game_versions).length > 0}
											<span class="text-xs text-muted-foreground">
												MC: {parseJsonArray(modpack.game_versions)[0]}
											</span>
										{/if}
									</div>
								</div>
							</div>
						</CardContent>
					</Card>
				{/each}
			</div>
		</div>
	</DialogContent>
</Dialog>