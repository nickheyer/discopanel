<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Card, CardContent, CardHeader, CardTitle, CardDescription, CardFooter } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '$lib/components/ui/dropdown-menu';
	import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '$lib/components/ui/dialog';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { Separator } from '$lib/components/ui/separator';
	import { Loader2, Plus, MoreVertical, Play, Square, RotateCw, Trash2, Blocks, Settings } from '@lucide/svelte/icons';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { Module, ModuleTemplate, ModulePort } from '$lib/proto/discopanel/v1/module_pb';
	import { ModuleStatus, ModuleCategory, ModuleProtocol, ModulePortSchema } from '$lib/proto/discopanel/v1/module_pb';
	import { create } from '@bufbuild/protobuf';
	import {
		ListModulesRequestSchema,
		ListModuleTemplatesRequestSchema,
		CreateModuleRequestSchema,
		StartModuleRequestSchema,
		StopModuleRequestSchema,
		RestartModuleRequestSchema,
		DeleteModuleRequestSchema
	} from '$lib/proto/discopanel/v1/module_pb';

	interface Props {
		server: Server;
		active?: boolean;
	}

	let { server, active = false }: Props = $props();

	let modules = $state<Module[]>([]);
	let templates = $state<ModuleTemplate[]>([]);
	let loading = $state(true);
	let actionLoading = $state<string | null>(null);
	let createDialogOpen = $state(false);
	let selectedTemplateId = $state<string>('');
	let newModuleName = $state('');
	let newModuleDescription = $state('');
	let creating = $state(false);

	// Configuration state
	let envVars = $state<Record<string, string>>({});
	let ports = $state<Array<{ name: string; containerPort: number; hostPort: number; protocol: ModuleProtocol }>>([]);
	let memory = $state(256);
	let autoStart = $state(true);
	let autoStop = $state(true);
	let startImmediately = $state(false);

	let hasLoaded = false;
	let previousServerId = $state(server.id);

	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			modules = [];
			templates = [];
			loading = true;
			hasLoaded = false;
		}
	});

	$effect(() => {
		if (active && !hasLoaded) {
			hasLoaded = true;
			loadModules();
			loadTemplates();
		}
	});

	async function loadModules(skipLoading = false) {
		try {
			if (!skipLoading) loading = true;
			const request = create(ListModulesRequestSchema, { serverId: server.id });
			const callOptions = skipLoading ? silentCallOptions : undefined;
			const response = await rpcClient.module.listModules(request, callOptions);
			modules = response.modules;
		} catch (error) {
			toast.error('Failed to load modules');
		} finally {
			loading = false;
		}
	}

	async function loadTemplates() {
		try {
			const request = create(ListModuleTemplatesRequestSchema, { includeCustom: true });
			const response = await rpcClient.module.listModuleTemplates(request, silentCallOptions);
			templates = response.templates;
		} catch (error) {
			console.error('Failed to load templates:', error);
		}
	}

	// Populate defaults when template is selected
	$effect(() => {
		if (selectedTemplateId) {
			const template = templates.find(t => t.id === selectedTemplateId);
			if (template) {
				// Set env var defaults from schema
				const newEnvVars: Record<string, string> = {};
				for (const envDef of template.envVarSchema) {
					newEnvVars[envDef.name] = envDef.defaultValue || '';
				}
				envVars = newEnvVars;

				// Set port defaults from schema
				ports = template.portSchema.map(p => ({
					name: p.name,
					containerPort: p.containerPort,
					hostPort: 0, // No host port by default for HTTP (uses proxy)
					protocol: p.protocol
				}));
			}
		} else {
			envVars = {};
			ports = [];
		}
	});

	function resetForm() {
		selectedTemplateId = '';
		newModuleName = '';
		newModuleDescription = '';
		envVars = {};
		ports = [];
		memory = 512;
		autoStart = true;
		autoStop = true;
		startImmediately = false;
	}

	async function handleModuleAction(action: 'start' | 'stop' | 'restart', module: Module) {
		actionLoading = module.id;
		try {
			switch (action) {
				case 'start':
					await rpcClient.module.startModule(create(StartModuleRequestSchema, { id: module.id }));
					toast.success(`Starting ${module.name}...`);
					break;
				case 'stop':
					await rpcClient.module.stopModule(create(StopModuleRequestSchema, { id: module.id }));
					toast.success(`Stopping ${module.name}...`);
					break;
				case 'restart':
					await rpcClient.module.restartModule(create(RestartModuleRequestSchema, { id: module.id }));
					toast.success(`Restarting ${module.name}...`);
					break;
			}
			await loadModules(true);
		} catch (error) {
			toast.error(`Failed to ${action} module: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			actionLoading = null;
		}
	}

	async function deleteModule(module: Module) {
		const confirmed = confirm(`Are you sure you want to delete "${module.name}"?\n\nThis will:\n- Stop and remove the container\n- Delete all module data\n\nThis action cannot be undone.`);
		if (!confirmed) return;

		actionLoading = module.id;
		try {
			await rpcClient.module.deleteModule(create(DeleteModuleRequestSchema, { id: module.id }));
			toast.success('Module deleted');
			await loadModules(true);
		} catch (error) {
			toast.error('Failed to delete module');
		} finally {
			actionLoading = null;
		}
	}

	async function createModule() {
		if (!selectedTemplateId || !newModuleName) {
			toast.error('Please select a template and enter a name');
			return;
		}

		creating = true;
		try {
			// Build port configuration
			const modulePorts: ModulePort[] = ports.map(p => create(ModulePortSchema, {
				name: p.name,
				containerPort: p.containerPort,
				hostPort: p.hostPort,
				protocol: p.protocol
			}));

			const request = create(CreateModuleRequestSchema, {
				serverId: server.id,
				templateId: selectedTemplateId,
				name: newModuleName,
				description: newModuleDescription,
				environment: envVars,
				ports: modulePorts,
				memory: memory,
				autoStart: autoStart,
				autoStop: autoStop,
				startImmediately: startImmediately
			});
			await rpcClient.module.createModule(request);
			toast.success('Module created');
			createDialogOpen = false;
			resetForm();
			await loadModules(true);
		} catch (error) {
			toast.error(`Failed to create module: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			creating = false;
		}
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
			case ModuleStatus.UNHEALTHY:
				return 'destructive';
			default:
				return 'outline';
		}
	}

	function getStatusDisplayName(status: ModuleStatus): string {
		switch (status) {
			case ModuleStatus.RUNNING:
				return 'RUNNING';
			case ModuleStatus.STOPPED:
				return 'STOPPED';
			case ModuleStatus.STARTING:
				return 'STARTING';
			case ModuleStatus.STOPPING:
				return 'STOPPING';
			case ModuleStatus.ERROR:
				return 'ERROR';
			case ModuleStatus.CREATING:
				return 'CREATING';
			case ModuleStatus.UNHEALTHY:
				return 'UNHEALTHY';
			default:
				return 'UNKNOWN';
		}
	}

	function getCategoryDisplayName(category: ModuleCategory): string {
		switch (category) {
			case ModuleCategory.WEBUI:
				return 'Web UI';
			case ModuleCategory.VOICE:
				return 'Voice';
			case ModuleCategory.MAP:
				return 'Map';
			case ModuleCategory.UTILITY:
				return 'Utility';
			case ModuleCategory.CUSTOM:
				return 'Custom';
			default:
				return 'Unknown';
		}
	}

	function getSelectedTemplateName(): string {
		if (!selectedTemplateId) return 'Select a template...';
		const template = templates.find(t => t.id === selectedTemplateId);
		return template?.name || 'Select a template...';
	}

	function getProtocolDisplayName(protocol: ModuleProtocol): string {
		switch (protocol) {
			case ModuleProtocol.HTTP:
				return 'HTTP (via proxy)';
			case ModuleProtocol.TCP:
				return 'TCP (direct)';
			default:
				return 'None';
		}
	}

	let selectedTemplate = $derived(templates.find(t => t.id === selectedTemplateId));
