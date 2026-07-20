<script lang="ts">
	import { onMount } from 'svelte';
	import { registerRefresh } from '$lib/stores/refresh';
	import { resolve } from '$app/paths';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { ProxyListener } from '$lib/proto/discopanel/v1/storage_pb';
	import type { ProxyListenerWithCount, ProxyRoute } from '$lib/proto/discopanel/v1/proxy_pb';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Button } from '$lib/components/ui/button';
	import { Switch } from '$lib/components/ui/switch';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { EmptyState, ConfirmDialog } from '$lib/components/app';
	import { routeStateLabel, routeStateClass, routeStatsSummary } from '$lib/proxy-route';
	import { serversStore } from '$lib/stores/servers';
	import { toast } from 'svelte-sonner';
	import {
		Save,
		Plus,
		Trash2,
		Loader2,
		Pencil,
		Star,
		Network,
		Globe,
		RotateCcw
	} from '@lucide/svelte';

	let loading = $state(true);
	let saving = $state(false);
	let proxyEnabled = $state(false);
	let savedProxyEnabled = $state(false);
	let baseURL = $state('');
	let savedBaseURL = $state('');
	let listenersWithCount = $state<ProxyListenerWithCount[]>([]);
	let deleteTarget = $state<ProxyListenerWithCount | null>(null);
	let deleteOpen = $state(false);
	let activeRoutes = $state<ProxyRoute[]>([]);

	// One dialog handles create and edit
	let dialogOpen = $state(false);
	let dialogSaving = $state(false);
	let editingId = $state<string | null>(null);
	let draft = $state({
		port: 25565,
		name: '',
		description: '',
		enabled: true,
		isDefault: false
	});
	let portError = $state('');
	let nextFreePort = $state(25565);

	let configDirty = $derived(proxyEnabled !== savedProxyEnabled || baseURL !== savedBaseURL);

	// Resolves route server ids to friendly names
	let serverNames = $derived(new Map($serversStore.map((s) => [s.id, s.name])));

	let activeRouteCount = $derived(activeRoutes.filter((r) => r.active).length);

	onMount(() => {
		loadAll();
		return registerRefresh(loadAll);
	});

	async function loadAll() {
		loading = true;
		try {
			await Promise.all([loadProxyConfig(), loadListeners(), loadActiveRoutes()]);
		} finally {
			loading = false;
		}
	}

	async function loadProxyConfig() {
		try {
			const status = await rpcClient.proxy.getProxyStatus({});
			proxyEnabled = status.enabled;
			savedProxyEnabled = status.enabled;
			baseURL = status.baseUrl || '';
			savedBaseURL = baseURL;
		} catch {
			toast.error('Failed to load proxy configuration');
		}
	}

	async function loadListeners() {
		try {
			const response = await rpcClient.proxy.getProxyListeners({});
			listenersWithCount = response.listeners;
			// Suggest the next free port for a new listener
			const usedPorts = new Set(listenersWithCount.map((lwc) => lwc.listener?.port || 0));
			let candidate = 25565;
			while (usedPorts.has(candidate)) {
				candidate++;
			}
			nextFreePort = candidate;
		} catch {
			toast.error('Failed to load proxy listeners');
		}
	}

	async function loadActiveRoutes() {
		try {
			const response = await rpcClient.proxy.getProxyRoutes({});
			activeRoutes = response.routes;
		} catch (error) {
			console.error('Failed to load active routes:', error);
		}
	}

	function validatePort(port: number): boolean {
		portError = '';

		if (!port || port < 1 || port > 65535) {
			portError = 'Port must be between 1 and 65535';
			return false;
		}

		const existingListener = listenersWithCount.find(
			(lwc) => lwc.listener?.port === port && lwc.listener?.id !== editingId
		);
		if (existingListener) {
			portError = `Port ${port} is already used by "${existingListener.listener?.name}"`;
			return false;
		}

		return true;
	}

	async function saveProxyConfig() {
		saving = true;
		try {
			await rpcClient.proxy.updateProxyConfig({
				enabled: proxyEnabled,
				baseUrl: baseURL
			});

			toast.success('Proxy configuration saved');
			await loadAll();
		} catch {
			toast.error('Failed to save proxy configuration');
		} finally {
			saving = false;
		}
	}

	function openCreateDialog() {
		editingId = null;
		draft = {
			port: nextFreePort,
			name: '',
			description: '',
			enabled: true,
			isDefault: listenersWithCount.length === 0
		};
		portError = '';
		dialogOpen = true;
	}

	function openEditDialog(listener: ProxyListener) {
		editingId = listener.id;
		draft = {
			port: listener.port,
			name: listener.name,
			description: listener.description,
			enabled: listener.enabled,
			isDefault: listener.isDefault
		};
		portError = '';
		dialogOpen = true;
	}

	async function submitDialog() {
		if (!draft.name.trim()) {
			toast.error('Listener name is required');
			return;
		}
		if (!editingId && !validatePort(draft.port)) return;

		dialogSaving = true;
		try {
			if (editingId) {
				await rpcClient.proxy.updateProxyListener({
					id: editingId,
					name: draft.name,
					description: draft.description,
					enabled: draft.enabled,
					isDefault: draft.isDefault
				});
				toast.success(`Listener "${draft.name}" updated`);
			} else {
				await rpcClient.proxy.createProxyListener({
					port: draft.port,
					name: draft.name,
					description: draft.description || '',
					enabled: draft.enabled,
					isDefault: draft.isDefault
				});
				toast.success(`Listener "${draft.name}" created`);
			}
			dialogOpen = false;
			await loadListeners();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to save listener');
		} finally {
			dialogSaving = false;
		}
	}

	async function setDefaultListener(listener: ProxyListener) {
		try {
			await rpcClient.proxy.updateProxyListener({
				id: listener.id,
				name: listener.name,
				description: listener.description,
				enabled: listener.enabled,
				isDefault: true
			});
			toast.success(`"${listener.name}" is now the default listener`);
			await loadListeners();
		} catch {
			toast.error('Failed to update listener');
		}
	}

	function requestDelete(lwc: ProxyListenerWithCount) {
		if (lwc.serverCount > 0) {
			toast.error(`Cannot delete: ${lwc.serverCount} servers are using this listener`);
			return;
		}
		deleteTarget = lwc;
		deleteOpen = true;
	}

	async function confirmDelete() {
		const listener = deleteTarget?.listener;
		if (!listener) return;
		await rpcClient.proxy.deleteProxyListener({ id: listener.id });
		toast.success(`Listener "${listener.name}" deleted`);
		await loadListeners();
	}

	type ListenerState = 'active' | 'inactive' | 'disabled';

	function listenerState(listener: ProxyListener | undefined, serverCount: number): ListenerState {
		if (!listener || !listener.enabled) return 'disabled';
		if (!proxyEnabled) return 'inactive';
		return serverCount > 0 ? 'active' : 'inactive';
	}

	const STATE_LABEL: Record<ListenerState, string> = {
		active: 'Serving traffic',
		inactive: 'Idle',
		disabled: 'Disabled'
	};

	const STATE_DOT: Record<ListenerState, string> = {
		active: 'bg-status-ok',
		inactive: 'bg-status-idle',
		disabled: 'bg-status-idle opacity-40'
	};
