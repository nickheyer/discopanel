<script lang="ts">
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Input } from '$lib/components/ui/input';
	import { serversStore, sortServersByActivity } from '$lib/stores/servers';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { Plus, Search, Play, Square, RotateCw, RefreshCcw, Trash2, Server as ServerIcon, Users, Zap, MemoryStick, Wifi } from '@lucide/svelte';
	import { type Server, ServerStatus, ModLoader } from '$lib/proto/discopanel/v1/common_pb';

	let servers = $derived($serversStore);
	let filteredServers = $state<Server[]>([]);
	let searchQuery = $state('');
	let loading = $state(false);

	$effect(() => {
		filterServers();
	});

	function filterServers() {
		let sorted = sortServersByActivity([...servers]);
		if (!searchQuery) {
			filteredServers = sorted;
		} else {
			const query = searchQuery.toLowerCase();
			filteredServers = sorted.filter(server =>
				server.name.toLowerCase().includes(query) ||
				server.description.toLowerCase().includes(query) ||
				server.mcVersion.toLowerCase().includes(query) ||
				String(server.modLoader).toLowerCase().includes(query)
			);
		}
	}

	async function handleServerAction(action: 'start' | 'stop' | 'restart' | 'recreate', server: Server) {
		loading = true;
		try {
			switch (action) {
				case 'start':
					await rpcClient.server.startServer({ id: server.id });
					toast.success(`Starting ${server.name}...`);
					break;
				case 'stop':
					await rpcClient.server.stopServer({ id: server.id });
					toast.success(`Stopping ${server.name}...`);
					break;
				case 'restart':
					await rpcClient.server.restartServer({ id: server.id });
					toast.success(`Restarting ${server.name}...`);
					break;
				case 'recreate':
					await rpcClient.server.recreateServer({ id: server.id });
					toast.success(`Recreating ${server.name}...`);
					break;
			}
		} catch (error) {
			toast.error(`Failed to ${action} server: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			loading = false;
		}
	}

	async function deleteServer(server: Server) {
		if (!confirm(`Are you sure you want to delete "${server.name}"? This action cannot be undone.`)) {
			return;
		}

		loading = true;
		try {
			await rpcClient.server.deleteServer({ id: server.id });
			serversStore.removeServer(server.id);
			toast.success(`Deleted ${server.name}`);
		} catch (error) {
			toast.error(`Failed to delete server: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			loading = false;
		}
	}

	function getStatusBadgeColor(status: ServerStatus): string {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'bg-green-500/10 text-green-500 border-green-500/20';
			case ServerStatus.STARTING:
			case ServerStatus.STOPPING:
			case ServerStatus.CREATING:
			case ServerStatus.RESTARTING:
				return 'bg-yellow-500/10 text-yellow-500 border-yellow-500/20';
			case ServerStatus.ERROR:
			case ServerStatus.UNHEALTHY:
				return 'bg-red-500/10 text-red-500 border-red-500/20';
			case ServerStatus.STOPPED:
			default:
				return 'bg-gray-500/10 text-gray-500 border-gray-500/20';
		}
	}

	function getStatusAccentColor(status: ServerStatus): string {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'via-green-500/50';
			case ServerStatus.STARTING:
			case ServerStatus.STOPPING:
			case ServerStatus.CREATING:
			case ServerStatus.RESTARTING:
				return 'via-yellow-500/50';
			case ServerStatus.ERROR:
			case ServerStatus.UNHEALTHY:
				return 'via-red-500/50';
			case ServerStatus.STOPPED:
			default:
				return 'via-gray-500/30';
		}
	}

	function getStatusDisplayName(status: ServerStatus): string {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'Running';
			case ServerStatus.STOPPED:
				return 'Stopped';
			case ServerStatus.STARTING:
				return 'Starting';
			case ServerStatus.STOPPING:
				return 'Stopping';
			case ServerStatus.ERROR:
				return 'Error';
			case ServerStatus.CREATING:
				return 'Creating';
			case ServerStatus.RESTARTING:
				return 'Restarting';
			case ServerStatus.UNHEALTHY:
				return 'Unhealthy';
			default:
				return 'Unknown';
		}
	}

	function getStatusDotColor(status: ServerStatus): string {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'bg-green-500 animate-pulse';
			case ServerStatus.STARTING:
			case ServerStatus.STOPPING:
			case ServerStatus.CREATING:
			case ServerStatus.RESTARTING:
				return 'bg-yellow-500 animate-pulse';
			case ServerStatus.ERROR:
			case ServerStatus.UNHEALTHY:
				return 'bg-red-500 animate-pulse';
			case ServerStatus.STOPPED:
			default:
				return 'bg-gray-400';
		}
	}

	function getModLoaderDisplay(modLoader: ModLoader): string {
		return ModLoader[modLoader].replace('_', ' ').toLowerCase();
	}
</script>

