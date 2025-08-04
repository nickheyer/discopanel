<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { Heart, Download, Search, RefreshCw, ExternalLink } from '@lucide/svelte';
	import type { IndexedModpack, ModpackSearchParams, ModpackSearchResponse } from '$lib/api/types';
	
	let searchParams = $state<ModpackSearchParams>({
		q: '',
		gameVersion: '',
		modLoader: '',
		page: 1
	});
	
	let searchResults = $state<ModpackSearchResponse | null>(null);
	let favorites = $state<IndexedModpack[]>([]);
	let loading = $state(false);
	let syncing = $state(false);
	let showFavorites = $state(false);
	
	// Available game versions and mod loaders
	const gameVersions = ['1.20.1', '1.19.4', '1.19.2', '1.18.2', '1.17.1', '1.16.5', '1.12.2'];
	const modLoaders = [
		{ value: '', label: 'All Loaders' },
		{ value: 'forge', label: 'Forge' },
		{ value: 'fabric', label: 'Fabric' },
		{ value: 'neoforge', label: 'NeoForge' },
		{ value: 'quilt', label: 'Quilt' }
	];
	
	async function searchModpacks() {
		loading = true;
		try {
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
			
			// Update favorites list
			if (result.is_favorited) {
				toast.success('Added to favorites');
			} else {
				toast.success('Removed from favorites');
			}
			
			if (showFavorites) {
				await loadFavorites();
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
	
	onMount(async () => {
		await Promise.all([
			searchModpacks(),
			loadFavorites()
		]);
		
		// If no results and not searching, prompt to sync
		if (searchResults && searchResults.total === 0 && !searchParams.q) {
			// Automatically sync on first visit if no modpacks exist
			syncModpacks();
		}
	});
	
	// Computed display list
	let displayModpacks = $derived(showFavorites ? favorites : (searchResults?.modpacks || []));
</script>

<div class="flex-1 space-y-4 p-8">
	<div class="flex items-center justify-between">
		<h2 class="text-3xl font-bold tracking-tight">Modpacks</h2>
		<div class="flex items-center gap-2">
			<Button
				variant={showFavorites ? "default" : "outline"}
				onclick={() => showFavorites = !showFavorites}
			>
				<Heart class="h-4 w-4 mr-2" />
				Favorites ({favorites.length})
			</Button>
		</div>
	</div>
	
	{#if !showFavorites}
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
				<Button onclick={searchModpacks} disabled={loading}>
					<Search class="h-4 w-4 mr-2" />
					Search
				</Button>
				<Button onclick={syncModpacks} disabled={syncing} variant="outline">
					<RefreshCw class={`h-4 w-4 mr-2 ${syncing ? 'animate-spin' : ''}`} />
					Sync
				</Button>
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
			<Card class="hover:shadow-lg transition-shadow">
				<CardHeader>
					<div class="flex items-start gap-4">
						{#if modpack.logo_url}
							<img 
								src={modpack.logo_url} 
								alt={modpack.name}
								class="w-16 h-16 rounded-md object-cover"
							/>
						{/if}
						<div class="flex-1 min-w-0">
							<CardTitle class="line-clamp-1">{modpack.name}</CardTitle>
							<div class="flex items-center gap-2 mt-1">
								<Badge variant="secondary" class="text-xs">
									{modpack.indexer}
								</Badge>
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
						>
							<Heart class={`h-4 w-4 ${modpack.is_favorited ? 'fill-current' : ''}`} />
						</Button>
					</div>
				</CardHeader>
				<CardContent>
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
						<a href={modpack.website_url} target="_blank" rel="noopener noreferrer">
							<Button variant="outline" size="sm">
								<ExternalLink class="h-3 w-3 mr-1" />
								View
							</Button>
						</a>
						<Button size="sm" onclick={() => goto(`/servers/new?modpack=${modpack.id}`)}>
							Use in Server
						</Button>
					</div>
				</CardContent>
			</Card>
		{/each}
	</div>
	
	{#if !showFavorites && searchResults && searchResults.total > searchResults.pageSize}
		<div class="flex items-center justify-center gap-2 mt-6">
			<Button
				variant="outline"
				disabled={(searchParams.page || 1) === 1}
				onclick={() => {
					searchParams.page = Math.max(1, (searchParams.page || 1) - 1);
					searchModpacks();
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
					searchModpacks();
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
						Loading modpacks from indexers for the first time...
					{/if}
				{/if}
			</p>
		</div>
	{/if}
</div>