</script>

{#if loading}
	<div class="space-y-4">
		<Skeleton class="h-48 rounded-xl" />
		<Skeleton class="h-64 rounded-xl" />
	</div>
{:else}
	<div class="space-y-4">
		<section class="overflow-hidden rounded-xl border bg-card">
			<div class="flex flex-wrap items-center justify-between gap-3 px-4 py-4">
				<div class="flex min-w-0 items-center gap-3">
					<div
						class="flex size-10 shrink-0 items-center justify-center rounded-lg border {proxyEnabled
							? 'border-primary/30 bg-primary/10 text-primary'
							: 'bg-muted/40 text-muted-foreground'}"
					>
						<Network class="size-5" />
					</div>
					<div class="min-w-0">
						<h3 class="text-sm font-semibold">Proxy routing</h3>
						<p class="text-xs text-muted-foreground">
							{#if proxyEnabled}
								{listenersWithCount.length}
								{listenersWithCount.length === 1 ? 'listener' : 'listeners'} ·
								{activeRouteCount} active {activeRouteCount === 1 ? 'route' : 'routes'}
							{:else}
								Players connect through hostnames instead of raw ports
							{/if}
						</p>
					</div>
				</div>
				<Switch
					checked={proxyEnabled}
					onCheckedChange={(checked) => (proxyEnabled = checked)}
					disabled={saving}
					aria-label="Enable proxy"
				/>
			</div>

			<div class="border-t px-4 py-4">
				<div class="max-w-md space-y-2">
					<Label for="base-url" class="flex items-center gap-1.5">
						<Globe class="size-3.5 text-muted-foreground" />
						Base domain
					</Label>
					<Input
						id="base-url"
						type="text"
						bind:value={baseURL}
						placeholder="minecraft.example.com"
						disabled={saving || !proxyEnabled}
					/>
					<p class="text-xs text-muted-foreground">
						Optional domain appended to server hostnames, so "survival" becomes
						"survival.minecraft.example.com"
					</p>
				</div>
			</div>

			{#if configDirty}
				<div class="flex items-center justify-end gap-2 border-t bg-muted/20 px-4 py-3">
					<Button
						variant="outline"
						size="sm"
						disabled={saving}
						onclick={() => {
							proxyEnabled = savedProxyEnabled;
							baseURL = savedBaseURL;
						}}
					>
						<RotateCcw class="size-4" />
						Discard
					</Button>
					<Button size="sm" onclick={saveProxyConfig} disabled={saving}>
						{#if saving}
							<Loader2 class="size-4 animate-spin" />
						{:else}
							<Save class="size-4" />
						{/if}
						Save changes
					</Button>
				</div>
			{/if}
		</section>

		{#if proxyEnabled}
			<section class="overflow-hidden rounded-xl border bg-card">
				<header
					class="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/30 px-4 py-3"
				>
					<div class="min-w-0">
						<h3 class="text-sm font-semibold">Listeners</h3>
						<p class="mt-0.5 text-xs text-muted-foreground">
							Ports the proxy accepts player connections on
						</p>
					</div>
					<Button size="sm" variant="outline" onclick={openCreateDialog}>
						<Plus class="size-4" />
						Add listener
					</Button>
				</header>

				{#if listenersWithCount.length === 0}
					<EmptyState
						icon={Network}
						title="No listeners yet"
						description="Add a listener port for the proxy to accept player connections."
					>
						<Button size="sm" onclick={openCreateDialog}>
							<Plus class="size-4" />
							Add listener
						</Button>
					</EmptyState>
				{:else}
					<div class="divide-y">
						{#each listenersWithCount as lwc (lwc.listener?.id)}
							{@const listener = lwc.listener}
							{@const state = listenerState(listener, lwc.serverCount)}
							{#if listener}
								<div
									class="group flex items-center gap-3 px-4 py-3 transition-colors hover:bg-accent/30"
								>
									<span
										class="size-2 shrink-0 rounded-full {STATE_DOT[state]}"
										title={STATE_LABEL[state]}
									></span>
									<div class="min-w-0 flex-1">
										<div class="flex flex-wrap items-center gap-2">
											<span class="truncate text-sm font-medium">{listener.name}</span>
											<Badge variant="secondary" class="tabular font-mono text-xs">
												:{listener.port}
											</Badge>
											{#if listener.isDefault}
												<Badge
													variant="outline"
													class="gap-1 border-primary/30 text-xs text-primary"
												>
													<Star class="size-3" />
													Default
												</Badge>
											{/if}
											{#if !listener.enabled}
												<Badge variant="outline" class="text-xs">Disabled</Badge>
											{/if}
										</div>
										<p class="mt-0.5 truncate text-xs text-muted-foreground">
											{#if listener.description}
												{listener.description} ·
											{/if}
											{lwc.serverCount > 0
												? `${lwc.serverCount} ${lwc.serverCount === 1 ? 'server' : 'servers'}`
												: 'no servers'}
										</p>
									</div>

									<div
										class="flex shrink-0 items-center gap-1 opacity-60 transition-opacity group-hover:opacity-100"
									>
										{#if !listener.isDefault}
											<Button
												variant="ghost"
												size="icon"
												class="size-8"
												onclick={() => setDefaultListener(listener)}
												title="Set as default"
											>
												<Star class="size-4" />
											</Button>
										{/if}
										<Button
											variant="ghost"
											size="icon"
											class="size-8"
											onclick={() => openEditDialog(listener)}
											title="Edit listener"
										>
											<Pencil class="size-4" />
										</Button>
										{#if listenersWithCount.length > 1 && lwc.serverCount === 0}
											<Button
												variant="ghost"
												size="icon"
												class="size-8 text-status-danger hover:bg-status-danger/10 hover:text-status-danger"
												onclick={() => requestDelete(lwc)}
												title="Delete listener"
											>
												<Trash2 class="size-4" />
											</Button>
										{/if}
									</div>
								</div>
							{/if}
						{/each}
					</div>
				{/if}
			</section>

			{#if activeRoutes.length > 0}
				<section class="overflow-hidden rounded-xl border bg-card">
					<header class="border-b bg-muted/30 px-4 py-3">
						<h3 class="text-sm font-semibold">Active routes</h3>
						<p class="mt-0.5 text-xs text-muted-foreground">
							Servers currently routed through the proxy
						</p>
					</header>
					<div class="divide-y">
						{#each activeRoutes as route (route.serverId)}
							{@const stats = routeStatsSummary(route)}
							<div class="flex items-center justify-between gap-3 px-4 py-2.5">
								<div class="min-w-0">
									<p class="truncate font-mono text-sm">{route.hostname}</p>
									{#if serverNames.get(route.serverId)}
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
		{/if}
	</div>
{/if}

<Dialog bind:open={dialogOpen}>
	<DialogContent class="sm:max-w-md">
		<DialogHeader>
			<DialogTitle>{editingId ? 'Edit listener' : 'New listener'}</DialogTitle>
			<DialogDescription>
				{editingId
					? 'Rename the listener or change how it behaves.'
					: 'Open a new port for the proxy to accept player connections.'}
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-4">
			<div class="grid gap-4 sm:grid-cols-[minmax(0,1fr)_7rem]">
				<div class="space-y-2">
					<Label for="listener-name">Name</Label>
					<Input id="listener-name" bind:value={draft.name} placeholder="e.g. Main, Development" />
				</div>
				<div class="space-y-2">
					<Label for="listener-port">Port</Label>
					<Input
						id="listener-port"
						type="number"
						bind:value={draft.port}
						disabled={!!editingId}
						oninput={(e) => validatePort(Number(e.currentTarget.value))}
						class={portError ? 'border-destructive' : editingId ? 'bg-muted' : ''}
					/>
				</div>
			</div>
			{#if portError}
				<p class="text-xs text-destructive">{portError}</p>
			{:else if editingId}
				<p class="text-xs text-muted-foreground">Listener ports cannot change after creation</p>
			{/if}

			<div class="space-y-2">
				<Label for="listener-description">Description</Label>
				<Input
					id="listener-description"
					bind:value={draft.description}
					placeholder="Optional description"
				/>
			</div>

			<div class="space-y-3 rounded-lg border px-3.5 py-3">
				<label class="flex cursor-pointer items-center justify-between gap-3 text-sm">
					<span>
						Enabled
						<span class="block text-xs font-normal text-muted-foreground">
							Accept connections on this port
						</span>
					</span>
					<Switch
						checked={draft.enabled}
						onCheckedChange={(checked) => (draft.enabled = checked)}
					/>
				</label>
				<label class="flex cursor-pointer items-center justify-between gap-3 border-t pt-3 text-sm">
					<span>
						Default listener
						<span class="block text-xs font-normal text-muted-foreground">
							New proxied servers use this port
						</span>
					</span>
					<Switch
						checked={draft.isDefault}
						onCheckedChange={(checked) => (draft.isDefault = checked)}
					/>
				</label>
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => (dialogOpen = false)} disabled={dialogSaving}>
				Cancel
			</Button>
			<Button onclick={submitDialog} disabled={dialogSaving || !draft.name.trim() || !!portError}>
				{#if dialogSaving}
					<Loader2 class="size-4 animate-spin" />
				{:else if editingId}
					<Save class="size-4" />
				{:else}
					<Plus class="size-4" />
				{/if}
				{editingId ? 'Save changes' : 'Add listener'}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>

<ConfirmDialog
	bind:open={deleteOpen}
	title="Delete listener {deleteTarget?.listener?.name ?? ''}?"
	description="The proxy will stop accepting connections on port {deleteTarget?.listener?.port ??
		''}."
	confirmLabel="Delete listener"
	destructive
	onConfirm={confirmDelete}
/>
