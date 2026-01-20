<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Badge } from '$lib/components/ui/badge';
	import { Alert, AlertDescription, AlertTitle } from '$lib/components/ui/alert';
	import { toast } from 'svelte-sonner';
	import { Heart, Download, Search, RefreshCw, ExternalLink, AlertCircle, Settings, Upload, Package, ArrowLeft, Trash2 } from '@lucide/svelte';
	import { create } from '@bufbuild/protobuf';
	import type { IndexedModpack, SearchModpacksRequest, SearchModpacksResponse, GetIndexerStatusResponse } from '$lib/proto/discopanel/v1/modpack_pb';
	import { SearchModpacksRequestSchema } from '$lib/proto/discopanel/v1/modpack_pb';
	import { rpcClient } from '$lib/api/rpc-client';
	import { debounce } from 'lodash-es';
	
	let searchParams = $state<SearchModpacksRequest>(create(SearchModpacksRequestSchema, {
		query: '',
		gameVersion: '',
		modLoader: '',
		indexer: '',
		page: 1,
		pageSize: 20
	}));
	
	let searchResults = $state<SearchModpacksResponse | null>(null);
	let favorites = $state<IndexedModpack[]>([]);
	let uploadedPacks = $state<IndexedModpack[]>([]);
	let loading = $state(false);
	let syncing = $state(false);
	let showFavorites = $state(false);
	let showUploaded = $state(false);
	let indexerStatus = $state<GetIndexerStatusResponse | null>(null);
	let fileInput = $state<HTMLInputElement | null>(null);
	let uploading = $state(false);
	let selectedIndexer = $state('modrinth'); // Default Modrinth since no API key initially
	let indexerName = $derived(selectedIndexer === 'fuego' ? 'CurseForge' : 'Modrinth');

	// Dynamic game versions and mod loaders from API
	let gameVersions = $state<string[]>([]);
	let modLoaders = $state<Array<{ value: string; label: string }>>([
		{ value: '', label: 'All Loaders' }
	]);
	
	onMount(async () => {
		await Promise.all([
			checkIndexerStatus(),
			loadFavorites(),
			loadUploadedPacks(),
			loadMinecraftVersions(),
			loadModLoaders()
		]);
	});
	
	async function loadMinecraftVersions() {
		try {
			const response = await rpcClient.minecraft.getMinecraftVersions({});
			gameVersions = response.versions.map(v => v.id);
		} catch (error) {
			console.error('Failed to load Minecraft versions:', error);
		}
	}
	
	async function loadModLoaders() {
		try {
			const response = await rpcClient.minecraft.getModLoaders({});
			const loaders = response.modloaders || [];
			modLoaders = [
				{ value: '', label: 'All Loaders' },
				...loaders.map((loader) => ({
					value: loader.name,
					label: loader.displayName || loader.name
				}))
			];
		} catch (error) {
			console.error('Failed to load mod loaders:', error);
		}
	}
	
	async function checkIndexerStatus() {
		try {
			const response = await rpcClient.modpack.getIndexerStatus({});
			indexerStatus = response;
		} catch (error) {
			console.error('Failed to check indexer status:', error);
		}
	}
	
	async function searchModpacks(resetPage = true) {
		loading = true;
		try {
			// Reset to page 1 when searching
			if (resetPage) {
				searchParams.page = 1;
			}
			const response = await rpcClient.modpack.searchModpacks({
				...searchParams,
				indexer: selectedIndexer
			});
			searchResults = response;
		} catch (error) {
			toast.error('Failed to search modpacks');
			console.error(error);
		} finally {
			loading = false;
		}
	}
	
	async function _syncModpacks() {
		syncing = true;
		try {
			const result = await rpcClient.modpack.syncModpacks({
				query: searchParams.query || '',
				gameVersion: searchParams.gameVersion || '',
				modLoader: searchParams.modLoader || '',
				indexer: selectedIndexer
			});
			toast.success(`Synced ${result.syncedCount} modpacks from ${indexerName}`);
			// Refresh search results
			await searchModpacks();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : 'Failed to sync modpacks');
			console.error(error);
		} finally {
			syncing = false;
		}
	}

	// Debounce sync to prevent indexer rate limiting the backend
	const syncModpacks = debounce(_syncModpacks, 1000, { leading: true, trailing: false });
	
	async function toggleFavorite(modpack: IndexedModpack) {
		try {
			const result = await rpcClient.modpack.toggleFavorite({ id: modpack.id });

			// Update the modpack in search results
			if (searchResults) {
				searchResults.modpacks = searchResults.modpacks.map(m =>
					m.id === modpack.id ? { ...m, isFavorited: result.isFavorited } : m
				);
			}

			// Update the modpack in uploaded packs
			uploadedPacks = uploadedPacks.map(m =>
				m.id === modpack.id ? { ...m, isFavorited: result.isFavorited } : m
			);

			// Update favorites list immediately
			if (result.isFavorited) {
				// Add to favorites if not already there
				if (!favorites.find(f => f.id === modpack.id)) {
					favorites = [...favorites, { ...modpack, isFavorited: true }];
				}
				toast.success('Added to favorites');
			} else {
				// Remove from favorites
				favorites = favorites.filter(f => f.id !== modpack.id);
				toast.success('Removed from favorites');
			}

			// If viewing favorites, we may need to update the display
			if (showFavorites && !result.isFavorited) {
				// Item was removed from favorites while viewing favorites
				// The reactive displayModpacks will automatically update
			}
		} catch (error) {
			toast.error('Failed to toggle favorite');
			console.error(error);
		}
	}
	
	async function loadFavorites() {
		try {
			const result = await rpcClient.modpack.listFavorites({});
			favorites = result.modpacks;
		} catch (error) {
			toast.error('Failed to load favorites');
			console.error(error);
		}
	}
	
	async function loadUploadedPacks() {
		try {
			// Use the indexer parameter to get only manual uploads
			const result = await rpcClient.modpack.searchModpacks({
				query: '',
				gameVersion: '',
				modLoader: '',
				indexer: 'manual',
				page: 1,
				pageSize: 100
			});
			uploadedPacks = result.modpacks || [];
		} catch (error) {
			console.error('Failed to load uploaded packs:', error);
		}
	}
	
	function formatNumber(num: number): string {
		if (num >= 1000000) {
			return `${(num / 1000000).toFixed(1)}M`;
		} else if (num >= 1000) {
			return `${(num / 1000).toFixed(1)}K`;
		}
		return num.toString();
	}
	
	function parseJsonArray(jsonStr: string): string[] {
		try {
			return JSON.parse(jsonStr);
		} catch {
			return [];
		}
	}
	
	async function handleModpackUpload(event: Event) {
		const input = event.target as HTMLInputElement;
		const files = input.files;
		if (!files || files.length === 0) return;

		const file = files[0];
		if (!file.name.endsWith('.zip')) {
			toast.error('Please select a valid modpack ZIP file');
			return;
		}

		uploading = true;
		try {
			const arrayBuffer = await file.arrayBuffer();
			const content = new Uint8Array(arrayBuffer);

			const result = await rpcClient.modpack.uploadModpack({
				filename: file.name,
				content: content,
				name: file.name.replace('.zip', ''),
				description: ''
			});

			toast.success(`Modpack "${result.modpack?.name}" uploaded successfully`);

			// Refresh the modpack list and uploaded packs
			await Promise.all([
				searchModpacks(),
				loadUploadedPacks()
			]);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : 'Failed to upload modpack');
		} finally {
			uploading = false;
			input.value = '';
		}
	}

	async function deleteModpack(modpack: IndexedModpack) {
		if (!confirm(`Are you sure you want to delete "${modpack.name}"? This action cannot be undone.`)) {
			return;
		}

		try {
			await rpcClient.modpack.deleteModpack({ id: modpack.id });

			toast.success(`Modpack "${modpack.name}" deleted successfully`);

			// Remove from local state
			uploadedPacks = uploadedPacks.filter(m => m.id !== modpack.id);
			favorites = favorites.filter(m => m.id !== modpack.id);

			// Refresh search results if showing
			if (!showFavorites && !showUploaded) {
				await searchModpacks();
			}
		} catch (error) {
			toast.error(error instanceof Error ? error.message : 'Failed to delete modpack');
		}
	}
	
	onMount(async () => {
		await Promise.all([
			searchModpacks(),
			loadFavorites(),
			loadUploadedPacks()
		]);

		if (searchResults && searchResults.total === 0 && !searchParams.query) {
			syncModpacks();
		}
	});

	// Computed display list with uploaded packs first
	let displayModpacks = $derived(
		showFavorites ? favorites :
		showUploaded ? uploadedPacks :
		(() => {
			const results = searchResults?.modpacks || [];
			const uploaded: IndexedModpack[] = [];
			const indexed: IndexedModpack[] = [];
			results.forEach((m) => (m.indexer === 'manual' ? uploaded : indexed).push(m))
			return [...uploaded, ...indexed];
		})()
	);
