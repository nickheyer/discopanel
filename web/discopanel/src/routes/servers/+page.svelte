<script lang="ts">
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuSeparator,
		DropdownMenuTrigger
	} from '$lib/components/ui/dropdown-menu';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import {
		PageHeader,
		StatusBadge,
		ServerAvatar,
		EmptyState,
		ConfirmDialog
	} from '$lib/components/app';
	import MetricsSparkline from '$lib/components/metrics-sparkline.svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { serversStore, sortServersByActivity, claimFullStats } from '$lib/stores/servers';
	import {
		loaderLabel,
		tpsTone,
		TONE_TEXT,
		canStart,
		canStop,
		canRestart,
		isUp,
		statusMeta
	} from '$lib/server-status';
	import { formatUptime } from '$lib/utils/time';
	import { toast } from 'svelte-sonner';
	import {
		Plus,
		Search,
		Play,
		Square,
		RotateCw,
		RefreshCcw,
		MoreHorizontal,
		Trash2,
		Server as ServerIcon,
		Users,
		Zap,
		MemoryStick,
		ArrowUpDown
	} from '@lucide/svelte';
	import { type Server, ServerStatus, ModLoader } from '$lib/proto/discopanel/v1/common_pb';

	const DELETE_WARNING =
		'This permanently removes the container, all server files, the world, and every mod and configuration.\nThis cannot be undone.';

	type SortKey = 'activity' | 'name' | 'players' | 'status';
	const SORT_OPTIONS: { key: SortKey; label: string }[] = [
		{ key: 'activity', label: 'Activity' },
		{ key: 'name', label: 'Name' },
		{ key: 'players', label: 'Players' },
		{ key: 'status', label: 'Status' }
	];

	let servers = $derived($serversStore);
	let searchQuery = $state('');
	let filter = $state<'all' | 'active' | 'stopped' | 'issues'>('all');
	let sortKey = $state<SortKey>('activity');
	let initialLoading = $state(true);
	let refreshing = $state(false);
	let actioningId = $state('');
	let deleteTarget = $state<Server | null>(null);
	let deleteOpen = $state(false);
	let now = $state(new Date());

	onMount(() => {
		const release = claimFullStats();
		serversStore
			.fetchServers(servers.length > 0, true)
			.catch(() => {})
			.finally(() => (initialLoading = false));
		const clock = setInterval(() => (now = new Date()), 30000);
		return () => {
			release();
			clearInterval(clock);
		};
	});

	function inFilter(server: Server): boolean {
		const issue = server.status === ServerStatus.ERROR || server.status === ServerStatus.UNHEALTHY;
		switch (filter) {
			case 'active':
				return server.status !== ServerStatus.STOPPED && !issue;
			case 'stopped':
				return server.status === ServerStatus.STOPPED;
			case 'issues':
				return issue;
			default:
				return true;
		}
	}

	let counts = $derived.by(() => {
		const issues = servers.filter(
			(s) => s.status === ServerStatus.ERROR || s.status === ServerStatus.UNHEALTHY
		).length;
		const stopped = servers.filter((s) => s.status === ServerStatus.STOPPED).length;
		return { all: servers.length, active: servers.length - issues - stopped, stopped, issues };
	});

	function applySort(list: Server[]): Server[] {
		switch (sortKey) {
			case 'name':
				return [...list].sort((a, b) => a.name.localeCompare(b.name));
			case 'players':
				return [...list].sort((a, b) => (b.playersOnline || 0) - (a.playersOnline || 0));
			case 'status':
				return [...list].sort((a, b) =>
					statusMeta(a.status).label.localeCompare(statusMeta(b.status).label)
				);
			default:
				return sortServersByActivity([...list]);
		}
	}

	let visibleServers = $derived.by(() => {
		const sorted = applySort(servers.filter(inFilter));
		if (!searchQuery) return sorted;
		const q = searchQuery.toLowerCase();
		return sorted.filter(
			(s) =>
				s.name.toLowerCase().includes(q) ||
				s.description.toLowerCase().includes(q) ||
				s.mcVersion.toLowerCase().includes(q) ||
				loaderLabel(s.modLoader).toLowerCase().includes(q)
		);
	});

	async function refresh() {
		refreshing = true;
		try {
			await serversStore.fetchServers(true, true);
		} finally {
			refreshing = false;
		}
	}

	async function handleAction(action: 'start' | 'stop' | 'restart' | 'recreate', server: Server) {
		actioningId = server.id;
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
			await serversStore.fetchServers(true, true);
		} catch {
			// Interceptor already toasts the failure
		} finally {
			actioningId = '';
		}
	}

	function requestDelete(server: Server) {
		deleteTarget = server;
		deleteOpen = true;
	}

	async function confirmDelete() {
		if (!deleteTarget) return;
		await rpcClient.server.deleteServer({ id: deleteTarget.id });
		serversStore.removeServer(deleteTarget.id);
		toast.success(`Deleted ${deleteTarget.name}`);
		deleteTarget = null;
	}

	function connectionLabel(server: Server): string {
		return server.proxyHostname || `:${server.port}`;
	}
