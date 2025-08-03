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
	import { Plus, Search, MoreVertical, Play, Square, RotateCw, Settings, Package, Trash2 } from '@lucide/svelte';
	import type { Server } from '$lib/api/types';

	let servers = $state<Server[]>([]);
	let filteredServers = $state<Server[]>([]);
	let searchQuery = $state('');
	let loading = $state(false);

	onMount(() => {
		const unsubscribe = serversStore.subscribe(value => {
			servers = value;
			filterServers();
		});

		return unsubscribe;
	});

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

<div class="flex-1 space-y-4 p-8 pt-6">
	<div class="flex items-center justify-between space-y-2">
		<div>
			<h2 class="text-3xl font-bold tracking-tight">Servers</h2>
			<p class="text-muted-foreground">Manage your Minecraft server instances</p>
		</div>
		<div class="flex items-center space-x-2">
			<Button href="/servers/new">
				<Plus class="h-4 w-4 mr-2" />
				New Server
			</Button>
		</div>
	</div>

	<div class="flex items-center space-x-2">
		<div class="relative flex-1 max-w-sm">
			<Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
			<Input
				type="search"
				placeholder="Search servers..."
				class="pl-8"
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
		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
			{#each filteredServers as server}
				<Card>
					<CardHeader>
						<div class="flex items-start justify-between">
							<div class="space-y-1">
								<CardTitle class="text-lg">{server.name}</CardTitle>
								<CardDescription class="text-sm">
									{server.description || 'No description'}
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
										<DropdownMenuItem onclick={() => handleServerAction('start', server)}>
											<Play class="h-4 w-4 mr-2" />
											Start
										</DropdownMenuItem>
									{/if}
									{#if server.status === 'running' || server.status === 'starting'}
										<DropdownMenuItem onclick={() => handleServerAction('stop', server)}>
											<Square class="h-4 w-4 mr-2" />
											Stop
										</DropdownMenuItem>
									{/if}
									{#if server.status === 'running'}
										<DropdownMenuItem onclick={() => handleServerAction('restart', server)}>
											<RotateCw class="h-4 w-4 mr-2" />
											Restart
										</DropdownMenuItem>
									{/if}
									<DropdownMenuSeparator />
									<DropdownMenuItem>
										<a href="/servers/{server.id}">
											<Settings class="h-4 w-4 mr-2" />
											Manage
										</a>
									</DropdownMenuItem>
									<DropdownMenuItem>
										<a href="/servers/{server.id}/mods">
											<Package class="h-4 w-4 mr-2" />
											Mods			
										</a>
									</DropdownMenuItem>
									<DropdownMenuSeparator />
									<DropdownMenuItem onclick={() => deleteServer(server)} class="text-destructive">
										<Trash2 class="h-4 w-4 mr-2" />
										Delete
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						</div>
					</CardHeader>
					<CardContent>
						<div class="space-y-3">
							<div class="flex items-center justify-between text-sm">
								<span class="text-muted-foreground">Status</span>
								<Badge variant={getStatusBadgeVariant(server.status)}>
									{server.status}
								</Badge>
							</div>
							<div class="flex items-center justify-between text-sm">
								<span class="text-muted-foreground">Version</span>
								<span>{server.mc_version}</span>
							</div>
							<div class="flex items-center justify-between text-sm">
								<span class="text-muted-foreground">Mod Loader</span>
								<Badge variant={getModLoaderBadgeVariant(server.mod_loader)}>
									{server.mod_loader}
								</Badge>
							</div>
							<div class="flex items-center justify-between text-sm">
								<span class="text-muted-foreground">Port</span>
								<span>{server.port}</span>
							</div>
							<div class="flex items-center justify-between text-sm">
								<span class="text-muted-foreground">Memory</span>
								<span>{server.memory} MB</span>
							</div>
						</div>
					</CardContent>
					<CardFooter>
						<Button class="w-full" href="/servers/{server.id}">
							Manage Server
						</Button>
					</CardFooter>
				</Card>
			{/each}
		</div>
	{/if}
</div>