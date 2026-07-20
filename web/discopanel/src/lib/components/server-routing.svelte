<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { resolve } from '$app/paths';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { Input } from '$lib/components/ui/input';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { CopyButton, EmptyState } from '$lib/components/app';
	import { serversStore } from '$lib/stores/servers';
	import { canAccessSettings } from '$lib/stores/auth';
	import {
		Loader2,
		Save,
		AlertCircle,
		Network,
		ArrowUpRight,
		Globe,
		Cable,
		Users,
		Container,
		ArrowRight,
		RotateCcw
	} from '@lucide/svelte';
	import type { Server } from '$lib/proto/discopanel/v1/storage_pb';
	import { ServerStatus } from '$lib/proto/discopanel/v1/storage_pb';
	import type { GetServerRoutingResponse, ProxyRoute } from '$lib/proto/discopanel/v1/proxy_pb';
	import { routeStateLabel, routeStateClass, routeStatsSummary } from '$lib/proxy-route';

	let { server, active = true }: { server: Server; active?: boolean } = $props();

	let loading = $state(true);
	let saving = $state(false);
	let hostname = $state('');
	let originalHostname = $state('');
	let hasChanges = $derived(hostname !== originalHostname);
	let routingInfo = $state<GetServerRoutingResponse | null>(null);
	let allRoutes = $state<ProxyRoute[]>([]);
	let hostnameError = $state('');
	let showSettingsLink = $derived($canAccessSettings);

	// Resolves route server ids to friendly names
	let serverNames = $derived(new Map($serversStore.map((s) => [s.id, s.name])));

	// Reload whenever the tab shows a different server
	let loadedServerId = $state('');
	$effect(() => {
		if (active && server.id !== loadedServerId) {
			loadedServerId = server.id;
			untrack(() => {
				loading = true;
				hostname = '';
				originalHostname = '';
				hostnameError = '';
				loadAll();
			});
		}
	});

	onMount(() => {
		if (active && server.id !== loadedServerId) {
			loadedServerId = server.id;
			loadAll();
		}
	});

	async function loadAll() {
		await Promise.all([loadRoutingInfo(), loadAllRoutes()]);
	}

	async function loadRoutingInfo() {
		try {
			loading = true;
			const response = await rpcClient.proxy.getServerRouting({ serverId: server.id });
			routingInfo = response;
			hostname = response.proxyHostname || '';
			originalHostname = hostname;
		} catch {
			toast.error('Failed to load routing information');
		} finally {
			loading = false;
		}
	}

	async function loadAllRoutes() {
		try {
			const response = await rpcClient.proxy.getProxyRoutes({});
			allRoutes = response.routes;
		} catch {
			// Route list is optional context
		}
	}

	function validateHostname(value: string) {
		if (!value) {
			hostnameError = '';
			return true;
		}

		const hostnameRegex =
			/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/i;
		if (!hostnameRegex.test(value)) {
			hostnameError = 'Invalid hostname format';
			return false;
		}

		const conflict = allRoutes.find(
			(route) =>
				route.hostname.toLowerCase() === value.toLowerCase() && route.serverId !== server.id
		);
		if (conflict) {
			hostnameError = 'Hostname already in use by another server';
			return false;
		}

		hostnameError = '';
		return true;
	}

	async function saveRouting() {
		if (!validateHostname(hostname)) return;

		saving = true;
		try {
			await rpcClient.proxy.updateServerRouting({
				serverId: server.id,
				proxyHostname: hostname
			});
			toast.success(hostname ? 'Hostname saved' : 'Hostname removed');
			originalHostname = hostname;
			await loadAll();
		} catch (error: unknown) {
			if (error instanceof Error && error.message.includes('Conflict')) {
				hostnameError = 'Hostname already in use by another server';
			} else {
				toast.error('Failed to save routing configuration');
			}
		} finally {
			saving = false;
		}
	}

	let routed = $derived(!!(routingInfo?.currentRoute || routingInfo?.proxyHostname));
	let routeLive = $derived(!!routingInfo?.currentRoute && server.status === ServerStatus.RUNNING);

	function fullHostname(): string {
		if (hostname) return hostname;
		if (routingInfo?.suggestedHostname) return routingInfo.suggestedHostname;
		return `${server.name.toLowerCase().replace(/\s+/g, '-')}.minecraft.local`;
	}

	// Address players actually use right now
	let playerAddress = $derived.by(() => {
		if (routed && routingInfo) {
			const host = routingInfo.proxyHostname || fullHostname();
			const port = routingInfo.listenPort || 25565;
			return port === 25565 ? host : `${host}:${port}`;
		}
		return `localhost:${server.port}`;
	});

	// Preview address for the value being typed
	let previewAddress = $derived.by(() => {
		const host = fullHostname();
		const port = routingInfo?.listenPort || 25565;
		return port === 25565 ? host : `${host}:${port}`;
	});

	// Own route pinned first in the shared route table
	let sortedRoutes = $derived(
		[...allRoutes].sort((a, b) => {
			if (a.serverId === server.id) return -1;
			if (b.serverId === server.id) return 1;
			return a.hostname.localeCompare(b.hostname);
		})
	);
