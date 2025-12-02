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
	import { toast } from 'svelte-sonner';
	import { ArrowLeft, Loader2, Package, Settings, HardDrive } from '@lucide/svelte';
	import type { CreateServerRequest, ModLoader, ModLoaderInfo, DockerImageInfo, IndexedModpack } from '$lib/api/types';
	import { Badge } from '$lib/components/ui/badge';
	import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import AdditionalPortsEditor from '$lib/components/additional-ports-editor.svelte';
	import DockerOverridesEditor from '$lib/components/docker-overrides-editor.svelte';
	import { getUniqueDockerImages, getDockerImageDisplayName } from '$lib/utils';
	import { canManageServers } from '$lib/stores/auth';

	let userCanManageServers = $derived($canManageServers);

	// Redirect if user doesn't have permission to create servers
	$effect(() => {
		if (!userCanManageServers) {
			toast.error('You do not have permission to create servers');
			goto('/servers');
		}
	});

	let loading = $state(false);
	let loadingVersions = $state(true);
	let minecraftVersions = $state<string[]>([]);
	let modLoaders = $state<ModLoaderInfo[]>([]);
	let dockerImages = $state<DockerImageInfo[]>([]);
	let latestVersion = $state('');
	let proxyEnabled = $state(false);
	let proxyBaseURL = $state('');
	let proxyListeners = $state<any[]>([]);
	let usedPorts = $state<Record<number, boolean>>({});
	let portError = $state('');
	let useProxyMode = $state(false); // Track connection mode separately
	
	// Modpack selection
	let showModpackDialog = $state(false);
	let selectedModpack = $state<IndexedModpack | null>(null);
	let favoriteModpacks = $state<IndexedModpack[]>([]);
	let modpackVersions = $state<any[]>([]);
	let selectedVersionId = $state<string>('');
	let loadingModpackVersions = $state(false);

	let formData = $state<CreateServerRequest>({
		name: '',
		description: '',
		mod_loader: 'vanilla',
		mc_version: '',
		port: 25565,
		max_players: 20,
		memory: 2048,
		docker_image: '',
		auto_start: false,
		detached: false,
		start_immediately: false,
		proxy_hostname: '',
		proxy_listener_id: '',
		use_base_url: false,
		additional_ports: [],
		docker_overrides: undefined
	});

	onMount(async () => {
		try {
			const [versionsData, loadersData, imagesData, proxyStatus, portData, listeners] = await Promise.all([
				api.getMinecraftVersions(),
				api.getModLoaders(),
				api.getDockerImages(),
				api.getProxyStatus(),
				api.getNextAvailablePort(),
				api.getProxyListeners()
			]);
			
			minecraftVersions = versionsData.versions;
			latestVersion = versionsData.latest;
			modLoaders = loadersData.modloaders;
			dockerImages = imagesData.images;
			proxyEnabled = proxyStatus.enabled;
			proxyBaseURL = proxyStatus.base_url || '';
			proxyListeners = listeners.filter((l: any) => l.enabled);
			
			// Set default listener if available
			const defaultListener = proxyListeners.find((l: any) => l.is_default);
			if (defaultListener) {
				formData.proxy_listener_id = defaultListener.id;
			} else if (proxyListeners.length > 0) {
				formData.proxy_listener_id = proxyListeners[0].id;
			}
			
			// Set the default port to the next available port
			formData.port = portData.port;
			usedPorts = portData.usedPorts;
			
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
	
	async function loadModpackVersions(modpackId: string) {
		loadingModpackVersions = true;
		modpackVersions = [];
		selectedVersionId = '';

		try {
			const response = await fetch(`/api/v1/modpacks/${modpackId}/versions`);
			if (!response.ok) throw new Error('Failed to get modpack versions');

			const data = await response.json();
			modpackVersions = data.versions || [];
		} catch (error) {
			console.error('Failed to load modpack versions:', error);
			modpackVersions = [];
		} finally {
			loadingModpackVersions = false;
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
			formData.name = config.name;
			formData.description = config.description;
			formData.mod_loader = config.mod_loader;
			formData.mc_version = config.mc_version;
			formData.memory = config.memory;
			formData.docker_image = config.docker_image;
			await loadModpackVersions(modpack.id);
		} catch (error) {
			toast.error('Failed to load modpack configuration');
			console.error(error);
			selectedModpack = null;
		}
	}
	
	function removeModpack() {
		selectedModpack = null;
		modpackVersions = [];
		selectedVersionId = '';
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

	function validatePort(port: number) {
		portError = '';
		
		if (port < 1 || port > 65535) {
			portError = 'Port must be between 1 and 65535';
			return false;
		}
		
		if (usedPorts[port]) {
			portError = 'This port is already in use';
			return false;
		}
		
		return true;
	}

	async function refreshAvailablePort() {
		try {
			const portData = await api.getNextAvailablePort();
			formData.port = portData.port;
			usedPorts = portData.usedPorts;
			portError = '';
		} catch (error) {
			console.error('Failed to get available port:', error);
		}
	}

	async function handleSubmit(e: Event) {
		e.preventDefault();
		
		if (!formData.name.trim()) {
			toast.error('Server name is required');
			return;
		}

		// Validate port only if not using proxy mode
		if (!useProxyMode && !validatePort(formData.port)) {
			toast.error('Please select a valid port');
			return;
		}

		loading = true;
		try {
			// Add modpack ID and version to the request if selected
			// For Modrinth, we need to send version_number instead of ID for better compatibility
			const selectedVersion = modpackVersions.find(v => v.id === selectedVersionId);
			const versionToSend = selectedModpack?.indexer === 'modrinth' && selectedVersion?.version_number
				? selectedVersion.version_number
				: selectedVersionId;

			const createRequest = {
				...formData,
				modpack_id: selectedModpack?.id || '',
				modpack_version_id: versionToSend || '',
				// When using proxy with hostname, set port to 0 to indicate proxy usage
				port: useProxyMode ? 0 : formData.port
			};
			
			// Create the server
			const server = await api.createServer(createRequest);
			toast.success(`Server "${server.name}" created successfully!`);
			goto(`/servers/${server.id}`);
		} catch (error) {
			toast.error(`Failed to create server: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			loading = false;
		}
	}

</script>

<div class="h-full overflow-y-auto bg-gradient-to-br from-background to-muted/10">
	<div class="space-y-8 p-4 sm:p-6 lg:p-8 pt-4 sm:pt-6">
	<div class="flex items-center gap-4 pb-6 border-b-2 border-border/50">
		<Button variant="ghost" size="icon" href="/servers" class="shrink-0 h-12 w-12 rounded-xl hover:bg-muted">
			<ArrowLeft class="h-5 w-5" />
		</Button>
		<div class="flex items-center gap-4">
			<div class="h-16 w-16 rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg">
				<Package class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1">
				<h2 class="text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">Create New Server</h2>
				<p class="text-base text-muted-foreground">Set up a new Minecraft server instance with your preferred configuration</p>
			</div>
		</div>
	</div>

	<form onsubmit={handleSubmit}>
		<div class="grid gap-6 lg:grid-cols-2">
			<Card class="relative overflow-hidden border-2 hover:border-primary/30 transition-colors shadow-xl bg-gradient-to-br from-card to-card/90">
				<div class="absolute top-0 right-0 w-48 h-48 bg-gradient-to-br from-primary/10 to-transparent rounded-full -mr-24 -mt-24"></div>
				<CardHeader class="pb-6">
					<div class="flex items-center gap-3">
						<div class="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
							<Settings class="h-5 w-5 text-primary" />
						</div>
						<div>
							<CardTitle class="text-2xl">Basic Information</CardTitle>
							<CardDescription class="text-base">Configure your server's basic settings and metadata</CardDescription>
						</div>
					</div>
				</CardHeader>
				<CardContent class="space-y-6">
					<div class="space-y-3">
						<Label class="text-sm font-medium">Configuration Method</Label>
						<div class="grid grid-cols-2 gap-3">
							<Button
								type="button"
								variant={selectedModpack ? "outline" : "default"}
								onclick={() => selectedModpack = null}
								class="justify-start h-auto py-3 px-4 transition-all hover:scale-[1.02]"
							>
								<div class="text-left">
									<div class="font-medium">Manual Configuration</div>
									<div class="text-xs text-muted-foreground mt-0.5">Start from scratch</div>
								</div>
							</Button>
							<Button
								type="button"
								variant={selectedModpack ? "default" : "outline"}
								onclick={() => showModpackDialog = true}
								disabled={loading || favoriteModpacks.length === 0}
								class="justify-start h-auto py-3 px-4 transition-all hover:scale-[1.02]"
							>
								<Package class="h-4 w-4 mr-2 shrink-0" />
								<div class="text-left">
									<div class="font-medium">{favoriteModpacks.length === 0 ? 'No Favorites' : 'From Modpack'}</div>
									<div class="text-xs text-muted-foreground mt-0.5">Use preset configuration</div>
								</div>
							</Button>
						</div>
						
						{#if selectedModpack}
							<Card class="border-2 border-primary/30 bg-gradient-to-br from-primary/10 to-primary/5 shadow-lg">
								<CardContent class="p-5">
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

											{#if modpackVersions.length > 0}
												<div class="mt-3">
													<Label for="modpack_version" class="text-xs font-medium text-muted-foreground">Version (optional)</Label>
													<Select
														type="single"
														value={selectedVersionId}
														onValueChange={(v) => selectedVersionId = v || ''}
														disabled={loading || loadingModpackVersions}
													>
														<SelectTrigger id="modpack_version" class="h-8 mt-1">
															<span class="text-sm">
																{selectedVersionId
																	? modpackVersions.find(v => v.id === selectedVersionId)?.display_name || 'Latest'
																	: 'Latest version'}
															</span>
														</SelectTrigger>
														<SelectContent>
															<SelectItem value="">
																Latest version
															</SelectItem>
															{#each modpackVersions as version (version.id)}
																<SelectItem value={version.id}>
																	{version.display_name}
																	{#if version.release_type}
																		<Badge variant={version.release_type === 'release' ? 'default' : version.release_type === 'beta' ? 'secondary' : 'outline'} class="ml-2 text-xs">
																			{version.release_type}
																		</Badge>
																	{/if}
																</SelectItem>
															{/each}
														</SelectContent>
													</Select>
												</div>
											{:else if loadingModpackVersions}
												<div class="mt-3 text-xs text-muted-foreground">
													<Loader2 class="h-3 w-3 animate-spin inline mr-1" />
													Loading versions...
												</div>
											{/if}
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
						<Label for="name" class="text-sm font-medium">Server Name <span class="text-destructive">*</span></Label>
						<Input
							id="name"
							placeholder="My Awesome Server"
							bind:value={formData.name}
							required
							disabled={loading}
							class="h-10"
						/>
					</div>

					<div class="space-y-2">
						<Label for="description" class="text-sm font-medium">Description <span class="text-muted-foreground text-xs">(optional)</span></Label>
						<Textarea
							id="description"
							placeholder="A fun server for friends..."
							bind:value={formData.description}
							disabled={loading}
							class="min-h-[80px] resize-none"
						/>
					</div>

					<Separator />

					<div class="space-y-2">
						<Label for="mc_version" class="text-sm font-medium">Minecraft Version</Label>
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
									{#each minecraftVersions as version (version)}
										<SelectItem value={version}>
											{version} {version === latestVersion ? '(Latest)' : ''}
										</SelectItem>
									{/each}
								</SelectContent>
							</Select>
						{/if}
					</div>

					<div class="space-y-2">
						<Label for="mod_loader" class="text-sm font-medium">Mod Loader</Label>
						<Select type="single" value={formData.mod_loader} onValueChange={(v: string | undefined) => formData.mod_loader = (v as ModLoader) ?? 'vanilla'} disabled={loading || !!selectedModpack}>
							<SelectTrigger id="mod_loader">
								<span>{modLoaders.find(l => l.Name === formData.mod_loader)?.DisplayName || 'Select a mod loader'}</span>
							</SelectTrigger>
							<SelectContent>
								{#each modLoaders as loader (loader.Name)}
									<SelectItem value={loader.Name}>
										{loader.DisplayName}
									</SelectItem>
								{/each}
							</SelectContent>
						</Select>
						{#if selectedModpack}
							<p class="text-sm text-muted-foreground">
								Mod loader auto-determined from modpack
							</p>
						{:else if formData.mod_loader === 'vanilla'}
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

			<Card class="relative overflow-hidden border-2 hover:border-primary/30 transition-colors shadow-xl bg-gradient-to-br from-card to-card/90">
				<div class="absolute top-0 right-0 w-48 h-48 bg-gradient-to-br from-primary/10 to-transparent rounded-full -mr-24 -mt-24"></div>
				<CardHeader class="pb-6">
					<div class="flex items-center gap-3">
						<div class="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
							<HardDrive class="h-5 w-5 text-primary" />
						</div>
						<div>
							<CardTitle class="text-2xl">Server Configuration</CardTitle>
							<CardDescription class="text-base">Fine-tune your server's performance and network settings</CardDescription>
						</div>
					</div>
				</CardHeader>
				<CardContent class="space-y-6">
					{#if proxyEnabled}
						<div class="space-y-4">
							<div class="space-y-2">
								<Label class="text-sm font-medium">Connection Method</Label>
								<div class="grid grid-cols-2 gap-3">
									<Button
										type="button"
										variant={!useProxyMode ? "default" : "outline"}
										onclick={() => {
											useProxyMode = false;
											formData.proxy_hostname = '';
											// Reset port error when switching to direct port
											portError = '';
										}}
										class="justify-start h-auto py-3 px-4"
									>
										<div class="text-left">
											<div class="font-medium">Direct Port</div>
											<div class="text-xs text-muted-foreground mt-0.5">Connect via port number</div>
										</div>
									</Button>
									<Button
										type="button"
										variant={useProxyMode ? "default" : "outline"}
										onclick={() => {
											useProxyMode = true;
											if (!formData.proxy_hostname) {
												formData.proxy_hostname = formData.name.toLowerCase().replace(/\s+/g, '-') || 'minecraft-server';
											}
											// Clear port error when using proxy
											portError = '';
										}}
										class="justify-start h-auto py-3 px-4"
									>
										<div class="text-left">
											<div class="font-medium">Custom Hostname</div>
											<div class="text-xs text-muted-foreground mt-0.5">Connect via domain name</div>
										</div>
									</Button>
								</div>
							</div>

							{#if useProxyMode}
								<div class="space-y-4">
									<!-- Listener Selection -->
									{#if proxyListeners.length > 0}
										<div class="space-y-2">
											<Label for="proxy_listener" class="text-sm font-medium">Proxy Listener</Label>
											<Select 
												type="single" 
												value={formData.proxy_listener_id} 
												onValueChange={(v) => formData.proxy_listener_id = v || ''}
												disabled={loading}
											>
												<SelectTrigger id="proxy_listener">
													<span>
														{proxyListeners.find(l => l.id === formData.proxy_listener_id)?.name || 'Select a listener'}
													</span>
												</SelectTrigger>
												<SelectContent>
													{#each proxyListeners as listener (listener.id)}
														<SelectItem value={listener.id}>
															{listener.name} (Port {listener.port})
															{#if listener.is_default}
																<span class="text-xs text-muted-foreground ml-2">[Default]</span>
															{/if}
														</SelectItem>
													{/each}
												</SelectContent>
											</Select>
											<p class="text-xs text-muted-foreground">
												Select which proxy port players will connect through
											</p>
										</div>
									{/if}

									<!-- Hostname Input -->
									<div class="space-y-2">
										<Label for="proxy_hostname" class="text-sm font-medium">Server Hostname</Label>
										<Input
											id="proxy_hostname"
											placeholder={proxyBaseURL ? "survival" : "survival.example.com"}
											bind:value={formData.proxy_hostname}
											disabled={loading}
											class="h-10"
										/>
										
										<!-- Base URL Checkbox -->
										{#if proxyBaseURL}
											<div class="flex items-center gap-2">
												<input
													type="checkbox"
													id="use_base_url"
													bind:checked={formData.use_base_url}
													class="h-4 w-4"
												/>
												<Label for="use_base_url" class="text-sm font-medium">
													Append base domain ({proxyBaseURL})
												</Label>
											</div>
										{/if}
										
										<p class="text-xs text-muted-foreground">
											{#if formData.use_base_url && proxyBaseURL}
												Players will connect using: {formData.proxy_hostname}.{proxyBaseURL}
											{:else}
												Players will connect using: {formData.proxy_hostname}
											{/if}
										</p>
									</div>
								</div>
							{:else}
								<div class="space-y-2">
									<div class="flex items-center justify-between">
										<Label for="port" class="text-sm font-medium">Server Port</Label>
										<Button
											type="button"
											variant="ghost"
											size="sm"
											onclick={refreshAvailablePort}
											disabled={loading}
										>
											Auto-assign
										</Button>
									</div>
									<Input
										id="port"
										type="number"
										min="1"
										max="65535"
										bind:value={formData.port}
										oninput={(e) => validatePort(Number(e.currentTarget.value))}
										disabled={loading}
										class="h-10 {portError ? 'border-destructive' : ''}"
									/>
									{#if portError}
										<p class="text-xs text-destructive">{portError}</p>
									{:else}
										<p class="text-xs text-muted-foreground">
											Default Minecraft port is 25565
										</p>
									{/if}
								</div>
							{/if}
						</div>
					{:else}
						<div class="space-y-2">
							<div class="flex items-center justify-between">
								<Label for="port" class="text-sm font-medium">Server Port</Label>
								<Button
									type="button"
									variant="ghost"
									size="sm"
									onclick={refreshAvailablePort}
									disabled={loading}
								>
									Auto-assign
								</Button>
							</div>
							<Input
								id="port"
								type="number"
								min="1"
								max="65535"
								bind:value={formData.port}
								oninput={(e) => validatePort(Number(e.currentTarget.value))}
								disabled={loading}
								class="h-10 {portError ? 'border-destructive' : ''}"
							/>
							{#if portError}
								<p class="text-xs text-destructive">{portError}</p>
							{:else}
								<p class="text-xs text-muted-foreground">
									Default Minecraft port is 25565
								</p>
							{/if}
						</div>
					{/if}

					<div class="space-y-2">
						<Label for="max_players" class="text-sm font-medium">Max Players</Label>
						<Input
							id="max_players"
							type="number"
							min="1"
							max="1000"
							bind:value={formData.max_players}
							disabled={loading}
							class="h-10"
						/>
					</div>

					<div class="space-y-2">
						<Label for="memory" class="text-sm font-medium">Memory Allocation (MB)</Label>
						<div class="flex gap-2">
							<Input
								id="memory"
								type="number"
								min="512"
								bind:value={formData.memory}
								disabled={loading}
								class="h-10"
							/>
						</div>
						<p class="text-xs text-muted-foreground">
							Recommended: {formData.mod_loader === 'vanilla' ? '2048' : '4096'} MB
						</p>
					</div>

					<Separator />

					<AdditionalPortsEditor
						bind:ports={formData.additional_ports}
						disabled={loading}
						usedPorts={usedPorts}
						onchange={(ports) => formData.additional_ports = ports}
					/>

					<Separator />

					<div class="space-y-2">
						<Label for="docker_image" class="text-sm font-medium">Docker Image <span class="text-muted-foreground text-xs">(Advanced)</span></Label>
						<Select type="single" value={formData.docker_image} onValueChange={(v: string | undefined) => formData.docker_image = v ?? ''} disabled={loading || loadingVersions}>
							<SelectTrigger id="docker_image">
								<span>{formData.docker_image ? getDockerImageDisplayName(formData.docker_image) : 'Auto-select (Recommended)'}</span>
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="">Auto-select (Recommended)</SelectItem>
								{#each getUniqueDockerImages(dockerImages.filter(img => !img.deprecated)) as image (image.tag)}
									<SelectItem value={image.tag}>
										{getDockerImageDisplayName(image)}
										{#if image.notes}
											<span class="text-xs text-muted-foreground ml-2">({image.notes})</span>
										{/if}
									</SelectItem>
								{/each}
							</SelectContent>
						</Select>
						<p class="text-xs text-muted-foreground">
							Leave as auto-select unless you have specific requirements
						</p>
					</div>

					<Separator />

					<div class="space-y-4">
						<h4 class="text-sm font-semibold">Lifecycle Management</h4>
						
						<div class="flex items-center justify-between p-4 rounded-lg bg-muted/50">
							<div class="space-y-0.5">
								<Label for="start_immediately" class="text-sm font-medium cursor-pointer">Start Immediately</Label>
								<p class="text-xs text-muted-foreground">
									Start the server right after creation
								</p>
							</div>
							<Switch
								id="start_immediately"
								bind:checked={formData.start_immediately}
								disabled={loading}
							/>
						</div>
						
						<div class="flex items-center justify-between p-4 rounded-lg bg-muted/50">
							<div class="space-y-0.5">
								<Label for="detached" class="text-sm font-medium cursor-pointer">Detached Mode</Label>
								<p class="text-xs text-muted-foreground">
									Server continues running when DiscoPanel stops (not available for proxied servers)
								</p>
							</div>
							<Switch
								id="detached"
								bind:checked={formData.detached}
								disabled={loading || useProxyMode}
								onCheckedChange={(checked) => {
									if (checked && useProxyMode) {
										toast.error("Cannot detach proxied servers");
										formData.detached = false;
										return;
									}
									formData.detached = checked;
									// If detaching, disable auto-start
									if (checked) {
										formData.auto_start = false;
									}
								}}
							/>
						</div>

						<div class="flex items-center justify-between p-4 rounded-lg bg-muted/50">
							<div class="space-y-0.5">
								<Label for="auto_start" class="text-sm font-medium cursor-pointer">Auto Start</Label>
								<p class="text-xs text-muted-foreground">
									Automatically start when DiscoPanel starts{formData.detached ? ' (disabled for detached servers)' : ''}
								</p>
							</div>
							<Switch
								id="auto_start"
								bind:checked={formData.auto_start}
								disabled={loading || formData.detached}
								onCheckedChange={(checked) => {
									if (formData.detached) {
										toast.error("Cannot enable auto-start for detached servers");
										formData.auto_start = false;
										return;
									}
									formData.auto_start = checked;
								}}
							/>
						</div>
					</div>
				</CardContent>
			</Card>

			<!-- Docker Overrides - Advanced Configuration -->
			<div class="lg:col-span-2">
				<DockerOverridesEditor
					bind:overrides={formData.docker_overrides}
					disabled={loading}
					onchange={(overrides) => formData.docker_overrides = overrides}
				/>
			</div>
		</div>

		<div class="flex justify-end gap-3 mt-8">
			<Button variant="outline" href="/servers" disabled={loading} size="lg">
				Cancel
			</Button>
			<Button type="submit" disabled={loading || loadingVersions} size="lg" class="min-w-[140px]">
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