<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Dialog, DialogContent } from '$lib/components/ui/dialog';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { AlertTriangle } from '@lucide/svelte';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import AliasHelper from '$lib/components/ui/AliasHelper.svelte';
	import ModuleTemplateMenu from './ModuleTemplateMenu.svelte';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type {
		ModuleTemplate,
		Module,
		ModulePort,
		ModuleDependency,
		ModuleEventHook
	} from '$lib/proto/discopanel/v1/module_pb';
	import { ModuleStatus, ModuleEventAction } from '$lib/proto/discopanel/v1/module_pb';
	import { TriggeredEventType } from '$lib/proto/discopanel/v1/event_pb';
	import { SERVER_EVENT_TYPES, getEventTypeLabel } from '$lib/utils/events';
	import {
		Loader2,
		ArrowLeft,
		Package,
		Check,
		Plus,
		Trash2,
		Copy,
		Save,
		X,
		Settings,
		Network,
		Variable,
		HardDrive,
		Wrench,
		Info,
		Play,
		Heart
	} from '@lucide/svelte';
	import { copyToClipboard as copyText } from '$lib/utils/clipboard';

	interface Props {
		open: boolean;
		mode: 'create' | 'edit';
		server?: Server;
		templates?: ModuleTemplate[];
		module?: Module;
		onSuccess: () => void;
		onTemplateDeleted?: () => void;
	}

	interface EnvVar {
		key: string;
		value: string;
	}
	interface VolumeMount {
		hostPath: string;
		containerPath: string;
		readOnly: boolean;
		createDir: boolean;
	}
	interface MetadataEntry {
		key: string;
		value: string;
	}

	type ConfigSection = 'general' | 'ports' | 'environment' | 'volumes' | 'advanced';

	let {
		open = $bindable(),
		mode,
		server,
		templates,
		module,
		onSuccess,
		onTemplateDeleted
	}: Props = $props();

	let step = $state<'select' | 'configure'>('select');
	let selectedTemplate = $state<ModuleTemplate | null>(null);
	let submitting = $state(false);
	let activeSection = $state<ConfigSection>('general');

	// Form state
	let name = $state('');
	let autoStart = $state(true);
	let followServerLifecycle = $state(true);
	let detached = $state(false);
	let memory = $state(512);
	let cpuLimit = $state(1.0);
	let uid = $state('');
	let gid = $state('');
	let initCommand = $state('');
	let initCommandDelay = $state(0);
	let restartAfterInit = $state(false);
	let startImmediately = $state(true);
	let envVars = $state<EnvVar[]>([]);
	let volumes = $state<VolumeMount[]>([]);
	let ports = $state<ModulePort[]>([]);
	let dependencies = $state<ModuleDependency[]>([]);
	let healthCheckInterval = $state(30);
	let healthCheckTimeout = $state(5);
	let healthCheckRetries = $state(3);
	let eventHooks = $state<ModuleEventHook[]>([]);
	let metadata = $state<MetadataEntry[]>([]);
	let serverModules = $state<Module[]>([]);

	let serverId = $derived(mode === 'create' ? server?.id : module?.serverId);
	let hasProxy = $derived(
		mode === 'create' ? !!server?.proxyHostname : !!module?.serverProxyHostname
	);

	const navItems: { id: ConfigSection; label: string; icon: typeof Settings }[] = [
		{ id: 'general', label: 'General', icon: Settings },
		{ id: 'ports', label: 'Ports', icon: Network },
		{ id: 'environment', label: 'Environment', icon: Variable },
		{ id: 'volumes', label: 'Volumes', icon: HardDrive },
		{ id: 'advanced', label: 'Advanced', icon: Wrench }
	];

	// Helpers
	function envVarsToJson(): string {
		const obj: Record<string, string> = {};
		for (const env of envVars) {
			if (env.key.trim()) obj[env.key.trim()] = env.value;
		}
		return JSON.stringify(obj);
	}

	function volumesToJson(): string {
		return JSON.stringify(
			volumes
				.filter((v) => v.hostPath.trim() && v.containerPath.trim())
				.map((v) => ({
					source: v.hostPath.trim(),
					target: v.containerPath.trim(),
					read_only: v.readOnly,
					create_dir: v.createDir
				}))
		);
	}

	function parseEnvVars(json: string): EnvVar[] {
		try {
			return Object.entries(JSON.parse(json || '{}')).map(([key, value]) => ({
				key,
				value: String(value)
			}));
		} catch {
			return [];
		}
	}

	function parseVolumes(json: string): VolumeMount[] {
		try {
			return JSON.parse(json || '[]').map((v: Record<string, unknown>) => ({
				hostPath: v.source || '',
				containerPath: v.target || '',
				readOnly: v.read_only || false,
				createDir: v.create_dir || false
			}));
		} catch {
			return [];
		}
	}

	function parsePorts(p: ModulePort[] | undefined): ModulePort[] {
		return (
			p?.map(
				(x) =>
					({
						name: x.name,
						containerPort: x.containerPort,
						hostPort: x.hostPort,
						protocol: x.protocol || 'tcp',
						proxyEnabled: x.proxyEnabled
					}) as ModulePort
			) || []
		);
	}

	function parseDependencies(d: ModuleDependency[] | undefined): ModuleDependency[] {
		return (
			d?.map(
				(x) =>
					({
						moduleId: x.moduleId,
						waitForHealthy: x.waitForHealthy,
						timeoutSeconds: x.timeoutSeconds
					}) as ModuleDependency
			) || []
		);
	}

	function parseEventHooks(h: ModuleEventHook[] | undefined): ModuleEventHook[] {
		return (
			h?.map(
				(x) =>
					({
						event: x.event,
						action: x.action,
						command: x.command,
						delaySeconds: x.delaySeconds,
						condition: x.condition
					}) as ModuleEventHook
			) || []
		);
	}

	function parseMetadata(m: { [key: string]: string } | undefined): MetadataEntry[] {
		return m ? Object.entries(m).map(([key, value]) => ({ key, value })) : [];
	}

	function metadataToMap(): { [key: string]: string } {
		const map: { [key: string]: string } = {};
		for (const e of metadata) {
			if (e.key.trim()) map[e.key.trim()] = e.value;
		}
		return map;
	}

	function addEnvVar() {
		envVars = [...envVars, { key: '', value: '' }];
	}
	function removeEnvVar(i: number) {
		envVars = envVars.filter((_, idx) => idx !== i);
	}
	function addVolume() {
		volumes = [...volumes, { hostPath: '', containerPath: '', readOnly: false, createDir: false }];
	}
	function removeVolume(i: number) {
		volumes = volumes.filter((_, idx) => idx !== i);
	}
	function addPort() {
		ports = [
			...ports,
			{ name: '', containerPort: 0, hostPort: 0, protocol: 'tcp', proxyEnabled: true } as ModulePort
		];
	}
	function removePort(i: number) {
		ports = ports.filter((_, idx) => idx !== i);
	}
	function addDependency() {
		dependencies = [
			...dependencies,
			{ moduleId: '', waitForHealthy: true, timeoutSeconds: 60 } as ModuleDependency
		];
	}
	function removeDependency(i: number) {
		dependencies = dependencies.filter((_, idx) => idx !== i);
	}
	function addEventHook() {
		eventHooks = [
			...eventHooks,
			{
				event: TriggeredEventType.SERVER_START,
				action: ModuleEventAction.START,
				command: '',
				delaySeconds: 0,
				condition: ''
			} as ModuleEventHook
		];
	}
	function removeEventHook(i: number) {
		eventHooks = eventHooks.filter((_, idx) => idx !== i);
	}
	function addMetadataEntry() {
		metadata = [...metadata, { key: '', value: '' }];
	}
	function removeMetadataEntry(i: number) {
		metadata = metadata.filter((_, idx) => idx !== i);
	}

	function getEventActionLabel(a: ModuleEventAction): string {
		const labels: Record<number, string> = {
			[ModuleEventAction.START]: 'Start',
			[ModuleEventAction.STOP]: 'Stop',
			[ModuleEventAction.RESTART]: 'Restart',
			[ModuleEventAction.EXEC]: 'Exec',
			[ModuleEventAction.RCON]: 'RCON'
		};
		return labels[a] || 'Unknown';
	}

	function getStatusLabel(s: ModuleStatus): string {
		const labels: Record<number, string> = {
			[ModuleStatus.RUNNING]: 'Running',
			[ModuleStatus.STOPPED]: 'Stopped',
			[ModuleStatus.STARTING]: 'Starting',
			[ModuleStatus.STOPPING]: 'Stopping',
			[ModuleStatus.ERROR]: 'Error',
			[ModuleStatus.CREATING]: 'Creating'
		};
		return labels[s] || 'Unknown';
	}

	function getStatusColor(s: ModuleStatus): string {
		const colors: Record<number, string> = {
			[ModuleStatus.RUNNING]: 'bg-green-500/20 text-green-400 border-green-500/30',
			[ModuleStatus.STOPPED]: 'bg-zinc-500/20 text-zinc-400 border-zinc-500/30',
			[ModuleStatus.STARTING]: 'bg-blue-500/20 text-blue-400 border-blue-500/30',
			[ModuleStatus.STOPPING]: 'bg-orange-500/20 text-orange-400 border-orange-500/30',
			[ModuleStatus.ERROR]: 'bg-red-500/20 text-red-400 border-red-500/30',
			[ModuleStatus.CREATING]: 'bg-purple-500/20 text-purple-400 border-purple-500/30'
		};
		return colors[s] || 'bg-zinc-500/20 text-zinc-400 border-zinc-500/30';
	}

	async function loadServerModules() {
		try {
			const response = await rpcClient.module.listModules(
				{ serverId: serverId || '' },
				silentCallOptions
			);
			serverModules =
				mode === 'edit' && module
					? response.modules.filter((m) => m.id !== module.id)
					: response.modules;
		} catch {
			serverModules = [];
		}
	}

	function resetForm() {
		name = '';
		autoStart = true;
		followServerLifecycle = true;
		detached = false;
		memory = 512;
		cpuLimit = 1.0;
		uid = '';
		gid = '';
		initCommand = '';
		initCommandDelay = 0;
		restartAfterInit = false;
		envVars = [];
		volumes = [];
		startImmediately = true;
		ports = [];
		dependencies = [];
		activeSection = 'general';
		healthCheckInterval = 30;
		healthCheckTimeout = 5;
		healthCheckRetries = 3;
		eventHooks = [];
		metadata = [];
		serverModules = [];
	}

	async function selectTemplate(template: ModuleTemplate) {
		selectedTemplate = template;
		name = template.name;
		const [portResponse] = await Promise.all([
			rpcClient.module
				.getNextAvailableModulePort({ serverId: serverId || '' })
				.catch(() => ({ port: 8100 })),
			loadServerModules()
		]);
		envVars = parseEnvVars(template.defaultEnv || '{}');
		volumes = parseVolumes(template.defaultVolumes || '[]');
		ports = parsePorts(template.ports);
		memory = template.defaultMemory;
		uid = template.defaultUid;
		gid = template.defaultGid;
		initCommand = template.defaultInitCommand;
		initCommandDelay = template.defaultInitCommandDelay;
		restartAfterInit = template.defaultRestartAfterInit;
		let nextPort = portResponse.port;
		for (const port of ports) {
			if (port.hostPort === 0) {
				port.hostPort = nextPort;
				nextPort++;
			}
		}
		eventHooks = parseEventHooks(template.defaultHooks);
		metadata = parseMetadata(template.metadata);
		step = 'configure';
	}

	async function handleDeleteTemplate(template: ModuleTemplate) {
		const confirmed = confirm(
			`Are you sure you want to delete template "${template.name}"?\n\nThis cannot be undone.`
		);
		if (!confirmed) return;

		try {
			await rpcClient.module.deleteModuleTemplate({ id: template.id });
			toast.success(`Template "${template.name}" deleted`);
			if (onTemplateDeleted) {
				onTemplateDeleted();
			}
		} catch (error) {
			toast.error(
				`Failed to delete template: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		}
	}

	let warnings = $state<string[]>([]);
	let warningResolve: ((proceed: boolean) => void) | null = null;

	function showWarnings(): Promise<boolean> {
		const w: string[] = [];

		if (ports.some((p) => p.proxyEnabled) && !hasProxy) {
			w.push(
				"One or more ports have proxy enabled, but this server has no proxy hostname configured. Proxy-routed ports won't be accessible from the host"
			);
		}

		if (ports.some((p) => p.hostPort === 0 && p.containerPort > 0)) {
			w.push(
				'One or more ports have no host port assigned. They will not be accessible from outside the container.'
			);
		}

		if (memory < 64) {
			w.push(
				`Memory limit is set to ${memory}MB, which is very low and may cause the container to be killed.`
			);
		}

		if (w.length === 0) return Promise.resolve(true);

		warnings = w;
		return new Promise((resolve) => {
			warningResolve = resolve;
		});
	}

	function handleWarningProceed() {
		warnings = [];
		warningResolve?.(true);
		warningResolve = null;
	}

	function handleWarningCancel() {
		warnings = [];
		warningResolve?.(false);
		warningResolve = null;
	}

	$effect(() => {
		if (open && mode === 'edit' && module) {
			name = module.name;
			autoStart = module.autoStart;
			followServerLifecycle = module.followServerLifecycle;
			detached = module.detached;
			memory = module.memory;
			cpuLimit = module.cpuLimit;
			uid = module.uid;
			gid = module.gid;
			initCommand = module.initCommand;
			initCommandDelay = module.initCommandDelay;
			restartAfterInit = module.restartAfterInit;
			envVars = parseEnvVars(module.envOverrides || '{}');
			volumes = parseVolumes(module.volumeOverrides || '[]');
			ports = parsePorts(module.ports);
			dependencies = parseDependencies(module.dependencies);
			healthCheckInterval = module.healthCheckInterval || 30;
			healthCheckTimeout = module.healthCheckTimeout || 5;
			healthCheckRetries = module.healthCheckRetries || 3;
			eventHooks = parseEventHooks(module.eventHooks);
			metadata = parseMetadata(module.metadata);
			loadServerModules();
		}
	});

	$effect(() => {
		if (!open) {
			step = 'select';
			selectedTemplate = null;
			resetForm();
		}
	});

	async function handleSubmit() {
		const proceed = await showWarnings();
		if (!proceed) return;

		submitting = true;
		try {
			const portsPayload = ports
				.filter((p) => p.containerPort > 0)
				.map((p) => ({
					name: p.name,
					containerPort: p.containerPort,
					hostPort: p.hostPort,
					protocol: p.protocol,
					proxyEnabled: p.proxyEnabled
				}));
			const depsPayload = dependencies
				.filter((d) => d.moduleId)
				.map((d) => ({
					moduleId: d.moduleId,
					waitForHealthy: d.waitForHealthy,
					timeoutSeconds: d.timeoutSeconds
				}));
			const hooksPayload = eventHooks.map((h) => ({
				event: h.event,
				action: h.action,
				command: h.command,
				delaySeconds: h.delaySeconds,
				condition: h.condition
			}));

			if (mode === 'create' && selectedTemplate) {
				await rpcClient.module.createModule({
					name,
					serverId: serverId || '',
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
					ports: portsPayload,
					dependencies: depsPayload,
					healthCheckInterval,
					healthCheckTimeout,
					healthCheckRetries,
					eventHooks: hooksPayload,
					metadata: metadataToMap(),
					uid,
					gid,
					initCommand,
					initCommandDelay,
					restartAfterInit
				});
				toast.success(`Module "${name}" created`);
			} else if (module) {
				await rpcClient.module.updateModule({
					id: module.id,
					name,
					envOverrides: envVarsToJson(),
					volumeOverrides: volumesToJson(),
					memory,
					cpuLimit,
					autoStart,
					followServerLifecycle,
					detached,
					ports: portsPayload,
					dependencies: depsPayload,
					healthCheckInterval,
					healthCheckTimeout,
					healthCheckRetries,
					eventHooks: hooksPayload,
					metadata: metadataToMap(),
					uid,
					gid,
					initCommand,
					initCommandDelay,
					restartAfterInit
				});
				toast.success(`Module "${name}" updated`);
			}
			open = false;
			onSuccess();
		} catch (error) {
			toast.error(`Failed: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			submitting = false;
		}
	}

	async function copyToClipboard(text: string) {
		const success = await copyText(text);
		if (success) {
			toast.success('Copied to clipboard');
		} else {
			toast.error('Failed to copy');
		}
	}
</script>

<Dialog bind:open>
	<DialogContent
		class="flex h-[85vh]! w-[95vw]! max-w-6xl! flex-col gap-0! overflow-hidden p-0!"
		showCloseButton={false}
	>
		{#if mode === 'create' && step === 'select'}
			<!-- Template Selection -->
			<div class="flex h-full flex-col">
				<!-- Header -->
				<div class="flex items-center justify-between border-b bg-muted/30 px-8 py-6">
					<div>
						<h2 class="text-2xl font-semibold tracking-tight">Add Module</h2>
						<p class="mt-1 text-muted-foreground">Select a module template to get started</p>
					</div>
					<Button variant="ghost" size="icon" onclick={() => (open = false)} class="h-10 w-10">
						<X class="h-5 w-5" />
					</Button>
				</div>

				<!-- Content -->
				<div class="flex-1 overflow-y-auto p-8">
					<ModuleTemplateMenu
						{templates}
						onSelect={selectTemplate}
						onDelete={handleDeleteTemplate}
					/>
				</div>
			</div>
		{:else}
			<!-- Configuration View -->
			<div class="flex h-full">
				<!-- Sidebar -->
				<div class="flex w-64 flex-col border-r bg-muted/30">
					<!-- Sidebar Header -->
					<div class="border-b p-6">
						{#if mode === 'create'}
							<button
								onclick={() => {
									step = 'select';
									selectedTemplate = null;
								}}
								class="mb-4 flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
							>
								<ArrowLeft class="h-4 w-4" />
								Back to templates
							</button>
						{/if}
						<div class="flex items-center gap-3">
							<div class="flex h-12 w-12 items-center justify-center rounded-xl bg-primary/10">
								<Package class="h-6 w-6 text-primary" />
							</div>
							<div class="min-w-0 flex-1">
								<h3 class="truncate font-semibold">
									{mode === 'create' ? selectedTemplate?.name : module?.templateName}
								</h3>
								{#if module}
									<div class="mt-1 flex items-center gap-2">
										<span
											class={`rounded-full border px-2 py-0.5 text-xs ${getStatusColor(module.status)}`}
										>
											{getStatusLabel(module.status)}
										</span>
									</div>
								{/if}
							</div>
						</div>
					</div>

					<!-- Navigation -->
					<nav class="flex-1 space-y-1 p-4">
						{#each navItems as item (item.id)}
							{@const Icon = item.icon}
							<button
								onclick={() => (activeSection = item.id)}
								class="flex w-full items-center gap-3 rounded-lg px-4 py-3 text-left transition-colors {activeSection ===
								item.id
									? 'bg-primary text-primary-foreground'
									: 'text-muted-foreground hover:bg-muted hover:text-foreground'}"
							>
								<Icon class="h-5 w-5" />
								<span class="font-medium">{item.label}</span>
							</button>
						{/each}
					</nav>

					<!-- Sidebar Footer -->
					<div class="space-y-4 border-t p-4">
						{#if module?.id}
							<div>
								<div class="mb-2 text-xs text-muted-foreground">Module ID</div>
								<div class="flex items-center gap-2">
									<code class="flex-1 truncate rounded bg-muted px-2 py-1.5 font-mono text-xs">
										{module.id}
									</code>
									<Button
										variant="ghost"
										size="icon"
										onclick={() => copyToClipboard(module.id)}
										class="h-8 w-8 shrink-0"
									>
										<Copy class="h-4 w-4" />
									</Button>
								</div>
							</div>
						{/if}
						<div class="rounded-lg bg-muted/50 p-4">
							<p class="mb-2 text-sm font-medium">Module Aliases</p>
							<p class="mb-3 text-xs text-muted-foreground">
								Use aliases for dynamic values in any configuration field.
							</p>
							<AliasHelper serverId={serverId || ''} moduleId={module?.id} showLabel />
						</div>
					</div>
				</div>

				<!-- Main Content -->
				<div class="flex min-w-0 flex-1 flex-col">
					<!-- Content Header -->
					<div class="flex items-center justify-between border-b bg-muted/30 px-8 py-6">
						<div>
							<h2 class="text-2xl font-semibold tracking-tight">
								{#if activeSection === 'general'}General Settings
								{:else if activeSection === 'ports'}Port Configuration
								{:else if activeSection === 'environment'}Environment Variables
								{:else if activeSection === 'volumes'}Volume Mounts
								{:else if activeSection === 'advanced'}Advanced Settings
								{/if}
							</h2>
							<p class="mt-1 text-muted-foreground">
								{#if activeSection === 'general'}Configure basic module settings and lifecycle
									behavior
								{:else if activeSection === 'ports'}Define network ports for container communication
								{:else if activeSection === 'environment'}Set environment variables for the
									container
								{:else if activeSection === 'volumes'}Mount host directories into the container
								{:else if activeSection === 'advanced'}Dependencies, health checks, hooks, and
									metadata
								{/if}
							</p>
						</div>
						<Button variant="ghost" size="icon" onclick={() => (open = false)} class="h-10 w-10">
							<X class="h-5 w-5" />
						</Button>
					</div>

					<!-- Scrollable Content Area -->
					<div class="flex-1 overflow-y-auto p-8">
						{#if activeSection === 'general'}
							<!-- General Section -->
							<div class="max-w-2xl space-y-8">
								<!-- Module Name -->
								<div class="space-y-3">
									<Label for="name" class="text-base font-medium">Module Name</Label>
									<Input
										id="name"
										bind:value={name}
										placeholder="Enter module name"
										class="h-12 text-base"
									/>
									<p class="text-sm text-muted-foreground">
										A unique identifier for this module instance
									</p>
								</div>

								<!-- Resources -->
								<div class="space-y-4">
									<h3 class="text-base font-medium">Resource Limits</h3>
									<div class="grid grid-cols-2 gap-6">
										<div class="space-y-3">
											<Label for="memory">Memory (MB)</Label>
											<Input
												id="memory"
												type="number"
												bind:value={memory}
												min={64}
												max={32768}
												class="h-12 text-base"
											/>
											<p class="text-sm text-muted-foreground">Minimum: 64 MB</p>
										</div>
										<div class="space-y-3">
											<Label for="cpu">CPU Limit (cores)</Label>
											<Input
												id="cpu"
												type="number"
												bind:value={cpuLimit}
												min={0.1}
												max={16}
												step={0.1}
												class="h-12 text-base"
											/>
											<p class="text-sm text-muted-foreground">Fraction of CPU cores</p>
										</div>
									</div>
								</div>

								<!-- Container User -->
								<div class="space-y-4">
									<h3 class="text-base font-medium">Container User</h3>
									<div class="grid grid-cols-2 gap-6">
										<div class="space-y-3">
											<Label for="uid">UID</Label>
											<Input
												id="uid"
												bind:value={uid}
												placeholder={'{{host.uid}}'}
												class="h-12 font-mono text-base"
											/>
											<p class="text-sm text-muted-foreground">User ID or alias</p>
										</div>
										<div class="space-y-3">
											<Label for="gid">GID</Label>
											<Input
												id="gid"
												bind:value={gid}
												placeholder={'{{host.gid}}'}
												class="h-12 font-mono text-base"
											/>
											<p class="text-sm text-muted-foreground">Group ID or alias</p>
										</div>
									</div>
								</div>

								<!-- Lifecycle -->
								<div class="space-y-4">
									<h3 class="text-base font-medium">Lifecycle Behavior</h3>
									<div class="space-y-4">
										<label
											class="flex cursor-pointer items-start gap-4 rounded-lg border p-4 transition-colors hover:bg-muted/50"
										>
											<Switch bind:checked={autoStart} class="mt-0.5" />
											<div class="space-y-1">
												<span class="font-medium">Auto-start</span>
												<p class="text-sm text-muted-foreground">
													Automatically start this module when the server starts
												</p>
											</div>
										</label>

										<label
											class="flex cursor-pointer items-start gap-4 rounded-lg border p-4 transition-colors hover:bg-muted/50"
										>
											<Switch bind:checked={followServerLifecycle} class="mt-0.5" />
											<div class="space-y-1">
												<span class="font-medium">Follow server lifecycle</span>
												<p class="text-sm text-muted-foreground">
													Stop this module when the server stops
												</p>
											</div>
										</label>

										<label
											class="flex cursor-pointer items-start gap-4 rounded-lg border p-4 transition-colors hover:bg-muted/50"
										>
											<Switch bind:checked={detached} class="mt-0.5" />
											<div class="space-y-1">
												<span class="font-medium">Detached mode</span>
												<p class="text-sm text-muted-foreground">
													Run independently of the server lifecycle
												</p>
											</div>
										</label>
									</div>
								</div>

								{#if mode === 'create'}
									<div class="rounded-lg border border-primary/20 bg-primary/5 p-4">
										<label class="flex cursor-pointer items-start gap-4">
											<Switch bind:checked={startImmediately} class="mt-0.5" />
											<div class="space-y-1">
												<span class="flex items-center gap-2 font-medium">
													<Play class="h-4 w-4" />
													Start immediately
												</span>
												<p class="text-sm text-muted-foreground">
													Launch the module as soon as it's created
												</p>
											</div>
										</label>
									</div>
								{/if}

								{#if module?.dataPath}
									<div class="space-y-3">
										<Label>Data Path</Label>
										<div class="flex items-center gap-3 rounded-lg bg-muted p-4">
											<HardDrive class="h-5 w-5 shrink-0 text-muted-foreground" />
											<code class="flex-1 truncate font-mono text-sm">{module.dataPath}</code>
											<Button
												variant="ghost"
												size="icon"
												onclick={() => copyToClipboard(module.dataPath)}
												class="h-8 w-8 shrink-0"
											>
												<Copy class="h-4 w-4" />
											</Button>
										</div>
									</div>
								{/if}
							</div>
						{:else if activeSection === 'ports'}
							<!-- Ports Section -->
							<div class="space-y-6">
								<div class="flex items-center justify-between">
									<div>
										<p class="text-muted-foreground">
											{ports.length} port{ports.length !== 1 ? 's' : ''} configured
										</p>
									</div>
									<Button onclick={addPort} class="gap-2">
										<Plus class="h-4 w-4" />
										Add Port
									</Button>
								</div>

								{#if ports.length > 0}
									<div class="space-y-4">
										{#each ports as port, i (port.containerPort)}
											<div class="space-y-4 rounded-xl border bg-card p-6">
												<div class="flex items-center justify-between">
													<span class="font-medium">Port {i + 1}</span>
													<Button
														variant="ghost"
														size="icon"
														onclick={() => removePort(i)}
														class="h-8 w-8 text-destructive hover:text-destructive"
													>
														<Trash2 class="h-4 w-4" />
													</Button>
												</div>

												<div class="grid grid-cols-4 gap-4">
													<div class="space-y-2">
														<Label>Name</Label>
														<Input bind:value={port.name} placeholder="http" class="h-11" />
													</div>
													<div class="space-y-2">
														<Label>Host Port</Label>
														<Input
															type="number"
															bind:value={port.hostPort}
															placeholder="8080"
															class="h-11"
														/>
													</div>
													<div class="space-y-2">
														<Label>Container Port</Label>
														<Input
															type="number"
															bind:value={port.containerPort}
															placeholder="8080"
															class="h-11"
														/>
													</div>
													<div class="space-y-2">
														<Label>Protocol</Label>
														<Select
															type="single"
															value={port.protocol}
															onValueChange={(v) => {
																if (v) port.protocol = v;
															}}
														>
															<SelectTrigger class="h-11">
																<span class="uppercase">{port.protocol}</span>
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

												<label class="flex items-center gap-3 pt-2">
													<Checkbox bind:checked={port.proxyEnabled} />
													<span class="text-sm">Enable proxy for this port</span>
												</label>
												{#if port.proxyEnabled && !hasProxy}
													<div
														class="flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-amber-700 dark:text-amber-400"
													>
														<Info class="mt-0.5 h-4 w-4 shrink-0" />
														<div class="flex-1 text-xs">
															<p>
																This server has no proxy hostname configured. Proxy-routed ports
																won't be accessible from the host.
															</p>
															<Button
																variant="outline"
																size="sm"
																class="mt-2 h-7 text-xs"
																onclick={() => {
																	port.proxyEnabled = false;
																	if (port.protocol === 'http') port.protocol = 'tcp';
																}}
															>
																Fix: switch to direct TCP binding
															</Button>
														</div>
													</div>
												{/if}
											</div>
										{/each}
									</div>
								{:else}
									<div
										class="flex flex-col items-center justify-center rounded-xl border border-dashed py-16 text-center"
									>
										<Network class="mb-4 h-12 w-12 text-muted-foreground/50" />
										<h3 class="mb-1 font-medium">No ports configured</h3>
										<p class="mb-4 text-sm text-muted-foreground">
											Add ports to expose container services
										</p>
										<Button onclick={addPort} variant="outline" class="gap-2">
											<Plus class="h-4 w-4" />
											Add Port
										</Button>
									</div>
								{/if}
							</div>
						{:else if activeSection === 'environment'}
							<!-- Environment Section -->
							<div class="space-y-6">
								<div class="flex items-center justify-between">
									<div>
										<p class="text-muted-foreground">
											{envVars.length} variable{envVars.length !== 1 ? 's' : ''} defined
										</p>
									</div>
									<Button onclick={addEnvVar} class="gap-2">
										<Plus class="h-4 w-4" />
										Add Variable
									</Button>
								</div>

								{#if envVars.length > 0}
									<div class="space-y-3">
										{#each envVars as env, i (i)}
											<div class="flex items-center gap-3 rounded-lg border bg-card p-4">
												<Input
													bind:value={env.key}
													placeholder="VARIABLE_NAME"
													class="h-11 w-56 font-mono"
												/>
												<span class="text-xl text-muted-foreground">=</span>
												<Input
													bind:value={env.value}
													placeholder="value"
													class="h-11 flex-1 font-mono"
												/>
												<Button
													variant="ghost"
													size="icon"
													onclick={() => removeEnvVar(i)}
													class="h-10 w-10 shrink-0 text-destructive hover:text-destructive"
												>
													<Trash2 class="h-4 w-4" />
												</Button>
											</div>
										{/each}
									</div>
								{:else}
									<div
										class="flex flex-col items-center justify-center rounded-xl border border-dashed py-16 text-center"
									>
										<Variable class="mb-4 h-12 w-12 text-muted-foreground/50" />
										<h3 class="mb-1 font-medium">No environment variables</h3>
										<p class="mb-4 text-sm text-muted-foreground">
											Add variables to configure the container
										</p>
										<Button onclick={addEnvVar} variant="outline" class="gap-2">
											<Plus class="h-4 w-4" />
											Add Variable
										</Button>
									</div>
								{/if}
							</div>
						{:else if activeSection === 'volumes'}
							<!-- Volumes Section -->
							<div class="space-y-6">
								<div class="flex items-center justify-between">
									<div>
										<p class="text-muted-foreground">
											{volumes.length} volume{volumes.length !== 1 ? 's' : ''} mounted
										</p>
									</div>
									<Button onclick={addVolume} class="gap-2">
										<Plus class="h-4 w-4" />
										Add Volume
									</Button>
								</div>

								{#if volumes.length > 0}
									<div class="space-y-4">
										{#each volumes as vol, i (i)}
											<div class="space-y-4 rounded-xl border bg-card p-6">
												<div class="flex items-center justify-between">
													<span class="font-medium">Volume {i + 1}</span>
													<Button
														variant="ghost"
														size="icon"
														onclick={() => removeVolume(i)}
														class="h-8 w-8 text-destructive hover:text-destructive"
													>
														<Trash2 class="h-4 w-4" />
													</Button>
												</div>

												<div class="grid grid-cols-2 gap-4">
													<div class="space-y-2">
														<Label>Host Path</Label>
														<Input
															bind:value={vol.hostPath}
															placeholder="/host/path"
															class="h-11 font-mono"
														/>
													</div>
													<div class="space-y-2">
														<Label>Container Path</Label>
														<Input
															bind:value={vol.containerPath}
															placeholder="/container/path"
															class="h-11 font-mono"
														/>
													</div>
												</div>

												<div class="flex items-center gap-6 pt-2">
													<label class="flex items-center gap-3">
														<Checkbox
															checked={vol.readOnly}
															onCheckedChange={(checked) => {
																vol.readOnly = !!checked;
																if (vol.readOnly) {
																	vol.createDir = false;
																}
															}}
														/>
														<span class="text-sm">Read-only mount</span>
													</label>
													<label class="flex items-center gap-3">
														<Checkbox
															checked={vol.createDir}
															onCheckedChange={(checked) => {
																vol.createDir = !!checked;
																if (vol.createDir) {
																	vol.readOnly = false;
																}
															}}
														/>
														<span class="text-sm">Pre-create directory</span>
													</label>
												</div>
											</div>
										{/each}
									</div>
								{:else}
									<div
										class="flex flex-col items-center justify-center rounded-xl border border-dashed py-16 text-center"
									>
										<HardDrive class="mb-4 h-12 w-12 text-muted-foreground/50" />
										<h3 class="mb-1 font-medium">No volumes mounted</h3>
										<p class="mb-4 text-sm text-muted-foreground">
											Mount host directories to persist data
										</p>
										<Button onclick={addVolume} variant="outline" class="gap-2">
											<Plus class="h-4 w-4" />
											Add Volume
										</Button>
									</div>
								{/if}
							</div>
						{:else if activeSection === 'advanced'}
							<!-- Advanced Section -->
							<div class="space-y-10">
								<!-- Dependencies -->
								<div class="space-y-4">
									<div class="flex items-center justify-between">
										<div>
											<h3 class="text-lg font-medium">Dependencies</h3>
											<p class="mt-1 text-sm text-muted-foreground">
												Modules that must be running before this one starts
											</p>
										</div>
										<Button
											onclick={addDependency}
											disabled={serverModules.length === 0}
											variant="outline"
											class="gap-2"
										>
											<Plus class="h-4 w-4" />
											Add
										</Button>
									</div>

									{#if dependencies.length > 0}
										<div class="space-y-3">
											{#each dependencies as dep, i (i)}
												<div class="flex items-center gap-4 rounded-lg border bg-card p-4">
													<Select
														type="single"
														value={dep.moduleId}
														onValueChange={(v) => {
															if (v) dep.moduleId = v;
														}}
													>
														<SelectTrigger class="h-11 w-64">
															<span
																>{serverModules.find((m) => m.id === dep.moduleId)?.name ||
																	'Select module...'}</span
															>
														</SelectTrigger>
														<SelectContent>
															{#each serverModules as mod (mod.id)}
																<SelectItem value={mod.id}>{mod.name}</SelectItem>
															{/each}
														</SelectContent>
													</Select>

													<label class="flex items-center gap-2">
														<Checkbox bind:checked={dep.waitForHealthy} />
														<span class="text-sm">Wait for healthy</span>
													</label>

													<div class="flex items-center gap-2">
														<Label class="text-sm whitespace-nowrap">Timeout (s)</Label>
														<Input
															type="number"
															bind:value={dep.timeoutSeconds}
															class="h-11 w-24"
														/>
													</div>

													<Button
														variant="ghost"
														size="icon"
														onclick={() => removeDependency(i)}
														class="ml-auto h-10 w-10 text-destructive hover:text-destructive"
													>
														<Trash2 class="h-4 w-4" />
													</Button>
												</div>
											{/each}
										</div>
									{:else}
										<div
											class="rounded-lg border border-dashed p-6 text-center text-muted-foreground"
										>
											{serverModules.length === 0
												? 'No other modules available on this server'
												: 'No dependencies configured'}
										</div>
									{/if}
								</div>

								<!-- Health Check -->
								<div class="space-y-4">
									<div>
										<h3 class="flex items-center gap-2 text-lg font-medium">
											<Heart class="h-5 w-5" />
											Health Check
										</h3>
										<p class="mt-1 text-sm text-muted-foreground">
											Configure how the module's health is monitored
										</p>
									</div>

									<div class="grid grid-cols-3 gap-6 rounded-lg border bg-card p-6">
										<div class="space-y-2">
											<Label>Interval (seconds)</Label>
											<Input type="number" bind:value={healthCheckInterval} min={5} class="h-11" />
											<p class="text-xs text-muted-foreground">Time between checks</p>
										</div>
										<div class="space-y-2">
											<Label>Timeout (seconds)</Label>
											<Input type="number" bind:value={healthCheckTimeout} min={1} class="h-11" />
											<p class="text-xs text-muted-foreground">Max wait for response</p>
										</div>
										<div class="space-y-2">
											<Label>Retries</Label>
											<Input type="number" bind:value={healthCheckRetries} min={1} class="h-11" />
											<p class="text-xs text-muted-foreground">Failures before unhealthy</p>
										</div>
									</div>
								</div>

								<!-- Init Command -->
								<div class="space-y-4">
									<div>
										<h3 class="text-lg font-medium">Init Command</h3>
										<p class="mt-1 text-sm text-muted-foreground">
											Execute a command inside the container after it starts
										</p>
									</div>

									<div class="space-y-4 rounded-lg border bg-card p-6">
										<div class="space-y-2">
											<Label>Command</Label>
											<Input
												bind:value={initCommand}
												placeholder="sh -c 'sed -i ...'"
												class="h-11 font-mono"
											/>
											<p class="text-xs text-muted-foreground">
												Shell command to exec inside the container after start
											</p>
										</div>
										<div class="grid grid-cols-2 gap-6">
											<div class="space-y-2">
												<Label>Delay (seconds)</Label>
												<Input type="number" bind:value={initCommandDelay} min={0} class="h-11" />
												<p class="text-xs text-muted-foreground">
													Seconds to wait after start before running
												</p>
											</div>
											<div class="flex items-center pt-6">
												<label class="flex items-center gap-3">
													<Checkbox bind:checked={restartAfterInit} />
													<div>
														<span class="text-sm font-medium">Restart after init</span>
														<p class="text-xs text-muted-foreground">
															Restart the container after the command runs
														</p>
													</div>
												</label>
											</div>
										</div>
									</div>
								</div>

								<!-- Event Hooks -->
								<div class="space-y-4">
									<div class="flex items-center justify-between">
										<div>
											<h3 class="text-lg font-medium">Event Hooks</h3>
											<p class="mt-1 text-sm text-muted-foreground">
												Actions to run when specific events occur
											</p>
										</div>
										<Button onclick={addEventHook} variant="outline" class="gap-2">
											<Plus class="h-4 w-4" />
											Add Hook
										</Button>
									</div>

									{#if eventHooks.length > 0}
										<div class="space-y-4">
											{#each eventHooks as hook, i (i)}
												<div class="space-y-4 rounded-xl border bg-card p-6">
													<div class="flex items-center justify-between">
														<span class="font-medium">Hook {i + 1}</span>
														<Button
															variant="ghost"
															size="icon"
															onclick={() => removeEventHook(i)}
															class="h-8 w-8 text-destructive hover:text-destructive"
														>
															<Trash2 class="h-4 w-4" />
														</Button>
													</div>

													<div class="grid grid-cols-3 gap-4">
														<div class="space-y-2">
															<Label>Event</Label>
															<Select
																type="single"
																value={String(hook.event)}
																onValueChange={(v) => {
																	if (v) hook.event = Number(v);
																}}
															>
																<SelectTrigger class="h-11">
																	<span>{getEventTypeLabel(hook.event)}</span>
																</SelectTrigger>
																<SelectContent>
																	{#each SERVER_EVENT_TYPES as { type, label }}
																		<SelectItem value={String(type)}>{label}</SelectItem>
																	{/each}
																</SelectContent>
															</Select>
														</div>
														<div class="space-y-2">
															<Label>Action</Label>
															<Select
																type="single"
																value={String(hook.action)}
																onValueChange={(v) => {
																	if (v) hook.action = Number(v);
																}}
															>
																<SelectTrigger class="h-11">
																	<span>{getEventActionLabel(hook.action)}</span>
																</SelectTrigger>
																<SelectContent>
																	<SelectItem value={String(ModuleEventAction.START)}
																		>Start</SelectItem
																	>
																	<SelectItem value={String(ModuleEventAction.STOP)}
																		>Stop</SelectItem
																	>
																	<SelectItem value={String(ModuleEventAction.RESTART)}
																		>Restart</SelectItem
																	>
																	<SelectItem value={String(ModuleEventAction.EXEC)}
																		>Exec</SelectItem
																	>
																	<SelectItem value={String(ModuleEventAction.RCON)}
																		>RCON</SelectItem
																	>
																</SelectContent>
															</Select>
														</div>
														<div class="space-y-2">
															<Label>Delay (seconds)</Label>
															<Input
																type="number"
																bind:value={hook.delaySeconds}
																min={0}
																class="h-11"
															/>
														</div>
													</div>

													{#if hook.action === ModuleEventAction.EXEC || hook.action === ModuleEventAction.RCON}
														<div class="space-y-2">
															<Label>Command</Label>
															<Input
																bind:value={hook.command}
																placeholder="Command to execute"
																class="h-11 font-mono"
															/>
														</div>
													{/if}

													<div class="space-y-2">
														<Label>Condition (optional)</Label>
														<Input
															bind:value={hook.condition}
															placeholder="Conditional expression"
															class="h-11 font-mono"
														/>
													</div>
												</div>
											{/each}
										</div>
									{:else}
										<div
											class="rounded-lg border border-dashed p-6 text-center text-muted-foreground"
										>
											No event hooks configured
										</div>
									{/if}
								</div>

								<!-- Metadata -->
								<div class="space-y-4">
									<div class="flex items-center justify-between">
										<div>
											<h3 class="flex items-center gap-2 text-lg font-medium">
												<Info class="h-5 w-5" />
												Metadata
											</h3>
											<p class="mt-1 text-sm text-muted-foreground">
												Custom key-value pairs for module configuration
											</p>
										</div>
										<Button onclick={addMetadataEntry} variant="outline" class="gap-2">
											<Plus class="h-4 w-4" />
											Add Entry
										</Button>
									</div>

									{#if metadata.length > 0}
										<div class="space-y-3">
											{#each metadata as entry, i (i)}
												<div class="flex items-center gap-3 rounded-lg border bg-card p-4">
													<Input
														bind:value={entry.key}
														placeholder="key"
														class="h-11 w-48 font-mono"
													/>
													<span class="text-xl text-muted-foreground">:</span>
													<Input
														bind:value={entry.value}
														placeholder="value"
														class="h-11 flex-1 font-mono"
													/>
													<Button
														variant="ghost"
														size="icon"
														onclick={() => removeMetadataEntry(i)}
														class="h-10 w-10 shrink-0 text-destructive hover:text-destructive"
													>
														<Trash2 class="h-4 w-4" />
													</Button>
												</div>
											{/each}
										</div>
									{:else}
										<div
											class="rounded-lg border border-dashed p-6 text-center text-muted-foreground"
										>
											No metadata entries
										</div>
									{/if}
								</div>
							</div>
						{/if}
					</div>

					<!-- Footer -->
					<div class="flex items-center justify-end gap-3 border-t bg-muted/30 px-8 py-5">
						{#if mode === 'create'}
							<Button
								variant="outline"
								onclick={() => {
									step = 'select';
									selectedTemplate = null;
								}}
								class="h-11 px-6"
							>
								Back
							</Button>
						{:else}
							<Button variant="outline" onclick={() => (open = false)} class="h-11 px-6">
								Cancel
							</Button>
						{/if}
						<Button
							onclick={handleSubmit}
							disabled={submitting || !name.trim()}
							class="h-11 gap-2 px-8"
						>
							{#if submitting}
								<Loader2 class="h-4 w-4 animate-spin" />
								{mode === 'create' ? 'Creating...' : 'Saving...'}
							{:else if mode === 'create'}
								<Check class="h-4 w-4" />
								Create Module
							{:else}
								<Save class="h-4 w-4" />
								Save Changes
							{/if}
						</Button>
					</div>
				</div>
			</div>
		{/if}
	</DialogContent>
</Dialog>

<AlertDialog.Root
	open={warnings.length > 0}
	onOpenChange={(o) => {
		if (!o) handleWarningCancel();
	}}
>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title class="flex items-center gap-2">
				<AlertTriangle class="h-5 w-5 text-amber-500" />
				Review Warnings
			</AlertDialog.Title>
			<AlertDialog.Description>
				The following issues were detected. You can still proceed, but you may want to review them
				first.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<div class="space-y-2 py-2">
			{#each warnings as warning (warning)}
				<div
					class="flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-400"
				>
					<AlertTriangle class="mt-0.5 h-4 w-4 shrink-0" />
					<span>{warning}</span>
				</div>
			{/each}
		</div>
		<AlertDialog.Footer>
			<AlertDialog.Cancel onclick={handleWarningCancel}>Go Back</AlertDialog.Cancel>
			<AlertDialog.Action onclick={handleWarningProceed}>
				{mode === 'create' ? 'Create Anyway' : 'Save Anyway'}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
