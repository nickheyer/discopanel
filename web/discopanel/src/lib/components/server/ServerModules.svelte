<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { Module, ModuleTemplate } from '$lib/proto/discopanel/v1/module_pb';
	import { ModuleStatus, ModuleEventType } from '$lib/proto/discopanel/v1/module_pb';
	import { Loader2, Plus, Play, Square, RotateCw, Settings, Trash2, Terminal, Cpu, ExternalLink, Package, RefreshCw, Puzzle, Link, Zap, Info } from '@lucide/svelte';
	import ModuleCreateDialog from './ModuleCreateDialog.svelte';
	import ModuleEditDialog from './ModuleEditDialog.svelte';
	import ModuleLogsDialog from './ModuleLogsDialog.svelte';
	import ModuleTemplateCreateDialog from './ModuleTemplateCreateDialog.svelte';

	interface Props {
		server: Server;
		active?: boolean;
	}

	let { server, active = false }: Props = $props();

	let modules = $state<Module[]>([]);
	let templates = $state<ModuleTemplate[]>([]);
	let loading = $state(true);
	let actionLoading = $state<string | null>(null);
	let aliasValues = $state<Record<string, string>>({});

	// Dialog state
	let createDialogOpen = $state(false);
	let editDialogOpen = $state(false);
	let logsDialogOpen = $state(false);
	let templateCreateDialogOpen = $state(false);
	let selectedModule = $state<Module | null>(null);

	let hasLoaded = false;
	let previousServerId = $state(server.id);
	let pollingInterval: ReturnType<typeof setInterval> | null = null;

	// Reset state when server changes
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			modules = [];
			templates = [];
			loading = true;
			hasLoaded = false;
			if (pollingInterval) {
				clearInterval(pollingInterval);
				pollingInterval = null;
			}
		}
	});

	$effect(() => {
		if (active && !hasLoaded) {
			hasLoaded = true;
			loadModules();
			loadTemplates();
			// Start polling
			pollingInterval = setInterval(() => loadModules(true), 5000);
		} else if (!active && pollingInterval) {
			clearInterval(pollingInterval);
			pollingInterval = null;
		}

		return () => {
			if (pollingInterval) {
				clearInterval(pollingInterval);
				pollingInterval = null;
			}
		};
	});

	async function loadModules(silent = false) {
		try {
			if (!silent) loading = true;
			const response = await rpcClient.module.listModules(
				{ serverId: server.id },
				silent ? silentCallOptions : undefined
			);
			modules = response.modules;
			modules.forEach(m => loadAliases(m.id));
		} catch (error) {
			if (!silent) toast.error('Failed to load modules');
		} finally {
			if (!silent) loading = false;
		}
	}

	async function loadTemplates() {
		try {
			const response = await rpcClient.module.listModuleTemplates({});
			templates = response.templates;
		} catch (error) {
			toast.error('Failed to load module templates');
		}
	}

	async function loadAliases(moduleId: string) {
		try {
			const response = await rpcClient.module.getResolvedAliases(
				{ serverId: server.id, moduleId },
				silentCallOptions
			);
			aliasValues = { ...aliasValues, ...response.aliases };
		} catch { /* ignore */ }
	}

	function resolve(input: string): string {
		return input.replace(/\{\{[^}]+\}\}/g, match => aliasValues[match] ?? match);
	}

	async function handleStartModule(module: Module) {
		actionLoading = module.id;
		try {
			await rpcClient.module.startModule({ id: module.id });
			toast.success(`Starting ${module.name}...`);
			await loadModules();
		} catch (error) {
			toast.error(`Failed to start module: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			actionLoading = null;
		}
	}

	async function handleStopModule(module: Module) {
		actionLoading = module.id;
		try {
			await rpcClient.module.stopModule({ id: module.id });
			toast.success(`Stopping ${module.name}...`);
			await loadModules();
		} catch (error) {
			toast.error(`Failed to stop module: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			actionLoading = null;
		}
	}

	async function handleRestartModule(module: Module) {
		actionLoading = module.id;
		try {
			await rpcClient.module.restartModule({ id: module.id });
			toast.success(`Restarting ${module.name}...`);
			await loadModules();
		} catch (error) {
			toast.error(`Failed to restart module: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			actionLoading = null;
		}
	}

	async function handleDeleteModule(module: Module) {
		const confirmed = confirm(`Are you sure you want to delete "${module.name}"?\n\nThis will stop and remove the container and all module data.`);
		if (!confirmed) return;

		actionLoading = module.id;
		try {
			await rpcClient.module.deleteModule({ id: module.id });
			toast.success(`Module "${module.name}" deleted`);
			await loadModules();
		} catch (error) {
			toast.error(`Failed to delete module: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			actionLoading = null;
		}
	}

	function openEditDialog(module: Module) {
		selectedModule = module;
		editDialogOpen = true;
	}

	function openLogsDialog(module: Module) {
		selectedModule = module;
		logsDialogOpen = true;
	}

	function getStatusBadgeVariant(status: ModuleStatus): 'default' | 'secondary' | 'destructive' | 'outline' {
		switch (status) {
			case ModuleStatus.RUNNING:
				return 'default';
			case ModuleStatus.STARTING:
			case ModuleStatus.STOPPING:
			case ModuleStatus.CREATING:
				return 'secondary';
			case ModuleStatus.ERROR:
				return 'destructive';
			default:
				return 'outline';
		}
	}

	function getStatusLabel(status: ModuleStatus): string {
		switch (status) {
			case ModuleStatus.RUNNING:
				return 'Running';
			case ModuleStatus.STOPPED:
				return 'Stopped';
			case ModuleStatus.STARTING:
				return 'Starting';
			case ModuleStatus.STOPPING:
				return 'Stopping';
			case ModuleStatus.ERROR:
				return 'Error';
			case ModuleStatus.CREATING:
				return 'Creating';
			default:
				return 'Unknown';
		}
	}

	function getEventTypeLabel(event: ModuleEventType): string {
		switch (event) {
			case ModuleEventType.SERVER_START: return 'Server Start';
			case ModuleEventType.SERVER_STOP: return 'Server Stop';
			case ModuleEventType.SERVER_HEALTHY: return 'Server Healthy';
			case ModuleEventType.PLAYER_JOIN: return 'Player Join';
			case ModuleEventType.PLAYER_LEAVE: return 'Player Leave';
			default: return 'Unknown';
		}
	}

	function getDependencyName(moduleId: string): string {
		const dep = modules.find(m => m.id === moduleId);
		return dep?.name || moduleId.slice(0, 8);
	}

	function hasAdvancedConfig(module: Module): boolean {
		return (module.dependencies?.length ?? 0) > 0 ||
			(module.eventHooks?.length ?? 0) > 0 ||
			Object.keys(module.metadata ?? {}).length > 0;
	}

	function handleModuleCreated() {
		loadModules();
		// Poll more frequently after creation to catch status transitions
		setTimeout(() => loadModules(true), 1000);
		setTimeout(() => loadModules(true), 3000);
	}

	function handleModuleUpdated() {
		loadModules();
	}

	function handleTemplateCreated() {
		loadTemplates();
	}
</script>

<Card class="h-full flex flex-col">
	<CardHeader>
		<div class="flex items-center justify-between">
			<div>
				<CardTitle>Server Modules</CardTitle>
				<p class="text-sm text-muted-foreground mt-1">
					Companion services attached to this server
				</p>
			</div>
			<div class="flex items-center gap-2">
				<Button variant="outline" size="sm" onclick={() => loadModules()} disabled={loading}>
					{#if loading}
						<Loader2 class="h-4 w-4 animate-spin" />
					{:else}
						<RefreshCw class="h-4 w-4" />
					{/if}
				</Button>
				<Button variant="outline" onclick={() => (templateCreateDialogOpen = true)}>
					<Puzzle class="h-4 w-4 mr-2" />
					Create Template
				</Button>
				<Button onclick={() => (createDialogOpen = true)} disabled={templates.length === 0}>
					<Plus class="h-4 w-4 mr-2" />
					Add Module
				</Button>
			</div>
		</div>
	</CardHeader>
	<CardContent class="flex-1 overflow-auto">
		{#if loading}
			<div class="flex items-center justify-center py-12">
				<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
			</div>
		{:else if modules.length === 0}
			<div class="flex flex-col items-center justify-center py-12 text-muted-foreground">
				<Package class="h-12 w-12 mb-4" />
				<p>No modules attached to this server</p>
				<p class="text-sm mt-2">Add a module to extend server functionality</p>
				{#if templates.length > 0}
					<Button class="mt-4" onclick={() => (createDialogOpen = true)}>
						<Plus class="h-4 w-4 mr-2" />
						Add Module
					</Button>
				{:else}
					<p class="text-xs mt-4 text-muted-foreground/60">No module templates available</p>
				{/if}
			</div>
		{:else}
			<div class="grid gap-4 grid-cols-1 lg:grid-cols-2">
				{#each modules as module}
					{@const isLoading = actionLoading === module.id}
					<Card class="group relative overflow-hidden border shadow-sm hover:shadow-md transition-all">
						<div class="absolute top-0 left-0 right-0 h-1 {module.status === ModuleStatus.RUNNING ? 'bg-green-500' : module.status === ModuleStatus.ERROR ? 'bg-red-500' : 'bg-gray-300'}"></div>
						<CardContent class="p-4">
							<div class="flex items-start justify-between mb-3">
								<div class="flex-1 min-w-0">
									<div class="flex items-center gap-2 mb-1">
										<h3 class="font-semibold truncate">{module.name}</h3>
										<Badge variant={getStatusBadgeVariant(module.status)} class="text-xs">
											{getStatusLabel(module.status)}
										</Badge>
									</div>
									<p class="text-sm text-muted-foreground truncate">{module.templateName}</p>
								</div>
								<div class="flex items-center gap-1 ml-2">
									{#if module.status === ModuleStatus.STOPPED}
										<Button
											size="icon"
											variant="ghost"
											onclick={() => handleStartModule(module)}
											disabled={isLoading}
											title="Start module"
											class="h-8 w-8"
										>
											{#if isLoading}
												<Loader2 class="h-4 w-4 animate-spin" />
											{:else}
												<Play class="h-4 w-4 text-green-500" />
											{/if}
										</Button>
									{:else if module.status === ModuleStatus.RUNNING}
										<Button
											size="icon"
											variant="ghost"
											onclick={() => handleStopModule(module)}
											disabled={isLoading}
											title="Stop module"
											class="h-8 w-8"
										>
											{#if isLoading}
												<Loader2 class="h-4 w-4 animate-spin" />
											{:else}
												<Square class="h-4 w-4 text-red-500" />
											{/if}
										</Button>
										<Button
											size="icon"
											variant="ghost"
											onclick={() => handleRestartModule(module)}
											disabled={isLoading}
											title="Restart module"
											class="h-8 w-8"
										>
											<RotateCw class="h-4 w-4" />
										</Button>
									{:else if module.status === ModuleStatus.STARTING || module.status === ModuleStatus.STOPPING || module.status === ModuleStatus.CREATING}
										<Button size="icon" variant="ghost" disabled class="h-8 w-8">
											<Loader2 class="h-4 w-4 animate-spin" />
										</Button>
									{:else if module.status === ModuleStatus.ERROR}
										<Button
											size="icon"
											variant="ghost"
											onclick={() => handleStartModule(module)}
											disabled={isLoading}
											title="Start module"
											class="h-8 w-8"
										>
											{#if isLoading}
												<Loader2 class="h-4 w-4 animate-spin" />
											{:else}
												<Play class="h-4 w-4 text-green-500" />
											{/if}
										</Button>
									{/if}
								</div>
							</div>

							<div class="text-xs mb-3 space-y-1">
								{#if module.ports?.length}
									<div class="flex flex-wrap gap-1.5">
										{#each module.ports as port}
											<Badge variant="outline" class="font-mono text-[10px] px-1.5 py-0">
												{port.name || 'Port'}: {port.hostPort || '?'}→{port.containerPort}/{(port.protocol || 'tcp').toUpperCase()}
											</Badge>
										{/each}
									</div>
								{:else}
									<span class="text-muted-foreground">No ports</span>
								{/if}
								{#if module.status === ModuleStatus.RUNNING && module.memoryUsage > 0}
									<div class="flex items-center gap-3 text-muted-foreground">
										<span><Cpu class="h-3 w-3 inline mr-1" />{module.memoryUsage.toFixed(0)} MB</span>
										<span>CPU: {module.cpuPercent.toFixed(1)}%</span>
									</div>
								{/if}
							</div>

							{#if module.accessUrls?.length}
								<div class="space-y-1 mb-3">
									{#each module.accessUrls as url}
										{@const resolved = resolve(url)}
										<div class="flex items-center gap-2 p-2 rounded bg-muted/50">
											<ExternalLink class="h-3 w-3 text-muted-foreground flex-shrink-0" />
											<a
												href={resolved}
												target="_blank"
												rel="noopener noreferrer"
												class="text-xs font-mono text-primary hover:underline truncate"
											>
												{resolved}
											</a>
										</div>
									{/each}
								</div>
							{/if}

							<!-- Advanced Configuration Summary -->
							{#if hasAdvancedConfig(module)}
								<div class="space-y-1.5 mb-3 text-xs">
									{#if module.dependencies && module.dependencies.length > 0}
										<div class="flex items-center gap-1.5 text-muted-foreground">
											<Link class="h-3 w-3" />
											<span>Depends on:</span>
											<span class="text-foreground">
												{module.dependencies.map(d => getDependencyName(d.moduleId)).join(', ')}
											</span>
										</div>
									{/if}
									{#if module.eventHooks && module.eventHooks.length > 0}
										<div class="flex items-center gap-1.5 text-muted-foreground">
											<Zap class="h-3 w-3" />
											<span>{module.eventHooks.length} hook{module.eventHooks.length > 1 ? 's' : ''}</span>
											<span class="text-muted-foreground/60">
												({module.eventHooks.map(h => getEventTypeLabel(h.event)).join(', ')})
											</span>
										</div>
									{/if}
									{#if module.metadata && Object.keys(module.metadata).length > 0}
										<div class="space-y-0.5">
											{#each Object.entries(module.metadata) as [key, value]}
												<div class="flex items-center gap-1.5 text-muted-foreground">
													<Info class="h-3 w-3 flex-shrink-0" />
													<span class="font-medium">{key}:</span>
													<span class="text-foreground truncate">{resolve(value)}</span>
												</div>
											{/each}
										</div>
									{/if}
								</div>
							{/if}

							<div class="flex items-center justify-between pt-2 border-t">
								<div class="flex items-center gap-1">
									{#if module.autoStart}
										<Badge variant="secondary" class="text-[10px] px-1.5 py-0">Auto-start</Badge>
									{/if}
									{#if module.followServerLifecycle}
										<Badge variant="secondary" class="text-[10px] px-1.5 py-0">Follows server</Badge>
									{/if}
									{#if module.detached}
										<Badge variant="secondary" class="text-[10px] px-1.5 py-0">Detached</Badge>
									{/if}
								</div>
								<div class="flex items-center gap-1">
									<Button
										size="icon"
										variant="ghost"
										onclick={() => openLogsDialog(module)}
										title="View logs"
										class="h-7 w-7"
									>
										<Terminal class="h-3.5 w-3.5" />
									</Button>
									<Button
										size="icon"
										variant="ghost"
										onclick={() => openEditDialog(module)}
										title="Edit module"
										class="h-7 w-7"
									>
										<Settings class="h-3.5 w-3.5" />
									</Button>
									<Button
										size="icon"
										variant="ghost"
										onclick={() => handleDeleteModule(module)}
										disabled={isLoading}
										title="Delete module"
										class="h-7 w-7 text-destructive hover:text-destructive"
									>
										<Trash2 class="h-3.5 w-3.5" />
									</Button>
								</div>
							</div>
						</CardContent>
					</Card>
				{/each}
			</div>
		{/if}
	</CardContent>
</Card>

<ModuleCreateDialog
	bind:open={createDialogOpen}
	{server}
	{templates}
	onCreated={handleModuleCreated}
/>

{#if selectedModule}
	<ModuleEditDialog
		bind:open={editDialogOpen}
		module={selectedModule}
		onUpdated={handleModuleUpdated}
	/>

	<ModuleLogsDialog
		bind:open={logsDialogOpen}
		module={selectedModule}
	/>
{/if}

<ModuleTemplateCreateDialog
	bind:open={templateCreateDialogOpen}
	onCreated={handleTemplateCreated}
/>
