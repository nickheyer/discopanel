<script lang="ts">
	import { page } from '$app/stores';
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

	let server = $state<Server | null>(null);
	let loading = $state(true);
	let actionLoading = $state(false);
	let serverId = $derived($page.params.id);

	onMount(() => {
		loadServer();
		const interval = setInterval(loadServer, 5000); // Poll every 5 seconds
		return () => clearInterval(interval);
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
	<div class="h-[calc(100vh-4rem)] flex flex-col p-8 pt-6">
		<div class="flex items-center justify-between flex-shrink-0 mb-4">
			<div>
				<h2 class="text-3xl font-bold tracking-tight">{server.name}</h2>
				<p class="text-muted-foreground">{server.description || 'No description'}</p>
			</div>
			<div class="flex items-center space-x-2">
				{#if server.status === 'stopped'}
					<Button onclick={() => handleServerAction('start')} disabled={actionLoading}>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 mr-2 animate-spin" />
						{:else}
							<Play class="h-4 w-4 mr-2" />
						{/if}
						Start
					</Button>
				{:else if server.status === 'running'}
					<Button variant="destructive" onclick={() => handleServerAction('stop')} disabled={actionLoading}>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 mr-2 animate-spin" />
						{:else}
							<Square class="h-4 w-4 mr-2" />
						{/if}
						Stop
					</Button>
					<Button variant="outline" onclick={() => handleServerAction('restart')} disabled={actionLoading}>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 mr-2 animate-spin" />
						{:else}
							<RotateCw class="h-4 w-4 mr-2" />
						{/if}
						Restart
					</Button>
				{/if}
				<Button 
					variant="outline" 
					onclick={() => handleDeleteServer()}
					disabled={actionLoading || server.status !== 'stopped'}
				>
					<Trash2 class="h-4 w-4 mr-2" />
					Delete
				</Button>
			</div>
		</div>

		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4 flex-shrink-0 mb-4">
			<Card>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
					<CardTitle class="text-sm font-medium">Status</CardTitle>
					<Activity class="h-4 w-4 text-muted-foreground" />
				</CardHeader>
				<CardContent>
					<div class="flex items-center space-x-2">
						<div class="h-2 w-2 rounded-full {getStatusColor(server.status).replace('text-', 'bg-')}"></div>
						<Badge variant={getStatusBadgeVariant(server.status)}>
							{server.status}
						</Badge>
					</div>
				</CardContent>
			</Card>

			<Card>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
					<CardTitle class="text-sm font-medium">Connection</CardTitle>
					<ExternalLink class="h-4 w-4 text-muted-foreground" />
				</CardHeader>
				<CardContent>
					<div class="flex items-center justify-between">
						<span class="font-mono text-sm">localhost:{server.port}</span>
						<Button
							size="icon"
							variant="ghost"
							onclick={() => copyToClipboard(`localhost:${server?.port ?? ''}`)}
						>
							<Copy class="h-3 w-3" />
						</Button>
					</div>
				</CardContent>
			</Card>

			<Card>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
					<CardTitle class="text-sm font-medium">Version</CardTitle>
					<Package class="h-4 w-4 text-muted-foreground" />
				</CardHeader>
				<CardContent>
					<div class="text-sm">
						<div>{server.mc_version}</div>
						<div class="text-muted-foreground capitalize">{server.mod_loader}</div>
					</div>
				</CardContent>
			</Card>

			<Card>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
					<CardTitle class="text-sm font-medium">Resources</CardTitle>
					<HardDrive class="h-4 w-4 text-muted-foreground" />
				</CardHeader>
				<CardContent>
					<div class="text-sm">
						<div>{server.memory} MB RAM</div>
						<div class="text-muted-foreground">{server.max_players} max players</div>
					</div>
				</CardContent>
			</Card>
		</div>

		<Tabs value="overview" class="flex-1 flex flex-col min-h-0">
			<TabsList>
				<TabsTrigger value="overview">Overview</TabsTrigger>
				<TabsTrigger value="console">Console</TabsTrigger>
				<TabsTrigger value="configuration">Configuration</TabsTrigger>
				<TabsTrigger value="mods">Mods</TabsTrigger>
				<TabsTrigger value="files">Files</TabsTrigger>
			</TabsList>

			<TabsContent value="overview" class="space-y-4">
				<Card>
					<CardHeader>
						<CardTitle>Server Information</CardTitle>
						<CardDescription>Detailed information about your server</CardDescription>
					</CardHeader>
					<CardContent>
						<dl class="grid grid-cols-1 gap-4 sm:grid-cols-2">
							<div>
								<dt class="text-sm font-medium text-muted-foreground">Server ID</dt>
								<dd class="mt-1 text-sm font-mono">{server.id}</dd>
							</div>
							<div>
								<dt class="text-sm font-medium text-muted-foreground">Container ID</dt>
								<dd class="mt-1 text-sm font-mono">{server.container_id || 'Not assigned'}</dd>
							</div>
							<div>
								<dt class="text-sm font-medium text-muted-foreground">Java Version</dt>
								<dd class="mt-1 text-sm">{server.java_version}</dd>
							</div>
							<div>
								<dt class="text-sm font-medium text-muted-foreground">Data Path</dt>
								<dd class="mt-1 text-sm font-mono break-all">{server.data_path}</dd>
							</div>
							<div>
								<dt class="text-sm font-medium text-muted-foreground">Created</dt>
								<dd class="mt-1 text-sm">{new Date(server.created_at).toLocaleString()}</dd>
							</div>
							<div>
								<dt class="text-sm font-medium text-muted-foreground">Last Updated</dt>
								<dd class="mt-1 text-sm">{new Date(server.updated_at).toLocaleString()}</dd>
							</div>
							{#if server.last_started}
								<div>
									<dt class="text-sm font-medium text-muted-foreground">Last Started</dt>
									<dd class="mt-1 text-sm">{new Date(server.last_started).toLocaleString()}</dd>
								</div>
							{/if}
							{#if server.proxy_port}
								<div>
									<dt class="text-sm font-medium text-muted-foreground">Proxy Port</dt>
									<dd class="mt-1 text-sm">{server.proxy_port}</dd>
								</div>
							{/if}
						</dl>
					</CardContent>
				</Card>
			</TabsContent>

			<TabsContent value="console" class="h-[600px]">
				<ServerConsole {server} />
			</TabsContent>

			<TabsContent value="configuration">
				<Card>
					<CardHeader>
						<CardTitle>Server Configuration</CardTitle>
						<CardDescription>Manage server.properties and other settings</CardDescription>
					</CardHeader>
					<CardContent>
						<div class="flex items-center justify-center py-8 text-muted-foreground">
							<Settings class="h-8 w-8 mr-2" />
							<span>Configuration feature coming soon</span>
						</div>
					</CardContent>
				</Card>
			</TabsContent>

			<TabsContent value="mods">
				<Card>
					<CardHeader>
						<CardTitle>Mod Management</CardTitle>
						<CardDescription>Add, remove, and configure mods</CardDescription>
					</CardHeader>
					<CardContent>
						<div class="flex items-center justify-center py-8 text-muted-foreground">
							<Package class="h-8 w-8 mr-2" />
							<span>Mod management feature coming soon</span>
						</div>
					</CardContent>
				</Card>
			</TabsContent>

			<TabsContent value="files">
				<Card>
					<CardHeader>
						<CardTitle>File Manager</CardTitle>
						<CardDescription>Browse and manage server files</CardDescription>
					</CardHeader>
					<CardContent>
						<div class="flex items-center justify-center py-8 text-muted-foreground">
							<HardDrive class="h-8 w-8 mr-2" />
							<span>File management feature coming soon</span>
						</div>
					</CardContent>
				</Card>
			</TabsContent>
		</Tabs>
	</div>
{:else}
	<div class="flex items-center justify-center h-96">
		<p class="text-muted-foreground">Server not found</p>
	</div>
{/if}