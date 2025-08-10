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
	import { Heart, Download, Search, RefreshCw, ExternalLink, AlertCircle, Settings, Upload, Package, ArrowLeft } from '@lucide/svelte';
	import type { IndexedModpack, ModpackSearchParams, ModpackSearchResponse } from '$lib/api/types';
	
	let searchParams = $state<ModpackSearchParams>({
		q: '',
		gameVersion: '',
		modLoader: '',
		page: 1
	});
	
	let searchResults = $state<ModpackSearchResponse | null>(null);
	let favorites = $state<IndexedModpack[]>([]);
	let uploadedPacks = $state<IndexedModpack[]>([]);
	let loading = $state(false);
	let syncing = $state(false);
	let showFavorites = $state(false);
	let showUploaded = $state(false);
	let indexerStatus = $state<any>(null);
	let fileInput = $state<HTMLInputElement | null>(null);
	let uploading = $state(false);
	
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
			const response = await fetch('/api/v1/minecraft/versions');
			if (response.ok) {
				const data = await response.json();
				gameVersions = data.versions || [];
			}
		} catch (error) {
			console.error('Failed to load Minecraft versions:', error);
		}
	}
	
	async function loadModLoaders() {
		try {
			const response = await fetch('/api/v1/minecraft/modloaders');
			if (response.ok) {
				const data = await response.json();
				const loaders = data.modloaders || [];
				modLoaders = [
					{ value: '', label: 'All Loaders' },
					...loaders.map((loader: any) => ({
						value: loader.Name,
						label: loader.DisplayName || loader.Name
					}))
				];
			}
		} catch (error) {
			console.error('Failed to load mod loaders:', error);
		}
	}
	
	async function checkIndexerStatus() {
		try {
			const response = await fetch('/api/v1/modpacks/status');
			if (response.ok) {
				indexerStatus = await response.json();
			}
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
			
			const params = new URLSearchParams();
			if (searchParams.q) params.append('q', searchParams.q);
			if (searchParams.gameVersion) params.append('gameVersion', searchParams.gameVersion);
			if (searchParams.modLoader) params.append('modLoader', searchParams.modLoader);
			params.append('page', searchParams.page?.toString() || '1');
			
			const response = await fetch(`/api/v1/modpacks?${params}`);
			if (!response.ok) throw new Error('Failed to search modpacks');
			
			searchResults = await response.json();
		} catch (error) {
			toast.error('Failed to search modpacks');
			console.error(error);
		} finally {
			loading = false;
		}
	}
	
	async function syncModpacks() {
		syncing = true;
		try {
			const response = await fetch('/api/v1/modpacks/sync', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					query: searchParams.q || '',
					gameVersion: searchParams.gameVersion || '',
					modLoader: searchParams.modLoader || '',
					indexer: 'fuego'
				})
			});
			
			if (!response.ok) {
				const error = await response.json();
				throw new Error(error.error || 'Failed to sync modpacks');
			}
			
			const result = await response.json();
			toast.success(`Synced ${result.synced} modpacks`);
			
			// Refresh search results
			await searchModpacks();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : 'Failed to sync modpacks');
			console.error(error);
		} finally {
			syncing = false;
		}
	}
	
	async function toggleFavorite(modpack: IndexedModpack) {
		try {
			const response = await fetch(`/api/v1/modpacks/${modpack.id}/favorite`, {
				method: 'POST'
			});
			
			if (!response.ok) throw new Error('Failed to toggle favorite');
			
			const result = await response.json();
			
			// Update the modpack in search results
			if (searchResults) {
				searchResults.modpacks = searchResults.modpacks.map(m => 
					m.id === modpack.id ? { ...m, is_favorited: result.is_favorited } : m
				);
			}
			
			// Update the modpack in uploaded packs
			uploadedPacks = uploadedPacks.map(m => 
				m.id === modpack.id ? { ...m, is_favorited: result.is_favorited } : m
			);
			
			// Update favorites list immediately
			if (result.is_favorited) {
				// Add to favorites if not already there
				if (!favorites.find(f => f.id === modpack.id)) {
					favorites = [...favorites, { ...modpack, is_favorited: true }];
				}
				toast.success('Added to favorites');
			} else {
				// Remove from favorites
				favorites = favorites.filter(f => f.id !== modpack.id);
				toast.success('Removed from favorites');
			}
			
			// If viewing favorites, we may need to update the display
			if (showFavorites && !result.is_favorited) {
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
			const response = await fetch('/api/v1/modpacks/favorites');
			if (!response.ok) throw new Error('Failed to load favorites');
			
			const result = await response.json();
			favorites = result.modpacks;
		} catch (error) {
			toast.error('Failed to load favorites');
			console.error(error);
		}
	}
	
	async function loadUploadedPacks() {
		try {
			// Use the indexer parameter to get only manual uploads
			const response = await fetch('/api/v1/modpacks?indexer=manual');
			if (!response.ok) throw new Error('Failed to load uploaded packs');
			
			const result = await response.json();
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
			const formData = new FormData();
			formData.append('modpack', file);
			
			const response = await fetch('/api/v1/modpacks/upload', {
				method: 'POST',
				body: formData
			});
			
			if (!response.ok) {
				const error = await response.json();
				throw new Error(error.error || 'Failed to upload modpack');
			}
			
			const result = await response.json();
			toast.success(`Modpack "${result.name}" uploaded successfully`);
			
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
	
	onMount(async () => {
		await Promise.all([
			searchModpacks(),
			loadFavorites(),
			loadUploadedPacks()
		]);

		if (searchResults && searchResults.total === 0 && !searchParams.q) {
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

<div class="flex-1 space-y-8 h-full p-8 pt-6 bg-gradient-to-br from-background to-muted/10">
	<div class="flex items-center justify-between pb-6 border-b-2 border-border/50">
		<div class="flex items-center gap-4">
			<div class="h-16 w-16 rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg">
				<Package class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1">
				<h2 class="text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">Modpacks</h2>
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
	
	{#if indexerStatus?.indexers?.fuego && !indexerStatus.indexers.fuego.apiKeyConfigured}
		<Alert>
			<AlertCircle class="h-4 w-4" />
			<AlertTitle>CurseForge API Key Required</AlertTitle>
			<AlertDescription>
				<div class="space-y-2">
					<p>To search and install CurseForge modpacks, you need to configure a CurseForge API key.</p>
					<div class="flex items-center gap-2 mt-2">
						<Button size="sm" href={indexerStatus.indexers.fuego.apiKeyUrl} target="_blank">
							<ExternalLink class="h-4 w-4 mr-2" />
							Get API Key
						</Button>
						<Button size="sm" variant="outline" href="/settings#curseforge">
							<Settings class="h-4 w-4 mr-2" />
							Configure API keys in Settings
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
					bind:value={searchParams.q}
					onkeydown={(e) => e.key === 'Enter' && searchModpacks()}
					class="flex-1"
				/>
				<Select type="single" value={searchParams.gameVersion} onValueChange={(v: string | undefined) => searchParams.gameVersion = v || ''} disabled={loading}>
					<SelectTrigger class="w-[180px]">
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
					<SelectTrigger class="w-[180px]">
						<span>{searchParams.modLoader ? modLoaders.find(l => l.value === searchParams.modLoader)?.label : 'All Loaders'}</span>
					</SelectTrigger>
					<SelectContent>
						{#each modLoaders as loader}
							<SelectItem value={loader.value}>{loader.label}</SelectItem>
						{/each}
					</SelectContent>
				</Select>
				<Button onclick={searchModpacks} disabled={loading} class="bg-gradient-to-r from-primary to-primary/80 hover:from-primary/90 hover:to-primary/70 shadow-md hover:shadow-lg transition-all hover:scale-[1.02]">
					<Search class="h-5 w-5 mr-2" />
					Search
				</Button>
				<Button onclick={syncModpacks} disabled={syncing} variant="outline" class="border-2 shadow-sm hover:shadow-md transition-all hover:scale-[1.02]">
					<RefreshCw class={`h-5 w-5 mr-2 ${syncing ? 'animate-spin' : ''}`} />
					Sync
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
			<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="relative">
					<div class="flex items-start gap-4">
						{#if modpack.logo_url}
							<img 
								src={modpack.logo_url} 
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
									{formatNumber(modpack.download_count)}
								</span>
							</div>
						</div>
						<Button
							size="icon"
							variant={modpack.is_favorited ? "default" : "outline"}
							onclick={() => toggleFavorite(modpack)}
							class="hover:scale-110 transition-transform"
						>
							<Heart class={`h-4 w-4 ${modpack.is_favorited ? 'fill-current' : ''}`} />
						</Button>
					</div>
				</CardHeader>
				<CardContent class="relative">
					<CardDescription class="line-clamp-2 mb-4">
						{modpack.summary}
					</CardDescription>
					
					<div class="space-y-2">
						{#if parseJsonArray(modpack.mod_loaders).length > 0}
							<div class="flex flex-wrap gap-1">
								{#each parseJsonArray(modpack.mod_loaders) as loader}
									<Badge variant="outline" class="text-xs">{loader}</Badge>
								{/each}
							</div>
						{/if}
						
						{#if parseJsonArray(modpack.game_versions).length > 0}
							<div class="text-xs text-muted-foreground">
								MC: {parseJsonArray(modpack.game_versions).slice(0, 3).join(', ')}
								{#if parseJsonArray(modpack.game_versions).length > 3}
									+{parseJsonArray(modpack.game_versions).length - 3} more
								{/if}
							</div>
						{/if}
					</div>
					
					<div class="flex items-center justify-between mt-4">
						{#if modpack.website_url}
							<a href={modpack.website_url} target="_blank" rel="noopener noreferrer">
								<Button variant="outline" size="sm">
									<ExternalLink class="h-3 w-3 mr-1" />
									View
								</Button>
							</a>
						{:else}
							<div></div>
						{/if}
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
					{#if searchParams.q}
						No modpacks found matching your search.
					{:else}
						No modpacks found.
					{/if}
				{/if}
			</p>
		</div>
	{/if}
</div>