<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { Module } from '$lib/proto/discopanel/v1/module_pb';
	import { ModuleStatus } from '$lib/proto/discopanel/v1/module_pb';
	import {
		Loader2,
		Play,
		Square,
		RotateCw,
		Settings,
		Trash2,
		Terminal,
		Cpu,
		Server,
		ExternalLink,
		Package,
		RefreshCw
	} from '@lucide/svelte';
	import ModuleDialog from '$lib/components/server/ModuleDialog.svelte';
	import ModuleLogsDialog from '$lib/components/server/ModuleLogsDialog.svelte';
	import { onMount, onDestroy } from 'svelte';

	let modules = $state<Module[]>([]);
	let loading = $state(true);
	let actionLoading = $state<string | null>(null);

	// Dialog state
	let editDialogOpen = $state(false);
	let logsDialogOpen = $state(false);
	let selectedModule = $state<Module | null>(null);

	let pollingInterval: ReturnType<typeof setInterval> | null = null;

	onMount(() => {
		loadModules();
		pollingInterval = setInterval(() => loadModules(true), 5000);
	});

	onDestroy(() => {
		if (pollingInterval) {
			clearInterval(pollingInterval);
		}
	});

	async function loadModules(silent = false) {
		try {
			if (!silent) loading = true;
			// Without serverId, it fetches all modules
			const response = await rpcClient.module.listModules(
				{},
				silent ? silentCallOptions : undefined
			);
			modules = response.modules;
		} catch {
			if (!silent) toast.error('Failed to load modules');
		} finally {
			if (!silent) loading = false;
		}
	}

	async function handleStartModule(module: Module) {
		actionLoading = module.id;
		try {
			await rpcClient.module.startModule({ id: module.id });
			toast.success(`Starting ${module.name}...`);
			await loadModules(true);
		} catch (error) {
			toast.error(
				`Failed to start module: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = null;
		}
	}

	async function handleStopModule(module: Module) {
		actionLoading = module.id;
		try {
			await rpcClient.module.stopModule({ id: module.id });
			toast.success(`Stopping ${module.name}...`);
			await loadModules(true);
		} catch (error) {
			toast.error(
				`Failed to stop module: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = null;
		}
	}

	async function handleRestartModule(module: Module) {
		actionLoading = module.id;
		try {
			await rpcClient.module.restartModule({ id: module.id });
			toast.success(`Restarting ${module.name}...`);
			await loadModules(true);
		} catch (error) {
			toast.error(
				`Failed to restart module: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = null;
		}
	}

	async function handleDeleteModule(module: Module) {
		const confirmed = confirm(
			`Are you sure you want to delete "${module.name}"?\n\nThis will stop and remove the container and all module data.`
		);
		if (!confirmed) return;

		actionLoading = module.id;
		try {
			await rpcClient.module.deleteModule({ id: module.id });
			toast.success(`Module "${module.name}" deleted`);
			await loadModules(true);
		} catch (error) {
			toast.error(
				`Failed to delete module: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
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

	function getStatusBadgeVariant(
		status: ModuleStatus
	): 'default' | 'secondary' | 'destructive' | 'outline' {
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
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<div>
			<h3 class="text-lg font-medium">Active Instances</h3>
			<p class="text-sm text-muted-foreground">All modules running across all your servers.</p>
		</div>
		<Button variant="outline" size="sm" onclick={() => loadModules()} disabled={loading}>
			{#if loading}
				<Loader2 class="h-4 w-4 animate-spin" />
			{:else}
				<RefreshCw class="h-4 w-4" />
			{/if}
		</Button>
	</div>

	{#if loading && modules.length === 0}
		<div class="flex items-center justify-center py-12">
			<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
		</div>
	{:else if modules.length === 0}
		<div
			class="flex flex-col items-center justify-center rounded-lg border bg-card py-12 text-center"
		>
			<Package class="mb-4 h-12 w-12 text-muted-foreground/50" />
			<h3 class="mb-1 text-lg font-medium">No Active Modules</h3>
			<p class="max-w-sm text-sm text-muted-foreground">
				You don't have any modules running on any of your servers right now.
			</p>
		</div>
	{:else}
		<div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
			{#each modules as module (module.id)}
				{@const isLoading = actionLoading === module.id}
				<Card
					class="group relative overflow-hidden border shadow-sm transition-all hover:shadow-md"
				>
					<div
						class="absolute top-0 right-0 left-0 h-1 {module.status === ModuleStatus.RUNNING
							? 'bg-green-500'
							: module.status === ModuleStatus.ERROR
								? 'bg-red-500'
								: 'bg-gray-300'}"
					></div>
					<CardContent class="p-4">
						<div class="mb-3 flex items-start justify-between">
							<div class="min-w-0 flex-1">
								<div class="mb-1 flex items-center gap-2">
									<h3 class="truncate font-semibold">{module.name}</h3>
									<Badge variant={getStatusBadgeVariant(module.status)} class="text-xs">
										{getStatusLabel(module.status)}
									</Badge>
								</div>
								<div class="flex items-center gap-2 truncate text-xs text-muted-foreground">
									<span class="flex items-center gap-1"
										><Server class="h-3 w-3" /> {module.serverName || module.serverId}</span
									>
									<span>•</span>
									<span class="truncate">{module.templateName}</span>
								</div>
							</div>
							<div class="ml-2 flex items-center gap-1">
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

						<div class="mb-3 space-y-1 text-xs">
							{#if module.status === ModuleStatus.RUNNING && module.memoryUsage > 0}
								<div class="flex items-center gap-3 text-muted-foreground">
									<span><Cpu class="mr-1 inline h-3 w-3" />{module.memoryUsage.toFixed(0)} MB</span>
									<span>CPU: {module.cpuPercent.toFixed(1)}%</span>
								</div>
							{:else}
								<div class="invisible flex items-center gap-3 text-muted-foreground/60">
									<span><Cpu class="mr-1 inline h-3 w-3" />0 MB</span>
								</div>
							{/if}
						</div>

						<div class="flex items-center justify-between border-t pt-2">
							<div class="flex items-center gap-1">
								{#if module.autoStart}
									<Badge variant="secondary" class="px-1.5 py-0 text-[10px]">Auto-start</Badge>
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
</div>

{#if selectedModule}
	<ModuleDialog
		bind:open={editDialogOpen}
		mode="edit"
		module={selectedModule}
		onSuccess={() => loadModules(true)}
	/>

	<ModuleLogsDialog bind:open={logsDialogOpen} module={selectedModule} />
{/if}
