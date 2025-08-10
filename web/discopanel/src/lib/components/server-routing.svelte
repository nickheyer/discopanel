<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { toast } from 'svelte-sonner';
	import { Input } from '$lib/components/ui/input';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Loader2, Globe, Save, Copy, AlertCircle, CheckCircle2, XCircle } from '@lucide/svelte';
	import type { Server } from '$lib/api/types';

	let { server, active }: { server: Server, active?: boolean } = $props();

	let loading = $state(true);
	let saving = $state(false);
	let routingInfo = $state<any>(null);
	let hostname = $state('');
	let originalHostname = $state('');
	let hasChanges = $derived(hostname !== originalHostname);
	let allRoutes = $state<any[]>([]);
	let hostnameError = $state('');
	let initialized = $state(false);
	let previousServerId = $state(server.id);

	// Reset state when server changes
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			// Reset state variables
			loading = true;
			saving = false;
			routingInfo = null;
			hostname = '';
			originalHostname = '';
			allRoutes = [];
			hostnameError = '';
			initialized = false;
		}
	});

	$effect(() => {
		if (server && !initialized && active) {
			initialized = true;
			loadRoutingInfo();
			loadAllRoutes();
		}
	});

	async function loadRoutingInfo() {
		try {
			loading = true;
			routingInfo = await api.getServerRouting(server.id);
			hostname = routingInfo.proxy_hostname || '';
			originalHostname = hostname;
		} catch (error) {
			toast.error('Failed to load routing information');
		} finally {
			loading = false;
		}
	}

	async function loadAllRoutes() {
		try {
			allRoutes = await api.getProxyRoutes();
		} catch (error) {
			// Not critical
		}
	}

	function validateHostname(value: string) {
		if (!value) {
			hostnameError = '';
			return true;
		}

		// Basic hostname validation
		const hostnameRegex = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/i;
		if (!hostnameRegex.test(value)) {
			hostnameError = 'Invalid hostname format';
			return false;
		}

		// Check for conflicts
		const conflict = allRoutes.find(route => 
			route.hostname.toLowerCase() === value.toLowerCase() && 
			route.server_id !== server.id
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
			await api.updateServerRouting(server.id, hostname);
			toast.success('Routing configuration saved');
			originalHostname = hostname;
			// Reload routing info to get updated server state
			await loadRoutingInfo();
			await loadAllRoutes();
		} catch (error: any) {
			if (error.message.includes('Conflict')) {
				hostnameError = 'Hostname already in use by another server';
			} else {
				toast.error('Failed to save routing configuration');
			}
		} finally {
			saving = false;
		}
	}

	function copyToClipboard(text: string) {
		navigator.clipboard.writeText(text);
		toast.success('Copied to clipboard');
	}

	function getFullHostname() {
		if (hostname) return hostname;
		if (routingInfo?.suggested_hostname) return routingInfo.suggested_hostname;
		return `${server.name.toLowerCase().replace(/\s+/g, '-')}.minecraft.local`;
	}

	function getConnectionString() {
		const host = getFullHostname();
		const port = routingInfo?.listen_port || 25565;
		return port === 25565 ? host : `${host}:${port}`;
	}
</script>

