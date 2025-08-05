<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { serversStore } from '$lib/stores/servers';
	import { goto } from '$app/navigation';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { Separator } from '$lib/components/ui/separator';
	import { toast } from 'svelte-sonner';
	import { Play, Square, RotateCw, Terminal, Settings, Package, HardDrive, Activity, Loader2, Copy, ExternalLink, Trash2 } from '@lucide/svelte';
	import type { Server } from '$lib/api/types';
	import ServerConsole from '$lib/components/server-console.svelte';
	import ServerConfiguration from '$lib/components/server-configuration.svelte';
	import ServerSettings from '$lib/components/server-settings.svelte';
	import ServerMods from '$lib/components/server-mods.svelte';
	import ServerFiles from '$lib/components/server-files.svelte';
	import ServerRouting from '$lib/components/server-routing.svelte';

	let server = $state<Server | null>(null);
	let loading = $state(true);
	let actionLoading = $state(false);
	let serverId = $derived(page.params.id);
	let activeTab = $state('overview');
	let routingInfo = $state<any>(null);

	let interval: number;

	onMount(() => {
		return () => {
			if (interval) clearInterval(interval);
		};
	});

	$effect(() => {
		if (serverId) {
			// Clear existing interval
			if (interval) clearInterval(interval);
			
			// Load server immediately
			loadServer();
			
			// Set up new interval
			interval = setInterval(loadServer, 5000); // Poll every 5 seconds
		}
	});

	async function loadServer() {
		try {
			if (!serverId) return;
			server = await api.getServer(serverId);
			serversStore.updateServer(server);
			
			// Load routing info if proxy is available
			try {
				routingInfo = await api.getServerRouting(serverId);
			} catch (error) {
				// Not critical if routing info fails
			}
		} catch (error) {
			if (!server) {
				toast.error('Failed to load server');
			}
		} finally {
			loading = false;
		}
	}

	async function handleServerAction(action: 'start' | 'stop' | 'restart') {
		if (!server) return;
		
		actionLoading = true;
		try {
			switch (action) {
				case 'start':
					await api.startServer(server.id);
					toast.success('Server is starting...');
					break;
				case 'stop':
					await api.stopServer(server.id);
					toast.success('Server is stopping...');
					break;
				case 'restart':
					await api.restartServer(server.id);
					toast.success('Server is restarting...');
					break;
			}
			await loadServer();
		} catch (error) {
			toast.error(`Failed to ${action} server: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			actionLoading = false;
		}
	}

	function getStatusColor(status: Server['status']) {
		switch (status) {
			case 'running':
				return 'text-green-500';
			case 'starting':
			case 'stopping':
				return 'text-yellow-500';
			case 'stopped':
				return 'text-gray-400';
			case 'error':
				return 'text-red-500';
			default:
				return 'text-gray-400';
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

	async function copyToClipboard(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast.success('Copied to clipboard!');
		} catch {
			toast.error('Failed to copy to clipboard');
		}
	}

	async function handleDeleteServer() {
		if (!server) return;
		
		const confirmed = confirm(`Are you sure you want to delete "${server.name}"?\n\nThis will:\n- Stop and remove the Docker container\n- Delete all server files and data\n- Remove all mods and configurations\n\nThis action cannot be undone!`);
		
		if (!confirmed) return;
		
		actionLoading = true;
		try {
			await api.deleteServer(server.id);
			serversStore.removeServer(server.id);
			toast.success('Server deleted successfully');
			goto('/servers');
		} catch (error) {
			toast.error(`Failed to delete server: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			actionLoading = false;
		}
	}
</script>

{#if loading && !server}
	<div class="flex items-center justify-center h-96">
		<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
	</div>
{:else if server}
	<div class="h-full flex flex-col p-8 pt-6 bg-gradient-to-br from-background to-muted/20">
		<div class="flex items-center justify-between flex-shrink-0 mb-8 pb-6 border-b-2 border-border/50">
			<div class="flex items-center gap-4">
				<div class="h-16 w-16 rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg">
					<Package class="h-8 w-8 text-primary" />
				</div>
				<div class="space-y-1">
					<h2 class="text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">{server.name}</h2>
					<p class="text-base text-muted-foreground">{server.description || 'No description provided'}</p>
				</div>
			</div>
			<div class="flex items-center gap-2">
				{#if server.status === 'stopped'}
					<Button 
						onclick={() => handleServerAction('start')} 
						disabled={actionLoading}
						size="default"
						class="bg-green-600 hover:bg-green-700 text-white shadow-lg hover:shadow-xl transition-all hover:scale-[1.02]"
					>
						{#if actionLoading}
							<Loader2 class="h-5 w-5 mr-2 animate-spin" />
						{:else}
							<Play class="h-5 w-5 mr-2" />
						{/if}
						Start Server
					</Button>
				{:else if server.status === 'running' || server.status === 'starting'}
					<Button 
						variant="destructive" 
						onclick={() => handleServerAction('stop')} 
						disabled={actionLoading}
						size="default"
						class="shadow-lg hover:shadow-xl transition-all hover:scale-[1.02]"
					>
						{#if actionLoading}
							<Loader2 class="h-5 w-5 mr-2 animate-spin" />
						{:else}
							<Square class="h-5 w-5 mr-2" />
						{/if}
						Stop Server
					</Button>
					{#if server.status === 'running'}
						<Button 
							variant="outline" 
							onclick={() => handleServerAction('restart')} 
							disabled={actionLoading}
							size="default"
							class="border-2 shadow-md hover:shadow-lg transition-all hover:scale-[1.02]"
						>
							{#if actionLoading}
								<Loader2 class="h-5 w-5 mr-2 animate-spin" />
							{:else}
								<RotateCw class="h-5 w-5 mr-2" />
							{/if}
							Restart
						</Button>
					{/if}
				{/if}
				<div class="ml-4 h-10 w-px bg-border/50"></div>
				<Button 
					variant="ghost" 
					onclick={() => handleDeleteServer()}
					disabled={actionLoading}
					size="default"
					class="text-destructive hover:text-white hover:bg-destructive transition-all"
				>
					<Trash2 class="h-5 w-5 mr-2" />
					Delete Server
				</Button>
			</div>
		</div>

		<div class="grid gap-6 md:grid-cols-2 lg:grid-cols-4 flex-shrink-0 mb-8">
			<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
					<CardTitle class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">Status</CardTitle>
					<div class="h-12 w-12 rounded-xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center group-hover:scale-110 transition-transform">
						<Activity class="h-6 w-6 text-primary" />
					</div>
				</CardHeader>
				<CardContent class="pt-2">
					<div class="flex items-center gap-3">
						<div class="relative">
							<div class="h-3 w-3 rounded-full {getStatusColor(server.status).replace('text-', 'bg-')}">
								{#if server.status === 'running'}
									<div class="absolute inset-0 rounded-full {getStatusColor(server.status).replace('text-', 'bg-')} animate-ping"></div>
								{/if}
							</div>
						</div>
						<Badge variant={getStatusBadgeVariant(server.status)} class="text-sm px-3 py-1 font-semibold shadow-sm">
							{server.status.toUpperCase()}
						</Badge>
					</div>
				</CardContent>
			</Card>

			<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
					<CardTitle class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">Connection</CardTitle>
					<div class="h-12 w-12 rounded-xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center group-hover:scale-110 transition-transform">
						<ExternalLink class="h-6 w-6 text-primary" />
					</div>
				</CardHeader>
				<CardContent class="pt-2">
					<div class="flex items-center justify-between p-3 rounded-xl bg-gradient-to-r from-muted/80 to-muted/40 backdrop-blur-sm border">
						<span class="font-mono text-base font-bold">
							{#if routingInfo?.proxy_enabled && (server.proxy_hostname || routingInfo?.current_route)}
								{server.proxy_hostname || routingInfo.current_route?.hostname || `localhost:${server.port}`}
							{:else}
								localhost:{server.port}
							{/if}
						</span>
						<Button
							size="icon"
							variant="ghost"
							onclick={() => {
								if (!server) return;
								const connectionString = routingInfo?.proxy_enabled && (server.proxy_hostname || routingInfo?.current_route)
									? (server.proxy_hostname || routingInfo.current_route?.hostname || `localhost:${server.port}`)
									: `localhost:${server.port}`;
								copyToClipboard(connectionString);
							}}
							class="hover:bg-primary/20 hover:text-primary"
						>
							<Copy class="h-4 w-4" />
						</Button>
					</div>
				</CardContent>
			</Card>

			<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
					<CardTitle class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">Version</CardTitle>
					<div class="h-12 w-12 rounded-xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center group-hover:scale-110 transition-transform">
						<Package class="h-6 w-6 text-primary" />
					</div>
				</CardHeader>
				<CardContent class="pt-2">
					<div class="space-y-2">
						<div class="text-3xl font-bold bg-gradient-to-r from-primary to-primary/70 bg-clip-text text-transparent">{server.mc_version}</div>
						<Badge variant="secondary" class="capitalize font-semibold px-3 py-1">
							{server.mod_loader === 'vanilla' ? '⚡ Vanilla' : server.mod_loader === 'forge' ? '🔨 Forge' : server.mod_loader === 'fabric' ? '🧵 Fabric' : server.mod_loader}
						</Badge>
					</div>
				</CardContent>
			</Card>

			<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
					<CardTitle class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">Resources</CardTitle>
					<div class="h-12 w-12 rounded-xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center group-hover:scale-110 transition-transform">
						<HardDrive class="h-6 w-6 text-primary" />
					</div>
				</CardHeader>
				<CardContent class="pt-2">
					<div class="space-y-3">
						<div>
							<div class="flex items-baseline gap-2">
								<span class="text-3xl font-bold">{(server.memory / 1024).toFixed(1)}</span>
								<span class="text-sm text-muted-foreground font-medium">GB RAM</span>
							</div>
							<div class="mt-2 h-3 bg-muted rounded-full overflow-hidden">
								<div class="h-full bg-gradient-to-r from-primary/60 to-primary/40 rounded-full transition-all duration-500" style="width: {Math.min((server.memory / 8192) * 100, 100)}%"></div>
							</div>
						</div>
						<div class="flex items-center justify-between pt-1">
							<span class="text-sm text-muted-foreground">Player Slots</span>
							<Badge variant="outline" class="font-semibold">{server.max_players}</Badge>
						</div>
					</div>
				</CardContent>
			</Card>
		</div>

		<Tabs value="overview" class="flex-1 flex flex-col min-h-0 overflow-hidden" onValueChange={(value) => activeTab = value}>
			<TabsList class="grid w-full max-w-3xl grid-cols-6 h-14 p-1 bg-muted/50 backdrop-blur-sm flex-shrink-0">
				<TabsTrigger value="overview" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium">Overview</TabsTrigger>
				<TabsTrigger value="console" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium">Console</TabsTrigger>
				<TabsTrigger value="configuration" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium">Configuration</TabsTrigger>
				<TabsTrigger value="mods" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium">Mods</TabsTrigger>
				<TabsTrigger value="files" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium">Files</TabsTrigger>
				<TabsTrigger value="routing" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium">Routing</TabsTrigger>
			</TabsList>

			<div class="flex-1 min-h-0 overflow-hidden relative">
				<TabsContent value="overview" class="absolute inset-0 overflow-y-auto space-y-4 p-4">
				<Card class="border-border/50 shadow-sm">
					<CardHeader class="pb-4">
						<CardTitle class="text-xl">Server Settings</CardTitle>
						<CardDescription>Edit your server configuration and restart to apply changes</CardDescription>
					</CardHeader>
					<CardContent>
						<ServerSettings {server} onUpdate={loadServer} />
					</CardContent>
				</Card>

				<Card class="border-border/50 shadow-sm">
					<CardHeader class="pb-4">
						<CardTitle class="text-xl">Server Information</CardTitle>
						<CardDescription>Detailed information about your server instance</CardDescription>
					</CardHeader>
					<CardContent>
						<dl class="grid grid-cols-1 gap-6 sm:grid-cols-2">
							<div class="space-y-1">
								<dt class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Server ID</dt>
								<dd class="text-sm font-mono">{server.id}</dd>
							</div>
							<div class="space-y-1">
								<dt class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Container ID</dt>
								<dd class="text-sm font-mono">{server.container_id || 'Not assigned'}</dd>
							</div>
							<div class="space-y-1">
								<dt class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Java Version</dt>
								<dd class="text-sm">{server.java_version}</dd>
							</div>
							<div class="space-y-1">
								<dt class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Data Path</dt>
								<dd class="text-sm font-mono break-all">{server.data_path}</dd>
							</div>
							<div class="space-y-1">
								<dt class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Created</dt>
								<dd class="text-sm">{new Date(server.created_at).toLocaleString()}</dd>
							</div>
							<div class="space-y-1">
								<dt class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Last Updated</dt>
								<dd class="text-sm">{new Date(server.updated_at).toLocaleString()}</dd>
							</div>
							{#if server.last_started}
								<div class="space-y-1">
									<dt class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Last Started</dt>
									<dd class="text-sm">{new Date(server.last_started).toLocaleString()}</dd>
								</div>
							{/if}
							{#if server.proxy_port}
								<div class="space-y-1">
									<dt class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Proxy Port</dt>
									<dd class="text-sm">{server.proxy_port}</dd>
								</div>
							{/if}
						</dl>
					</CardContent>
				</Card>
				</TabsContent>

				<TabsContent value="console" class="absolute inset-0">
					<ServerConsole {server} active={activeTab === 'console'} />
				</TabsContent>

				<TabsContent value="configuration" class="absolute inset-0">
					<ServerConfiguration {server} />
				</TabsContent>

				<TabsContent value="mods" class="absolute inset-0">
					<ServerMods {server} active={activeTab === 'mods'} />
				</TabsContent>

				<TabsContent value="files" class="absolute inset-0">
					<ServerFiles {server} active={activeTab === 'files'} />
				</TabsContent>

				<TabsContent value="routing" class="absolute inset-0 overflow-y-auto p-4">
					<ServerRouting {server} onUpdate={loadServer} />
				</TabsContent>
			</div>
		</Tabs>
	</div>
{:else}
	<div class="flex items-center justify-center h-96">
		<p class="text-muted-foreground">Server not found</p>
	</div>
{/if}