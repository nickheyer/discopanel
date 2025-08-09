<script lang="ts">
	import { onMount } from 'svelte';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardFooter } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '$lib/components/ui/dropdown-menu';
	import { Input } from '$lib/components/ui/input';
	import { serversStore } from '$lib/stores/servers';
	import { api } from '$lib/api/client';
	import { toast } from 'svelte-sonner';
	import { Plus, Search, MoreVertical, Play, Square, RotateCw, Settings, Package, Trash2, Server as ServerIcon } from '@lucide/svelte';
	import type { Server } from '$lib/api/types';

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
				server.mc_version.toLowerCase().includes(query) ||
				server.mod_loader.toLowerCase().includes(query)
			);
		}
	}

	async function handleServerAction(action: 'start' | 'stop' | 'restart', server: Server) {
		loading = true;
		try {
			switch (action) {
				case 'start':
					await api.startServer(server.id);
					toast.success(`Starting ${server.name}...`);
					break;
				case 'stop':
					await api.stopServer(server.id);
					toast.success(`Stopping ${server.name}...`);
					break;
				case 'restart':
					await api.restartServer(server.id);
					toast.success(`Restarting ${server.name}...`);
					break;
			}
			await serversStore.fetchServers();
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
			await api.deleteServer(server.id);
			serversStore.removeServer(server.id);
			toast.success(`Deleted ${server.name}`);
		} catch (error) {
			toast.error(`Failed to delete server: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			loading = false;
		}
	}

	function getStatusBadgeVariant(status: Server['status']): 'default' | 'secondary' | 'destructive' | 'outline' {
		switch (status) {
			case 'running':
				return 'default';
			case 'starting':
			case 'stopping':
				return 'secondary';
			case 'error':
				return 'destructive';
			default:
				return 'outline';
		}
	}

	function getModLoaderBadgeVariant(modLoader: Server['mod_loader']): 'default' | 'secondary' | 'outline' {
		switch (modLoader) {
			case 'forge':
			case 'neoforge':
				return 'default';
			case 'fabric':
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
									{#if server.status === 'stopped' || server.status === 'error'}
										<DropdownMenuItem class="flex flew-row" onclick={() => handleServerAction('start', server)}>
											<Play class="h-4 w-4 mr-2" />
											Start
										</DropdownMenuItem>
									{/if}
									{#if server.status === 'running' || server.status === 'starting'}
										<DropdownMenuItem class="flex flew-row" onclick={() => handleServerAction('stop', server)}>
											<Square class="h-4 w-4 mr-2" />
											Stop
										</DropdownMenuItem>
									{/if}
									{#if server.status === 'running'}
										<DropdownMenuItem  class="flex flew-row" onclick={() => handleServerAction('restart', server)}>
											<RotateCw class="h-4 w-4 mr-2" />
											Restart
										</DropdownMenuItem>
									{/if}
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
									{#if server.status === 'running'}
										<div class="h-2 w-2 rounded-full bg-green-500 animate-pulse"></div>
									{/if}
									<Badge variant={getStatusBadgeVariant(server.status)} class="font-semibold">
										{server.status.toUpperCase()}
									</Badge>
								</div>
							</div>
							<div class="flex items-center justify-between">
								<span class="text-sm font-medium text-muted-foreground">Version</span>
								<span class="font-semibold">{server.mc_version}</span>
							</div>
							<div class="flex items-center justify-between">
								<span class="text-sm font-medium text-muted-foreground">Mod Loader</span>
								<Badge variant={getModLoaderBadgeVariant(server.mod_loader)} class="capitalize">
									{server.mod_loader === 'vanilla' ? '⚡ Vanilla' : server.mod_loader === 'forge' ? '🔨 Forge' : server.mod_loader === 'fabric' ? '🧵 Fabric' : server.mod_loader}
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