</script>

<Card class="h-full flex flex-col">
	<CardHeader>
		<div class="flex items-center justify-between">
			<div>
				<CardTitle class="flex items-center gap-2">
					<Blocks class="h-5 w-5" />
					Modules
				</CardTitle>
				<CardDescription class="mt-1">
					Sidecar containers that extend your server with web UIs, maps, voice chat, and more
				</CardDescription>
			</div>
			<Dialog bind:open={createDialogOpen}>
				<DialogTrigger>
					{#snippet child({ props })}
						<Button {...props}>
							<Plus class="h-4 w-4 mr-2" />
							Add Module
						</Button>
					{/snippet}
				</DialogTrigger>
				<DialogContent class="sm:max-w-[600px] max-h-[85vh] overflow-y-auto">
					<DialogHeader>
						<DialogTitle>Add Module</DialogTitle>
						<DialogDescription>
							Create a new sidecar module for your server
						</DialogDescription>
					</DialogHeader>
					<div class="space-y-4 py-4">
						<!-- Template Selection -->
						<div class="space-y-2">
							<Label for="template">Template</Label>
							<Select type="single" bind:value={selectedTemplateId}>
								<SelectTrigger>
									<span>{getSelectedTemplateName()}</span>
								</SelectTrigger>
								<SelectContent>
									{#each templates as template}
										<SelectItem value={template.id}>
											<div class="flex items-center gap-2">
												<span>{template.name}</span>
												<Badge variant="outline" class="text-xs">
													{getCategoryDisplayName(template.category)}
												</Badge>
											</div>
										</SelectItem>
									{/each}
								</SelectContent>
							</Select>
							{#if selectedTemplate}
								<p class="text-sm text-muted-foreground">{selectedTemplate.description}</p>
							{/if}
						</div>

						<!-- Basic Info -->
						<div class="space-y-2">
							<Label for="name">Name</Label>
							<Input
								id="name"
								placeholder="My Module"
								bind:value={newModuleName}
							/>
						</div>
						<div class="space-y-2">
							<Label for="description">Description (optional)</Label>
							<Textarea
								id="description"
								placeholder="What does this module do?"
								bind:value={newModuleDescription}
							/>
						</div>

						{#if selectedTemplate}
							<Separator />

							<!-- Port Configuration -->
							{#if ports.length > 0}
								<div class="space-y-3">
									<Label class="flex items-center gap-2">
										<Settings class="h-4 w-4" />
										Port Configuration
									</Label>
									{#each ports as port, i}
										<div class="grid grid-cols-3 gap-2 p-3 border rounded-md bg-muted/30">
											<div class="space-y-1">
												<Label class="text-xs text-muted-foreground">{port.name}</Label>
												<div class="text-sm font-medium">{getProtocolDisplayName(port.protocol)}</div>
											</div>
											<div class="space-y-1">
												<Label class="text-xs text-muted-foreground">Container Port</Label>
												<Input
													type="number"
													bind:value={ports[i].containerPort}
													class="h-8"
												/>
											</div>
											{#if port.protocol === ModuleProtocol.TCP}
												<div class="space-y-1">
													<Label class="text-xs text-muted-foreground">Host Port</Label>
													<Input
														type="number"
														bind:value={ports[i].hostPort}
														placeholder="Auto"
														class="h-8"
													/>
												</div>
											{:else}
												<div class="space-y-1">
													<Label class="text-xs text-muted-foreground">Access</Label>
													<div class="text-sm text-muted-foreground pt-1">Via server hostname</div>
												</div>
											{/if}
										</div>
									{/each}
								</div>
							{/if}

							<!-- Environment Variables -->
							{#if selectedTemplate.envVarSchema.length > 0}
								<div class="space-y-3">
									<Label>Environment Variables</Label>
									{#each selectedTemplate.envVarSchema as envDef}
										<div class="space-y-1">
											<Label class="text-xs font-normal">
												{envDef.name}
												{#if envDef.required}
													<span class="text-destructive">*</span>
												{/if}
											</Label>
											{#if envDef.isSecret}
												<Input
													type="password"
													placeholder={envDef.description}
													value={envVars[envDef.name] || ''}
													oninput={(e) => envVars[envDef.name] = e.currentTarget.value}
												/>
											{:else}
												<Input
													placeholder={envDef.description}
													value={envVars[envDef.name] || ''}
													oninput={(e) => envVars[envDef.name] = e.currentTarget.value}
												/>
											{/if}
											{#if envDef.description}
												<p class="text-xs text-muted-foreground">{envDef.description}</p>
											{/if}
										</div>
									{/each}
								</div>
							{/if}

							<Separator />

							<!-- Resources -->
							<div class="space-y-2">
								<Label for="memory">Memory Limit (MB)</Label>
								<Input
									id="memory"
									type="number"
									bind:value={memory}
									min={64}
									max={8192}
								/>
							</div>

							<!-- Lifecycle Options -->
							<div class="space-y-3">
								<Label>Lifecycle</Label>
								<div class="flex items-center justify-between">
									<div>
										<div class="text-sm font-medium">Auto-start with server</div>
										<div class="text-xs text-muted-foreground">Start this module when the server starts</div>
									</div>
									<Switch bind:checked={autoStart} />
								</div>
								<div class="flex items-center justify-between">
									<div>
										<div class="text-sm font-medium">Auto-stop with server</div>
										<div class="text-xs text-muted-foreground">Stop this module when the server stops</div>
									</div>
									<Switch bind:checked={autoStop} />
								</div>
								<div class="flex items-center justify-between">
									<div>
										<div class="text-sm font-medium">Start immediately</div>
										<div class="text-xs text-muted-foreground">Start the module right after creation</div>
									</div>
									<Switch bind:checked={startImmediately} />
								</div>
							</div>
						{/if}
					</div>
					<DialogFooter>
						<Button variant="outline" onclick={() => { createDialogOpen = false; resetForm(); }}>
							Cancel
						</Button>
						<Button onclick={createModule} disabled={creating || !selectedTemplateId || !newModuleName}>
							{#if creating}
								<Loader2 class="h-4 w-4 mr-2 animate-spin" />
							{/if}
							Create Module
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	</CardHeader>
	<CardContent class="flex-1 overflow-auto">
		{#if loading}
			<div class="flex items-center justify-center py-12">
				<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
			</div>
		{:else if modules.length === 0}
			<div class="flex flex-col items-center justify-center py-12 text-muted-foreground">
				<Blocks class="h-12 w-12 mb-4" />
				<p>No modules installed</p>
				<p class="text-sm mt-2">Add modules to extend your server's functionality</p>
				<Button class="mt-4" onclick={() => createDialogOpen = true}>
					<Plus class="h-4 w-4 mr-2" />
					Add Your First Module
				</Button>
			</div>
		{:else}
			<div class="grid gap-4 md:grid-cols-2">
				{#each modules as module}
					<Card class="relative overflow-hidden border-2 hover:border-primary/30 transition-all">
						<CardHeader class="pb-2">
							<div class="flex items-start justify-between">
								<div class="space-y-1 flex-1">
									<CardTitle class="text-lg">{module.name}</CardTitle>
									<CardDescription class="text-sm line-clamp-2">
										{module.description || 'No description'}
									</CardDescription>
								</div>
								<DropdownMenu>
									<DropdownMenuTrigger>
										{#snippet child({ props })}
											<Button variant="ghost" size="icon" disabled={actionLoading === module.id} {...props}>
												{#if actionLoading === module.id}
													<Loader2 class="h-4 w-4 animate-spin" />
												{:else}
													<MoreVertical class="h-4 w-4" />
												{/if}
											</Button>
										{/snippet}
									</DropdownMenuTrigger>
									<DropdownMenuContent align="end">
										<DropdownMenuLabel>Actions</DropdownMenuLabel>
										<DropdownMenuSeparator />
										{#if module.status === ModuleStatus.STOPPED || module.status === ModuleStatus.ERROR}
											<DropdownMenuItem onclick={() => handleModuleAction('start', module)}>
												<Play class="h-4 w-4 mr-2" />
												Start
											</DropdownMenuItem>
										{/if}
										{#if module.status === ModuleStatus.RUNNING || module.status === ModuleStatus.UNHEALTHY || module.status === ModuleStatus.STARTING}
											<DropdownMenuItem onclick={() => handleModuleAction('stop', module)}>
												<Square class="h-4 w-4 mr-2" />
												Stop
											</DropdownMenuItem>
										{/if}
										{#if module.status === ModuleStatus.RUNNING || module.status === ModuleStatus.UNHEALTHY}
											<DropdownMenuItem onclick={() => handleModuleAction('restart', module)}>
												<RotateCw class="h-4 w-4 mr-2" />
												Restart
											</DropdownMenuItem>
										{/if}
										<DropdownMenuSeparator />
										<DropdownMenuItem class="text-destructive" onclick={() => deleteModule(module)}>
											<Trash2 class="h-4 w-4 mr-2" />
											Delete
										</DropdownMenuItem>
									</DropdownMenuContent>
								</DropdownMenu>
							</div>
						</CardHeader>
						<CardContent class="pt-2">
							<div class="space-y-2">
								<div class="flex items-center justify-between">
									<span class="text-sm text-muted-foreground">Status</span>
									<div class="flex items-center gap-2">
										{#if module.status === ModuleStatus.RUNNING}
											<div class="h-2 w-2 rounded-full bg-green-500 animate-pulse"></div>
										{/if}
										<Badge variant={getStatusBadgeVariant(module.status)}>
											{getStatusDisplayName(module.status)}
										</Badge>
									</div>
								</div>
								<div class="flex items-center justify-between">
									<span class="text-sm text-muted-foreground">Category</span>
									<Badge variant="outline">
										{getCategoryDisplayName(module.category)}
									</Badge>
								</div>
								<div class="flex items-center justify-between">
									<span class="text-sm text-muted-foreground">Image</span>
									<span class="text-sm font-mono truncate max-w-[180px]" title={module.dockerImage}>
										{module.dockerImage.split('/').pop()?.split(':')[0] || module.dockerImage}
									</span>
								</div>
								{#if module.status === ModuleStatus.RUNNING && (module.memoryUsage > 0 || module.cpuPercent > 0)}
									<div class="pt-2 mt-2 border-t space-y-1">
										<div class="flex items-center justify-between">
											<span class="text-xs text-muted-foreground">Memory</span>
											<span class="text-xs font-mono">{(module.memoryUsage / 1024 / 1024).toFixed(0)} MB</span>
										</div>
										<div class="flex items-center justify-between">
											<span class="text-xs text-muted-foreground">CPU</span>
											<span class="text-xs font-mono">{module.cpuPercent.toFixed(1)}%</span>
										</div>
									</div>
								{/if}
							</div>
						</CardContent>
						<CardFooter class="pt-2">
							<div class="flex gap-2 w-full">
								{#if module.status === ModuleStatus.STOPPED || module.status === ModuleStatus.ERROR}
									<Button
										class="flex-1"
										onclick={() => handleModuleAction('start', module)}
										disabled={actionLoading === module.id}
									>
										{#if actionLoading === module.id}
											<Loader2 class="h-4 w-4 mr-2 animate-spin" />
										{:else}
											<Play class="h-4 w-4 mr-2" />
										{/if}
										Start
									</Button>
								{:else if module.status === ModuleStatus.RUNNING || module.status === ModuleStatus.UNHEALTHY}
									<Button
										variant="destructive"
										class="flex-1"
										onclick={() => handleModuleAction('stop', module)}
										disabled={actionLoading === module.id}
									>
										{#if actionLoading === module.id}
											<Loader2 class="h-4 w-4 mr-2 animate-spin" />
										{:else}
											<Square class="h-4 w-4 mr-2" />
										{/if}
										Stop
									</Button>
								{:else}
									<Button variant="secondary" class="flex-1" disabled>
										<Loader2 class="h-4 w-4 mr-2 animate-spin" />
										{getStatusDisplayName(module.status)}
									</Button>
								{/if}
							</div>
						</CardFooter>
					</Card>
				{/each}
			</div>
		{/if}
	</CardContent>
</Card>