<div class="flex-1 space-y-8 h-full p-8 pt-6 bg-gradient-to-br from-background to-muted/10">
	<div class="flex items-center justify-between pb-6 border-b-2 border-border/50">
		<div class="flex items-center gap-4">
			<div class="h-16 w-16 rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg animate-in fade-in-50 duration-500">
				<ServerIcon class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1 animate-in slide-in-from-left-5 duration-500">
				<h2 class="text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">Servers</h2>
				<p class="text-base text-muted-foreground">Manage and monitor your Minecraft server instances</p>
			</div>
		</div>
		<div class="flex items-center gap-2 animate-in slide-in-from-right-5 duration-500">
			<Button href="/servers/new" size="default" class="bg-gradient-to-r from-primary to-primary/80 hover:from-primary/90 hover:to-primary/70 shadow-lg hover:shadow-xl hover:scale-[1.02] transition-all">
				<Plus class="h-5 w-5 mr-2" />
				New Server
			</Button>
		</div>
	</div>

	<div class="flex items-center gap-4 animate-in fade-in-50 slide-in-from-bottom-2 duration-500" style="animation-delay: 100ms">
		<div class="relative flex-1 max-w-md">
			<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-muted-foreground" />
			<Input
				type="search"
				placeholder="Search by name, version, or mod loader..."
				class="pl-10 h-11 border-2 focus:border-primary/50"
				bind:value={searchQuery}
			/>
		</div>
		{#if servers.length > 0}
			<div class="flex items-center gap-4 text-sm text-muted-foreground">
				<div class="flex items-center gap-1.5">
					<div class="h-2 w-2 rounded-full bg-green-500"></div>
					<span>{servers.filter(s => s.status === ServerStatus.RUNNING).length} running</span>
				</div>
				<div class="flex items-center gap-1.5">
					<div class="h-2 w-2 rounded-full bg-gray-400"></div>
					<span>{servers.filter(s => s.status === ServerStatus.STOPPED).length} stopped</span>
				</div>
				{#if servers.some(s => s.status === ServerStatus.ERROR || s.status === ServerStatus.UNHEALTHY)}
					<div class="flex items-center gap-1.5">
						<div class="h-2 w-2 rounded-full bg-red-500"></div>
						<span class="text-red-500">{servers.filter(s => s.status === ServerStatus.ERROR || s.status === ServerStatus.UNHEALTHY).length} issues</span>
					</div>
				{/if}
			</div>
		{/if}
	</div>

	{#if filteredServers.length === 0}
		<Card class="animate-in fade-in-50 slide-in-from-bottom-5 duration-500 border-border/50">
			<CardContent class="text-center py-16">
				{#if servers.length === 0}
					<div class="mx-auto h-16 w-16 rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center mb-4">
						<ServerIcon class="h-8 w-8 text-primary" />
					</div>
					<h3 class="text-lg font-semibold mb-1">No servers yet</h3>
					<p class="text-sm text-muted-foreground mb-6">Create your first Minecraft server to get started</p>
					<Button href="/servers/new" class="bg-gradient-to-r from-primary to-primary/80 hover:from-primary/90 hover:to-primary/70 shadow-lg hover:shadow-xl hover:scale-[1.02] transition-all">
						<Plus class="h-4 w-4 mr-2" />
						Create Server
					</Button>
				{:else}
					<div class="mx-auto h-16 w-16 rounded-2xl bg-gradient-to-br from-muted to-muted/50 flex items-center justify-center mb-4">
						<Search class="h-8 w-8 text-muted-foreground" />
					</div>
					<h3 class="text-lg font-semibold mb-1">No results found</h3>
					<p class="text-sm text-muted-foreground">Try adjusting your search query.</p>
				{/if}
			</CardContent>
		</Card>
	{:else}
		<div class="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
			{#each filteredServers as server, i (server.id)}
				<Card
					class="group relative overflow-hidden border-0 shadow-lg hover:shadow-2xl transition-all duration-500 bg-gradient-to-br from-background via-background/95 to-background/90 hover:-translate-y-1 animate-in fade-in-50 slide-in-from-bottom-3 h-full"
					style="animation-delay: {150 + i * 75}ms"
				>
					<!-- Status accent line -->
					<div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent {getStatusAccentColor(server.status)} to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
					<!-- Hover wash -->
					<div class="absolute inset-0 bg-gradient-to-br from-primary/5 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>

					<CardHeader class="relative pb-3">
						<div class="flex items-start justify-between gap-2">
							<div class="flex items-start gap-3 flex-1 min-w-0">
								<div class="h-10 w-10 shrink-0 rounded-xl bg-gradient-to-br from-blue-500/20 to-blue-600/10 flex items-center justify-center group-hover:scale-110 transition-transform duration-300">
									<ServerIcon class="h-5 w-5 text-blue-500" />
								</div>
								<div class="min-w-0 flex-1 space-y-1">
									<CardTitle class="text-lg font-semibold truncate">{server.name}</CardTitle>
									<CardDescription class="text-xs line-clamp-1">
										{server.description || 'No description provided'}
									</CardDescription>
								</div>
							</div>
							<!-- Action bar -->
							<div class="flex shrink-0 rounded-md border border-border/60 overflow-hidden shadow-sm">
								{#if server.status === ServerStatus.STOPPED || server.status === ServerStatus.ERROR}
									<button title="Start" disabled={loading} class="flex items-center justify-center h-7 w-7 border-r border-border/60 text-muted-foreground hover:bg-green-500/10 hover:text-green-500 transition-colors disabled:opacity-50" onclick={() => handleServerAction('start', server)}>
										<Play class="h-3 w-3" />
									</button>
								{/if}
								{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY || server.status === ServerStatus.STARTING}
									<button title="Stop" disabled={loading} class="flex items-center justify-center h-7 w-7 border-r border-border/60 text-muted-foreground hover:bg-red-500/10 hover:text-red-500 transition-colors disabled:opacity-50" onclick={() => handleServerAction('stop', server)}>
										<Square class="h-2.5 w-2.5" />
									</button>
								{/if}
								{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY}
									<button title="Restart" disabled={loading} class="flex items-center justify-center h-7 w-7 border-r border-border/60 text-muted-foreground hover:bg-yellow-500/10 hover:text-yellow-500 transition-colors disabled:opacity-50" onclick={() => handleServerAction('restart', server)}>
										<RotateCw class="h-3 w-3" />
									</button>
								{/if}
								<button title="Recreate" disabled={loading} class="flex items-center justify-center h-7 w-7 border-r border-border/60 text-muted-foreground hover:bg-muted transition-colors disabled:opacity-50" onclick={() => handleServerAction('recreate', server)}>
									<RefreshCcw class="h-3 w-3" />
								</button>
								<button title="Delete" disabled={loading} class="flex items-center justify-center h-7 w-7 text-muted-foreground hover:bg-destructive/10 hover:text-destructive transition-colors disabled:opacity-50" onclick={() => deleteServer(server)}>
									<Trash2 class="h-3 w-3" />
								</button>
							</div>
						</div>

						<!-- Status badge row -->
						<div class="flex items-center gap-2 mt-2">
							<div class="h-2 w-2 rounded-full {getStatusDotColor(server.status)}"></div>
							<Badge variant="outline" class="text-xs border {getStatusBadgeColor(server.status)}">
								{getStatusDisplayName(server.status)}
							</Badge>
							<span class="text-xs text-muted-foreground">{server.mcVersion}</span>
							{#if server.modLoader !== ModLoader.VANILLA && server.modLoader !== ModLoader.UNSPECIFIED}
								<Badge variant="outline" class="text-xs capitalize">{getModLoaderDisplay(server.modLoader)}</Badge>
							{/if}
						</div>
					</CardHeader>

					<CardContent class="relative pt-0 pb-3 flex-1">
						<!-- Stats grid -->
						<div class="grid grid-cols-2 gap-3">
							<div class="flex items-center gap-2 p-2.5 rounded-lg bg-muted/30">
								<Wifi class="h-3.5 w-3.5 text-muted-foreground shrink-0" />
								<div class="min-w-0">
									<p class="text-[10px] text-muted-foreground uppercase tracking-wider">Port</p>
									<p class="text-sm font-mono font-semibold truncate">{server.port}</p>
								</div>
							</div>
							<div class="flex items-center gap-2 p-2.5 rounded-lg bg-muted/30">
								<MemoryStick class="h-3.5 w-3.5 text-muted-foreground shrink-0" />
								<div class="min-w-0">
									<p class="text-[10px] text-muted-foreground uppercase tracking-wider">Memory</p>
									<p class="text-sm font-semibold truncate">{(server.memory / 1024).toFixed(1)} GB</p>
								</div>
							</div>
							{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY}
								<div class="flex items-center gap-2 p-2.5 rounded-lg bg-muted/30">
									<Users class="h-3.5 w-3.5 text-green-500 shrink-0" />
									<div class="min-w-0">
										<p class="text-[10px] text-muted-foreground uppercase tracking-wider">Players</p>
										<p class="text-sm font-semibold">{server.playersOnline || 0} <span class="text-muted-foreground font-normal">/ {server.maxPlayers}</span></p>
									</div>
								</div>
								<div class="flex items-center gap-2 p-2.5 rounded-lg bg-muted/30">
									<Zap class="h-3.5 w-3.5 {server.tps && server.tps >= 18 ? 'text-green-500' : server.tps && server.tps >= 15 ? 'text-yellow-500' : server.tps ? 'text-red-500' : 'text-muted-foreground'} shrink-0" />
									<div class="min-w-0">
										<p class="text-[10px] text-muted-foreground uppercase tracking-wider">TPS</p>
										<p class="text-sm font-semibold {server.tps && server.tps >= 18 ? 'text-green-500' : server.tps && server.tps >= 15 ? 'text-yellow-500' : server.tps ? 'text-red-500' : ''}">{server.tps ? server.tps.toFixed(1) : '—'}</p>
									</div>
								</div>
							{/if}
						</div>
					</CardContent>

					<!-- Clickable overlay for bottom half -->
					<a href="/servers/{server.id}" class="absolute inset-x-0 bottom-0 h-1/2 z-10 cursor-pointer flex items-end justify-center pb-4 opacity-0 group-hover:opacity-100 transition-all duration-300 bg-gradient-to-t from-primary/10 to-transparent">
					</a>
				</Card>
			{/each}
		</div>
	{/if}
</div>