{#if loading}
	<div class="flex items-center justify-center py-8">
		<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
	</div>
{:else if !routingInfo?.proxy_enabled}
	<Alert>
		<AlertCircle class="h-4 w-4" />
		<AlertDescription>
			Proxy routing is not enabled. Enable it in your configuration file to use custom hostnames for your servers.
		</AlertDescription>
	</Alert>
{:else}
	<div class="space-y-4">
		<!-- Status Card -->
		<Card>
			<CardHeader>
				<CardTitle class="flex items-center gap-2">
					<Globe class="h-5 w-5" />
					Current Routing Status
				</CardTitle>
				<CardDescription>
					How players can connect to your server
				</CardDescription>
			</CardHeader>
			<CardContent class="space-y-4">
				{#if routingInfo.current_route || routingInfo.proxy_hostname}
					<div class="flex items-center gap-2">
						<Badge variant="default" class="gap-1">
							<CheckCircle2 class="h-3 w-3" />
							Active Route
						</Badge>
						<span class="text-sm text-muted-foreground">
							Players can connect using the hostname below
						</span>
					</div>
				{:else if server.status === 'running'}
					<div class="flex items-center gap-2">
						<Badge variant="secondary" class="gap-1">
							<AlertCircle class="h-3 w-3" />
							No Active Route
						</Badge>
						<span class="text-sm text-muted-foreground">
							Configure a hostname below to enable proxy routing
						</span>
					</div>
				{:else}
					<div class="flex items-center gap-2">
						<Badge variant="outline" class="gap-1">
							<XCircle class="h-3 w-3" />
							Server Offline
						</Badge>
						<span class="text-sm text-muted-foreground">
							Start the server to activate routing
						</span>
					</div>
				{/if}

				<div class="p-4 bg-muted rounded-lg">
					<div class="flex items-center justify-between">
						<div>
							<p class="text-sm font-medium mb-1">Connection Address</p>
							<p class="font-mono text-lg">{getConnectionString()}</p>
						</div>
						<Button
							variant="outline"
							size="icon"
							onclick={() => copyToClipboard(getConnectionString())}
						>
							<Copy class="h-4 w-4" />
						</Button>
					</div>
				</div>
			</CardContent>
		</Card>

		<!-- Configuration Card -->
		<Card>
			<CardHeader>
				<CardTitle>Hostname Configuration</CardTitle>
				<CardDescription>
					Set a custom hostname for players to connect to your server
				</CardDescription>
			</CardHeader>
			<CardContent class="space-y-4">
				<div class="space-y-2">
					<Label for="hostname">Custom Hostname</Label>
					<Input
						id="hostname"
						type="text"
						bind:value={hostname}
						placeholder={routingInfo.suggested_hostname || 'minecraft.example.com'}
						oninput={(e) => validateHostname(e.currentTarget.value)}
						class={hostnameError ? 'border-destructive' : ''}
					/>
					{#if hostnameError}
						<p class="text-sm text-destructive">{hostnameError}</p>
					{:else if hostname}
						<p class="text-sm text-muted-foreground">
							Players will connect using: <span class="font-mono">{getConnectionString()}</span>
						</p>
					{:else}
						<p class="text-sm text-muted-foreground">
							Leave empty to use the default hostname based on your server name
						</p>
					{/if}
				</div>

				{#if routingInfo.base_url}
					<Alert>
						<AlertDescription>
							<p class="font-medium mb-1">DNS Configuration Required</p>
							<p class="text-sm">
								Make sure to add a DNS record pointing <code class="font-mono">{getFullHostname()}</code> to your server's IP address.
							</p>
						</AlertDescription>
					</Alert>
				{/if}

				<div class="flex justify-end gap-2">
					<Button
						variant="outline"
						onclick={() => { hostname = originalHostname; hostnameError = ''; }}
						disabled={!hasChanges || saving}
					>
						Cancel
					</Button>
					<Button
						onclick={saveRouting}
						disabled={!hasChanges || saving || !!hostnameError}
					>
						{#if saving}
							<Loader2 class="h-4 w-4 mr-2 animate-spin" />
						{:else}
							<Save class="h-4 w-4 mr-2" />
						{/if}
						Save Changes
					</Button>
				</div>
			</CardContent>
		</Card>

		<!-- Other Servers Using Proxy -->
		{#if allRoutes.length > 0}
			<Card>
				<CardHeader>
					<CardTitle>Active Routes</CardTitle>
					<CardDescription>
						Other servers currently using proxy routing
					</CardDescription>
				</CardHeader>
				<CardContent>
					<div class="space-y-2">
						{#each allRoutes as route}
							<div class="flex items-center justify-between p-3 rounded-lg bg-muted/50">
								<div>
									<p class="font-mono text-sm">{route.hostname}</p>
									<p class="text-xs text-muted-foreground">
										{route.server_id === server.id ? '(This server)' : `Server: ${route.server_id.slice(0, 8)}...`}
									</p>
								</div>
								{#if route.active}
									<Badge variant="default" class="text-xs">Active</Badge>
								{:else}
									<Badge variant="outline" class="text-xs">Inactive</Badge>
								{/if}
							</div>
						{/each}
					</div>
				</CardContent>
			</Card>
		{/if}
	</div>
{/if}