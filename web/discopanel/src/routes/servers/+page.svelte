<script lang="ts">
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Input } from '$lib/components/ui/input';
	import { resolve } from '$app/paths';
	import { serversStore, sortServersByActivity } from '$lib/stores/servers';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import {
		Plus,
		Search,
		Play,
		Square,
		RotateCw,
		RefreshCcw,
		Trash2,
		Server as ServerIcon,
		Users,
		Zap,
		MemoryStick,
		Wifi
	} from '@lucide/svelte';
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
			filteredServers = sorted.filter(
				(server) =>
					server.name.toLowerCase().includes(query) ||
					server.description.toLowerCase().includes(query) ||
					server.mcVersion.toLowerCase().includes(query) ||
					String(server.modLoader).toLowerCase().includes(query)
			);
		}
	}

	async function handleServerAction(
		action: 'start' | 'stop' | 'restart' | 'recreate',
		server: Server
	) {
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
			toast.error(
				`Failed to ${action} server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			loading = false;
		}
	}

	async function deleteServer(server: Server) {
		if (
			!confirm(`Are you sure you want to delete "${server.name}"? This action cannot be undone.`)
		) {
			return;
		}

		loading = true;
		try {
			await rpcClient.server.deleteServer({ id: server.id });
			serversStore.removeServer(server.id);
			toast.success(`Deleted ${server.name}`);
		} catch (error) {
			toast.error(
				`Failed to delete server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
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
			case ServerStatus.PROVISIONING:
				return 'bg-yellow-500/10 text-yellow-500 border-yellow-500/20';
			case ServerStatus.PAUSED:
				return 'bg-blue-500/10 text-blue-500 border-blue-500/20';
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
			case ServerStatus.PROVISIONING:
				return 'via-yellow-500/50';
			case ServerStatus.PAUSED:
				return 'via-blue-500/50';
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
			case ServerStatus.PROVISIONING:
				return 'Provisioning';
			case ServerStatus.PAUSED:
				return 'Sleeping';
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
			case ServerStatus.PROVISIONING:
				return 'bg-yellow-500 animate-pulse';
			case ServerStatus.PAUSED:
				return 'bg-blue-500';
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

<div class="h-full flex-1 space-y-8 bg-linear-to-br from-background to-muted/10 p-8 pt-6">
	<div class="flex items-center justify-between border-b-2 border-border/50 pb-6">
		<div class="flex items-center gap-4">
			<div
				class="flex h-16 w-16 animate-in items-center justify-center rounded-2xl bg-linear-to-br from-primary/20 to-primary/10 shadow-lg duration-500 fade-in-50"
			>
				<ServerIcon class="h-8 w-8 text-primary" />
			</div>
			<div class="animate-in space-y-1 duration-500 slide-in-from-left-5">
				<h2
					class="bg-linear-to-r from-foreground to-foreground/70 bg-clip-text text-4xl font-bold tracking-tight text-transparent"
				>
					Servers
				</h2>
				<p class="text-base text-muted-foreground">
					Manage and monitor your Minecraft server instances
				</p>
			</div>
		</div>
		<div class="flex animate-in items-center gap-2 duration-500 slide-in-from-right-5">
			<Button
				href="/servers/new"
				size="default"
				class="bg-linear-to-r from-primary to-primary/80 shadow-lg transition-all hover:scale-[1.02] hover:from-primary/90 hover:to-primary/70 hover:shadow-xl"
			>
				<Plus class="mr-2 h-5 w-5" />
				New Server
			</Button>
		</div>
	</div>

	<div
		class="flex animate-in items-center gap-4 duration-500 fade-in-50 slide-in-from-bottom-2"
		style="animation-delay: 100ms"
	>
		<div class="relative max-w-md flex-1">
			<Search class="absolute top-1/2 left-3 h-5 w-5 -translate-y-1/2 text-muted-foreground" />
			<Input
				type="search"
				placeholder="Search by name, version, or mod loader..."
				class="h-11 border-2 pl-10 focus:border-primary/50"
				bind:value={searchQuery}
			/>
		</div>
		{#if servers.length > 0}
			<div class="flex items-center gap-4 text-sm text-muted-foreground">
				<div class="flex items-center gap-1.5">
					<div class="h-2 w-2 rounded-full bg-green-500"></div>
					<span>{servers.filter((s) => s.status === ServerStatus.RUNNING).length} running</span>
				</div>
				<div class="flex items-center gap-1.5">
					<div class="h-2 w-2 rounded-full bg-gray-400"></div>
					<span>{servers.filter((s) => s.status === ServerStatus.STOPPED).length} stopped</span>
				</div>
				{#if servers.some((s) => s.status === ServerStatus.ERROR || s.status === ServerStatus.UNHEALTHY)}
					<div class="flex items-center gap-1.5">
						<div class="h-2 w-2 rounded-full bg-red-500"></div>
						<span class="text-red-500"
							>{servers.filter(
								(s) => s.status === ServerStatus.ERROR || s.status === ServerStatus.UNHEALTHY
							).length} issues</span
						>
					</div>
				{/if}
			</div>
		{/if}
	</div>

	{#if filteredServers.length === 0}
		<Card class="animate-in border-border/50 duration-500 fade-in-50 slide-in-from-bottom-5">
			<CardContent class="py-16 text-center">
				{#if servers.length === 0}
					<div
						class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-linear-to-br from-primary/20 to-primary/10"
					>
						<ServerIcon class="h-8 w-8 text-primary" />
					</div>
					<h3 class="mb-1 text-lg font-semibold">No servers yet</h3>
					<p class="mb-6 text-sm text-muted-foreground">
						Create your first Minecraft server to get started
					</p>
					<Button
						href="/servers/new"
						class="bg-linear-to-r from-primary to-primary/80 shadow-lg transition-all hover:scale-[1.02] hover:from-primary/90 hover:to-primary/70 hover:shadow-xl"
					>
						<Plus class="mr-2 h-4 w-4" />
						Create Server
					</Button>
				{:else}
					<div
						class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-linear-to-br from-muted to-muted/50"
					>
						<Search class="h-8 w-8 text-muted-foreground" />
					</div>
					<h3 class="mb-1 text-lg font-semibold">No results found</h3>
					<p class="text-sm text-muted-foreground">Try adjusting your search query.</p>
				{/if}
			</CardContent>
		</Card>
	{:else}
		<div class="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
			{#each filteredServers as server, i (server.id)}
				<Card
					class="group relative h-full animate-in overflow-hidden border-0 bg-linear-to-br from-background via-background/95 to-background/90 shadow-lg transition-all duration-500 fade-in-50 slide-in-from-bottom-3 hover:-translate-y-1 hover:shadow-2xl"
					style="animation-delay: {150 + i * 75}ms"
				>
					<!-- Status accent line -->
					<div
						class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent {getStatusAccentColor(
							server.status
						)} to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
					></div>
					<!-- Hover wash -->
					<div
						class="absolute inset-0 bg-linear-to-br from-primary/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
					></div>

					<CardHeader class="relative pb-3">
						<div class="flex items-start justify-between gap-2">
							<div class="flex min-w-0 flex-1 items-start gap-3">
								<div
									class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-linear-to-br from-blue-500/20 to-blue-600/10 transition-transform duration-300 group-hover:scale-110"
								>
									<ServerIcon class="h-5 w-5 text-blue-500" />
								</div>
								<div class="min-w-0 flex-1 space-y-1">
									<CardTitle class="truncate text-lg font-semibold">{server.name}</CardTitle>
									<CardDescription class="line-clamp-1 text-xs">
										{server.description || 'No description provided'}
									</CardDescription>
								</div>
							</div>
							<!-- Action bar -->
							<div
								class="flex shrink-0 overflow-hidden rounded-md border border-border/60 shadow-sm"
							>
								{#if server.status === ServerStatus.STOPPED || server.status === ServerStatus.ERROR}
									<button
										title="Start"
										disabled={loading}
										class="flex h-7 w-7 items-center justify-center border-r border-border/60 text-muted-foreground transition-colors hover:bg-green-500/10 hover:text-green-500 disabled:opacity-50"
										onclick={() => handleServerAction('start', server)}
									>
										<Play class="h-3 w-3" />
									</button>
								{/if}
								{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY || server.status === ServerStatus.STARTING || server.status === ServerStatus.PAUSED}
									<button
										title="Stop"
										disabled={loading}
										class="flex h-7 w-7 items-center justify-center border-r border-border/60 text-muted-foreground transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
										onclick={() => handleServerAction('stop', server)}
									>
										<Square class="h-2.5 w-2.5" />
									</button>
								{/if}
								{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY}
									<button
										title="Restart"
										disabled={loading}
										class="flex h-7 w-7 items-center justify-center border-r border-border/60 text-muted-foreground transition-colors hover:bg-yellow-500/10 hover:text-yellow-500 disabled:opacity-50"
										onclick={() => handleServerAction('restart', server)}
									>
										<RotateCw class="h-3 w-3" />
									</button>
								{/if}
								<button
									title="Recreate"
									disabled={loading}
									class="flex h-7 w-7 items-center justify-center border-r border-border/60 text-muted-foreground transition-colors hover:bg-muted disabled:opacity-50"
									onclick={() => handleServerAction('recreate', server)}
								>
									<RefreshCcw class="h-3 w-3" />
								</button>
								<button
									title="Delete"
									disabled={loading}
									class="flex h-7 w-7 items-center justify-center text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
									onclick={() => deleteServer(server)}
								>
									<Trash2 class="h-3 w-3" />
								</button>
							</div>
						</div>

						<!-- Status badge row -->
						<div class="mt-2 flex items-center gap-2">
							<div class="h-2 w-2 rounded-full {getStatusDotColor(server.status)}"></div>
							<Badge variant="outline" class="border text-xs {getStatusBadgeColor(server.status)}">
								{getStatusDisplayName(server.status)}
							</Badge>
							<span class="text-xs text-muted-foreground">{server.mcVersion}</span>
							{#if server.modLoader !== ModLoader.VANILLA && server.modLoader !== ModLoader.UNSPECIFIED}
								<Badge variant="outline" class="text-xs capitalize"
									>{getModLoaderDisplay(server.modLoader)}</Badge
								>
							{/if}
						</div>
					</CardHeader>

					<CardContent class="relative flex-1 pt-0 pb-3">
						<!-- Stats grid -->
						<div class="grid grid-cols-2 gap-3">
							<div class="flex items-center gap-2 rounded-lg bg-muted/30 p-2.5">
								<Wifi class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
								<div class="min-w-0">
									<p class="text-[10px] tracking-wider text-muted-foreground uppercase">Port</p>
									<p class="truncate font-mono text-sm font-semibold">{server.port}</p>
								</div>
							</div>
							<div class="flex items-center gap-2 rounded-lg bg-muted/30 p-2.5">
								<MemoryStick class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
								<div class="min-w-0">
									<p class="text-[10px] tracking-wider text-muted-foreground uppercase">Memory</p>
									<p class="truncate text-sm font-semibold">
										{(server.memory / 1024).toFixed(1)} GB
									</p>
								</div>
							</div>
							{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY}
								<div class="flex items-center gap-2 rounded-lg bg-muted/30 p-2.5">
									<Users class="h-3.5 w-3.5 shrink-0 text-green-500" />
									<div class="min-w-0">
										<p class="text-[10px] tracking-wider text-muted-foreground uppercase">
											Players
										</p>
										<p class="text-sm font-semibold">
											{server.playersOnline || 0}
											<span class="font-normal text-muted-foreground">/ {server.maxPlayers}</span>
										</p>
									</div>
								</div>
								<div class="flex items-center gap-2 rounded-lg bg-muted/30 p-2.5">
									<Zap
										class="h-3.5 w-3.5 {server.tps && server.tps >= 18
											? 'text-green-500'
											: server.tps && server.tps >= 15
												? 'text-yellow-500'
												: server.tps
													? 'text-red-500'
													: 'text-muted-foreground'} shrink-0"
									/>
									<div class="min-w-0">
										<p class="text-[10px] tracking-wider text-muted-foreground uppercase">TPS</p>
										<p
											class="text-sm font-semibold {server.tps && server.tps >= 18
												? 'text-green-500'
												: server.tps && server.tps >= 15
													? 'text-yellow-500'
													: server.tps
														? 'text-red-500'
														: ''}"
										>
											{server.tps ? server.tps.toFixed(1) : '-'}
										</p>
									</div>
								</div>
							{/if}
						</div>
					</CardContent>

					<!-- Clickable overlay for bottom half -->
					<a
						href={resolve(`/servers/${server.id}`)}
						class="absolute inset-x-0 bottom-0 z-10 flex h-1/2 cursor-pointer items-end justify-center bg-gradient-to-t from-primary/10 to-transparent pb-4 opacity-0 transition-all duration-300 group-hover:opacity-100"
					>
					</a>
				</Card>
			{/each}
		</div>
	{/if}
</div>
