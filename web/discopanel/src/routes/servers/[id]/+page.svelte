<script lang="ts">
	import { page } from '$app/state';
	import { onMount, untrack } from 'svelte';
	import { slide } from 'svelte/transition';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { serversStore } from '$lib/stores/servers';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import ScrollToTop from '$lib/components/scroll-to-top.svelte';
	import { toast } from 'svelte-sonner';
	import {
		Play,
		Square,
		RotateCw,
		RefreshCcw,
		MoreVertical,
		Package,
		Activity,
		Loader2,
		Copy,
		ExternalLink,
		Trash2,
		Cpu,
		Info,
		ChevronDown
	} from '@lucide/svelte';
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger
	} from '$lib/components/ui/dropdown-menu';
	import { create } from '@bufbuild/protobuf';
	import type { Timestamp } from '@bufbuild/protobuf/wkt';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import { ServerStatus, ModLoader } from '$lib/proto/discopanel/v1/common_pb';
	import type { GetServerRoutingResponse } from '$lib/proto/discopanel/v1/proxy_pb';
	import {
		GetServerRequestSchema,
		DeleteServerRequestSchema,
		StartServerRequestSchema,
		StopServerRequestSchema,
		RestartServerRequestSchema,
		RecreateServerRequestSchema
	} from '$lib/proto/discopanel/v1/server_pb';
	import { formatBytes } from '$lib/utils';
	import { copyToClipboard as copyText } from '$lib/utils/clipboard';
	import ServerConsole from '$lib/components/server-console.svelte';
	import ServerConfiguration from '$lib/components/server-configuration.svelte';
	import ServerSettings from '$lib/components/server-settings.svelte';
	import ServerMods from '$lib/components/server-mods.svelte';
	import ServerFiles from '$lib/components/files/server-files.svelte';
	import ServerRouting from '$lib/components/server-routing.svelte';
	import ServerTasks from '$lib/components/server-tasks.svelte';
	import ServerModules from '$lib/components/server/ServerModules.svelte';
	import ServerPerformance from '$lib/components/server-performance.svelte';
	import ServerMetricsCharts from '$lib/components/server-metrics-charts.svelte';

	let server = $state<Server | null>(null);
	let loading = $state(true);
	let actionLoading = $state(false);
	let serverId = $derived(page.params.id);
	let prevServerId = $state<string | undefined>(undefined);
	let activeTab = $state('overview');
	let routingInfo = $state<GetServerRoutingResponse | null>(null);

	let interval: ReturnType<typeof setInterval> | undefined;

	// Metrics panel expands below the stats cards
	let showMetrics = $state(
		typeof localStorage !== 'undefined' &&
			localStorage.getItem('discopanel.metricsPanel') === 'open'
	);

	function toggleMetricsPanel() {
		showMetrics = !showMetrics;
		try {
			localStorage.setItem('discopanel.metricsPanel', showMetrics ? 'open' : 'closed');
		} catch {
			// View preference is best effort
		}
	}

	// Helper function to convert protobuf Timestamp to Date
	function timestampToDate(timestamp: Timestamp | undefined): Date {
		if (!timestamp) return new Date();
		// Protobuf timestamp has seconds as bigint and nanos as number
		return new Date(Number(timestamp.seconds) * 1000 + timestamp.nanos / 1_000_000);
	}

	onMount(() => {
		return () => {
			if (interval) clearInterval(interval);
		};
	});

	$effect(() => {
		if (serverId) {
			if (interval) clearInterval(interval);
			// Reset state when switching servers
			const prev = untrack(() => prevServerId);
			if (prev !== serverId) {
				untrack(() => {
					loading = true;
					prevServerId = serverId;
				});
			}
			// Initial load - full screen loader
			// Initial tab data loading requests - corner loader
			loadServer(true);
			interval = setInterval(() => loadServer(true), 5000); // Poll every 5 seconds
		}
	});

	async function loadServer(skipLoading = false) {
		if (!serverId) return;
		const requestedId = serverId;
		try {
			const request = create(GetServerRequestSchema, { id: requestedId });
			const callOptions = skipLoading ? silentCallOptions : undefined;
			const response = await rpcClient.server.getServer(request, callOptions);
			// Only update if we're still on the same server
			if (response.server && serverId === requestedId) {
				server = response.server;
				serversStore.updateServer(server);
				loading = false;
			}
		} catch {
			// Only show error if still on the same server and no data yet
			if (serverId === requestedId && !server) {
				toast.error('Failed to load server');
				loading = false;
			}
		}
	}

	async function handleServerAction(action: 'start' | 'stop' | 'restart' | 'recreate') {
		if (!server) return;

		actionLoading = true;
		try {
			switch (action) {
				case 'start': {
					const startRequest = create(StartServerRequestSchema, { id: server.id });
					await rpcClient.server.startServer(startRequest);
					toast.success('Server is starting...');
					break;
				}
				case 'stop': {
					const stopRequest = create(StopServerRequestSchema, { id: server.id });
					await rpcClient.server.stopServer(stopRequest);
					toast.success('Server is stopping...');
					break;
				}
				case 'restart': {
					const restartRequest = create(RestartServerRequestSchema, { id: server.id });
					await rpcClient.server.restartServer(restartRequest);
					toast.success('Server is restarting...');
					break;
				}
				case 'recreate': {
					const recreateRequest = create(RecreateServerRequestSchema, { id: server.id });
					await rpcClient.server.recreateServer(recreateRequest);
					toast.success('Server is being recreated...');
					break;
				}
			}
			await loadServer();
		} catch (error) {
			toast.error(
				`Failed to ${action} server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = false;
		}
	}

	async function copyToClipboard(text?: string) {
		if (!text) return;
		const success = await copyText(text);
		if (success) {
			toast.success('Copied to clipboard!');
		} else {
			toast.error('Failed to copy to clipboard');
		}
	}

	function getPlayerCountColors(percent: number) {
		// gray (0%) -> blue (1-25%) -> cyan (26-50%) -> green (51-75%) -> amber (76-90%) -> red (91-100%)
		if (percent === 0)
			return {
				bg: '107 114 128',
				text: '107 114 128',
				barFrom: '156 163 175',
				barTo: '107 114 128'
			}; // gray
		if (percent <= 25)
			return { bg: '59 130 246', text: '59 130 246', barFrom: '59 130 246', barTo: '99 102 241' }; // blue -> indigo
		if (percent <= 50)
			return { bg: '6 182 212', text: '6 182 212', barFrom: '6 182 212', barTo: '20 184 166' }; // cyan -> teal
		if (percent <= 75)
			return { bg: '34 197 94', text: '34 197 94', barFrom: '34 197 94', barTo: '16 185 129' }; // green -> emerald
		if (percent <= 90)
			return { bg: '245 158 11', text: '245 158 11', barFrom: '245 158 11', barTo: '234 179 8' }; // amber -> yellow
		return { bg: '239 68 68', text: '239 68 68', barFrom: '239 68 68', barTo: '249 115 22' }; // red -> orange
	}

	async function handleDeleteServer() {
		if (!server) return;

		const confirmed = confirm(
			`Are you sure you want to delete "${server.name}"?\n\nThis will:\n- Stop and remove the Docker container\n- Delete all server files and data\n- Remove all mods and configurations\n\nThis action cannot be undone!`
		);

		if (!confirmed) return;

		actionLoading = true;
		try {
			const deleteRequest = create(DeleteServerRequestSchema, { id: server.id });
			await rpcClient.server.deleteServer(deleteRequest);
			serversStore.removeServer(server.id);
			toast.success('Server deleted successfully');
			goto(resolve('/servers'));
		} catch (error) {
			toast.error(
				`Failed to delete server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = false;
		}
	}
</script>

{#if loading && !server}
	<div class="flex h-96 items-center justify-center">
		<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
	</div>
{:else if server}
	<div
		class="flex h-full flex-col bg-linear-to-br from-background to-muted/20 p-4 pt-4 sm:p-6 sm:pt-6 lg:p-8"
	>
		<div
			class="mb-4 flex shrink-0 flex-col items-start justify-between gap-4 border-b-2 border-border/50 pb-4 sm:mb-6 sm:flex-row sm:items-center sm:pb-6"
		>
			<div class="flex items-center gap-3 sm:gap-4">
				<div
					class="flex h-12 w-12 items-center justify-center rounded-xl bg-linear-to-br from-primary/20 to-primary/10 shadow-lg sm:h-16 sm:w-16 sm:rounded-2xl"
				>
					<Package class="h-6 w-6 text-primary sm:h-8 sm:w-8" />
				</div>
				<div class="space-y-1">
					<h2
						class="bg-linear-to-r from-foreground to-foreground/70 bg-clip-text text-2xl font-bold tracking-tight text-transparent sm:text-3xl lg:text-4xl"
					>
						{server.name}
					</h2>
					<p class="text-sm text-muted-foreground sm:text-base">{server.description || ''}</p>
					{#if server.description || !server.description || server.description === ''}
						<p class="mt-1 text-xs text-muted-foreground/70">
							Created {timestampToDate(server.createdAt).toLocaleDateString(undefined, {
								month: 'short',
								day: 'numeric',
								year: 'numeric'
							})}
							{#if server.lastStarted}
								| Last started {(() => {
									const date = timestampToDate(server.lastStarted);
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
			<div class="flex w-full flex-wrap items-center gap-2 sm:w-auto">
				{#if server.status === ServerStatus.STOPPED || !server.containerId}
					<Button
						onclick={() => handleServerAction('start')}
						disabled={actionLoading ||
							server.status === ServerStatus.STARTING ||
							server.status === ServerStatus.STOPPING ||
							server.status === ServerStatus.PROVISIONING ||
							server.status === ServerStatus.CREATING ||
							server.status === ServerStatus.RESTARTING}
						size="default"
						class="bg-green-600 text-white shadow-lg transition-all hover:scale-[1.02] hover:bg-green-700 hover:shadow-xl"
					>
						{#if actionLoading}
							<Loader2 class="mr-1 h-4 w-4 animate-spin sm:mr-2 sm:h-5 sm:w-5" />
						{:else}
							<Play class="mr-1 h-4 w-4 sm:mr-2 sm:h-5 sm:w-5" />
						{/if}
						<span class="sm:inline">Start</span>
					</Button>
				{:else if server.status === ServerStatus.ERROR}
					<Button
						onclick={() => handleServerAction('restart')}
						disabled={actionLoading}
						size="default"
						class="bg-yellow-600 text-white shadow-lg transition-all hover:scale-[1.02] hover:bg-yellow-700 hover:shadow-xl"
					>
						{#if actionLoading}
							<Loader2 class="mr-1 h-4 w-4 animate-spin sm:mr-2 sm:h-5 sm:w-5" />
						{:else}
							<RotateCw class="mr-1 h-4 w-4 sm:mr-2 sm:h-5 sm:w-5" />
						{/if}
						<span class="sm:inline">Restart</span>
					</Button>
					<Button
						variant="destructive"
						onclick={() => handleServerAction('stop')}
						disabled={actionLoading}
						size="default"
						class="shadow-lg transition-all hover:scale-[1.02] hover:shadow-xl"
					>
						{#if actionLoading}
							<Loader2 class="mr-1 h-4 w-4 animate-spin sm:mr-2 sm:h-5 sm:w-5" />
						{:else}
							<Square class="mr-1 h-4 w-4 sm:mr-2 sm:h-5 sm:w-5" />
						{/if}
						<span class="sm:inline">Stop</span>
					</Button>
				{:else if server.status === ServerStatus.RUNNING || server.status === ServerStatus.STARTING || server.status === ServerStatus.UNHEALTHY || server.status === ServerStatus.PAUSED || server.status === ServerStatus.PROVISIONING}
					<Button
						variant="destructive"
						onclick={() => handleServerAction('stop')}
						disabled={actionLoading}
						size="default"
						class="shadow-lg transition-all hover:scale-[1.02] hover:shadow-xl"
					>
						{#if actionLoading}
							<Loader2 class="mr-1 h-4 w-4 animate-spin sm:mr-2 sm:h-5 sm:w-5" />
						{:else}
							<Square class="mr-1 h-4 w-4 sm:mr-2 sm:h-5 sm:w-5" />
						{/if}
						<span class="sm:inline">Stop</span>
					</Button>
					<Button
						variant="outline"
						onclick={() => handleServerAction('restart')}
						disabled={actionLoading}
						size="default"
						class="hidden border-2 shadow-md transition-all hover:scale-[1.02] hover:shadow-lg sm:flex"
					>
						{#if actionLoading}
							<Loader2 class="mr-1 h-4 w-4 animate-spin sm:mr-2 sm:h-5 sm:w-5" />
						{:else}
							<RotateCw class="mr-1 h-4 w-4 sm:mr-2 sm:h-5 sm:w-5" />
						{/if}
						Restart
					</Button>
				{:else if server.status === ServerStatus.STOPPING}
					<Button variant="secondary" disabled={true} size="default" class="shadow-lg">
						<Loader2 class="mr-1 h-4 w-4 animate-spin sm:mr-2 sm:h-5 sm:w-5" />
						<span class="sm:inline">Stopping...</span>
					</Button>
				{/if}
				<div class="ml-2 hidden h-10 w-px bg-border/50 sm:ml-4 sm:block"></div>
				<DropdownMenu>
					<DropdownMenuTrigger>
						{#snippet child({ props })}
							<Button
								variant="ghost"
								size="icon"
								disabled={actionLoading}
								{...props}
								class="hidden sm:flex"
							>
								<MoreVertical class="h-4 w-4" />
								<span class="sr-only">More actions</span>
							</Button>
						{/snippet}
					</DropdownMenuTrigger>
					<DropdownMenuContent align="end">
						<DropdownMenuItem class="flew-row flex" onclick={() => handleServerAction('recreate')}>
							<RefreshCcw class="mr-2 h-4 w-4" />
							Force Recreate
						</DropdownMenuItem>
						<DropdownMenuItem
							class="flew-row flex text-destructive"
							onclick={() => handleDeleteServer()}
						>
							<Trash2 class="mr-2 h-4 w-4" />
							Delete Server
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			</div>
		</div>

		<div class="mb-3 shrink-0 sm:mb-4">
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 sm:gap-5 xl:grid-cols-4">
				<Card
					class="group relative overflow-hidden border-0 bg-linear-to-br from-background via-background/95 to-background/90 pb-0 shadow-xl transition-all duration-500 hover:-translate-y-1 hover:shadow-2xl"
				>
					{#if server.status === ServerStatus.RUNNING}
						<div
							class="pointer-events-none absolute inset-0 bg-linear-to-br from-green-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
						<div
							class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-green-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
					{:else if server.status === ServerStatus.UNHEALTHY}
						<div
							class="pointer-events-none absolute inset-0 bg-linear-to-br from-purple-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
						<div
							class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-purple-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
					{:else if server.status === ServerStatus.STOPPED}
						<div
							class="pointer-events-none absolute inset-0 bg-linear-to-br from-gray-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
						<div
							class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-gray-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
					{:else if server.status === ServerStatus.STARTING}
						<div
							class="pointer-events-none absolute inset-0 bg-linear-to-br from-yellow-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
						<div
							class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-yellow-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
					{:else if server.status === ServerStatus.CREATING}
						<div
							class="pointer-events-none absolute inset-0 bg-linear-to-br from-blue-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
						<div
							class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-blue-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
					{:else}
						<div
							class="pointer-events-none absolute inset-0 bg-linear-to-br from-orange-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
						<div
							class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-orange-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
						></div>
					{/if}

					<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
						<div class="space-y-1">
							<CardTitle
								class="text-xs font-bold tracking-widest text-muted-foreground/70 uppercase"
								>Server Status</CardTitle
							>
							<p class="text-xs text-muted-foreground/50">Live monitoring</p>
						</div>
						<div class="relative">
							{#if server.status === ServerStatus.RUNNING}
								<div
									class="absolute inset-0 rounded-2xl bg-linear-to-br from-green-500/20 to-green-600/20 opacity-0 blur-xl transition-opacity duration-500 group-hover:opacity-100"
								></div>
								<div
									class="relative flex h-14 w-14 items-center justify-center rounded-2xl bg-linear-to-br from-green-500/10 to-green-600/10 transition-all duration-500 group-hover:scale-110 group-hover:rotate-3"
								>
									<div class="relative">
										<Activity class="h-7 w-7 text-green-500" />
									</div>
								</div>
							{:else if server.status === ServerStatus.UNHEALTHY}
								<div
									class="absolute inset-0 rounded-2xl bg-linear-to-br from-purple-500/20 to-purple-600/20 opacity-0 blur-xl transition-opacity duration-500 group-hover:opacity-100"
								></div>
								<div
									class="relative flex h-14 w-14 items-center justify-center rounded-2xl bg-linear-to-br from-purple-500/10 to-purple-600/10 transition-all duration-500 group-hover:scale-110 group-hover:rotate-3"
								>
									<div class="relative">
										<Activity class="h-7 w-7 text-purple-500" />
									</div>
								</div>
							{:else if server.status === ServerStatus.STOPPED}
								<div
									class="absolute inset-0 rounded-2xl bg-linear-to-br from-gray-500/20 to-gray-600/20 opacity-0 blur-xl transition-opacity duration-500 group-hover:opacity-100"
								></div>
								<div
									class="relative flex h-14 w-14 items-center justify-center rounded-2xl bg-linear-to-br from-gray-500/10 to-gray-600/10 transition-all duration-500 group-hover:scale-110 group-hover:rotate-3"
								>
									<Square class="h-7 w-7 text-gray-500" />
								</div>
							{:else if server.status === ServerStatus.STARTING}
								<div
									class="absolute inset-0 rounded-2xl bg-linear-to-br from-yellow-500/20 to-yellow-600/20 opacity-0 blur-xl transition-opacity duration-500 group-hover:opacity-100"
								></div>
								<div
									class="relative flex h-14 w-14 items-center justify-center rounded-2xl bg-linear-to-br from-yellow-500/10 to-yellow-600/10 transition-all duration-500 group-hover:scale-110 group-hover:rotate-3"
								>
									<Loader2 class="h-7 w-7 animate-spin text-yellow-500" />
								</div>
							{:else if server.status === ServerStatus.CREATING}
								<div
									class="absolute inset-0 rounded-2xl bg-linear-to-br from-blue-500/20 to-blue-600/20 opacity-0 blur-xl transition-opacity duration-500 group-hover:opacity-100"
								></div>
								<div
									class="relative flex h-14 w-14 items-center justify-center rounded-2xl bg-linear-to-br from-blue-500/10 to-blue-600/10 transition-all duration-500 group-hover:scale-110 group-hover:rotate-3"
								>
									<Loader2 class="h-7 w-7 animate-spin text-blue-500" />
								</div>
							{:else}
								<div
									class="absolute inset-0 rounded-2xl bg-linear-to-br from-orange-500/20 to-orange-600/20 opacity-0 blur-xl transition-opacity duration-500 group-hover:opacity-100"
								></div>
								<div
									class="relative flex h-14 w-14 items-center justify-center rounded-2xl bg-linear-to-br from-orange-500/10 to-orange-600/10 transition-all duration-500 group-hover:scale-110 group-hover:rotate-3"
								>
									<RotateCw class="h-7 w-7 animate-pulse text-orange-500" />
								</div>
							{/if}
						</div>
					</CardHeader>
					<CardContent class="flex-1 pt-1">
						<div class="space-y-4">
							<div class="relative">
								<div
									class="flex h-20 items-center justify-center overflow-hidden rounded-xl border border-border/30 bg-linear-to-br from-muted/30 to-muted/10"
								>
									{#if server.status === ServerStatus.RUNNING}
										<div class="heartbeat-container">
											{#each Array(5) as _, i (i)}
												<div
													class="heartbeat-bar bg-green-500"
													style="animation-delay: {i * 0.15}s"
												></div>
											{/each}
										</div>
									{:else if server.status === ServerStatus.UNHEALTHY}
										<div class="heartbeat-container">
											{#each Array(5) as _, i (i)}
												<div
													class="heartbeat-bar heartbeat-erratic text-purple-500"
													style="animation-delay: {i * 0.1}s; height: {20 + Math.random() * 30}px"
												></div>
											{/each}
										</div>
									{:else if server.status === ServerStatus.STOPPED}
										<div class="h-0.5 w-full bg-gray-500/50"></div>
									{:else if server.status === ServerStatus.STARTING}
										<div class="heartbeat-container">
											{#each Array(5) as _, i (i)}
												<div
													class="heartbeat-bar heartbeat-slow bg-yellow-500"
													style="animation-delay: {i * 0.2}s"
												></div>
											{/each}
										</div>
									{:else if server.status === ServerStatus.CREATING}
										<div class="heartbeat-container">
											{#each Array(5) as _, i (i)}
												<div
													class="heartbeat-bar heartbeat-slow bg-blue-500"
													style="animation-delay: {i * 0.2}s"
												></div>
											{/each}
										</div>
									{:else}
										<div class="heartbeat-container">
											{#each Array(5) as _, i (i)}
												<div
													class="heartbeat-bar heartbeat-slow bg-orange-500"
													style="animation-delay: {i * 0.25}s"
												></div>
											{/each}
										</div>
									{/if}
								</div>
							</div>

							<div class="space-y-2 text-center">
								<div class="text-2xl font-bold">
									{#if server.status === ServerStatus.RUNNING}
										<span class="text-green-500">RUNNING</span>
									{:else if server.status === ServerStatus.UNHEALTHY}
										<span class="text-purple-500">BUSY</span>
									{:else if server.status === ServerStatus.STOPPED}
										<span class="text-gray-500">STOPPED</span>
									{:else if server.status === ServerStatus.STARTING}
										<span class="text-yellow-500">STARTING</span>
									{:else if server.status === ServerStatus.STOPPING}
										<span class="text-orange-500">STOPPING</span>
									{:else if server.status === ServerStatus.CREATING}
										<span class="text-blue-500">CREATING</span>
									{:else if server.status === ServerStatus.RESTARTING}
										<span class="text-orange-500">RESTARTING</span>
									{:else if server.status === ServerStatus.PROVISIONING}
										<span class="text-yellow-500">PROVISIONING</span>
									{:else if server.status === ServerStatus.PAUSED}
										<span class="text-blue-500">SLEEPING</span>
									{:else if server.status === ServerStatus.ERROR}
										<span class="text-red-500">ERROR</span>
									{:else}
										<span class="text-muted-foreground">UNKNOWN</span>
									{/if}
								</div>
								<p class="text-xs text-muted-foreground/70">
									{#if server.status === ServerStatus.RUNNING}
										Online and accepting players
									{:else if server.status === ServerStatus.UNHEALTHY}
										Server temporarily unresponsive
									{:else if server.status === ServerStatus.STOPPED}
										Server is currently offline
									{:else if server.status === ServerStatus.STARTING}
										Initializing server components
									{:else if server.status === ServerStatus.STOPPING}
										Shutting down gracefully
									{:else if server.status === ServerStatus.CREATING}
										Setting up server container
									{:else if server.status === ServerStatus.RESTARTING}
										Server is restarting
									{:else if server.status === ServerStatus.PROVISIONING}
										Installing server files and mods
									{:else if server.status === ServerStatus.PAUSED}
										Paused while idle - joins will wake it
									{:else if server.status === ServerStatus.ERROR}
										Server encountered an error
									{:else}
										Status: {server.status}
									{/if}
								</p>
							</div>
						</div>
					</CardContent>

					<!-- Health Check Section (bottom strip) -->
					<ServerPerformance {server} />
				</Card>

				<Card
					class="group relative flex flex-col overflow-hidden border-0 bg-linear-to-br from-background via-background/95 to-background/90 pb-0 shadow-xl transition-all duration-500 hover:-translate-y-1 hover:shadow-2xl"
				>
					<div
						class="pointer-events-none absolute inset-0 bg-linear-to-br from-blue-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
					></div>
					<div
						class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-blue-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
					></div>

					<!-- Connection Section (top half) -->
					<div class="flex min-h-0 flex-1 flex-col">
						<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
							<div class="space-y-1">
								<CardTitle
									class="text-xs font-bold tracking-widest text-muted-foreground/70 uppercase"
									>Connection</CardTitle
								>
								<p class="text-xs text-muted-foreground/50">Server address</p>
							</div>
							<div class="relative">
								<div
									class="absolute inset-0 rounded-2xl bg-linear-to-br from-blue-500/20 to-blue-600/20 opacity-0 blur-xl transition-opacity duration-500 group-hover:opacity-100"
								></div>
								<div
									class="relative flex h-14 w-14 items-center justify-center rounded-2xl bg-linear-to-br from-blue-500/10 to-blue-600/10 transition-all duration-500 group-hover:scale-110 group-hover:rotate-3"
								>
									<ExternalLink class="h-7 w-7 text-blue-500 group-hover:animate-pulse" />
								</div>
							</div>
						</CardHeader>
						<CardContent class="flex-1 pt-1 pb-6">
							<div class="group/copy relative">
								<div
									class="absolute inset-0 rounded-xl bg-linear-to-r from-blue-500/10 to-purple-500/10 opacity-0 blur-xl transition-opacity duration-500 group-hover/copy:opacity-100"
								></div>
								<div
									class="relative flex items-center justify-between rounded-xl border border-border/50 bg-linear-to-r from-muted/50 to-muted/30 p-3 backdrop-blur-sm transition-all duration-300 group-hover/copy:border-primary/30"
								>
									<div class="min-w-0 flex-1">
										<span class="block truncate font-mono text-sm font-bold text-foreground/90">
											{#if server.proxyHostname}
												{server.proxyHostname}
											{:else}
												localhost:{server.port}
											{/if}
										</span>
										<span class="mt-1 block text-xs text-muted-foreground/60">Click to copy</span>
									</div>
									<Button
										size="icon"
										variant="ghost"
										onclick={() => {
											if (!server) return;
											const connectionString = server.proxyHostname || `localhost:${server.port}`;
											copyToClipboard(connectionString);
										}}
										class="transition-all duration-300 hover:scale-110 hover:bg-primary/20 hover:text-primary"
									>
										<Copy class="h-4 w-4" />
									</Button>
								</div>
							</div>
						</CardContent>
					</div>

					<!-- Players Section (bottom strip) -->
					{#if server.playersOnline !== undefined}
						{@const maxPlayers = server.maxPlayersSlp || server.maxPlayers}
						{@const playersPercent = (server.playersOnline / maxPlayers) * 100}
						{@const colors = getPlayerCountColors(playersPercent)}
						<div
							class="mt-auto border-t border-border/30 transition-colors duration-500"
							style="background: linear-gradient(to bottom, rgb({colors.bg} / 0.05), transparent);"
						>
							<div class="px-6 pt-3 pb-4">
								<div class="mb-2 flex items-center justify-between">
									<span
										class="text-[10px] font-bold tracking-widest text-muted-foreground/70 uppercase"
										>Players</span
									>
									<span
										class="font-mono text-sm font-bold transition-colors duration-500"
										style="color: rgb({colors.text});">{server.playersOnline}/{maxPlayers}</span
									>
								</div>
								<div
									class="relative mb-2 h-2 overflow-hidden rounded-full bg-linear-to-r from-muted/50 to-muted/30"
								>
									<div
										class="relative h-full rounded-full transition-all duration-700"
										style="width: {Math.min(
											playersPercent,
											100
										)}%; background: linear-gradient(to right, rgb({colors.barFrom}), rgb({colors.barTo}));"
									>
										<div
											class="absolute inset-0 bg-linear-to-r from-transparent via-white/20 to-transparent"
										></div>
									</div>
								</div>
								{#if server.playerSample && server.playerSample.length > 0}
									<div class="flex flex-wrap gap-1.5">
										{#each server.playerSample as playerName (playerName)}
											<div
												class="flex items-center gap-1 rounded border px-1.5 py-0.5 transition-colors duration-500"
												style="background: rgb({colors.bg} / 0.1); border-color: rgb({colors.bg} / 0.2);"
											>
												<img
													src="https://mc-heads.net/avatar/{playerName}/16"
													alt={playerName}
													class="h-4 w-4 rounded-sm"
													onerror={(e) => {
														const target = e.currentTarget as HTMLImageElement;
														target.style.display = 'none';
													}}
												/>
												<span class="text-[10px] font-medium text-foreground/80">{playerName}</span>
											</div>
										{/each}
									</div>
								{:else}
									<p class="text-[10px] text-muted-foreground/50">No players online</p>
								{/if}
							</div>
						</div>
					{:else}
						<div
							class="mt-auto border-t border-border/30 bg-linear-to-b from-gray-500/5 to-transparent"
						>
							<div class="px-6 pt-3 pb-4">
								<div class="mb-2 flex items-center justify-between">
									<span
										class="text-[10px] font-bold tracking-widest text-muted-foreground/70 uppercase"
										>Players</span
									>
									<span class="font-mono text-sm text-muted-foreground/50">--</span>
								</div>
								<p class="text-[10px] text-muted-foreground/50">Server offline</p>
							</div>
						</div>
					{/if}
				</Card>

				<Card
					class="group relative flex flex-col overflow-hidden border-0 bg-linear-to-br from-background via-background/95 to-background/90 shadow-xl transition-all duration-500 hover:-translate-y-1 hover:shadow-2xl"
				>
					<div
						class="pointer-events-none absolute inset-0 bg-linear-to-br from-purple-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
					></div>
					<div
						class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-purple-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
					></div>

					<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
						<div class="space-y-1">
							<CardTitle
								class="text-xs font-bold tracking-widest text-muted-foreground/70 uppercase"
								>Server Info</CardTitle
							>
							<p class="text-xs text-muted-foreground/50">Details & versions</p>
						</div>
						<div class="relative">
							<div
								class="absolute inset-0 rounded-2xl bg-linear-to-br from-purple-500/20 to-purple-600/20 opacity-0 blur-xl transition-opacity duration-500 group-hover:opacity-100"
							></div>
							<div
								class="relative flex h-12 w-12 items-center justify-center rounded-2xl bg-linear-to-br from-purple-500/10 to-purple-600/10 transition-all duration-500 group-hover:scale-110 group-hover:rotate-3"
							>
								<Info class="h-6 w-6 text-purple-500 group-hover:animate-pulse" />
							</div>
						</div>
					</CardHeader>
					<CardContent class="flex-1 scrollbar-thin overflow-y-auto pt-0 pb-2">
						<div class="space-y-1.5">
							<div class="flex items-center justify-between">
								<span class="text-[10px] text-muted-foreground/60">Minecraft</span>
								<span class="font-mono text-[11px] font-semibold text-purple-500"
									>{server.mcVersion}</span
								>
							</div>
							{#if server.javaVersion}
								<div class="flex items-center justify-between">
									<span class="text-[10px] text-muted-foreground/60">Java</span>
									<span class="font-mono text-[11px] font-semibold text-purple-400"
										>Java {server.javaVersion}</span
									>
								</div>
							{/if}
							<div class="flex items-center justify-between">
								<span class="text-[10px] text-muted-foreground/60">Mod Loader</span>
								{#if server.modLoader === ModLoader.VANILLA}
									<Badge
										variant="secondary"
										class="h-4 border-yellow-500/20 bg-yellow-500/10 px-1.5 py-0 text-[10px]"
									>
										Vanilla
									</Badge>
								{:else if server.modLoader === ModLoader.FORGE || server.modLoader === ModLoader.NEOFORGE}
									<Badge
										variant="secondary"
										class="h-4 border-orange-500/20 bg-orange-500/10 px-1.5 py-0 text-[10px]"
									>
										{server.modLoader === ModLoader.FORGE ? 'Forge' : 'NeoForge'}
									</Badge>
								{:else if server.modLoader === ModLoader.FABRIC}
									<Badge
										variant="secondary"
										class="h-4 border-blue-500/20 bg-blue-500/10 px-1.5 py-0 text-[10px]"
									>
										Fabric
									</Badge>
								{:else}
									<Badge variant="secondary" class="h-4 px-1.5 py-0 text-[10px] capitalize">
										{ModLoader[server.modLoader]}
									</Badge>
								{/if}
							</div>
							<div
								class="group/copy flex cursor-pointer items-center justify-between"
								onclick={() => copyToClipboard(server?.id)}
							>
								<span class="text-[10px] text-muted-foreground/60">Server ID</span>
								<div class="flex items-center gap-1">
									<span class="max-w-20 truncate font-mono text-[10px] text-muted-foreground/70">
										{server.id}
									</span>
									<Copy
										class="h-2.5 w-2.5 text-muted-foreground/40 opacity-0 transition-opacity group-hover/copy:opacity-100"
									/>
								</div>
							</div>
						</div>
					</CardContent>
				</Card>

				<Card
					class="group relative overflow-hidden border-0 bg-linear-to-br from-background via-background/95 to-background/90 shadow-xl transition-all duration-500 hover:-translate-y-1 hover:shadow-2xl"
				>
					<div
						class="pointer-events-none absolute inset-0 bg-linear-to-br from-orange-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
					></div>
					<div
						class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-orange-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
					></div>
					<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
						<div class="space-y-1">
							<CardTitle
								class="text-xs font-bold tracking-widest text-muted-foreground/70 uppercase"
								>Performance</CardTitle
							>
							<p class="text-xs text-muted-foreground/50">Resources & metrics</p>
						</div>
						<div class="relative">
							<div
								class="absolute inset-0 rounded-2xl bg-linear-to-br from-orange-500/20 to-orange-600/20 opacity-0 blur-xl transition-opacity duration-500 group-hover:opacity-100"
							></div>
							<div
								class="relative flex h-14 w-14 items-center justify-center rounded-2xl bg-linear-to-br from-orange-500/10 to-orange-600/10 transition-all duration-500 group-hover:scale-110 group-hover:rotate-3"
							>
								<Cpu class="h-7 w-7 text-orange-500 group-hover:animate-pulse" />
							</div>
						</div>
					</CardHeader>
					<CardContent class="pt-1">
						<div class="space-y-3">
							<!-- Memory Usage -->
							<div>
								<div class="mb-1.5 flex items-center justify-between">
									<span class="text-xs font-semibold text-muted-foreground/70">MEMORY</span>
									{#if server.memoryUsage}
										<span class="font-mono text-xs text-orange-500">
											{(Number(server.memoryUsage) / 1024).toFixed(2)} / {(
												server.memory / 1024
											).toFixed(1)} GB
										</span>
									{:else}
										<span class="font-mono text-xs text-muted-foreground/50">
											{(server.memory / 1024).toFixed(1)} GB allocated
										</span>
									{/if}
								</div>
								<div
									class="relative h-3 overflow-hidden rounded-full bg-linear-to-r from-muted/50 to-muted/30"
								>
									{#if server.memoryUsage}
										<div
											class="relative h-full rounded-full bg-linear-to-r from-orange-500 to-yellow-500 transition-all duration-700"
											style="width: {Math.min(
												(Number(server.memoryUsage) / server.memory) * 100,
												100
											)}%"
										>
											<div
												class="absolute inset-0 bg-linear-to-r from-transparent via-white/20 to-transparent"
											></div>
										</div>
									{:else}
										<div class="h-full bg-muted/50"></div>
									{/if}
								</div>
								{#if server.memoryUsage}
									<p class="mt-1 text-[10px] text-muted-foreground/50">
										{((Number(server.memoryUsage) / server.memory) * 100).toFixed(1)}% used
									</p>
								{/if}
							</div>

							<!-- CPU Usage -->
							<div>
								<div class="mb-1.5 flex items-center justify-between">
									<span class="text-xs font-semibold text-muted-foreground/70">CPU</span>
									{#if server.cpuPercent !== undefined && server.cpuCores > 0}
										<span class="font-mono text-xs text-blue-500"
											>{(server.cpuPercent / server.cpuCores).toFixed(1)}%</span
										>
									{:else if server.cpuPercent !== undefined}
										<span class="font-mono text-xs text-blue-500"
											>{server.cpuPercent.toFixed(1)}%</span
										>
									{:else}
										<span class="font-mono text-xs text-muted-foreground/50">--</span>
									{/if}
								</div>
								{#if server.cpuPercent !== undefined && server.cpuCores > 0}
									<div
										class="flex h-3 {server.cpuCores > 16 ? 'gap-px' : 'gap-0.5'}"
										title="{server.cpuPercent.toFixed(0)}% total across {server.cpuCores} cores"
									>
										{#each Array.from({ length: server.cpuCores }) as _, i (i)}
											{@const fill = Math.min(Math.max(server.cpuPercent - i * 100, 0), 100)}
											<div
												class="relative h-full flex-1 overflow-hidden rounded-full bg-linear-to-r from-muted/50 to-muted/30"
											>
												{#if fill > 0}
													<div
														class="relative h-full rounded-full bg-linear-to-r from-blue-500 to-cyan-500 transition-all duration-700"
														style="width: {fill}%"
													>
														<div
															class="absolute inset-0 bg-linear-to-r from-transparent via-white/20 to-transparent"
														></div>
													</div>
												{/if}
											</div>
										{/each}
									</div>
									<p class="mt-1 text-[10px] text-muted-foreground/50">
										{(server.cpuPercent / 100).toFixed(1)} of {server.cpuCores} cores in use
									</p>
								{:else}
									<div
										class="relative h-3 overflow-hidden rounded-full bg-linear-to-r from-muted/50 to-muted/30"
									>
										{#if server.cpuPercent !== undefined}
											<div
												class="relative h-full rounded-full bg-linear-to-r from-blue-500 to-cyan-500 transition-all duration-700"
												style="width: {Math.min(server.cpuPercent, 100)}%"
											>
												<div
													class="absolute inset-0 bg-linear-to-r from-transparent via-white/20 to-transparent"
												></div>
											</div>
										{:else}
											<div class="h-full bg-muted/50"></div>
										{/if}
									</div>
								{/if}
							</div>

							<!-- Disk Usage -->
							<div>
								<div class="mb-1.5 flex items-center justify-between">
									<span class="text-xs font-semibold text-muted-foreground/70">STORAGE</span>
									{#if Number(server.diskUsage) > 0}
										<span class="font-mono text-xs text-purple-500"
											>{formatBytes(Number(server.diskUsage))}</span
										>
									{:else}
										<span class="font-mono text-xs text-muted-foreground/50">--</span>
									{/if}
								</div>
								<div
									class="relative h-3 overflow-hidden rounded-full bg-linear-to-r from-muted/50 to-muted/30"
									title={Number(server.diskTotal) > 0
										? `this server ${formatBytes(Number(server.diskUsage))}, disk ${formatBytes(
												Number(server.diskUsed)
											)} of ${formatBytes(Number(server.diskTotal))} used`
										: 'measuring disk usage'}
								>
									{#if Number(server.diskTotal) > 0}
										<!-- everything used on the volume -->
										<div
											class="absolute inset-y-0 left-0 rounded-full bg-muted-foreground/25 transition-all duration-700"
											style="width: {Math.min(
												(Number(server.diskUsed) / Number(server.diskTotal)) * 100,
												100
											)}%"
										></div>
										<!-- server slice, min width stays visible on huge disks -->
										{#if Number(server.diskUsage) > 0}
											<div
												class="absolute inset-y-0 left-0 rounded-full bg-linear-to-r from-purple-500 to-pink-500 transition-all duration-700"
												style="width: {Math.min(
													Math.max((Number(server.diskUsage) / Number(server.diskTotal)) * 100, 2),
													100
												)}%"
											>
												<div
													class="absolute inset-0 bg-linear-to-r from-transparent via-white/20 to-transparent"
												></div>
											</div>
										{/if}
									{/if}
								</div>
								<p
									class="mt-1 flex items-center justify-between text-[10px] text-muted-foreground/50"
								>
									<span>
										{#if Number(server.worldSize) > 0}
											world {formatBytes(Number(server.worldSize))}
										{:else if Number(server.diskUsage) > 0}
											no world saved yet
										{:else}
											measuring server data...
										{/if}
									</span>
									{#if Number(server.diskTotal) > 0}
										<span
											>{formatBytes(Number(server.diskTotal) - Number(server.diskUsed), 1)} free of {formatBytes(
												Number(server.diskTotal),
												1
											)}</span
										>
									{/if}
								</p>
							</div>

							<!-- TPS -->
							{#if server.tps > 0}
								{@const tpsPercent = (server.tps / 20) * 100}
								<div>
									<div class="mb-1.5 flex items-center justify-between">
										<span class="text-xs font-semibold text-muted-foreground/70">TPS</span>
										<span class="font-mono text-xs text-green-500">{server.tps.toFixed(1)}</span>
									</div>
									<div
										class="relative h-3 overflow-hidden rounded-full bg-linear-to-r from-muted/50 to-muted/30"
									>
										<div
											class="relative h-full rounded-full bg-linear-to-r from-green-500 to-emerald-500 transition-all duration-700"
											style="width: {Math.min(tpsPercent, 100)}%"
										>
											<div
												class="absolute inset-0 bg-linear-to-r from-transparent via-white/20 to-transparent"
											></div>
										</div>
									</div>
								</div>
							{/if}
						</div>
					</CardContent>
				</Card>
			</div>

			<div class="mt-3">
				<button
					type="button"
					onclick={toggleMetricsPanel}
					class="flex w-full items-center gap-2 rounded-xl border border-border/30 bg-linear-to-br from-background via-background/95 to-background/90 px-4 py-1.5 text-xs font-medium tracking-wide text-muted-foreground/50 uppercase transition-colors duration-300 hover:text-muted-foreground"
				>
					Metrics
					<ChevronDown
						class="h-3.5 w-3.5 transition-transform duration-300 {showMetrics ? 'rotate-180' : ''}"
					/>
				</button>
				{#if showMetrics}
					<div transition:slide={{ duration: 250 }} class="pt-2">
						<ServerMetricsCharts {server} />
					</div>
				{/if}
			</div>
		</div>

		<Tabs
			value="overview"
			class="flex min-h-0 flex-1 flex-col gap-4"
			onValueChange={(value) => {
				activeTab = value;
			}}
		>
			<div class="w-full flex-shrink-0 overflow-x-auto">
				<TabsList
					class="inline-flex h-12 w-full min-w-max bg-muted/50 p-1 backdrop-blur-sm sm:grid sm:h-14 sm:grid-cols-8"
				>
					<TabsTrigger
						value="overview"
						class="px-3 text-xs font-medium whitespace-nowrap data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-lg sm:px-4 sm:text-sm"
						>Overview</TabsTrigger
					>
					<TabsTrigger
						value="console"
						class="px-3 text-xs font-medium whitespace-nowrap data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-lg sm:px-4 sm:text-sm"
						>Console</TabsTrigger
					>
					<TabsTrigger
						value="configuration"
						class="px-3 text-xs font-medium whitespace-nowrap data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-lg sm:px-4 sm:text-sm"
						>Config</TabsTrigger
					>
					<TabsTrigger
						value="mods"
						class="px-3 text-xs font-medium whitespace-nowrap data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-lg sm:px-4 sm:text-sm"
						>Mods</TabsTrigger
					>
					<TabsTrigger
						value="modules"
						class="px-3 text-xs font-medium whitespace-nowrap data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-lg sm:px-4 sm:text-sm"
						>Modules</TabsTrigger
					>
					<TabsTrigger
						value="files"
						class="px-3 text-xs font-medium whitespace-nowrap data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-lg sm:px-4 sm:text-sm"
						>Files</TabsTrigger
					>
					<TabsTrigger
						value="tasks"
						class="px-3 text-xs font-medium whitespace-nowrap data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-lg sm:px-4 sm:text-sm"
						>Tasks</TabsTrigger
					>
					<TabsTrigger
						value="routing"
						class="px-3 text-xs font-medium whitespace-nowrap data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-lg sm:px-4 sm:text-sm"
						>Routing</TabsTrigger
					>
				</TabsList>
			</div>

			<div class="min-h-0 flex-1 overflow-hidden">
				<TabsContent value="overview" class="h-full space-y-4">
					<Card class="border-border/50 shadow-sm">
						<CardHeader class="pb-4">
							<CardTitle class="text-xl">Server Settings</CardTitle>
							<CardDescription
								>Edit your server configuration and restart to apply changes</CardDescription
							>
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

				<TabsContent value="modules" class="h-full">
					<ServerModules {server} active={activeTab === 'modules'} />
				</TabsContent>

				<TabsContent value="files" class="h-full">
					<ServerFiles {server} active={activeTab === 'files'} />
				</TabsContent>

				<TabsContent value="tasks" class="h-full overflow-y-auto">
					<ServerTasks {server} active={activeTab === 'tasks'} />
				</TabsContent>

				<TabsContent value="routing" class="h-full overflow-y-auto">
					<ServerRouting {server} bind:router={routingInfo} active={activeTab === 'routing'} />
				</TabsContent>
			</div>
		</Tabs>
	</div>
{:else}
	<div class="flex h-96 items-center justify-center">
		<p class="text-muted-foreground">Server not found</p>
	</div>
{/if}

<ScrollToTop />

<style>
	@keyframes shimmer {
		0% {
			transform: translateX(-100%);
		}
		100% {
			transform: translateX(100%);
		}
	}
	@keyframes gradient-x {
		0%,
		100% {
			background-position: 0% 50%;
		}
		50% {
			background-position: 100% 50%;
		}
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
		0%,
		100% {
			height: 8px;
			opacity: 0.3;
		}
		50% {
			height: 40px;
			opacity: 1;
		}
	}

	@keyframes heartbeat-erratic {
		0%,
		100% {
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
		0%,
		100% {
			height: 8px;
			opacity: 0.2;
		}
		50% {
			height: 30px;
			opacity: 0.7;
		}
	}
</style>
