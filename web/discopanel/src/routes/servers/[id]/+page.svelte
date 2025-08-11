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
	import ScrollToTop from '$lib/components/scroll-to-top.svelte';
	import { toast } from 'svelte-sonner';
	import { Play, Square, RotateCw, Terminal, Settings, Package, HardDrive, Activity, Loader2, Copy, ExternalLink, Trash2, Cpu, Info } from '@lucide/svelte';
	import type { Server } from '$lib/api/types';
	import { formatBytes } from '$lib/utils';
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

	async function copyToClipboard(text?: string) {
		if (!text) return;
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
	<div class="h-full flex flex-col p-4 sm:p-6 lg:p-8 pt-4 sm:pt-6 bg-gradient-to-br from-background to-muted/20">
		<div class="flex flex-col sm:flex-row items-start sm:items-center justify-between flex-shrink-0 mb-4 sm:mb-6 pb-4 sm:pb-6 border-b-2 border-border/50 gap-4">
			<div class="flex items-center gap-3 sm:gap-4">
				<div class="h-12 w-12 sm:h-16 sm:w-16 rounded-xl sm:rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg">
					<Package class="h-6 w-6 sm:h-8 sm:w-8 text-primary" />
				</div>
				<div class="space-y-1">
					<h2 class="text-2xl sm:text-3xl lg:text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">{server.name}</h2>
					<p class="text-sm sm:text-base text-muted-foreground">{server.description || ''}</p>
					{#if server.description || (!server.description || server.description === '')}
						<p class="text-xs text-muted-foreground/70 mt-1">
							Created {new Date(server.created_at).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}
							{#if server.last_started}
								• Last started {(() => {
									const date = new Date(server.last_started);
									const now = new Date();
									const diff = now.getTime() - date.getTime();
									const hours = Math.floor(diff / (1000 * 60 * 60));
									if (hours < 1) return 'just now';
									if (hours < 24) return `${hours}h ago`;
									const days = Math.floor(hours / 24);
									if (days === 1) return 'yesterday';
									if (days < 7) return `${days}d ago`;
									return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
								})()}
							{/if}
						</p>
					{/if}
				</div>
			</div>
			<div class="flex flex-wrap items-center gap-2 w-full sm:w-auto">
				{#if server.status === 'stopped' || !server.container_id}
					<Button 
						onclick={() => handleServerAction('start')} 
						disabled={actionLoading || server.status === 'starting' || server.status === 'stopping'}
						size="default"
						class="bg-green-600 hover:bg-green-700 text-white shadow-lg hover:shadow-xl transition-all hover:scale-[1.02]"
					>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2 animate-spin" />
						{:else}
							<Play class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2" />
						{/if}
						<span class="sm:inline">Start</span>
					</Button>
				{:else if server.status === 'error'}
					<Button 
						onclick={() => handleServerAction('restart')} 
						disabled={actionLoading}
						size="default"
						class="bg-yellow-600 hover:bg-yellow-700 text-white shadow-lg hover:shadow-xl transition-all hover:scale-[1.02]"
					>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2 animate-spin" />
						{:else}
							<RotateCw class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2" />
						{/if}
						<span class="sm:inline">Restart</span>
					</Button>
					<Button 
						variant="destructive" 
						onclick={() => handleServerAction('stop')} 
						disabled={actionLoading}
						size="default"
						class="shadow-lg hover:shadow-xl transition-all hover:scale-[1.02]"
					>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2 animate-spin" />
						{:else}
							<Square class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2" />
						{/if}
						<span class="sm:inline">Stop</span>
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
							<Loader2 class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2 animate-spin" />
						{:else}
							<Square class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2" />
						{/if}
						<span class="sm:inline">Stop</span>
					</Button>
					<Button 
						variant="outline" 
						onclick={() => handleServerAction('restart')} 
						disabled={actionLoading}
						size="default"
						class="border-2 shadow-md hover:shadow-lg transition-all hover:scale-[1.02] hidden sm:flex"
					>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2 animate-spin" />
						{:else}
							<RotateCw class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2" />
						{/if}
						Restart
					</Button>
				{:else if server.status === 'stopping'}
					<Button 
						variant="secondary" 
						disabled={true}
						size="default"
						class="shadow-lg"
					>
						<Loader2 class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2 animate-spin" />
						<span class="sm:inline">Stopping...</span>
					</Button>
				{/if}
				<div class="ml-2 sm:ml-4 h-10 w-px bg-border/50 hidden sm:block"></div>
				<Button 
					variant="ghost" 
					onclick={() => handleDeleteServer()}
					disabled={actionLoading}
					size="default"
					class="text-destructive hover:text-white hover:bg-destructive transition-all hidden sm:flex"
				>
					<Trash2 class="h-4 w-4 sm:h-5 sm:w-5 mr-1 sm:mr-2" />
					<span class="hidden lg:inline">Delete Server</span>
					<span class="lg:hidden">Delete</span>
				</Button>
			</div>
		</div>

		<div class="grid gap-4 sm:gap-5 grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 flex-shrink-0 mb-6 sm:mb-8">
			<Card class="group relative overflow-hidden border-0 shadow-xl hover:shadow-2xl transition-all duration-500 bg-gradient-to-br from-background via-background/95 to-background/90 hover:-translate-y-1">
				{#if server.status === 'running'}
					<div class="absolute inset-0 bg-gradient-to-br from-green-500/5 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
					<div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent via-green-500/50 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				{:else if server.status === 'unhealthy'}
					<div class="absolute inset-0 bg-gradient-to-br from-red-500/5 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
					<div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent via-red-500/50 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				{:else if server.status === 'stopped'}
					<div class="absolute inset-0 bg-gradient-to-br from-gray-500/5 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
					<div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent via-gray-500/50 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				{:else if server.status === 'starting'}
					<div class="absolute inset-0 bg-gradient-to-br from-yellow-500/5 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
					<div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent via-yellow-500/50 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				{:else}
					<div class="absolute inset-0 bg-gradient-to-br from-orange-500/5 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
					<div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent via-orange-500/50 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				{/if}
				
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
					<div class="space-y-1">
						<CardTitle class="text-xs font-bold text-muted-foreground/70 uppercase tracking-widest">Server Status</CardTitle>
						<p class="text-xs text-muted-foreground/50">Live monitoring</p>
					</div>
					<div class="relative">
						{#if server.status === 'running'}
							<div class="absolute inset-0 bg-gradient-to-br from-green-500/20 to-green-600/20 rounded-2xl blur-xl opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
							<div class="relative h-14 w-14 rounded-2xl bg-gradient-to-br from-green-500/10 to-green-600/10 flex items-center justify-center group-hover:scale-110 group-hover:rotate-3 transition-all duration-500">
								<div class="relative">
									<Activity class="h-7 w-7 text-green-500" />
								</div>
							</div>
						{:else if server.status === 'unhealthy'}
							<div class="absolute inset-0 bg-gradient-to-br from-red-500/20 to-red-600/20 rounded-2xl blur-xl opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
							<div class="relative h-14 w-14 rounded-2xl bg-gradient-to-br from-red-500/10 to-red-600/10 flex items-center justify-center group-hover:scale-110 group-hover:rotate-3 transition-all duration-500">
								<Activity class="h-7 w-7 text-red-500 animate-pulse" />
							</div>
						{:else if server.status === 'stopped'}
							<div class="absolute inset-0 bg-gradient-to-br from-gray-500/20 to-gray-600/20 rounded-2xl blur-xl opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
							<div class="relative h-14 w-14 rounded-2xl bg-gradient-to-br from-gray-500/10 to-gray-600/10 flex items-center justify-center group-hover:scale-110 group-hover:rotate-3 transition-all duration-500">
								<Square class="h-7 w-7 text-gray-500" />
							</div>
						{:else if server.status === 'starting'}
							<div class="absolute inset-0 bg-gradient-to-br from-yellow-500/20 to-yellow-600/20 rounded-2xl blur-xl opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
							<div class="relative h-14 w-14 rounded-2xl bg-gradient-to-br from-yellow-500/10 to-yellow-600/10 flex items-center justify-center group-hover:scale-110 group-hover:rotate-3 transition-all duration-500">
								<Loader2 class="h-7 w-7 text-yellow-500 animate-spin" />
							</div>
						{:else}
							<div class="absolute inset-0 bg-gradient-to-br from-orange-500/20 to-orange-600/20 rounded-2xl blur-xl opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
							<div class="relative h-14 w-14 rounded-2xl bg-gradient-to-br from-orange-500/10 to-orange-600/10 flex items-center justify-center group-hover:scale-110 group-hover:rotate-3 transition-all duration-500">
								<RotateCw class="h-7 w-7 text-orange-500 animate-pulse" />
							</div>
						{/if}
					</div>
				</CardHeader>
				<CardContent class="pt-1">
					<div class="space-y-4">
						<div class="relative">
							<div class="flex items-center justify-center h-20 rounded-xl bg-gradient-to-br from-muted/30 to-muted/10 border border-border/30 overflow-hidden">
								{#if server.status === 'running'}
									<div class="heartbeat-container">
										{#each Array(5) as _, i}
											<div class="heartbeat-bar bg-green-500" style="animation-delay: {i * 0.15}s"></div>
										{/each}
									</div>
								{:else if server.status === 'unhealthy'}
									<div class="heartbeat-container">
										{#each Array(5) as _, i}
											<div class="heartbeat-bar heartbeat-erratic bg-red-500" style="animation-delay: {i * 0.1}s; height: {20 + Math.random() * 30}px"></div>
										{/each}
									</div>
								{:else if server.status === 'stopped'}
									<div class="w-full h-0.5 bg-gray-500/50"></div>
								{:else if server.status === 'starting'}
									<div class="heartbeat-container">
										{#each Array(5) as _, i}
											<div class="heartbeat-bar heartbeat-slow bg-yellow-500" style="animation-delay: {i * 0.2}s"></div>
										{/each}
									</div>
								{:else}
									<div class="heartbeat-container">
										{#each Array(5) as _, i}
											<div class="heartbeat-bar heartbeat-slow bg-orange-500" style="animation-delay: {i * 0.25}s"></div>
										{/each}
									</div>
								{/if}
							</div>
						</div>
						
						<div class="text-center space-y-2">
							<div class="text-2xl font-bold">
								{#if server.status === 'running'}
									<span class="text-green-500">RUNNING</span>
								{:else if server.status === 'unhealthy'}
									<span class="text-red-500">UNHEALTHY</span>
								{:else if server.status === 'stopped'}
									<span class="text-gray-500">STOPPED</span>
								{:else if server.status === 'starting'}
									<span class="text-yellow-500">STARTING</span>
								{:else if server.status === 'stopping'}
									<span class="text-orange-500">STOPPING</span>
								{:else}
									<span class="text-muted-foreground">{server.status.toUpperCase()}</span>
								{/if}
							</div>
							<p class="text-xs text-muted-foreground/70">
								{#if server.status === 'running'}
									Server healthy and responding
								{:else if server.status === 'unhealthy'}
									Server experiencing issues
								{:else if server.status === 'stopped'}
									Server is currently offline
								{:else if server.status === 'starting'}
									Initializing server components
								{:else if server.status === 'stopping'}
									Shutting down gracefully
								{:else}
									Status: {server.status}
								{/if}
							</p>
						</div>
					</div>
				</CardContent>
			</Card>

			<Card class="group relative overflow-hidden border-0 shadow-xl hover:shadow-2xl transition-all duration-500 bg-gradient-to-br from-background via-background/95 to-background/90 hover:-translate-y-1">
				<div class="absolute inset-0 bg-gradient-to-br from-blue-500/5 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				<div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent via-blue-500/50 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
					<div class="space-y-1">
						<CardTitle class="text-xs font-bold text-muted-foreground/70 uppercase tracking-widest">Connection</CardTitle>
						<p class="text-xs text-muted-foreground/50">Server address</p>
					</div>
					<div class="relative">
						<div class="absolute inset-0 bg-gradient-to-br from-blue-500/20 to-blue-600/20 rounded-2xl blur-xl opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
						<div class="relative h-14 w-14 rounded-2xl bg-gradient-to-br from-blue-500/10 to-blue-600/10 flex items-center justify-center group-hover:scale-110 group-hover:rotate-3 transition-all duration-500">
							<ExternalLink class="h-7 w-7 text-blue-500 group-hover:animate-pulse" />
						</div>
					</div>
				</CardHeader>
				<CardContent class="pt-1">
					<div class="relative group/copy">
						<div class="absolute inset-0 bg-gradient-to-r from-blue-500/10 to-purple-500/10 rounded-xl blur-xl opacity-0 group-hover/copy:opacity-100 transition-opacity duration-500"></div>
						<div class="relative flex items-center justify-between p-3 rounded-xl bg-gradient-to-r from-muted/50 to-muted/30 backdrop-blur-sm border border-border/50 group-hover/copy:border-primary/30 transition-all duration-300">
							<div class="flex-1 min-w-0">
								<span class="font-mono text-sm font-bold block truncate text-foreground/90">
									{#if routingInfo?.proxy_enabled && (server.proxy_hostname || routingInfo?.current_route)}
										{server.proxy_hostname || routingInfo.current_route?.hostname || `localhost:${server.port}`}
									{:else}
										localhost:{server.port}
									{/if}
								</span>
								<span class="text-xs text-muted-foreground/60 mt-1 block">Click to copy</span>
							</div>
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
								class="hover:bg-primary/20 hover:text-primary transition-all duration-300 hover:scale-110"
							>
								<Copy class="h-4 w-4" />
							</Button>
						</div>
					</div>
				</CardContent>
			</Card>

			<Card class="group relative overflow-hidden border-0 shadow-xl hover:shadow-2xl transition-all duration-500 bg-gradient-to-br from-background via-background/95 to-background/90 hover:-translate-y-1">
				<div class="absolute inset-0 bg-gradient-to-br from-purple-500/5 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				<div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent via-purple-500/50 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
					<div class="space-y-1">
						<CardTitle class="text-xs font-bold text-muted-foreground/70 uppercase tracking-widest">Server Info</CardTitle>
						<p class="text-xs text-muted-foreground/50">Details & versions</p>
					</div>
					<div class="relative">
						<div class="absolute inset-0 bg-gradient-to-br from-purple-500/20 to-purple-600/20 rounded-2xl blur-xl opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
						<div class="relative h-14 w-14 rounded-2xl bg-gradient-to-br from-purple-500/10 to-purple-600/10 flex items-center justify-center group-hover:scale-110 group-hover:rotate-3 transition-all duration-500">
							<Info class="h-7 w-7 text-purple-500 group-hover:animate-pulse" />
						</div>
					</div>
				</CardHeader>
				<CardContent class="pt-1">
					<div class="space-y-2.5 max-h-[180px] overflow-y-auto scrollbar-thin">
						<!-- Versions Section -->
						<div class="space-y-1.5">
							<div class="flex items-center justify-between">
								<span class="text-[10px] text-muted-foreground/60">Minecraft</span>
								<span class="text-[11px] font-mono font-semibold text-purple-500">{server.mc_version}</span>
							</div>
							<div class="flex items-center justify-between">
								<span class="text-[10px] text-muted-foreground/60">Java</span>
								<span class="text-[11px] font-mono font-semibold text-purple-400">Java {server.java_version}</span>
							</div>
							<div class="flex items-center justify-between">
								<span class="text-[10px] text-muted-foreground/60">Mod Loader</span>
								{#if server.mod_loader === 'vanilla'}
									<Badge variant="secondary" class="text-[10px] px-1.5 py-0 h-4 bg-yellow-500/10 border-yellow-500/20">
										⚡ Vanilla
									</Badge>
								{:else if server.mod_loader.toLowerCase().includes('forge')}
									<Badge variant="secondary" class="text-[10px] px-1.5 py-0 h-4 bg-orange-500/10 border-orange-500/20">
										🔨 {server.mod_loader[0].toUpperCase() + server.mod_loader.slice(1)}
									</Badge>
								{:else if server.mod_loader === 'fabric'}
									<Badge variant="secondary" class="text-[10px] px-1.5 py-0 h-4 bg-blue-500/10 border-blue-500/20">
										🧵 Fabric
									</Badge>
								{:else}
									<Badge variant="secondary" class="capitalize text-[10px] px-1.5 py-0 h-4">
										{server.mod_loader}
									</Badge>
								{/if}
							</div>
						</div>
						
						<!-- IDs Section -->
						<div class="pt-1.5 border-t border-border/20 space-y-1.5">
							<div class="flex items-center justify-between group/copy cursor-pointer" 
								onclick={() => copyToClipboard(server?.id)}>
								<span class="text-[10px] text-muted-foreground/60">Server ID</span>
								<div class="flex items-center gap-1">
									<span class="text-[10px] font-mono text-muted-foreground/70 truncate max-w-[80px]">
										{server.id}
									</span>
									<Copy class="h-2.5 w-2.5 text-muted-foreground/40 opacity-0 group-hover/copy:opacity-100 transition-opacity" />
								</div>
							</div>
							{#if server.container_id}
								<div class="flex items-center justify-between group/copy cursor-pointer" 
									onclick={() => copyToClipboard(server?.container_id)}>
									<span class="text-[10px] text-muted-foreground/60">Container</span>
									<div class="flex items-center gap-1">
										<span class="text-[10px] font-mono text-muted-foreground/70 truncate max-w-[80px]">
											{server.container_id}
										</span>
										<Copy class="h-2.5 w-2.5 text-muted-foreground/40 opacity-0 group-hover/copy:opacity-100 transition-opacity" />
									</div>
								</div>
							{/if}
						</div>
						
						<!-- Data Path (tooltip on hover) -->
						<div class="pt-1.5 border-t border-border/20">
							<div class="flex items-center justify-between group/path">
								<span class="text-[10px] text-muted-foreground/60">Data Path</span>
								<span class="text-[10px] font-mono text-muted-foreground/70 truncate max-w-[100px]" title={server.data_path}>
									.../{server.data_path.split('/').slice(-2).join('/')}
								</span>
							</div>
						</div>
					</div>
				</CardContent>
			</Card>

			<Card class="group relative overflow-hidden border-0 shadow-xl hover:shadow-2xl transition-all duration-500 bg-gradient-to-br from-background via-background/95 to-background/90 hover:-translate-y-1">
				<div class="absolute inset-0 bg-gradient-to-br from-orange-500/5 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				<div class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent via-orange-500/50 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
					<div class="space-y-1">
						<CardTitle class="text-xs font-bold text-muted-foreground/70 uppercase tracking-widest">Resources</CardTitle>
						<p class="text-xs text-muted-foreground/50">System usage</p>
					</div>
					<div class="relative">
						<div class="absolute inset-0 bg-gradient-to-br from-orange-500/20 to-orange-600/20 rounded-2xl blur-xl opacity-0 group-hover:opacity-100 transition-opacity duration-500"></div>
						<div class="relative h-14 w-14 rounded-2xl bg-gradient-to-br from-orange-500/10 to-orange-600/10 flex items-center justify-center group-hover:scale-110 group-hover:rotate-3 transition-all duration-500">
							<Cpu class="h-7 w-7 text-orange-500 group-hover:animate-pulse" />
						</div>
					</div>
				</CardHeader>
				<CardContent class="pt-1">
					<div class="space-y-3">
						<!-- Memory Usage -->
						<div>
							<div class="flex items-center justify-between mb-1.5">
								<span class="text-xs font-semibold text-muted-foreground/70">MEMORY</span>
								{#if server.memory_usage}
									<span class="text-xs font-mono text-orange-500">
										{(server.memory_usage / 1024).toFixed(2)} / {(server.memory / 1024).toFixed(1)} GB
									</span>
								{:else}
									<span class="text-xs font-mono text-muted-foreground/50">
										{(server.memory / 1024).toFixed(1)} GB allocated
									</span>
								{/if}
							</div>
							<div class="relative h-3 bg-gradient-to-r from-muted/50 to-muted/30 rounded-full overflow-hidden">
								{#if server.memory_usage}
									<div class="relative h-full bg-gradient-to-r from-orange-500 to-yellow-500 rounded-full transition-all duration-700" 
										style="width: {Math.min((server.memory_usage / server.memory) * 100, 100)}%">
										<div class="absolute inset-0 bg-gradient-to-r from-transparent via-white/20 to-transparent"></div>
									</div>
								{:else}
									<div class="h-full bg-muted/50"></div>
								{/if}
							</div>
							{#if server.memory_usage}
								<p class="text-[10px] text-muted-foreground/50 mt-1">
									{((server.memory_usage / server.memory) * 100).toFixed(1)}% used
								</p>
							{/if}
						</div>

						<!-- CPU Usage -->
						<div>
							<div class="flex items-center justify-between mb-1.5">
								<span class="text-xs font-semibold text-muted-foreground/70">CPU</span>
								{#if server.cpu_percent !== undefined}
									<span class="text-xs font-mono text-blue-500">{server.cpu_percent.toFixed(1)}%</span>
								{:else}
									<span class="text-xs font-mono text-muted-foreground/50">--</span>
								{/if}
							</div>
							<div class="relative h-3 bg-gradient-to-r from-muted/50 to-muted/30 rounded-full overflow-hidden">
								{#if server.cpu_percent !== undefined}
									<div class="relative h-full bg-gradient-to-r from-blue-500 to-cyan-500 rounded-full transition-all duration-700" 
										style="width: {Math.min(server.cpu_percent, 100)}%">
										<div class="absolute inset-0 bg-gradient-to-r from-transparent via-white/20 to-transparent"></div>
									</div>
								{:else}
									<div class="h-full bg-muted/50"></div>
								{/if}
							</div>
						</div>

						<!-- Disk Usage -->
						<div>
							<div class="flex items-center justify-between mb-1.5">
								<span class="text-xs font-semibold text-muted-foreground/70">STORAGE</span>
								{#if server.disk_usage !== undefined && server.disk_usage > 0}
									<span class="text-xs font-mono text-purple-500">{formatBytes(server.disk_usage)} (world)</span>
								{:else}
									<span class="text-xs font-mono text-muted-foreground/50">--</span>
								{/if}
							</div>
							<div class="relative h-3 bg-gradient-to-r from-muted/50 to-muted/30 rounded-full overflow-hidden">
								{#if server.disk_usage !== undefined && server.disk_usage > 0 && server.disk_total}
									{@const diskPercent = (server.disk_usage / server.disk_total) * 100}
									<div class="relative h-full bg-gradient-to-r from-purple-500 to-pink-500 rounded-full transition-all duration-700" 
										style="width: {Math.min(diskPercent, 100)}%">
										<div class="absolute inset-0 bg-gradient-to-r from-transparent via-white/20 to-transparent"></div>
									</div>
								{:else}
									<div class="h-full bg-muted/50"></div>
								{/if}
							</div>
							{#if server.disk_usage !== undefined && server.disk_usage > 0}
								<p class="text-[10px] text-muted-foreground/50 mt-1">
									{#if server.disk_total}
										{((server.disk_usage / server.disk_total) * 100).toFixed(1)}% of {formatBytes(server.disk_total)} used
									{/if}
								</p>
							{/if}
						</div>

					</div>
				</CardContent>
			</Card>
		</div>

		<Tabs value="overview" class="flex-1 flex flex-col min-h-0 gap-4" onValueChange={(value) => {
			activeTab = value
		}}>
			<div class="flex-shrink-0 w-full overflow-x-auto">
				<TabsList class="inline-flex w-full min-w-max sm:grid sm:grid-cols-6 h-12 sm:h-14 p-1 bg-muted/50 backdrop-blur-sm">
					<TabsTrigger value="overview" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium text-xs sm:text-sm whitespace-nowrap px-3 sm:px-4">Overview</TabsTrigger>
					<TabsTrigger value="console" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium text-xs sm:text-sm whitespace-nowrap px-3 sm:px-4">Console</TabsTrigger>
					<TabsTrigger value="configuration" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium text-xs sm:text-sm whitespace-nowrap px-3 sm:px-4">Config</TabsTrigger>
					<TabsTrigger value="mods" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium text-xs sm:text-sm whitespace-nowrap px-3 sm:px-4">Mods</TabsTrigger>
					<TabsTrigger value="files" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium text-xs sm:text-sm whitespace-nowrap px-3 sm:px-4">Files</TabsTrigger>
					<TabsTrigger value="routing" class="data-[state=active]:bg-background data-[state=active]:shadow-lg data-[state=active]:text-foreground font-medium text-xs sm:text-sm whitespace-nowrap px-3 sm:px-4">Routing</TabsTrigger>
				</TabsList>
			</div>

			<div class="flex-1 min-h-0 overflow-hidden">
				<TabsContent value="overview" class="h-full space-y-4">
					<Card class="border-border/50 shadow-sm">
						<CardHeader class="pb-4">
							<CardTitle class="text-xl">Server Settings</CardTitle>
							<CardDescription>Edit your server configuration and restart to apply changes</CardDescription>
						</CardHeader>
						<CardContent>
							<ServerSettings {server} onUpdate={loadServer} />
						</CardContent>
					</Card>
				</TabsContent>

				<TabsContent value="console" class="h-full">
					<ServerConsole {server} active={activeTab === 'console'} />
				</TabsContent>

				<TabsContent value="configuration" class="h-full overflow-y-auto">
					<ServerConfiguration {server} />
				</TabsContent>

				<TabsContent value="mods" class="h-full">
					<ServerMods {server} active={activeTab === 'mods'} />
				</TabsContent>

				<TabsContent value="files" class="h-full">
					<ServerFiles {server} active={activeTab === 'files'} />
				</TabsContent>

				<TabsContent value="routing" class="h-full overflow-y-auto">
					<ServerRouting {server} active={activeTab === 'routing'} />
				</TabsContent>
			</div>
		</Tabs>
	</div>
{:else}
	<div class="flex items-center justify-center h-96">
		<p class="text-muted-foreground">Server not found</p>
	</div>
{/if}

<ScrollToTop />

<style>
	@keyframes shimmer {
		0% { transform: translateX(-100%); }
		100% { transform: translateX(100%); }
	}
	.animate-shimmer {
		animation: shimmer 2s infinite;
	}
	@keyframes gradient-x {
		0%, 100% { background-position: 0% 50%; }
		50% { background-position: 100% 50%; }
	}
	
	.heartbeat-container {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 4px;
		height: 100%;
	}
	
	.heartbeat-bar {
		width: 4px;
		height: 40px;
		border-radius: 2px;
		animation: heartbeat 4s ease-in-out infinite;
	}
	
	.heartbeat-bar.heartbeat-erratic {
		animation: heartbeat-erratic 1.8s ease-in-out infinite;
	}
	
	.heartbeat-bar.heartbeat-slow {
		animation: heartbeat-slow 3.5s ease-in-out infinite;
	}
	
	@keyframes heartbeat {
		0%, 100% {
			height: 8px;
			opacity: 0.3;
		}
		50% {
			height: 40px;
			opacity: 1;
		}
	}
	
	@keyframes heartbeat-erratic {
		0%, 100% {
			height: 5px;
			opacity: 0.2;
		}
		25% {
			height: 25px;
			opacity: 0.8;
		}
		50% {
			height: 15px;
			opacity: 0.5;
		}
		75% {
			height: 35px;
			opacity: 0.9;
		}
	}
	
	@keyframes heartbeat-slow {
		0%, 100% {
			height: 8px;
			opacity: 0.2;
		}
		50% {
			height: 30px;
			opacity: 0.7;
		}
	}
</style>