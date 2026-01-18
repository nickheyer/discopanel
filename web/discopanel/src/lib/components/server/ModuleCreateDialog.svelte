<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Badge } from '$lib/components/ui/badge';
	import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import DynamicIcon from '$lib/components/ui/DynamicIcon.svelte';
	import AliasHelper from '$lib/components/ui/AliasHelper.svelte';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { ModuleTemplate, Module, ModulePort, ModuleDependency, ModuleEventHook } from '$lib/proto/discopanel/v1/module_pb';
	import { ModuleTemplateType, ModuleEventType, ModuleEventAction } from '$lib/proto/discopanel/v1/module_pb';
	import { Loader2, ArrowLeft, Package, Check, Plus, Trash2, ChevronDown, ChevronUp } from '@lucide/svelte';

	interface Props {
		open: boolean;
		server: Server;
		templates: ModuleTemplate[];
		onCreated: () => void;
	}

	interface EnvVar {
		key: string;
		value: string;
	}

	interface VolumeMount {
		hostPath: string;
		containerPath: string;
		readOnly: boolean;
	}

	interface PortConfig {
		name: string;
		containerPort: number;
		hostPort: number;
		protocol: string;
		proxyEnabled: boolean;
	}

	interface Dependency {
		moduleId: string;
		waitForHealthy: boolean;
		timeoutSeconds: number;
	}

	interface EventHook {
		event: ModuleEventType;
		action: ModuleEventAction;
		command: string;
		delaySeconds: number;
		condition: string;
	}

	interface MetadataEntry {
		key: string;
		value: string;
	}

	let { open = $bindable(), server, templates, onCreated }: Props = $props();

	// Step state
	let step = $state<'select' | 'configure'>('select');
	let selectedTemplate = $state<ModuleTemplate | null>(null);
	let creating = $state(false);

	// Form state
	let name = $state('');
	let autoStart = $state(true);
	let followServerLifecycle = $state(true);
	let detached = $state(false);
	let memory = $state(256);
	let cpuLimit = $state(1.0);
	let startImmediately = $state(true);

	// Environment variables and volumes as editable arrays
	let envVars = $state<EnvVar[]>([]);
	let volumes = $state<VolumeMount[]>([]);

	// Port configuration (consolidated)
	let ports = $state<PortConfig[]>([]);
	let dependencies = $state<Dependency[]>([]);
	let healthCheckInterval = $state(30);
	let healthCheckTimeout = $state(5);
	let healthCheckRetries = $state(3);
	let eventHooks = $state<EventHook[]>([]);
	let metadata = $state<MetadataEntry[]>([]);

	// Other modules for the same server (for dependency selection)
	let serverModules = $state<Module[]>([]);

	// Collapsible sections
	let advancedExpanded = $state(false);

	// Convert env vars array to JSON string for API
	function envVarsToJson(): string {
		const obj: Record<string, string> = {};
		for (const env of envVars) {
			if (env.key.trim()) {
				obj[env.key.trim()] = env.value;
			}
		}
		return JSON.stringify(obj);
	}

	// Convert volumes array to JSON string for API
	function volumesToJson(): string {
		return JSON.stringify(
			volumes
				.filter((v) => v.hostPath.trim() && v.containerPath.trim())
				.map((v) => ({
					source: v.hostPath.trim(),
					target: v.containerPath.trim(),
					read_only: v.readOnly
				}))
		);
	}

	// Parse JSON string to env vars array
	function parseEnvVars(json: string): EnvVar[] {
		try {
			const obj = JSON.parse(json || '{}');
			return Object.entries(obj).map(([key, value]) => ({ key, value: String(value) }));
		} catch {
			return [];
		}
	}

	// Parse JSON string to volumes array
	function parseVolumes(json: string): VolumeMount[] {
		try {
			const arr = JSON.parse(json || '[]');
			return arr.map((v: { source?: string; target?: string; read_only?: boolean }) => ({
				hostPath: v.source || '',
				containerPath: v.target || '',
				readOnly: v.read_only || false
			}));
		} catch {
			return [];
		}
	}

	function addEnvVar() {
		envVars = [...envVars, { key: '', value: '' }];
	}

	function removeEnvVar(index: number) {
		envVars = envVars.filter((_, i) => i !== index);
	}

	function addVolume() {
		volumes = [...volumes, { hostPath: '', containerPath: '', readOnly: false }];
	}

	function removeVolume(index: number) {
		volumes = volumes.filter((_, i) => i !== index);
	}

	// Port helpers
	function addPort() {
		ports = [...ports, { name: '', containerPort: 0, hostPort: 0, protocol: 'tcp', proxyEnabled: selectedTemplate?.supportsProxy ?? true }];
	}

	function removePort(index: number) {
		ports = ports.filter((_, i) => i !== index);
	}

	function parsePorts(modulePorts: ModulePort[] | undefined): PortConfig[] {
		if (!modulePorts) return [];
		return modulePorts.map(p => ({
			name: p.name,
			containerPort: p.containerPort,
			hostPort: p.hostPort,
			protocol: p.protocol || 'tcp',
			proxyEnabled: p.proxyEnabled
		}));
	}

	// Dependencies helpers
	function addDependency() {
		dependencies = [...dependencies, { moduleId: '', waitForHealthy: true, timeoutSeconds: 60 }];
	}

	function removeDependency(index: number) {
		dependencies = dependencies.filter((_, i) => i !== index);
	}

	// Event hooks helpers
	function addEventHook() {
		eventHooks = [...eventHooks, {
			event: ModuleEventType.SERVER_START,
			action: ModuleEventAction.START,
			command: '',
			delaySeconds: 0,
			condition: ''
		}];
	}

	function removeEventHook(index: number) {
		eventHooks = eventHooks.filter((_, i) => i !== index);
	}

	function parseEventHooks(hooks: ModuleEventHook[] | undefined): EventHook[] {
		if (!hooks) return [];
		return hooks.map(h => ({
			event: h.event,
			action: h.action,
			command: h.command,
			delaySeconds: h.delaySeconds,
			condition: h.condition
		}));
	}

	// Metadata helpers
	function addMetadataEntry() {
		metadata = [...metadata, { key: '', value: '' }];
	}

	function removeMetadataEntry(index: number) {
		metadata = metadata.filter((_, i) => i !== index);
	}

	function parseMetadata(meta: { [key: string]: string } | undefined): MetadataEntry[] {
		if (!meta) return [];
		return Object.entries(meta).map(([key, value]) => ({ key, value }));
	}

	function metadataToMap(): { [key: string]: string } {
		const map: { [key: string]: string } = {};
		for (const entry of metadata) {
			if (entry.key.trim()) {
				map[entry.key.trim()] = entry.value;
			}
		}
		return map;
	}

	// Helper functions for enum labels
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

	function getEventActionLabel(action: ModuleEventAction): string {
		switch (action) {
			case ModuleEventAction.START: return 'Start Module';
			case ModuleEventAction.STOP: return 'Stop Module';
			case ModuleEventAction.RESTART: return 'Restart Module';
			case ModuleEventAction.EXEC: return 'Execute Command';
			case ModuleEventAction.RCON: return 'RCON Command';
			default: return 'Unknown';
		}
	}

	// Load other modules for dependency selection
	async function loadServerModules() {
		try {
			const response = await rpcClient.module.listModules({ serverId: server.id }, silentCallOptions);
			serverModules = response.modules;
		} catch {
			serverModules = [];
		}
	}

	// Filter templates by category
	let categories = $derived.by(() => {
		const cats = new Set<string>();
		templates.forEach((t) => {
			if (t.category) cats.add(t.category);
		});
		return Array.from(cats).sort();
	});

	let selectedCategory = $state<string | null>(null);

	let filteredTemplates = $derived.by(() => {
		if (!selectedCategory) return templates;
		return templates.filter((t) => t.category === selectedCategory);
	});

	let builtinTemplates = $derived.by(() => filteredTemplates.filter((t) => t.type === ModuleTemplateType.BUILTIN));
	let customTemplates = $derived.by(() => filteredTemplates.filter((t) => t.type === ModuleTemplateType.CUSTOM));

	// Reset form when dialog opens/closes or template changes
	$effect(() => {
		if (!open) {
			step = 'select';
			selectedTemplate = null;
			resetForm();
		}
	});

	async function selectTemplate(template: ModuleTemplate) {
		selectedTemplate = template;

		// Pre-populate form with template defaults
		name = template.name;

		// Fetch next available port and load existing modules in parallel
		const [portResponse] = await Promise.all([
			rpcClient.module.getNextAvailableModulePort({ serverId: server.id }).catch(() => ({ port: 8100 })),
			loadServerModules()
		]);

		// Pre-populate with template defaults - user can modify before submitting
		envVars = parseEnvVars(template.defaultEnv || '{}');
		volumes = parseVolumes(template.defaultVolumes || '[]');

		// Pre-populate ports from template, auto-allocating host ports
		ports = parsePorts(template.ports);
		let nextPort = portResponse.port;
		for (const port of ports) {
			if (port.hostPort === 0) {
				port.hostPort = nextPort;
				nextPort++;
			}
		}

		eventHooks = parseEventHooks(template.defaultHooks);
		metadata = parseMetadata(template.metadata);

		// Reset advanced settings
		healthCheckInterval = 30;
		healthCheckTimeout = 5;
		healthCheckRetries = 3;
		dependencies = [];
		advancedExpanded = false;

		step = 'configure';
	}

	function resetForm() {
		name = '';
		autoStart = true;
		followServerLifecycle = true;
		detached = false;
		memory = 256;
		cpuLimit = 1.0;
		envVars = [];
		volumes = [];
		startImmediately = true;
		selectedCategory = null;
		ports = [];
		dependencies = [];
		healthCheckInterval = 30;
		healthCheckTimeout = 5;
		healthCheckRetries = 3;
		eventHooks = [];
		metadata = [];
		serverModules = [];
		advancedExpanded = false;
	}

	function goBack() {
		step = 'select';
		selectedTemplate = null;
	}

	async function handleCreate() {
		if (!selectedTemplate) return;

		creating = true;
		try {
			await rpcClient.module.createModule({
				name,
				serverId: server.id,
				templateId: selectedTemplate.id,
				config: '{}',
				envOverrides: envVarsToJson(),
				volumeOverrides: volumesToJson(),
				memory,
				cpuLimit,
				autoStart,
				followServerLifecycle,
				detached,
				startImmediately,
				ports: ports
					.filter(p => p.containerPort > 0)
					.map(p => ({
						name: p.name,
						containerPort: p.containerPort,
						hostPort: p.hostPort,
						protocol: p.protocol,
						proxyEnabled: p.proxyEnabled
					})),
				dependencies: dependencies
					.filter(d => d.moduleId)
					.map(d => ({
						moduleId: d.moduleId,
						waitForHealthy: d.waitForHealthy,
						timeoutSeconds: d.timeoutSeconds
					})),
				healthCheckInterval,
				healthCheckTimeout,
				healthCheckRetries,
				eventHooks: eventHooks.map(h => ({
					event: h.event,
					action: h.action,
					command: h.command,
					delaySeconds: h.delaySeconds,
					condition: h.condition
				})),
				metadata: metadataToMap()
			});
			toast.success(`Module "${name}" created${startImmediately ? ' and starting' : ''}`);
			open = false;
			onCreated();
		} catch (error) {
			toast.error(`Failed to create module: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			creating = false;
		}
	}
</script>

<Dialog bind:open>
	<DialogContent class="max-w-2xl max-h-[90vh] overflow-y-auto">
		{#if step === 'select'}
			<DialogHeader>
				<DialogTitle>Add Module</DialogTitle>
				<DialogDescription>Select a module template to add to your server</DialogDescription>
			</DialogHeader>

			{#if categories.length > 0}
				<div class="flex flex-wrap gap-2 mb-4">
					<Button
						variant={selectedCategory === null ? 'default' : 'outline'}
						size="sm"
						onclick={() => (selectedCategory = null)}
					>
						All
					</Button>
					{#each categories as category}
						<Button
							variant={selectedCategory === category ? 'default' : 'outline'}
							size="sm"
							onclick={() => (selectedCategory = category)}
						>
							{category}
						</Button>
					{/each}
				</div>
			{/if}

			<div class="space-y-4">
				{#if builtinTemplates.length > 0}
					<div>
						<h4 class="text-sm font-medium text-muted-foreground mb-2">Built-in Templates</h4>
						<div class="grid gap-2 grid-cols-1 sm:grid-cols-2">
							{#each builtinTemplates as template}
								<button
									class="flex items-start gap-3 p-3 rounded-lg border hover:bg-muted/50 transition-colors text-left"
									onclick={() => selectTemplate(template)}
								>
									<div class="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center flex-shrink-0">
										<DynamicIcon name={template.icon} class="h-5 w-5 text-primary" fallback="Package" />
									</div>
									<div class="flex-1 min-w-0">
										<div class="flex items-center gap-2">
											<span class="font-medium truncate">{template.name}</span>
											{#if template.category}
												<Badge variant="secondary" class="text-[10px] px-1.5 py-0">{template.category}</Badge>
											{/if}
										</div>
										<p class="text-xs text-muted-foreground line-clamp-2 mt-0.5">{template.description}</p>
									</div>
								</button>
							{/each}
						</div>
					</div>
				{/if}

				{#if customTemplates.length > 0}
					<div>
						<h4 class="text-sm font-medium text-muted-foreground mb-2">Custom Templates</h4>
						<div class="grid gap-2 grid-cols-1 sm:grid-cols-2">
							{#each customTemplates as template}
								<button
									class="flex items-start gap-3 p-3 rounded-lg border hover:bg-muted/50 transition-colors text-left"
									onclick={() => selectTemplate(template)}
								>
									<div class="h-10 w-10 rounded-lg bg-secondary/50 flex items-center justify-center flex-shrink-0">
										<DynamicIcon name={template.icon} class="h-5 w-5 text-muted-foreground" fallback="Package" />
									</div>
									<div class="flex-1 min-w-0">
										<div class="flex items-center gap-2">
											<span class="font-medium truncate">{template.name}</span>
											{#if template.category}
												<Badge variant="secondary" class="text-[10px] px-1.5 py-0">{template.category}</Badge>
											{/if}
										</div>
										<p class="text-xs text-muted-foreground line-clamp-2 mt-0.5">{template.description}</p>
									</div>
								</button>
							{/each}
						</div>
					</div>
				{/if}

				{#if templates.length === 0}
					<div class="flex flex-col items-center justify-center py-8 text-muted-foreground">
						<Package class="h-12 w-12 mb-4" />
						<p>No module templates available</p>
					</div>
				{/if}
			</div>
		{:else if step === 'configure' && selectedTemplate}
			<DialogHeader>
				<div class="flex items-center gap-2">
					<Button variant="ghost" size="icon" onclick={goBack} class="h-8 w-8">
						<ArrowLeft class="h-4 w-4" />
					</Button>
					<div>
						<DialogTitle>Configure {selectedTemplate.name}</DialogTitle>
						<DialogDescription>Configure the module instance settings</DialogDescription>
					</div>
				</div>
			</DialogHeader>

			<div class="flex items-center gap-4 py-2 px-1 text-xs text-muted-foreground border-b mb-2">
				<span class="flex-1">Use template aliases for dynamic values in env vars, volumes, and metadata.</span>
				<AliasHelper serverId={server.id} showLabel />
			</div>

			<div class="space-y-6 py-4">
				<!-- Basic Info -->
				<div class="space-y-4">
					<h4 class="text-sm font-medium">Basic Information</h4>
					<div class="space-y-2">
						<Label for="name">Module Name</Label>
						<Input id="name" bind:value={name} placeholder="Enter module name" />
					</div>
				</div>

				<!-- Port Configuration -->
				<div class="space-y-4">
					<div class="flex items-center justify-between">
						<div>
							<h4 class="text-sm font-medium">Ports</h4>
							<p class="text-xs text-muted-foreground">Configure port mappings. Host port 0 = auto-allocate.</p>
						</div>
						<Button variant="outline" size="sm" onclick={addPort}>
							<Plus class="h-3 w-3 mr-1" />
							Add Port
						</Button>
					</div>
					{#if ports.length > 0}
						<div class="space-y-3">
							{#each ports as port, index}
								<div class="p-3 border rounded-lg space-y-3">
									<div class="flex items-center gap-2">
										<Input
											placeholder="Port name (e.g., Web UI)"
											bind:value={port.name}
											class="flex-1 text-sm"
										/>
										<Button
											variant="ghost"
											size="icon"
											onclick={() => removePort(index)}
											class="h-8 w-8 text-destructive hover:text-destructive"
										>
											<Trash2 class="h-4 w-4" />
										</Button>
									</div>
									<div class="grid grid-cols-3 gap-2">
										<div class="space-y-1">
											<Label class="text-xs">Host Port</Label>
											<Input type="number" bind:value={port.hostPort} min={0} max={65535} class="text-sm" placeholder="0=auto" />
										</div>
										<div class="space-y-1">
											<Label class="text-xs">Container Port</Label>
											<Input type="number" bind:value={port.containerPort} min={1} max={65535} class="text-sm" />
										</div>
										<div class="space-y-1">
											<Label class="text-xs">Protocol</Label>
											<Select type="single" value={port.protocol} onValueChange={(v) => { if (v) port.protocol = v; }}>
												<SelectTrigger class="text-sm h-9">
													<span>{port.protocol.toUpperCase()}</span>
												</SelectTrigger>
												<SelectContent>
													<SelectItem value="tcp">TCP</SelectItem>
													<SelectItem value="udp">UDP</SelectItem>
													<SelectItem value="minecraft">MINECRAFT</SelectItem>
													<SelectItem value="http">HTTP</SelectItem>
												</SelectContent>
											</Select>
										</div>
									</div>
									<div class="flex items-center gap-2 pt-1">
										<Switch bind:checked={port.proxyEnabled} />
										<Label class="text-xs">Route through proxy</Label>
									</div>
								</div>
							{/each}
						</div>
					{:else}
						<p class="text-xs text-muted-foreground italic">No ports configured</p>
					{/if}
				</div>

				<!-- Lifecycle Configuration -->
				<div class="space-y-4">
					<h4 class="text-sm font-medium">Lifecycle Settings</h4>
					<div class="space-y-3">
						<div class="flex items-center justify-between">
							<div>
								<Label for="autoStart">Auto-start</Label>
								<p class="text-xs text-muted-foreground">Start module when DiscoPanel starts</p>
							</div>
							<Switch id="autoStart" bind:checked={autoStart} />
						</div>
						<div class="flex items-center justify-between">
							<div>
								<Label for="followServerLifecycle">Follow Server Lifecycle</Label>
								<p class="text-xs text-muted-foreground">Stop module when server stops</p>
							</div>
							<Switch id="followServerLifecycle" bind:checked={followServerLifecycle} />
						</div>
						<div class="flex items-center justify-between">
							<div>
								<Label for="detached">Detached Mode</Label>
								<p class="text-xs text-muted-foreground">Keep running when DiscoPanel shuts down</p>
							</div>
							<Switch id="detached" bind:checked={detached} />
						</div>
					</div>
				</div>

				<!-- Resource Limits -->
				<div class="space-y-4">
					<h4 class="text-sm font-medium">Resource Limits</h4>
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-2">
							<Label for="memory">Memory (MB)</Label>
							<Input id="memory" type="number" bind:value={memory} min={64} max={32768} step={64} />
						</div>
						<div class="space-y-2">
							<Label for="cpuLimit">CPU Limit (cores)</Label>
							<Input id="cpuLimit" type="number" bind:value={cpuLimit} min={0.1} max={16} step={0.1} />
						</div>
					</div>
				</div>

				<!-- Environment Variables -->
				<div class="space-y-4">
					<div class="flex items-center justify-between">
						<div>
							<h4 class="text-sm font-medium">Environment Variables</h4>
							<p class="text-xs text-muted-foreground">Override or add environment variables. Use aliases for dynamic values.</p>
						</div>
						<Button variant="outline" size="sm" onclick={addEnvVar}>
							<Plus class="h-3 w-3 mr-1" />
							Add
						</Button>
					</div>
					{#if envVars.length > 0}
						<div class="space-y-2">
							{#each envVars as env, index}
								<div class="flex items-center gap-2">
									<Input
										placeholder="KEY"
										bind:value={env.key}
										class="w-32 font-mono text-sm"
									/>
									<span class="text-muted-foreground">=</span>
									<Input
										placeholder="value"
										bind:value={env.value}
										class="flex-1 font-mono text-sm"
									/>
									<Button
										variant="ghost"
										size="icon"
										onclick={() => removeEnvVar(index)}
										class="h-8 w-8 text-destructive hover:text-destructive"
									>
										<Trash2 class="h-4 w-4" />
									</Button>
								</div>
							{/each}
						</div>
					{:else}
						<p class="text-xs text-muted-foreground italic">No environment variables configured</p>
					{/if}
				</div>

				<!-- Volume Mounts -->
				<div class="space-y-4">
					<div class="flex items-center justify-between">
						<div>
							<h4 class="text-sm font-medium">Volume Mounts</h4>
							<p class="text-xs text-muted-foreground">Mount host directories into the container. Use aliases for dynamic paths.</p>
						</div>
						<Button variant="outline" size="sm" onclick={addVolume}>
							<Plus class="h-3 w-3 mr-1" />
							Add
						</Button>
					</div>
					{#if volumes.length > 0}
						<div class="space-y-2">
							{#each volumes as volume, index}
								<div class="flex items-center gap-2">
									<Input
										placeholder="/host/path"
										bind:value={volume.hostPath}
										class="flex-1 font-mono text-sm"
									/>
									<span class="text-muted-foreground">:</span>
									<Input
										placeholder="/container/path"
										bind:value={volume.containerPath}
										class="flex-1 font-mono text-sm"
									/>
									<label class="flex items-center gap-1 text-xs text-muted-foreground whitespace-nowrap">
										<input type="checkbox" bind:checked={volume.readOnly} class="h-3 w-3" />
										RO
									</label>
									<Button
										variant="ghost"
										size="icon"
										onclick={() => removeVolume(index)}
										class="h-8 w-8 text-destructive hover:text-destructive"
									>
										<Trash2 class="h-4 w-4" />
									</Button>
								</div>
							{/each}
						</div>
					{:else}
						<p class="text-xs text-muted-foreground italic">No volume mounts configured</p>
					{/if}
				</div>

				<!-- Advanced Settings (Collapsible) -->
				<div class="border rounded-lg">
					<button
						type="button"
						class="flex items-center justify-between w-full p-4 text-left hover:bg-muted/50 transition-colors"
						onclick={() => (advancedExpanded = !advancedExpanded)}
					>
						<div>
							<h4 class="text-sm font-medium">Advanced Settings</h4>
							<p class="text-xs text-muted-foreground">Additional ports, dependencies, event hooks, and metadata</p>
						</div>
						{#if advancedExpanded}
							<ChevronUp class="h-4 w-4 text-muted-foreground" />
						{:else}
							<ChevronDown class="h-4 w-4 text-muted-foreground" />
						{/if}
					</button>

					{#if advancedExpanded}
						<div class="p-4 space-y-6 border-t">
							<!-- Dependencies -->
							<div class="space-y-4">
								<div class="flex items-center justify-between">
									<div>
										<h4 class="text-sm font-medium">Module Dependencies</h4>
										<p class="text-xs text-muted-foreground">Modules that must start before this one</p>
									</div>
									<Button variant="outline" size="sm" onclick={addDependency} disabled={serverModules.length === 0}>
										<Plus class="h-3 w-3 mr-1" />
										Add
									</Button>
								</div>
								{#if dependencies.length > 0}
									<div class="space-y-2">
										{#each dependencies as dep, index}
											<div class="flex items-center gap-2 p-2 border rounded-lg">
												<Select type="single" value={dep.moduleId} onValueChange={(v) => { if (v) dep.moduleId = v; }}>
													<SelectTrigger class="flex-1 text-sm">
														<span>{serverModules.find(m => m.id === dep.moduleId)?.name || 'Select module...'}</span>
													</SelectTrigger>
													<SelectContent>
														{#each serverModules as mod}
															<SelectItem value={mod.id}>{mod.name}</SelectItem>
														{/each}
													</SelectContent>
												</Select>
												<label class="flex items-center gap-1 text-xs text-muted-foreground whitespace-nowrap">
													<input type="checkbox" bind:checked={dep.waitForHealthy} class="h-3 w-3" />
													Wait healthy
												</label>
												<Input type="number" bind:value={dep.timeoutSeconds} min={0} max={600} class="w-20 text-sm" placeholder="60s" />
												<Button
													variant="ghost"
													size="icon"
													onclick={() => removeDependency(index)}
													class="h-8 w-8 text-destructive hover:text-destructive"
												>
													<Trash2 class="h-4 w-4" />
												</Button>
											</div>
										{/each}
									</div>
								{:else}
									<p class="text-xs text-muted-foreground italic">
										{serverModules.length === 0 ? 'No other modules on this server' : 'No dependencies configured'}
									</p>
								{/if}
							</div>

							<!-- Health Check Settings -->
							<div class="space-y-4">
								<h4 class="text-sm font-medium">Health Check Settings</h4>
								<p class="text-xs text-muted-foreground">Configure how dependencies wait for this module to be healthy</p>
								<div class="grid grid-cols-3 gap-4">
									<div class="space-y-2">
										<Label for="healthCheckInterval">Interval (seconds)</Label>
										<Input id="healthCheckInterval" type="number" bind:value={healthCheckInterval} min={5} max={300} />
									</div>
									<div class="space-y-2">
										<Label for="healthCheckTimeout">Timeout (seconds)</Label>
										<Input id="healthCheckTimeout" type="number" bind:value={healthCheckTimeout} min={1} max={60} />
									</div>
									<div class="space-y-2">
										<Label for="healthCheckRetries">Retries</Label>
										<Input id="healthCheckRetries" type="number" bind:value={healthCheckRetries} min={1} max={10} />
									</div>
								</div>
							</div>

							<!-- Event Hooks -->
							<div class="space-y-4">
								<div class="flex items-center justify-between">
									<div>
										<h4 class="text-sm font-medium">Event Hooks</h4>
										<p class="text-xs text-muted-foreground">Trigger actions based on server lifecycle events</p>
									</div>
									<Button variant="outline" size="sm" onclick={addEventHook}>
										<Plus class="h-3 w-3 mr-1" />
										Add
									</Button>
								</div>
								{#if eventHooks.length > 0}
									<div class="space-y-3">
										{#each eventHooks as hook, index}
											<div class="p-3 border rounded-lg space-y-2">
												<div class="flex items-center justify-between">
													<span class="text-xs font-medium text-muted-foreground">Hook {index + 1}</span>
													<Button
														variant="ghost"
														size="icon"
														onclick={() => removeEventHook(index)}
														class="h-6 w-6 text-destructive hover:text-destructive"
													>
														<Trash2 class="h-3 w-3" />
													</Button>
												</div>
												<div class="grid grid-cols-2 gap-2">
													<div class="space-y-1">
														<Label class="text-xs">Event</Label>
														<Select type="single" value={getEventTypeLabel(hook.event)} onValueChange={(v) => {
															if (v === 'Server Start') hook.event = ModuleEventType.SERVER_START;
															else if (v === 'Server Stop') hook.event = ModuleEventType.SERVER_STOP;
															else if (v === 'Server Healthy') hook.event = ModuleEventType.SERVER_HEALTHY;
															else if (v === 'Player Join') hook.event = ModuleEventType.PLAYER_JOIN;
															else if (v === 'Player Leave') hook.event = ModuleEventType.PLAYER_LEAVE;
														}}>
															<SelectTrigger class="text-sm">
																<span>{getEventTypeLabel(hook.event)}</span>
															</SelectTrigger>
															<SelectContent>
																<SelectItem value="Server Start">Server Start</SelectItem>
																<SelectItem value="Server Stop">Server Stop</SelectItem>
																<SelectItem value="Server Healthy">Server Healthy</SelectItem>
																<SelectItem value="Player Join">Player Join</SelectItem>
																<SelectItem value="Player Leave">Player Leave</SelectItem>
															</SelectContent>
														</Select>
													</div>
													<div class="space-y-1">
														<Label class="text-xs">Action</Label>
														<Select type="single" value={getEventActionLabel(hook.action)} onValueChange={(v) => {
															if (v === 'Start Module') hook.action = ModuleEventAction.START;
															else if (v === 'Stop Module') hook.action = ModuleEventAction.STOP;
															else if (v === 'Restart Module') hook.action = ModuleEventAction.RESTART;
															else if (v === 'Execute Command') hook.action = ModuleEventAction.EXEC;
															else if (v === 'RCON Command') hook.action = ModuleEventAction.RCON;
														}}>
															<SelectTrigger class="text-sm">
																<span>{getEventActionLabel(hook.action)}</span>
															</SelectTrigger>
															<SelectContent>
																<SelectItem value="Start Module">Start Module</SelectItem>
																<SelectItem value="Stop Module">Stop Module</SelectItem>
																<SelectItem value="Restart Module">Restart Module</SelectItem>
																<SelectItem value="Execute Command">Execute Command</SelectItem>
																<SelectItem value="RCON Command">RCON Command</SelectItem>
															</SelectContent>
														</Select>
													</div>
												</div>
												{#if hook.action === ModuleEventAction.EXEC || hook.action === ModuleEventAction.RCON}
													<div class="space-y-1">
														<Label class="text-xs">Command</Label>
														<Input bind:value={hook.command} placeholder={hook.action === ModuleEventAction.RCON ? 'say Hello' : '/bin/sh -c "..."'} class="font-mono text-sm" />
													</div>
												{/if}
												<div class="grid grid-cols-2 gap-2">
													<div class="space-y-1">
														<Label class="text-xs">Delay (seconds)</Label>
														<Input type="number" bind:value={hook.delaySeconds} min={0} max={3600} class="text-sm" />
													</div>
													<div class="space-y-1">
														<Label class="text-xs">Condition (optional)</Label>
														<Input bind:value={hook.condition} placeholder={'{{server.players_online}} == 0'} class="font-mono text-sm" />
													</div>
												</div>
											</div>
										{/each}
									</div>
								{:else}
									<p class="text-xs text-muted-foreground italic">No event hooks configured</p>
								{/if}
							</div>

							<!-- Metadata -->
							<div class="space-y-4">
								<div class="flex items-center justify-between">
									<div>
										<h4 class="text-sm font-medium">Metadata</h4>
										<p class="text-xs text-muted-foreground">Custom key-value pairs. Values support alias substitution.</p>
									</div>
									<Button variant="outline" size="sm" onclick={addMetadataEntry}>
										<Plus class="h-3 w-3 mr-1" />
										Add
									</Button>
								</div>
								{#if metadata.length > 0}
									<div class="space-y-2">
										{#each metadata as entry, index}
											<div class="flex items-center gap-2">
												<Input
													placeholder="key"
													bind:value={entry.key}
													class="w-32 font-mono text-sm"
												/>
												<span class="text-muted-foreground">:</span>
												<Input
													placeholder="value"
													bind:value={entry.value}
													class="flex-1 font-mono text-sm"
												/>
												<Button
													variant="ghost"
													size="icon"
													onclick={() => removeMetadataEntry(index)}
													class="h-8 w-8 text-destructive hover:text-destructive"
												>
													<Trash2 class="h-4 w-4" />
												</Button>
											</div>
										{/each}
									</div>
								{:else}
									<p class="text-xs text-muted-foreground italic">No metadata configured</p>
								{/if}
							</div>
						</div>
					{/if}
				</div>

				<!-- Start Immediately -->
				<div class="flex items-center justify-between p-4 rounded-lg bg-muted/50">
					<div>
						<Label for="startImmediately">Start Immediately</Label>
						<p class="text-xs text-muted-foreground">Start the module right after creation</p>
					</div>
					<Switch id="startImmediately" bind:checked={startImmediately} />
				</div>
			</div>

			<DialogFooter>
				<Button variant="outline" onclick={goBack}>Back</Button>
				<Button onclick={handleCreate} disabled={creating || !name.trim()}>
					{#if creating}
						<Loader2 class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<Check class="h-4 w-4 mr-2" />
					{/if}
					Create Module
				</Button>
			</DialogFooter>
		{/if}
	</DialogContent>
</Dialog>