</script>

<div class="flex-1 space-y-8 h-full p-8 pt-6 bg-linear-to-br from-background to-muted/10">
	<div class="flex items-center justify-between pb-6 border-b-2 border-border/50">
		<div class="flex items-center gap-4">
			<div class="h-16 w-16 rounded-2xl bg-linear-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg">
				<Package class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1">
				<h2 class="text-4xl font-bold tracking-tight bg-linear-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">Modpacks</h2>
				<p class="text-base text-muted-foreground">Browse and install modpacks for your servers</p>
			</div>
		</div>
		<div class="flex items-center gap-2">
			<Button
				variant={showUploaded ? "outline" : "default"}
				onclick={() => {
					showUploaded = !showUploaded;
					if (showUploaded) showFavorites = false;
				}}
				class="shadow-md hover:shadow-lg transition-all hover:scale-[1.02]"
			>
				{#if showUploaded}
					<ArrowLeft class="h-5 w-5 mr-2" />
					Back to all
				{:else}
					<Upload class="h-5 w-5 mr-2" />
					Uploaded ({uploadedPacks.length})
				{/if}
			</Button>
			<Button
				variant={showFavorites ? "outline" : "default"}
				onclick={() => {
					showFavorites = !showFavorites;
					if (showFavorites) showUploaded = false;
				}}
				class="shadow-md hover:shadow-lg transition-all hover:scale-[1.02]"
			>
				{#if showFavorites}
					<ArrowLeft class="h-5 w-5 mr-2" />
					Back to all
				{:else}
					<Heart class="h-5 w-5 mr-2" />
					Favorites ({favorites.length})
				{/if}
			</Button>
		</div>
	</div>
	
	{#if selectedIndexer === 'fuego' && indexerStatus && !indexerStatus.indexersAvailable['fuego']}
		<Alert>
			<AlertCircle class="h-4 w-4" />
			<AlertTitle>CurseForge API Key Required</AlertTitle>
			<AlertDescription>
				<div class="space-y-2">
					<p>To sync modpacks from CurseForge, you need to configure a CurseForge API key in the settings.</p>
					<div class="flex items-center gap-2 mt-2">
						<Button size="sm" href="/settings">
							<Settings class="h-4 w-4 mr-2" />
							Configure in Settings
						</Button>
					</div>
				</div>
			</AlertDescription>
		</Alert>
	{/if}
	
	{#if !showFavorites && !showUploaded}
		<div class="flex flex-col gap-4">
			<div class="flex gap-2">
				<Input
					placeholder="Search modpacks..."
					bind:value={searchParams.query}
					onkeydown={(e) => e.key === 'Enter' && searchModpacks()}
					class="flex-1"
				/>
				<Select type="single" value={searchParams.gameVersion} onValueChange={(v: string | undefined) => searchParams.gameVersion = v || ''} disabled={loading}>
					<SelectTrigger class="w-45">
						<span>{searchParams.gameVersion || 'All Versions'}</span>
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="">All Versions</SelectItem>
						{#each gameVersions as version}
							<SelectItem value={version}>{version}</SelectItem>
						{/each}
					</SelectContent>
				</Select>
				<Select type="single" value={searchParams.modLoader} onValueChange={(v: string | undefined) => searchParams.modLoader = v || ''} disabled={loading}>
					<SelectTrigger class="w-45">
						<span>{searchParams.modLoader ? modLoaders.find(l => l.value === searchParams.modLoader)?.label : 'All Loaders'}</span>
					</SelectTrigger>
					<SelectContent>
						{#each modLoaders as loader}
							<SelectItem value={loader.value}>{loader.label}</SelectItem>
						{/each}
					</SelectContent>
				</Select>
				<Select type="single" value={selectedIndexer} onValueChange={(v: string | undefined) => {
					selectedIndexer = v || 'modrinth';
					syncModpacks();
				}} disabled={syncing}>
					<SelectTrigger class="w-45">
						<span>{indexerName}</span>
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="modrinth">Modrinth</SelectItem>
						<SelectItem value="fuego">CurseForge</SelectItem>
					</SelectContent>
				</Select>
				<Button onclick={() => searchModpacks(true)} disabled={loading} class="bg-linear-to-r from-primary to-primary/80 hover:from-primary/90 hover:to-primary/70 shadow-md hover:shadow-lg transition-all hover:scale-[1.02]">
					<Search class="h-5 w-5 mr-2" />
					Search
				</Button>
				<Button onclick={syncModpacks} disabled={syncing || (selectedIndexer === 'fuego' && indexerStatus && !indexerStatus.indexersAvailable['fuego'])} variant="outline" class="border-2 shadow-sm hover:shadow-md transition-all hover:scale-[1.02]">
					<RefreshCw class={`h-5 w-5 mr-2 ${syncing ? 'animate-spin' : ''}`} />
					Sync {indexerName}
				</Button>
				<Button onclick={() => fileInput?.click()} disabled={uploading} variant="outline" class="border-2 shadow-sm hover:shadow-md transition-all hover:scale-[1.02]">
					<Upload class="h-5 w-5 mr-2" />
					Upload Modpack
				</Button>
				<input
					bind:this={fileInput}
					type="file"
					accept=".zip"
					onchange={handleModpackUpload}
					class="hidden"
				/>
			</div>
			{#if searchResults?.total === 0 && !loading}
				<p class="text-sm text-muted-foreground">
					No modpacks found locally. Click "Sync" to fetch modpacks from Indexers.
				</p>
			{/if}
		</div>
	{/if}
	
	<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
		{#each displayModpacks as modpack}
			<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-linear-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-linear-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="relative">
					<div class="flex items-start gap-4">
						{#if modpack.logoUrl}
							<img
								src={modpack.logoUrl} 
								alt={modpack.name}
								class="w-16 h-16 rounded-md object-cover"
							/>
						{/if}
						<div class="flex-1 min-w-0">
							<CardTitle class="line-clamp-1 text-xl font-semibold">{modpack.name}</CardTitle>
							<div class="flex items-center gap-2 mt-1">
								<Badge variant="secondary" class="text-xs font-semibold">
									{modpack.indexer === 'manual' ? 'Manual Upload' : modpack.indexer}
								</Badge>
								{#if modpack.indexer === 'manual'}
									<Badge variant="outline" class="text-xs">
										<Upload class="h-3 w-3 mr-1" />
										Uploaded
									</Badge>
								{/if}
								<span class="text-xs text-muted-foreground">
									<Download class="h-3 w-3 inline mr-1" />
									{formatNumber(modpack.downloadCount)}
								</span>
							</div>
						</div>
						<Button
							size="icon"
							variant={modpack.isFavorited ? "default" : "outline"}
							onclick={() => toggleFavorite(modpack)}
							class="hover:scale-110 transition-transform"
						>
							<Heart class={`h-4 w-4 ${modpack.isFavorited ? 'fill-current' : ''}`} />
						</Button>
					</div>
				</CardHeader>
				<CardContent class="relative">
					<CardDescription class="line-clamp-2 mb-4">
						{modpack.summary}
					</CardDescription>
					
					<div class="space-y-2">
						{#if parseJsonArray(modpack.modLoaders).length > 0}
							<div class="flex flex-wrap gap-1">
								{#each parseJsonArray(modpack.modLoaders) as loader}
									<Badge variant="outline" class="text-xs">{loader}</Badge>
								{/each}
							</div>
						{/if}
						
						{#if parseJsonArray(modpack.gameVersions).length > 0}
							<div class="text-xs text-muted-foreground">
								MC: {parseJsonArray(modpack.gameVersions).slice(0, 3).join(', ')}
								{#if parseJsonArray(modpack.gameVersions).length > 3}
									+{parseJsonArray(modpack.gameVersions).length - 3} more
								{/if}
							</div>
						{/if}
					</div>
					
					<div class="flex items-center justify-between mt-4 gap-2">
						<div class="flex items-center gap-2">
							{#if modpack.websiteUrl}
								<a href={modpack.websiteUrl} target="_blank" rel="noopener noreferrer">
									<Button variant="outline" size="sm">
										<ExternalLink class="h-3 w-3 mr-1" />
										View
									</Button>
								</a>
							{/if}
							{#if modpack.indexer === 'manual'}
								<Button
									variant="outline"
									size="sm"
									onclick={() => deleteModpack(modpack)}
									class="text-destructive hover:bg-destructive hover:text-destructive-foreground"
								>
									<Trash2 class="h-3 w-3 mr-1" />
									Delete
								</Button>
							{/if}
						</div>
						<Button size="sm" onclick={() => goto(`/servers/new?modpack=${modpack.id}`)} class="font-semibold shadow-sm hover:shadow-md transition-all hover:scale-[1.02]">
							Use in Server
						</Button>
					</div>
				</CardContent>
			</Card>
		{/each}
	</div>
	
	{#if !showFavorites && !showUploaded && searchResults && searchResults.total > searchResults.pageSize}
		<div class="flex items-center justify-center gap-2 mt-6">
			<Button
				variant="outline"
				disabled={(searchParams.page || 1) === 1}
				onclick={() => {
					searchParams.page = Math.max(1, (searchParams.page || 1) - 1);
					searchModpacks(false);
				}}
			>
				Previous
			</Button>
			<span class="text-sm text-muted-foreground">
				Page {searchParams.page} of {Math.ceil(searchResults.total / searchResults.pageSize)}
			</span>
			<Button
				variant="outline"
				disabled={(searchParams.page || 1) >= Math.ceil(searchResults.total / searchResults.pageSize)}
				onclick={() => {
					searchParams.page = (searchParams.page || 1) + 1;
					searchModpacks(false);
				}}
			>
				Next
			</Button>
		</div>
	{/if}
	
	{#if displayModpacks.length === 0}
		<div class="text-center py-12">
			<p class="text-muted-foreground">
				{#if showFavorites}
					No favorite modpacks yet. Browse the modpacks list and click the heart icon to add favorites.
				{:else if loading}
					Loading modpacks...
				{:else if syncing}
					Syncing modpacks...
				{:else}
					{#if searchParams.query}
						No modpacks found matching your search.
					{:else}
						No modpacks found.
					{/if}
				{/if}
			</p>
		</div>
	{/if}
</div>