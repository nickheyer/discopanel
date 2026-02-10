<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Label } from '$lib/components/ui/label';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { Separator } from '$lib/components/ui/separator';
	import { rpcClient } from '$lib/api/rpc-client';
	import { isAdmin } from '$lib/stores/auth';
	import { toast } from 'svelte-sonner';
	import { ArrowLeft, Loader2, Package, Settings, HardDrive, AlertCircle, Check } from '@lucide/svelte';
	import { create } from '@bufbuild/protobuf';
	import type { CreateServerRequest } from '$lib/proto/discopanel/v1/server_pb';
	import { CreateServerRequestSchema } from '$lib/proto/discopanel/v1/server_pb';
	import { ModLoader, type ProxyListener } from '$lib/proto/discopanel/v1/common_pb';
	import type { ModLoaderInfo, DockerImage } from '$lib/proto/discopanel/v1/minecraft_pb';
	import type { IndexedModpack, Version } from '$lib/proto/discopanel/v1/modpack_pb';
	import { Badge } from '$lib/components/ui/badge';
	import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import AdditionalPortsEditor from '$lib/components/additional-ports-editor.svelte';
	import DockerOverridesEditor from '$lib/components/docker-overrides-editor.svelte';
	import { getUniqueDockerImages, getDockerImageDisplayName, isValidImageReferenceFormat, debounce } from '$lib/utils';

	let loading = $state(false);
	let loadingVersions = $state(true);
	let minecraftVersions = $state<string[]>([]);
	let modLoaders = $state<ModLoaderInfo[]>([]);
	let dockerImages = $state<DockerImage[]>([]);
	let latestVersion = $state('');
	let proxyEnabled = $state(false);
	let proxyBaseURL = $state('');
	let proxyListeners = $state<ProxyListener[]>([]);
	let usedPorts = $state<Record<number, boolean>>({});
	let portError = $state('');
	let dockerImageError = $state('');
	let validatingDockerImage = $state(false);
	let dockerImageValid = $state<boolean | null>(null); // null = not validated, true = valid, false = invalid

	// Docker image mode tracking (explicit, no string inference)
	type DockerImageMode = 'preset' | 'custom';
	let dockerImageMode = $state<DockerImageMode>('preset');
	let selectedPresetTag = $state('');          // e.g., 'java21'
	let customImageValue = $state('');           // e.g., 'my-registry/image:tag'
	let userEditedCustomImage = $state(false); // Track if user has manually edited

	let useProxyMode = $state(false); // Track connection mode separately
	
	// Modpack selection
	let showModpackDialog = $state(false);
	let selectedModpack = $state<IndexedModpack | null>(null);
	let favoriteModpacks = $state<IndexedModpack[]>([]);
	let modpackVersions = $state<Version[]>([]);
	let selectedVersionId = $state<string>('');
	let loadingModpackVersions = $state(false);

	let formData = $state<CreateServerRequest>(create(CreateServerRequestSchema, {
		name: '',
		description: '',
		modLoader: ModLoader.UNSPECIFIED,
		mcVersion: '',
		port: 25565,
		maxPlayers: 20,
		memory: 2048,
		dockerImage: '',
		autoStart: false,
		detached: false,
		startImmediately: false,
		proxyHostname: '',
		proxyListenerId: '',
		useBaseUrl: false,
		additionalPorts: [],
		dockerOverrides: undefined,
		modpackId: '',
		modpackVersionId: ''
	}));

	onMount(async () => {
		try {
			const [versionsData, loadersData, imagesData, proxyStatus, portData, listeners] = await Promise.all([
				rpcClient.minecraft.getMinecraftVersions({}),
				rpcClient.minecraft.getModLoaders({}),
				rpcClient.minecraft.getDockerImages({}),
				rpcClient.proxy.getProxyStatus({}),
				rpcClient.server.getNextAvailablePort({}),
				rpcClient.proxy.getProxyListeners({})
			]);

			minecraftVersions = versionsData.versions.map(v => v.id);
			latestVersion = versionsData.latest;
			modLoaders = loadersData.modloaders;
			dockerImages = imagesData.images;
			proxyEnabled = proxyStatus.enabled;
			proxyBaseURL = proxyStatus.baseUrl || '';
			proxyListeners = listeners.listeners
				.map(l => l.listener)
				.filter((l): l is ProxyListener => l !== undefined && l.enabled);

			// Set default listener if available
			const defaultListener = proxyListeners.find(l => l?.isDefault);
			if (defaultListener) {
				formData.proxyListenerId = defaultListener.id;
			} else if (proxyListeners.length > 0) {
				formData.proxyListenerId = proxyListeners[0]?.id || '';
			}
			
			// Set the default port to the next available port
			formData.port = portData.port;
			usedPorts = Object.fromEntries(
				portData.usedPorts?.map(p => [p.port, p.inUse]) || []
			);

			if (!formData.mcVersion && latestVersion) {
				formData.mcVersion = latestVersion;
			}
			
			// Load favorite modpacks
			await loadFavoriteModpacks();
			
			// Check if modpack was passed in URL
			const urlParams = new URLSearchParams(window.location.search);
			const modpackId = urlParams.get('modpack');
			if (modpackId) {
				// Load and select the modpack
				try {
					const response = await rpcClient.modpack.getModpack({ id: modpackId });
					if (response.modpack) {
						await selectModpack(response.modpack);
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
			const result = await rpcClient.modpack.listFavorites({});
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
			const data = await rpcClient.modpack.getModpackVersions({
				id: modpackId,
				gameVersion: '',
				modLoader: ''
			});
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
			const response = await rpcClient.modpack.getModpackConfig({ id: modpack.id });

			// Config is a map of key-value pairs (response.config) - currently unused
			formData.name = modpack.name || '';
			formData.description = modpack.summary || '';
			// Convert mod loader string to enum - this needs mapping
			formData.modLoader = 0; // Will be set based on modpack data
			formData.mcVersion = modpack.mcVersion || '';
			formData.memory = modpack.recommendedRam || 2048;

			// Initialize docker image mode if modpack specifies one
			if (modpack.dockerImage) {
				initializeDockerImageMode(modpack.dockerImage);
			} else {
				initializeDockerImageMode('');
			}

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
		formData.modLoader = 0; // VANILLA
		formData.mcVersion = latestVersion || '';
		formData.dockerImage = '';
		formData.memory = 2048;
		initializeDockerImageMode('');
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
			const portData = await rpcClient.server.getNextAvailablePort({});
			formData.port = portData.port;
			usedPorts = Object.fromEntries(
				portData.usedPorts?.map(p => [p.port, p.inUse]) || []
			);
			portError = '';
		} catch (error) {
			console.error('Failed to get available port:', error);
		}
	}

	async function validateDockerImage(image: string) {
		dockerImageError = '';

		// Allow empty for auto-select
		if (!image || image.trim() === '') {
			dockerImageValid = true;
			return;
		}

		// Basic format check
		if (!isValidImageReferenceFormat(image)) {
			dockerImageError = 'Invalid image format. Example: itzg/minecraft-server:java21';
			dockerImageValid = false;
			return;
		}

		// Async validation with backend
		validatingDockerImage = true;
		try {
			const result = await rpcClient.minecraft.validateDockerImage({ image });
			if (!result.valid) {
				dockerImageError = result.error || 'Image not found';
				dockerImageValid = false;
			} else {
				dockerImageValid = true;
				dockerImageError = '';
			}
		} catch (error) {
			dockerImageError = `Failed to validate image: ${error instanceof Error ? error.message : 'Unknown error'}`;
			dockerImageValid = false;
		} finally {
			validatingDockerImage = false;
		}
	}

	// Debounced validation
	const debouncedValidateDockerImage = debounce(validateDockerImage, 700);

	// Initialize docker image mode ONCE
	function initializeDockerImageMode(dockerImage: string) {
		if (!dockerImage) {
			dockerImageMode = 'preset';
			selectedPresetTag = '';
			customImageValue = '';
		} else if (dockerImage.startsWith('itzg/minecraft-server:')) {
			dockerImageMode = 'preset';
			selectedPresetTag = dockerImage.substring('itzg/minecraft-server:'.length);
			customImageValue = '';
		} else {
			dockerImageMode = 'custom';
			selectedPresetTag = '';
			customImageValue = dockerImage;
		}
		dockerImageValid = true;
		dockerImageError = '';
	}

	// Derive formData.dockerImage from explicit mode
	$effect(() => {
		if (dockerImageMode === 'custom') {
			formData.dockerImage = customImageValue;
		} else if (selectedPresetTag) {
			formData.dockerImage = 'itzg/minecraft-server:' + selectedPresetTag;
		} else {
			formData.dockerImage = ''; // Auto-select
		}
	});

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

		// Docker image is already set in formData.dockerImage via derived state
		const finalDockerImage = formData.dockerImage;

		// Validate custom Docker image if provided
		if (dockerImageMode === 'custom' && finalDockerImage) {
			// If we haven't validated yet or the validation failed, validate now
			if (dockerImageValid !== true) {
				await validateDockerImage(finalDockerImage);
				if (dockerImageValid === false) {
					toast.error('Invalid Docker image');
					return;
				}
			}
		}

		loading = true;
		try {
			// Add modpack ID and version to the request if selected
			// For Modrinth, we need to send versionNumber instead of ID for better compatibility
			const selectedVersion = modpackVersions.find(v => v.id === selectedVersionId);
			const versionToSend = selectedModpack?.indexer === 'modrinth' && selectedVersion?.versionNumber
				? selectedVersion.versionNumber
				: selectedVersionId;

			const createRequest = {
				...formData,
				dockerImage: finalDockerImage,
				modpackId: selectedModpack?.id || '',
				modpackVersionId: versionToSend || '',
				// When using proxy with hostname, set port to 0 to indicate proxy usage
				port: useProxyMode ? 0 : formData.port
			};

			// Create the server
			const response = await rpcClient.server.createServer(createRequest);
			toast.success(`Server "${response.server?.name}" created successfully!`);
			goto(`/servers/${response.server?.id}`);
		} catch (error) {
			toast.error(`Failed to create server: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			loading = false;
		}
	}

</script>

<div class="h-full overflow-y-auto bg-linear-to-br from-background to-muted/10">
	<div class="space-y-8 p-4 sm:p-6 lg:p-8 pt-4 sm:pt-6">
	<div class="flex items-center gap-4 pb-6 border-b-2 border-border/50">
		<Button variant="ghost" size="icon" href="/servers" class="shrink-0 h-12 w-12 rounded-xl hover:bg-muted">
			<ArrowLeft class="h-5 w-5" />
		</Button>
		<div class="flex items-center gap-4">
			<div class="h-16 w-16 rounded-2xl bg-linear-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg">
				<Package class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1">
				<h2 class="text-4xl font-bold tracking-tight bg-linear-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">Create New Server</h2>
				<p class="text-base text-muted-foreground">Set up a new Minecraft server instance with your preferred configuration</p>
			</div>
		</div>
	</div>

	<form onsubmit={handleSubmit}>
		<div class="grid gap-6 lg:grid-cols-2">
			<Card class="border-2 hover:border-primary/30 transition-colors shadow-xl bg-linear-to-br from-card to-card/90">
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
							<Card class="border-2 border-primary/30 bg-linear-to-br from-primary/10 to-primary/5 shadow-lg">
								<CardContent class="p-5">
									<div class="flex items-start gap-3">
										{#if selectedModpack.logoUrl}
											<img
												src={selectedModpack.logoUrl}
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
												{#if parseJsonArray(selectedModpack.gameVersions).length > 0}
													<Badge variant="secondary" class="text-xs">
														MC {parseJsonArray(selectedModpack.gameVersions)[0]}
													</Badge>
												{/if}
												{#if parseJsonArray(selectedModpack.modLoaders).length > 0}
													<Badge variant="secondary" class="text-xs">
														{parseJsonArray(selectedModpack.modLoaders)[0]}
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
																	? modpackVersions.find(v => v.id === selectedVersionId)?.displayName || 'Latest'
																	: 'Latest version'}
															</span>
														</SelectTrigger>
														<SelectContent>
															<SelectItem value="">
																Latest version
															</SelectItem>
															{#each modpackVersions as version (version.id)}
																<SelectItem value={version.id}>
																	{version.displayName}
																	{#if version.releaseType}
																		<Badge variant={version.releaseType === 'release' ? 'default' : version.releaseType === 'beta' ? 'secondary' : 'outline'} class="ml-2 text-xs">
																			{version.releaseType}
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
						<Label for="mcVersion" class="text-sm font-medium">Minecraft Version</Label>
						{#if loadingVersions}
							<div class="flex items-center justify-center p-4">
								<Loader2 class="h-4 w-4 animate-spin" />
							</div>
						{:else}
							<Select type="single" value={formData.mcVersion} onValueChange={(v: string | undefined) => formData.mcVersion = v ?? ''} disabled={loading}>
								<SelectTrigger id="mcVersion">
									<span>{formData.mcVersion || 'Select a version'}</span>
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
						<Label for="modLoader" class="text-sm font-medium">Mod Loader</Label>
						<Select type="single" value={formData.modLoader.toString()} onValueChange={(v: string | undefined) => {
							// Convert string name to enum value
							const loaderName = v ?? 'VANILLA';
							formData.modLoader = ModLoader[loaderName.toUpperCase() as keyof typeof ModLoader] ?? ModLoader.VANILLA;
						}} disabled={loading || !!selectedModpack}>
							<SelectTrigger id="modLoader">
								<span>{modLoaders.find(l => l.name.toLowerCase() === Object.keys(ModLoader).find(k => ModLoader[k as keyof typeof ModLoader] === formData.modLoader)?.toLowerCase())?.displayName || 'Select a mod loader'}</span>
							</SelectTrigger>
							<SelectContent>
								{#each modLoaders as loader (loader.name)}
									<SelectItem value={loader.name}>
										{loader.displayName}
									</SelectItem>
								{/each}
							</SelectContent>
						</Select>
						{#if selectedModpack}
							<p class="text-sm text-muted-foreground">
								Mod loader auto-determined from modpack
							</p>
						{:else if formData.modLoader === ModLoader.VANILLA}
							<p class="text-sm text-muted-foreground">
								No mod support - vanilla Minecraft server
							</p>
						{:else if modLoaders.find(l => l.name.toLowerCase() === Object.keys(ModLoader).find(k => ModLoader[k as keyof typeof ModLoader] === formData.modLoader)?.toLowerCase())?.supportsMods}
							<p class="text-sm text-muted-foreground">
								This mod loader supports mods
							</p>
						{/if}
					</div>
				</CardContent>
			</Card>

			<Card class="border-2 hover:border-primary/30 transition-colors shadow-xl bg-linear-to-br from-card to-card/90">
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
											formData.proxyHostname = '';
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
											if (!formData.proxyHostname) {
												formData.proxyHostname = formData.name.toLowerCase().replace(/\s+/g, '-') || 'minecraft-server';
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
												value={formData.proxyListenerId}
												onValueChange={(v) => formData.proxyListenerId = v || ''}
												disabled={loading}
											>
												<SelectTrigger id="proxy_listener">
													<span>
														{proxyListeners.find(l => l.id === formData.proxyListenerId)?.name || 'Select a listener'}
													</span>
												</SelectTrigger>
												<SelectContent>
													{#each proxyListeners as listener (listener.id)}
														<SelectItem value={listener.id}>
															{listener.name} (Port {listener.port})
															{#if listener.isDefault}
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
											bind:value={formData.proxyHostname}
											disabled={loading}
											class="h-10"
										/>
										
										<!-- Base URL Checkbox -->
										{#if proxyBaseURL}
											<div class="flex items-center gap-2">
												<input
													type="checkbox"
													id="use_base_url"
													bind:checked={formData.useBaseUrl}
													class="h-4 w-4"
												/>
												<Label for="use_base_url" class="text-sm font-medium">
													Append base domain ({proxyBaseURL})
												</Label>
											</div>
										{/if}
										
										<p class="text-xs text-muted-foreground">
											{#if formData.useBaseUrl && proxyBaseURL}
												Players will connect using: {formData.proxyHostname}.{proxyBaseURL}
											{:else}
												Players will connect using: {formData.proxyHostname}
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
							bind:value={formData.maxPlayers}
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
							Recommended: {formData.modLoader === ModLoader.VANILLA ? '2048' : '4096'} MB
						</p>
					</div>

					<Separator />

					<AdditionalPortsEditor
						bind:ports={formData.additionalPorts}
						disabled={loading}
						usedPorts={usedPorts}
						onchange={(ports) => formData.additionalPorts = ports}
					/>

					<Separator />


				<div class="space-y-2">
					<Label for="docker_image" class="text-sm font-medium">Docker Image <span class="text-muted-foreground text-xs">(Advanced)</span></Label>
					<Select type="single" value={dockerImageMode === 'custom' ? '__custom__' : selectedPresetTag} onValueChange={(value: string | undefined) => {
					const newValue = value || '';
					if (newValue === '__custom__') {
						dockerImageMode = 'custom';
						selectedPresetTag = '';
						userEditedCustomImage = false;
						dockerImageValid = null;
						if (customImageValue) {
							debouncedValidateDockerImage(customImageValue);
						}
					} else {
						dockerImageMode = 'preset';
						selectedPresetTag = newValue;
						customImageValue = '';
						userEditedCustomImage = false;
						dockerImageValid = true;
						dockerImageError = '';
					}
				}} disabled={loading || loadingVersions}>
						<SelectTrigger id="docker_image">
							<span>
								{#if dockerImageMode === 'custom'}
									Custom Image
								{:else if selectedPresetTag}
									{getDockerImageDisplayName(selectedPresetTag, dockerImages)}
									<span class="text-xs text-muted-foreground ml-1">
										(itzg/minecraft-server:{selectedPresetTag})
									</span>
								{:else}
									Auto-select (Recommended)
								{/if}
							</span>
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="">Auto-select (Recommended)</SelectItem>
							<SelectItem value="__custom__">Custom Image...</SelectItem>
							{#if getUniqueDockerImages(dockerImages).length > 0}
								<div class="px-2 py-1.5 text-xs font-semibold text-muted-foreground">
									Preset Images
								</div>
							{/if}
							{#each getUniqueDockerImages(dockerImages) as image (image.tag)}
								<SelectItem value={image.tag}>
									{getDockerImageDisplayName(image)}
									<span class="text-xs text-muted-foreground ml-1">
										(itzg/minecraft-server:{image.tag})
									</span>
								</SelectItem>
							{/each}
						</SelectContent>
					</Select>

					{#if dockerImageMode === 'custom'}
						<div class="mt-2 space-y-2">
							<Input
								id="custom_docker_image"
								type="text"
								value={customImageValue}
								placeholder="e.g., itzg/minecraft-server:java21 or my-registry/image:tag"
								oninput={(e) => {
									customImageValue = e.currentTarget.value;
									userEditedCustomImage = true;
									dockerImageValid = null;
									dockerImageError = '';
									if (customImageValue) {
										debouncedValidateDockerImage(customImageValue);
									}
								}}
								class={dockerImageValid === false ? 'border-destructive' : ''}
								disabled={loading}
							/>
							{#if validatingDockerImage}
								<div class="flex items-center gap-2 text-xs text-muted-foreground">
									<Loader2 class="h-3 w-3 animate-spin" />
									Validating image...
								</div>
							{:else if dockerImageError}
								<div class="flex items-center gap-2 text-destructive text-xs">
									<AlertCircle class="h-3 w-3" />
									{dockerImageError}
								</div>
							{:else if dockerImageValid === true && userEditedCustomImage}
								<div class="flex items-center gap-2 text-green-600 text-xs">
									<Check class="h-3 w-3" />
									Image validated
								</div>
							{/if}
						</div>
					{:else}
						<p class="text-xs text-muted-foreground">
							{#if selectedPresetTag}
								Full reference: itzg/minecraft-server:{selectedPresetTag}
							{:else}
								DiscoPanel will automatically select the best Java version for your Minecraft version
							{/if}
						</p>
					{/if}
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
								bind:checked={formData.startImmediately}
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
										formData.autoStart = false;
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
								bind:checked={formData.autoStart}
								disabled={loading || formData.detached}
								onCheckedChange={(checked) => {
									if (formData.detached) {
										toast.error("Cannot enable auto-start for detached servers");
										formData.autoStart = false;
										return;
									}
									formData.autoStart = checked;
								}}
							/>
						</div>
					</div>
				</CardContent>
			</Card>

			<!-- Docker Overrides - Advanced Configuration -->
			<div class="lg:col-span-2">
				<DockerOverridesEditor
					bind:overrides={formData.dockerOverrides}
					disabled={loading}
					onchange={(overrides) => formData.dockerOverrides = overrides}
					isAdmin={$isAdmin}
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
								{#if modpack.logoUrl}
									<img 
										src={modpack.logoUrl} 
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
										{#if parseJsonArray(modpack.gameVersions).length > 0}
											<span class="text-xs text-muted-foreground">
												MC: {parseJsonArray(modpack.gameVersions)[0]}
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