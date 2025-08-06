<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Button } from '$lib/components/ui/button';
	import { Switch } from '$lib/components/ui/switch';
	import { Badge } from '$lib/components/ui/badge';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { toast } from 'svelte-sonner';
	import { 
		Save, Plus, Trash2, Loader2, Globe, AlertCircle, 
		Server, Activity, CheckCircle2, XCircle, Network,
		Settings, Info, Edit, Star
	} from '@lucide/svelte';

	interface ProxyListener {
		id: string;
		port: number;
		name: string;
		description: string;
		enabled: boolean;
		is_default: boolean;
		server_count?: number;
		created_at?: string;
		updated_at?: string;
	}

	let loading = $state(true);
	let saving = $state(false);
	let proxyEnabled = $state(false);
	let baseURL = $state('');
	let listeners = $state<ProxyListener[]>([]);
	let editingListener = $state<ProxyListener | null>(null);
	let newListener = $state<Partial<ProxyListener>>({
		port: 25565,
		name: '',
		description: '',
		enabled: true,
		is_default: false
	});
	let portError = $state('');
	let activeRoutes = $state<any[]>([]);

	onMount(() => {
		loadAll();
	});

	async function loadAll() {
		loading = true;
		try {
			await Promise.all([
				loadProxyConfig(),
				loadListeners(),
				loadActiveRoutes()
			]);
		} finally {
			loading = false;
		}
	}

	async function loadProxyConfig() {
		try {
			const status = await api.getProxyStatus();
			proxyEnabled = status.enabled;
			baseURL = status.base_url || '';
		} catch (error) {
			toast.error('Failed to load proxy configuration');
		}
	}

	async function loadListeners() {
		try {
			listeners = await api.getProxyListeners();
			// Set default port for new listener
			if (listeners.length > 0) {
				const usedPorts = new Set(listeners.map(l => l.port));
				let nextPort = 25565;
				while (usedPorts.has(nextPort)) {
					nextPort++;
				}
				newListener.port = nextPort;
			}
		} catch (error) {
			toast.error('Failed to load proxy listeners');
		}
	}

	async function loadActiveRoutes() {
		try {
			activeRoutes = await api.getProxyRoutes();
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
		
		// Check if port is already used by another listener
		const existingListener = listeners.find(l => l.port === port && l.id !== editingListener?.id);
		if (existingListener) {
			portError = `Port ${port} is already used by listener "${existingListener.name}"`;
			return false;
		}
		
		return true;
	}

	async function saveProxyConfig() {
		saving = true;
		try {
			await api.updateProxyConfig({
				enabled: proxyEnabled,
				base_url: baseURL
			});
			
			toast.success('Proxy configuration saved');
			await loadAll();
		} catch (error) {
			toast.error('Failed to save proxy configuration');
		} finally {
			saving = false;
		}
	}

	async function createListener() {
		if (!newListener.name) {
			toast.error('Listener name is required');
			return;
		}
		
		if (!validatePort(newListener.port!)) {
			return;
		}

		try {
			await api.createProxyListener({
				port: newListener.port!,
				name: newListener.name,
				description: newListener.description || '',
				enabled: newListener.enabled,
				is_default: newListener.is_default
			});
			
			toast.success(`Listener "${newListener.name}" created`);
			
			// Reset form
			newListener = {
				port: 25565,
				name: '',
				description: '',
				enabled: true,
				is_default: false
			};
			
			await loadListeners();
		} catch (error: any) {
			toast.error(error.message || 'Failed to create listener');
		}
	}

	async function updateListener(listener: ProxyListener) {
		try {
			await api.updateProxyListener(listener.id, {
				name: listener.name,
				description: listener.description,
				enabled: listener.enabled,
				is_default: listener.is_default
			});
			
			toast.success(`Listener "${listener.name}" updated`);
			editingListener = null;
			await loadListeners();
		} catch (error) {
			toast.error('Failed to update listener');
		}
	}

	async function deleteListener(listener: ProxyListener) {
		if (listener.server_count && listener.server_count > 0) {
			toast.error(`Cannot delete: ${listener.server_count} servers are using this listener`);
			return;
		}

		if (confirm(`Delete listener "${listener.name}" on port ${listener.port}?`)) {
			try {
				await api.deleteProxyListener(listener.id);
				toast.success(`Listener "${listener.name}" deleted`);
				await loadListeners();
			} catch (error: any) {
				toast.error(error.message || 'Failed to delete listener');
			}
		}
	}

	async function setDefaultListener(listener: ProxyListener) {
		listener.is_default = true;
		await updateListener(listener);
	}

	function getListenerStatus(listener: ProxyListener): 'active' | 'inactive' | 'disabled' {
		if (!listener.enabled) return 'disabled';
		if (!proxyEnabled) return 'inactive';
		return listener.server_count && listener.server_count > 0 ? 'active' : 'inactive';
	}

	function getStatusColor(status: string): string {
		switch (status) {
			case 'active': return 'text-green-500';
			case 'inactive': return 'text-yellow-500';
			case 'disabled': return 'text-gray-500';
			default: return 'text-gray-500';
		}
	}

	function getStatusIcon(status: string) {
		switch (status) {
			case 'active': return CheckCircle2;
			case 'disabled': return XCircle;
			default: return AlertCircle;
		}
	}
</script>

<div class="space-y-6">
	<!-- Global Proxy Configuration -->
	<Card>
		<CardHeader>
			<div class="flex items-center justify-between">
				<div class="flex items-center gap-3">
					<Network class="h-5 w-5 text-primary" />
					<div>
						<CardTitle>Proxy Configuration</CardTitle>
						<CardDescription>
							Global proxy settings and base domain configuration
						</CardDescription>
					</div>
				</div>
				<Switch
					checked={proxyEnabled}
					onCheckedChange={(checked) => proxyEnabled = checked}
					disabled={loading || saving}
				/>
			</div>
		</CardHeader>
		<CardContent class="space-y-4">
			<div class="space-y-2">
				<Label for="base-url">Base Domain</Label>
				<Input
					id="base-url"
					type="text"
					bind:value={baseURL}
					placeholder="minecraft.example.com"
					disabled={saving || !proxyEnabled}
				/>
				<p class="text-xs text-muted-foreground">
					Optional base domain that will be appended to server hostnames (e.g., "survival" becomes "survival.minecraft.example.com")
				</p>
			</div>
			
			<div class="flex justify-end">
				<Button
					onclick={saveProxyConfig}
					disabled={saving}
				>
					{#if saving}
						<Loader2 class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<Save class="h-4 w-4 mr-2" />
					{/if}
					Save Configuration
				</Button>
			</div>
		</CardContent>
	</Card>

	{#if loading}
		<Card>
			<CardContent class="flex items-center justify-center py-12">
				<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
			</CardContent>
		</Card>
	{:else if !proxyEnabled}
		<Alert>
			<Info class="h-4 w-4" />
			<AlertDescription>
				Enable the proxy system to allow servers to use custom hostnames instead of direct port connections.
			</AlertDescription>
		</Alert>
	{:else}
		<!-- Proxy Listeners -->
		<Card>
			<CardHeader>
				<div class="flex items-center justify-between">
					<div>
						<CardTitle>Proxy Listeners</CardTitle>
						<CardDescription>
							Configure individual proxy listening ports
						</CardDescription>
					</div>
					<Badge variant="outline" class="gap-1">
						<Server class="h-3 w-3" />
						{listeners.length} {listeners.length === 1 ? 'Listener' : 'Listeners'}
					</Badge>
				</div>
			</CardHeader>
			<CardContent class="space-y-4">
				<!-- Existing Listeners -->
				{#if listeners.length > 0}
					<div class="space-y-3">
						{#each listeners as listener}
							{@const status = getListenerStatus(listener)}
							{@const StatusIcon = getStatusIcon(status)}
							<div class="p-4 rounded-lg border bg-card">
								{#if editingListener?.id === listener.id}
									<!-- Edit Mode -->
									<div class="space-y-3">
										<div class="grid grid-cols-2 gap-3">
											<div class="space-y-2">
												<Label>Name</Label>
												<Input
													bind:value={editingListener.name}
													placeholder="Listener name"
												/>
											</div>
											<div class="space-y-2">
												<Label>Port</Label>
												<Input
													type="number"
													value={listener.port}
													disabled
													class="bg-muted"
												/>
											</div>
										</div>
										<div class="space-y-2">
											<Label>Description</Label>
											<Input
												bind:value={editingListener.description}
												placeholder="Optional description"
											/>
										</div>
										<div class="flex items-center justify-between">
											<div class="flex items-center gap-4">
												<div class="flex items-center gap-2">
													<Switch
														checked={editingListener?.enabled ?? false}
														onCheckedChange={(checked) => { if (editingListener) editingListener.enabled = checked }}
													/>
													<Label>Enabled</Label>
												</div>
												<div class="flex items-center gap-2">
													<Switch
														checked={editingListener?.is_default ?? false}
														onCheckedChange={(checked) => { if (editingListener) editingListener.is_default = checked }}
													/>
													<Label>Default</Label>
												</div>
											</div>
											<div class="flex gap-2">
												<Button
													variant="outline"
													size="sm"
													onclick={() => editingListener = null}
												>
													Cancel
												</Button>
												<Button
													size="sm"
													onclick={() => updateListener(editingListener!)}
												>
													Save
												</Button>
											</div>
										</div>
									</div>
								{:else}
									<!-- View Mode -->
									<div class="flex items-start justify-between">
										<div class="space-y-2">
											<div class="flex items-center gap-3">
												<StatusIcon class="h-4 w-4 {getStatusColor(status)}" />
												<span class="font-semibold">{listener.name}</span>
												<Badge variant="secondary" class="font-mono">:{listener.port}</Badge>
												{#if listener.is_default}
													<Badge variant="default" class="gap-1">
														<Star class="h-3 w-3" />
														Default
													</Badge>
												{/if}
												{#if !listener.enabled}
													<Badge variant="outline">Disabled</Badge>
												{/if}
											</div>
											
											{#if listener.description}
												<p class="text-sm text-muted-foreground">{listener.description}</p>
											{/if}
											
											{#if listener.server_count && listener.server_count > 0}
												<p class="text-xs text-muted-foreground">
													{listener.server_count} {listener.server_count === 1 ? 'server' : 'servers'} using this listener
												</p>
											{:else}
												<p class="text-xs text-muted-foreground">
													No servers using this listener
												</p>
											{/if}
										</div>
										
										<div class="flex gap-2">
											{#if !listener.is_default}
												<Button
													variant="ghost"
													size="icon"
													class="h-8 w-8"
													onclick={() => setDefaultListener(listener)}
													title="Set as default"
												>
													<Star class="h-4 w-4" />
												</Button>
											{/if}
											<Button
												variant="ghost"
												size="icon"
												class="h-8 w-8"
												onclick={() => editingListener = {...listener}}
											>
												<Edit class="h-4 w-4" />
											</Button>
											{#if listeners.length > 1 && (!listener.server_count || listener.server_count === 0)}
												<Button
													variant="ghost"
													size="icon"
													class="h-8 w-8"
													onclick={() => deleteListener(listener)}
												>
													<Trash2 class="h-4 w-4" />
												</Button>
											{/if}
										</div>
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/if}

				<!-- Add New Listener -->
				<div class="border-t pt-4">
					<h4 class="font-medium mb-3">Add New Listener</h4>
					<div class="space-y-3">
						<div class="grid grid-cols-2 gap-3">
							<div class="space-y-2">
								<Label>Name</Label>
								<Input
									bind:value={newListener.name}
									placeholder="e.g., Secondary, Development"
								/>
							</div>
							<div class="space-y-2">
								<Label>Port</Label>
								<Input
									type="number"
									bind:value={newListener.port}
									oninput={(e) => validatePort(Number(e.currentTarget.value))}
									class={portError ? 'border-destructive' : ''}
								/>
								{#if portError}
									<p class="text-xs text-destructive">{portError}</p>
								{/if}
							</div>
						</div>
						<div class="space-y-2">
							<Label>Description (Optional)</Label>
							<Input
								bind:value={newListener.description}
								placeholder="Optional description for this listener"
							/>
						</div>
						<div class="flex items-center justify-between">
							<div class="flex items-center gap-4">
								<div class="flex items-center gap-2">
									<Switch
										checked={newListener.enabled}
										onCheckedChange={(checked) => newListener.enabled = checked}
									/>
									<Label>Enabled</Label>
								</div>
								{#if listeners.length === 0}
									<div class="flex items-center gap-2">
										<Switch
											checked={newListener.is_default}
											onCheckedChange={(checked) => newListener.is_default = checked}
										/>
										<Label>Set as Default</Label>
									</div>
								{/if}
							</div>
							<Button
								onclick={createListener}
								disabled={!newListener.name || !!portError}
							>
								<Plus class="h-4 w-4 mr-2" />
								Add Listener
							</Button>
						</div>
					</div>
				</div>
			</CardContent>
		</Card>

		<!-- Active Routes -->
		{#if activeRoutes.length > 0}
			<Card>
				<CardHeader>
					<CardTitle>Active Routes</CardTitle>
					<CardDescription>
						Servers currently using proxy routing
					</CardDescription>
				</CardHeader>
				<CardContent>
					<div class="space-y-2">
						{#each activeRoutes as route}
							<div class="flex items-center justify-between p-3 rounded-lg bg-muted/50">
								<div class="flex items-center gap-3">
									<Activity class="h-4 w-4 {route.active ? 'text-green-500' : 'text-gray-500'}" />
									<div>
										<p class="font-mono text-sm">{route.hostname}</p>
										<p class="text-xs text-muted-foreground">
											Server: {route.server_id.slice(0, 8)}...
										</p>
									</div>
								</div>
								<Badge variant={route.active ? "default" : "outline"}>
									{route.active ? 'Active' : 'Inactive'}
								</Badge>
							</div>
						{/each}
					</div>
				</CardContent>
			</Card>
		{/if}
	{/if}
</div>