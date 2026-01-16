<script lang="ts">
	import { onMount } from 'svelte';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardFooter } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '$lib/components/ui/dropdown-menu';
	import { Input } from '$lib/components/ui/input';
	import { serversStore } from '$lib/stores/servers';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { Plus, Search, MoreVertical, Play, Square, RotateCw, RefreshCcw, Settings, Trash2, Server as ServerIcon } from '@lucide/svelte';
	import { type Server, ServerStatus, ModLoader } from '$lib/proto/discopanel/v1/common_pb';

	let servers = $derived($serversStore);
	let filteredServers = $state<Server[]>([]);
	let searchQuery = $state('');
	let loading = $state(false);

	$effect(() => {
		filterServers();
	});

	function filterServers() {
		if (!searchQuery) {
			filteredServers = servers;
		} else {
			const query = searchQuery.toLowerCase();
			filteredServers = servers.filter(server =>
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

	function getStatusBadgeVariant(status: ServerStatus): 'default' | 'secondary' | 'destructive' | 'outline' {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'default';
			case ServerStatus.STARTING:
			case ServerStatus.STOPPING:
			case ServerStatus.CREATING:
				return 'secondary';
			case ServerStatus.ERROR:
				return 'destructive';
			default:
				return 'outline';
		}
	}

	function getStatusDisplayName(status: ServerStatus): string {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'RUNNING';
			case ServerStatus.STOPPED:
				return 'STOPPED';
			case ServerStatus.STARTING:
				return 'STARTING';
			case ServerStatus.STOPPING:
				return 'STOPPING';
			case ServerStatus.ERROR:
				return 'ERROR';
			case ServerStatus.CREATING:
				return 'CREATING';
			case ServerStatus.RESTARTING:
				return 'RESTARTING';
			case ServerStatus.UNHEALTHY:
				return 'UNHEALTHY';
			default:
				return 'UNKNOWN';
		}
	}

	function getModLoaderBadgeVariant(modLoader: ModLoader): 'default' | 'secondary' | 'outline' {
		switch (modLoader) {
			case ModLoader.FORGE:
			case ModLoader.NEOFORGE:
				return 'default';
			case ModLoader.FABRIC:
				return 'secondary';
			default:
				return 'outline';
		}
	}

</script>

<div class="flex-1 space-y-8 h-full p-8 pt-6 bg-gradient-to-br from-background to-muted/10">
	<div class="flex items-center justify-between pb-6 border-b-2 border-border/50">
		<div class="flex items-center gap-4">
			<div class="h-16 w-16 rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg">
				<ServerIcon class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1">
				<h2 class="text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">Servers</h2>
				<p class="text-base text-muted-foreground">Manage and monitor your Minecraft server instances</p>
			</div>
		</div>
		<div class="flex items-center gap-2">
			<Button href="/servers/new" size="default" class="bg-gradient-to-r from-primary to-primary/80 hover:from-primary/90 hover:to-primary/70 shadow-lg hover:shadow-xl hover:scale-[1.02] transition-all">
				<Plus class="h-5 w-5 mr-2" />
				New Server
			</Button>
		</div>
	</div>

	<div class="flex items-center gap-4">
		<div class="relative flex-1 max-w-md">
			<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-muted-foreground" />
			<Input
				type="search"
				placeholder="Search by name, version, or mod loader..."
				class="pl-10 h-11 border-2 focus:border-primary/50"
				bind:value={searchQuery}
			/>
		</div>
	</div>

	{#if filteredServers.length === 0}
		<Card>
			<CardContent class="text-center py-12">
				{#if servers.length === 0}
					<Plus class="mx-auto h-12 w-12 text-muted-foreground" />
					<h3 class="mt-2 text-lg font-semibold">No servers</h3>
					<p class="mt-1 text-sm text-muted-foreground">Get started by creating a new server.</p>
					<div class="mt-6">
						<Button href="/servers/new">
							<Plus class="h-4 w-4 mr-2" />
							New Server
						</Button>
					</div>
				{:else}
					<Search class="mx-auto h-12 w-12 text-muted-foreground" />
					<h3 class="mt-2 text-lg font-semibold">No results found</h3>
					<p class="mt-1 text-sm text-muted-foreground">Try adjusting your search query.</p>
				{/if}
			</CardContent>
		</Card>
	{:else}
		<div class="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
			{#each filteredServers as server}
				<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
					<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
					<CardHeader class="relative">
						<div class="flex items-start justify-between">
							<div class="space-y-2 flex-1">
								<CardTitle class="text-xl font-semibold">{server.name}</CardTitle>
								<CardDescription class="text-sm line-clamp-2">
									{server.description || 'No description provided'}
								</CardDescription>
							</div>
							<DropdownMenu>
								<DropdownMenuTrigger>
									{#snippet child({ props })}
										<Button variant="ghost" size="icon" disabled={loading} {...props}>
											<MoreVertical class="h-4 w-4" />
											<span class="sr-only">Open menu</span>
										</Button>
									{/snippet}
								</DropdownMenuTrigger>
								<DropdownMenuContent align="end">
									<DropdownMenuLabel>Actions</DropdownMenuLabel>
									<DropdownMenuSeparator />
									{#if server.status === ServerStatus.STOPPED || server.status === ServerStatus.ERROR}
										<DropdownMenuItem class="flex flew-row" onclick={() => handleServerAction('start', server)}>
											<Play class="h-4 w-4 mr-2" />
											Start
										</DropdownMenuItem>
									{/if}
									{#if (server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY)|| server.status === ServerStatus.STARTING}
										<DropdownMenuItem class="flex flew-row" onclick={() => handleServerAction('stop', server)}>
											<Square class="h-4 w-4 mr-2" />
											Stop
										</DropdownMenuItem>
									{/if}
									{#if (server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY)}
										<DropdownMenuItem  class="flex flew-row" onclick={() => handleServerAction('restart', server)}>
											<RotateCw class="h-4 w-4 mr-2" />
											Restart
										</DropdownMenuItem>
									{/if}
									<DropdownMenuItem class="flex flew-row" onclick={() => handleServerAction('recreate', server)}>
										<RefreshCcw class="h-4 w-4 mr-2" />
										Recreate
									</DropdownMenuItem>
									<DropdownMenuItem class="flex flew-row text-destructive" onclick={() => deleteServer(server)}>
										<Trash2 class="h-4 w-4 mr-2" />
										Delete
									</DropdownMenuItem>
									<DropdownMenuSeparator />
									<DropdownMenuItem class="flex flew-row">
										<a href="/servers/{server.id}">
											<Settings class="h-4 w-4 mr-2" />
											Manage
										</a>
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						</div>
					</CardHeader>
					<CardContent class="relative">
						<div class="space-y-3">
							<div class="flex items-center justify-between">
								<span class="text-sm font-medium text-muted-foreground">Status</span>
								<div class="flex items-center gap-2">
									{#if server.status === ServerStatus.RUNNING}
										<div class="h-2 w-2 rounded-full bg-green-500 animate-pulse"></div>
									{/if}
									<Badge variant={getStatusBadgeVariant(server.status)} class="font-semibold">
										{getStatusDisplayName(server.status)}
									</Badge>
								</div>
							</div>
							<div class="flex items-center justify-between">
								<span class="text-sm font-medium text-muted-foreground">Version</span>
								<span class="font-semibold">{server.mcVersion}</span>
							</div>
							<div class="flex items-center justify-between">
								<span class="text-sm font-medium text-muted-foreground">Mod Loader</span>
								<Badge variant={getModLoaderBadgeVariant(server.modLoader)} class="capitalize">
									{ModLoader[server.modLoader].replace('_', ' ').toLowerCase()}
								</Badge>
							</div>
							<div class="pt-3 mt-3 border-t space-y-2">
								<div class="flex items-center justify-between">
									<span class="text-sm font-medium text-muted-foreground">Connection</span>
									<span class="font-mono text-sm font-semibold">:{server.port}</span>
								</div>
								<div class="flex items-center justify-between">
									<span class="text-sm font-medium text-muted-foreground">Memory</span>
									<span class="font-semibold">{(server.memory / 1024).toFixed(1)} GB</span>
								</div>
							</div>
						</div>
					</CardContent>
					<CardFooter class="relative pt-4">
						<Button class="w-full h-11 font-semibold shadow-lg hover:shadow-xl transition-all hover:scale-[1.02]" href="/servers/{server.id}">
							Manage Server
						</Button>
					</CardFooter>
				</Card>
			{/each}
		</div>
	{/if}
</div>