</script>

<svelte:head>
	<title>Servers · DiscoPanel</title>
</svelte:head>

<div class="mx-auto w-full max-w-6xl space-y-5 p-4 sm:p-6 2xl:max-w-7xl">
	<PageHeader title="Servers" description="Manage every server on this panel">
		<Button
			variant="ghost"
			size="icon"
			class="size-8"
			onclick={refresh}
			disabled={refreshing}
			title="Refresh"
		>
			<RefreshCcw class="size-4 {refreshing ? 'animate-spin' : ''}" />
		</Button>
	</PageHeader>

	{#if initialLoading && servers.length === 0}
		<div class="space-y-3">
			<Skeleton class="h-9 rounded-lg" />
			<Skeleton class="h-64 rounded-lg" />
		</div>
	{:else if servers.length === 0}
		<div class="rounded-xl border bg-card">
			<EmptyState
				icon={ServerIcon}
				title="No servers yet"
				description="Create your first Minecraft server and invite your friends."
			>
				<Button href={resolve('/servers/new')} class="glow-primary">
					<Plus class="size-4" />
					Create server
				</Button>
			</EmptyState>
		</div>
	{:else}
		<div class="flex flex-wrap items-center gap-2">
			<div class="relative min-w-52 flex-1 sm:max-w-xs">
				<Search class="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
				<Input
					type="search"
					placeholder="Search servers..."
					class="h-8 pl-8"
					bind:value={searchQuery}
				/>
			</div>
			<div class="flex items-center gap-1">
				{#each [{ key: 'all', label: 'All', count: counts.all }, { key: 'active', label: 'Active', count: counts.active }, { key: 'stopped', label: 'Stopped', count: counts.stopped }, { key: 'issues', label: 'Issues', count: counts.issues }] as f (f.key)}
					{#if f.key === 'all' || f.count > 0}
						<Button
							variant={filter === f.key ? 'secondary' : 'ghost'}
							size="sm"
							class="h-8 gap-1.5 {f.key === 'issues' && f.count > 0 ? 'text-status-danger' : ''}"
							onclick={() => (filter = f.key as typeof filter)}
						>
							{f.label}
							<span class="tabular text-xs text-muted-foreground">{f.count}</span>
						</Button>
					{/if}
				{/each}
			</div>
			<Button href={resolve('/servers/new')} size="sm" class="glow-primary ml-auto h-8">
				<Plus class="size-4" />
				<span class="hidden sm:inline">New server</span>
			</Button>
			<Select
				type="single"
				value={sortKey}
				onValueChange={(v) => (sortKey = (v as SortKey) || 'activity')}
			>
				<SelectTrigger class="h-8 w-auto gap-1.5 text-xs">
					<ArrowUpDown class="size-3.5 text-muted-foreground" />
					<span>{SORT_OPTIONS.find((o) => o.key === sortKey)?.label}</span>
				</SelectTrigger>
				<SelectContent>
					{#each SORT_OPTIONS as option (option.key)}
						<SelectItem value={option.key}>{option.label}</SelectItem>
					{/each}
				</SelectContent>
			</Select>
		</div>

		{#if visibleServers.length === 0}
			<div class="rounded-xl border bg-card">
				<EmptyState
					icon={Search}
					title="No matching servers"
					description="Try a different search or filter."
				/>
			</div>
		{:else}
			<div class="overflow-hidden rounded-xl border bg-card">
				<div class="divide-y">
					{#each visibleServers as server (server.id)}
						{@const busy = actioningId === server.id}
						<div
							class="group relative flex items-center gap-3 px-3 py-3 transition-colors hover:bg-accent/40 sm:px-4"
						>
							<ServerAvatar name={server.name} favicon={server.favicon} size="md" />
							<div class="min-w-0 flex-1">
								<div class="flex items-center gap-2">
									<a
										href={resolve(`/servers/${server.id}`)}
										class="truncate text-sm font-medium after:absolute after:inset-0"
									>
										{server.name}
									</a>
									<StatusBadge status={server.status} />
								</div>
								<div
									class="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground"
								>
									<span>{server.mcVersion}</span>
									{#if server.modLoader !== ModLoader.UNSPECIFIED && server.modLoader !== ModLoader.VANILLA}
										<span>·</span>
										<span>{loaderLabel(server.modLoader)}</span>
									{/if}
									<span>·</span>
									<span class="font-mono">{connectionLabel(server)}</span>
									{#if server.status === ServerStatus.RUNNING && server.lastStarted}
										<span>·</span>
										<span>up {formatUptime(server.lastStarted, now)}</span>
									{/if}
								</div>
							</div>

							<div class="tabular hidden shrink-0 items-center gap-4 text-xs md:flex">
								{#if isUp(server.status)}
									<span
										class="flex w-14 items-center gap-1.5 text-muted-foreground"
										title="Players online"
									>
										<Users class="size-3.5 shrink-0" />
										{server.playersOnline || 0}/{server.maxPlayers}
									</span>
									<span
										class="flex w-12 items-center gap-1.5 {server.tps > 0
											? TONE_TEXT[tpsTone(server.tps)]
											: 'text-muted-foreground/40'}"
										title="Ticks per second"
									>
										<Zap class="size-3.5 shrink-0" />
										{server.tps > 0 ? server.tps.toFixed(1) : '--'}
									</span>
									<span
										class="hidden w-14 items-center gap-1.5 text-muted-foreground lg:flex"
										title="Memory in use"
									>
										<MemoryStick class="size-3.5 shrink-0" />
										{Number(server.memoryUsage) > 0
											? `${(Number(server.memoryUsage) / 1024).toFixed(1)}G`
											: '--'}
									</span>
								{:else}
									<span class="w-14"></span>
									<span class="w-12"></span>
									<span class="hidden w-14 lg:block"></span>
								{/if}
							</div>

							{#if server.status === ServerStatus.RUNNING}
								<div class="hidden xl:block">
									<MetricsSparkline serverId={server.id} />
								</div>
							{:else}
								<div class="hidden w-24 xl:block"></div>
							{/if}

							<div
								class="relative z-10 flex shrink-0 items-center gap-1 opacity-80 transition-opacity group-hover:opacity-100"
							>
								{#if canStart(server.status)}
									<Button
										variant="ghost"
										size="icon"
										class="size-8 text-status-ok hover:bg-status-ok/10 hover:text-status-ok"
										disabled={busy}
										title="Start"
										onclick={() => handleAction('start', server)}
									>
										<Play class="size-4" />
									</Button>
								{:else if canStop(server.status)}
									<Button
										variant="ghost"
										size="icon"
										class="size-8 text-status-danger hover:bg-status-danger/10 hover:text-status-danger"
										disabled={busy}
										title="Stop"
										onclick={() => handleAction('stop', server)}
									>
										<Square class="size-4" />
									</Button>
								{/if}
								{#if canRestart(server.status)}
									<Button
										variant="ghost"
										size="icon"
										class="hidden size-8 sm:inline-flex"
										disabled={busy}
										title="Restart"
										onclick={() => handleAction('restart', server)}
									>
										<RotateCw class="size-4" />
									</Button>
								{/if}
								<DropdownMenu>
									<DropdownMenuTrigger>
										{#snippet child({ props })}
											<Button
												{...props}
												variant="ghost"
												size="icon"
												class="size-8"
												disabled={busy}
												title="More"
											>
												<MoreHorizontal class="size-4" />
											</Button>
										{/snippet}
									</DropdownMenuTrigger>
									<DropdownMenuContent align="end">
										<DropdownMenuItem
											onclick={() => handleAction('recreate', server)}
											disabled={busy}
										>
											<RefreshCcw class="mr-2 size-4" />
											Recreate container
										</DropdownMenuItem>
										<DropdownMenuSeparator />
										<DropdownMenuItem
											variant="destructive"
											onclick={() => requestDelete(server)}
											disabled={busy}
										>
											<Trash2 class="mr-2 size-4" />
											Delete server
										</DropdownMenuItem>
									</DropdownMenuContent>
								</DropdownMenu>
							</div>
						</div>
					{/each}
				</div>
			</div>
			<p class="text-xs text-muted-foreground">
				{visibleServers.length} of {servers.length}
				{servers.length === 1 ? 'server' : 'servers'}
			</p>
		{/if}
	{/if}
</div>

<ConfirmDialog
	bind:open={deleteOpen}
	title="Delete {deleteTarget?.name ?? 'server'}?"
	description={DELETE_WARNING}
	confirmLabel="Delete server"
	destructive
	onConfirm={confirmDelete}
/>