</script>

{#snippet pathNode(icon: typeof Users, title: string, sub: string, tone: 'active' | 'muted')}
	{@const Icon = icon}
	<div
		class="flex min-w-0 items-center gap-2.5 rounded-lg border px-3 py-2 {tone === 'active'
			? 'border-primary/30 bg-primary/5'
			: 'bg-muted/30'}"
	>
		<Icon class="size-4 shrink-0 {tone === 'active' ? 'text-primary' : 'text-muted-foreground'}" />
		<div class="min-w-0">
			<p class="truncate text-xs font-medium">{title}</p>
			<p class="truncate font-mono text-[11px] text-muted-foreground">{sub}</p>
		</div>
	</div>
{/snippet}

{#if loading}
	<div class="space-y-4">
		<Skeleton class="h-44 rounded-xl" />
		<Skeleton class="h-56 rounded-xl" />
	</div>
{:else if !routingInfo?.proxyEnabled}
	<div class="rounded-xl border bg-card">
		<EmptyState
			icon={Network}
			title="Proxy routing is disabled"
			description="With the proxy enabled, players connect through a hostname like play.example.com instead of a port number."
		>
			{#if showSettingsLink}
				<Button href="{resolve('/settings')}?tab=routing" variant="outline">
					Open routing settings
					<ArrowUpRight class="size-3.5" />
				</Button>
			{/if}
		</EmptyState>
	</div>
{:else}
	<div class="space-y-4">
		<section class="overflow-hidden rounded-xl border bg-card">
			<div class="flex flex-wrap items-center justify-between gap-3 px-5 pt-4">
				<span class="stat-label">Player address</span>
				{#if routed}
					<span
						class="inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium {routeLive
							? 'border-status-ok/25 bg-status-ok/10 text-status-ok'
							: 'border-status-busy/25 bg-status-busy/10 text-status-busy'}"
					>
						<Globe class="size-3" />
						{routeLive ? 'Routed via proxy' : 'Route activates on start'}
					</span>
				{:else}
					<span
						class="inline-flex items-center gap-1.5 rounded-full border border-status-idle/25 bg-status-idle/10 px-2 py-0.5 text-xs font-medium text-status-idle"
					>
						<Cable class="size-3" />
						Direct connection
					</span>
				{/if}
			</div>

			<div class="px-5 py-4">
				<div
					class="flex items-center justify-between gap-3 rounded-lg border bg-muted/40 py-2 pr-2 pl-4"
				>
					<p class="truncate font-mono text-lg" title={playerAddress}>{playerAddress}</p>
					<CopyButton text={playerAddress} label="Copy address" />
				</div>
				<p class="mt-2 text-xs text-muted-foreground">
					What players type into their multiplayer server list
					{#if !routed && server.status !== ServerStatus.RUNNING}
						· start the server after setting a hostname to activate the route
					{/if}
				</p>
			</div>

			<div class="border-t bg-muted/20 px-5 py-3.5">
				<div class="flex flex-wrap items-center gap-2">
					{@render pathNode(Users, 'Players', routed ? playerAddress : 'direct connect', 'active')}
					<ArrowRight class="size-3.5 shrink-0 text-muted-foreground/60" />
					{#if routed}
						{@render pathNode(
							Network,
							'Proxy listener',
							`:${routingInfo.listenPort || 25565}`,
							'active'
						)}
						<ArrowRight class="size-3.5 shrink-0 text-muted-foreground/60" />
					{/if}
					{@render pathNode(
						Container,
						server.name,
						`container :${server.port}`,
						routed ? 'muted' : 'active'
					)}
				</div>
			</div>
		</section>

		<section class="overflow-hidden rounded-xl border bg-card">
			<header class="border-b bg-muted/30 px-4 py-3">
				<h3 class="text-sm font-semibold">Hostname</h3>
				<p class="mt-0.5 text-xs text-muted-foreground">
					Route this server through the proxy with a custom hostname
				</p>
			</header>
			<div class="space-y-4 px-4 py-4">
				<div class="max-w-lg space-y-2">
					<Label for="hostname">Custom hostname</Label>
					<Input
						id="hostname"
						type="text"
						bind:value={hostname}
						placeholder={routingInfo.suggestedHostname || 'minecraft.example.com'}
						oninput={(e) => validateHostname(e.currentTarget.value)}
						class={hostnameError ? 'border-destructive' : ''}
					/>
					{#if hostnameError}
						<p class="text-sm text-destructive">{hostnameError}</p>
					{:else if hostname}
						<p class="text-xs text-muted-foreground">
							Players will connect using <span class="font-mono text-foreground"
								>{previewAddress}</span
							>
						</p>
					{:else if originalHostname}
						<p class="text-xs text-muted-foreground">
							Leave empty and save to remove the route and go back to direct port access
						</p>
					{:else}
						<p class="text-xs text-muted-foreground">
							Leave empty to keep connecting directly via port {server.port}
						</p>
					{/if}
				</div>

				{#if hostname && routingInfo.baseUrl}
					<Alert>
						<AlertCircle class="size-4" />
						<AlertDescription>
							<p class="mb-1 font-medium">DNS configuration required</p>
							<p class="text-sm">
								Add a DNS record pointing
								<code class="font-mono">{fullHostname()}</code> to this machine's IP address.
							</p>
						</AlertDescription>
					</Alert>
				{/if}

				{#if hasChanges}
					<div class="flex items-center justify-end gap-2 border-t pt-3">
						<Button
							variant="outline"
							size="sm"
							onclick={() => {
								hostname = originalHostname;
								hostnameError = '';
							}}
							disabled={saving}
						>
							<RotateCcw class="size-4" />
							Discard
						</Button>
						<Button size="sm" onclick={saveRouting} disabled={saving || !!hostnameError}>
							{#if saving}
								<Loader2 class="size-4 animate-spin" />
							{:else}
								<Save class="size-4" />
							{/if}
							Save hostname
						</Button>
					</div>
				{/if}
			</div>
		</section>

		<section class="overflow-hidden rounded-xl border bg-card">
			<header class="flex items-baseline justify-between gap-2 border-b bg-muted/30 px-4 py-3">
				<div>
					<h3 class="text-sm font-semibold">Exposed ports</h3>
					<p class="mt-0.5 text-xs text-muted-foreground">
						Ports this server publishes on the host
					</p>
				</div>
				<a
					href="{resolve(`/servers/${server.id}`)}?tab=settings#network"
					class="shrink-0 text-xs text-primary hover:underline"
				>
					Manage ports
				</a>
			</header>
			<div class="divide-y">
				<div class="flex items-center justify-between px-4 py-2.5">
					<div class="min-w-0">
						<p class="text-sm font-medium">Game port</p>
						<p class="text-xs text-muted-foreground">
							{routed ? 'Reached through the proxy listener' : 'Primary Minecraft port'}
						</p>
					</div>
					<span class="tabular font-mono text-sm">{server.port}</span>
				</div>
				{#each server.additionalPorts || [] as port (port.name + port.hostPort)}
					<div class="flex items-center justify-between px-4 py-2.5">
						<div class="min-w-0">
							<p class="truncate text-sm font-medium">{port.name || 'Additional port'}</p>
							<p class="text-xs text-muted-foreground">
								container {port.containerPort} · {port.protocol || 'tcp'}
							</p>
						</div>
						<span class="tabular font-mono text-sm">{port.hostPort}</span>
					</div>
				{/each}
			</div>
		</section>

		{#if sortedRoutes.length > 0}
			<section class="overflow-hidden rounded-xl border bg-card">
				<header class="border-b bg-muted/30 px-4 py-3">
					<h3 class="text-sm font-semibold">Active routes</h3>
					<p class="mt-0.5 text-xs text-muted-foreground">
						Every hostname the proxy currently serves
					</p>
				</header>
				<div class="divide-y">
					{#each sortedRoutes as route (route.serverId)}
						{@const isSelf = route.serverId === server.id}
						{@const stats = routeStatsSummary(route)}
						<div
							class="flex items-center justify-between gap-3 px-4 py-2.5 {isSelf
								? 'bg-primary/[0.03]'
								: ''}"
						>
							<div class="min-w-0">
								<p class="truncate font-mono text-sm">{route.hostname}</p>
								{#if isSelf}
									<p class="text-xs font-medium text-primary">This server</p>
								{:else if serverNames.get(route.serverId)}
									<a
										href={resolve(`/servers/${route.serverId}`)}
										class="text-xs text-muted-foreground hover:text-foreground hover:underline"
									>
										{serverNames.get(route.serverId)}
									</a>
								{:else}
									<p class="font-mono text-xs text-muted-foreground">
										{route.serverId.slice(0, 8)}
									</p>
								{/if}
								{#if stats}
									<p class="text-xs text-muted-foreground tabular-nums">{stats}</p>
								{/if}
							</div>
							<Badge variant="outline" class="text-xs {routeStateClass(route)}">
								{routeStateLabel(route)}
							</Badge>
						</div>
					{/each}
				</div>
			</section>
		{/if}
	</div>
{/if}
