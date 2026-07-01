<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Dialog, DialogContent } from '$lib/components/ui/dialog';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import AliasHelper from '$lib/components/ui/AliasHelper.svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { ModuleEventAction, type ModuleTemplate } from '$lib/proto/discopanel/v1/module_pb';
	import { TriggeredEventType } from '$lib/proto/discopanel/v1/event_pb';
	import {
		Loader2,
		Plus,
		Trash2,
		Package,
		X,
		FileText,
		Container,
		Network,
		Variable,
		HardDrive,
		Wrench,
		Heart,
		Info
	} from '@lucide/svelte';

	interface Props {
		open: boolean;
		mode?: 'create' | 'edit';
		template?: ModuleTemplate;
		onSuccess: () => void;
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

	interface PortConfig {
		name: string;
		containerPort: number;
		hostPort: number;
		protocol: string;
		proxyEnabled: boolean;
	}

	interface EventHook {
		event: TriggeredEventType;
		action: ModuleEventAction;
		command: string;
		delaySeconds: number;
		condition: string;
	}

	interface MetadataEntry {
		key: string;
		value: string;
	}

	type ConfigSection = 'basic' | 'docker' | 'ports' | 'environment' | 'volumes' | 'advanced';

	let { open = $bindable(), mode = 'create', template, onSuccess }: Props = $props();

	let submitting = $state(false);
	let activeSection = $state<ConfigSection>('basic');

	// Form state
	let name = $state('');
	let description = $state('');
	let dockerImage = $state('');
	let healthCheckPath = $state('');
	let healthCheckPort = $state(0);
	let requiresServer = $state(true);
	let supportsProxy = $state(true);
	let icon = $state('');
	let category = $state('');
	let documentation = $state('');
	let defaultUid = $state('');
	let defaultGid = $state('');
	let defaultInitCommand = $state('');
	let defaultInitCommandDelay = $state(0);
	let defaultRestartAfterInit = $state(false);

	// Environment variables and volumes as editable arrays
	let envVars = $state<EnvVar[]>([]);
	let volumes = $state<VolumeMount[]>([]);

	// Port configuration
	let ports = $state<PortConfig[]>([]);
	let suggestedDependencies = $state('');
	let defaultHooks = $state<EventHook[]>([]);
	let metadata = $state<MetadataEntry[]>([]);

	const navItems: { id: ConfigSection; label: string; icon: typeof FileText }[] = [
		{ id: 'basic', label: 'Basic Info', icon: FileText },
		{ id: 'docker', label: 'Docker', icon: Container },
		{ id: 'ports', label: 'Ports', icon: Network },
		{ id: 'environment', label: 'Environment', icon: Variable },
		{ id: 'volumes', label: 'Volumes', icon: HardDrive },
		{ id: 'advanced', label: 'Advanced', icon: Wrench }
	];

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
					read_only: v.readOnly,
					create_dir: v.createDir
				}))
		);
	}

	function addEnvVar() {
		envVars = [...envVars, { key: '', value: '' }];
	}

	function removeEnvVar(index: number) {
		envVars = envVars.filter((_, i) => i !== index);
	}

	function addVolume() {
		volumes = [...volumes, { hostPath: '', containerPath: '', readOnly: false, createDir: false }];
	}

	function removeVolume(index: number) {
		volumes = volumes.filter((_, i) => i !== index);
	}

	function addPort() {
		ports = [
			...ports,
			{ name: '', containerPort: 0, hostPort: 0, protocol: 'tcp', proxyEnabled: supportsProxy }
		];
	}

	function removePort(index: number) {
		ports = ports.filter((_, i) => i !== index);
	}

	function addDefaultHook() {
		defaultHooks = [
			...defaultHooks,
			{
				event: TriggeredEventType.SERVER_START,
				action: ModuleEventAction.START,
				command: '',
				delaySeconds: 0,
				condition: ''
			}
		];
	}

	function removeDefaultHook(index: number) {
		defaultHooks = defaultHooks.filter((_, i) => i !== index);
	}

	function addMetadataEntry() {
		metadata = [...metadata, { key: '', value: '' }];
	}

	function removeMetadataEntry(index: number) {
		metadata = metadata.filter((_, i) => i !== index);
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

	function getEventTypeLabel(event: TriggeredEventType): string {
		switch (event) {
			case TriggeredEventType.SERVER_START:
				return 'Server Start';
			case TriggeredEventType.SERVER_STOP:
				return 'Server Stop';
			case TriggeredEventType.SERVER_HEALTHY:
				return 'Server Healthy';
			case TriggeredEventType.PLAYER_JOIN:
				return 'Player Join';
			case TriggeredEventType.PLAYER_LEAVE:
				return 'Player Leave';
			default:
				return 'Unknown';
		}
	}

	function getEventActionLabel(action: ModuleEventAction): string {
		switch (action) {
			case ModuleEventAction.START:
				return 'Start Module';
			case ModuleEventAction.STOP:
				return 'Stop Module';
			case ModuleEventAction.RESTART:
				return 'Restart Module';
			case ModuleEventAction.EXEC:
				return 'Execute Command';
			case ModuleEventAction.RCON:
				return 'RCON Command';
			default:
				return 'Unknown';
		}
	}

	$effect(() => {
		if (open) {
			if (mode === 'edit' && template) {
				loadTemplateData(template);
			} else if (mode === 'create') {
				resetForm();
			}
		} else {
			resetForm();
		}
	});

	function loadTemplateData(t: ModuleTemplate) {
		name = t.name;
		description = t.description;
		dockerImage = t.dockerImage;
		healthCheckPath = t.healthCheckPath;
		healthCheckPort = t.healthCheckPort;
		requiresServer = t.requiresServer;
		supportsProxy = t.supportsProxy;
		icon = t.icon;
		category = t.category;
		documentation = t.documentation;
		defaultUid = t.defaultUid;
		defaultGid = t.defaultGid;
		defaultInitCommand = t.defaultInitCommand;
		defaultInitCommandDelay = t.defaultInitCommandDelay;
		defaultRestartAfterInit = t.defaultRestartAfterInit;

		try {
			const envObj = JSON.parse(t.defaultEnv || '{}');
			envVars = Object.entries(envObj).map(([key, value]) => ({ key, value: String(value) }));
		} catch {
			envVars = [];
		}

		try {
			volumes = JSON.parse(t.defaultVolumes || '[]');
		} catch {
			volumes = [];
		}

		ports = t.ports.map((p) => ({
			name: p.name,
			containerPort: p.containerPort,
			hostPort: p.hostPort,
			protocol: p.protocol,
			proxyEnabled: p.proxyEnabled
		}));

		suggestedDependencies = t.suggestedDependencies.join(', ');
		defaultHooks = t.defaultHooks.map((h) => ({
			event: h.event,
			action: h.action,
			command: h.command,
			delaySeconds: h.delaySeconds,
			condition: h.condition
		}));

		metadata = Object.entries(t.metadata || {}).map(([key, value]) => ({ key, value }));
		activeSection = 'basic';
	}

	function resetForm() {
		name = '';
		description = '';
		dockerImage = '';
		healthCheckPath = '';
		healthCheckPort = 0;
		requiresServer = true;
		supportsProxy = true;
		icon = '';
		category = '';
		documentation = '';
		defaultUid = '';
		defaultGid = '';
		defaultInitCommand = '';
		defaultInitCommandDelay = 0;
		defaultRestartAfterInit = false;
		envVars = [];
		volumes = [];
		ports = [];
		suggestedDependencies = '';
		defaultHooks = [];
		metadata = [];
		activeSection = 'basic';
	}

	async function handleSubmit() {
		if (!name.trim() || !dockerImage.trim()) return;

		submitting = true;
		try {
			const payload = {
				name: name.trim(),
				description: description.trim(),
				dockerImage: dockerImage.trim(),
				defaultEnv: envVarsToJson(),
				defaultVolumes: volumesToJson(),
				healthCheckPath: healthCheckPath.trim(),
				healthCheckPort,
				requiresServer,
				supportsProxy,
				icon: icon.trim(),
				category: category.trim(),
				documentation: documentation.trim(),
				ports: ports
					.filter((p) => p.containerPort > 0)
					.map((p) => ({
						name: p.name,
						containerPort: p.containerPort,
						hostPort: p.hostPort,
						protocol: p.protocol,
						proxyEnabled: p.proxyEnabled
					})),
				suggestedDependencies: suggestedDependencies.trim()
					? suggestedDependencies
							.split(',')
							.map((s) => s.trim())
							.filter((s) => s)
					: [],
				defaultHooks: defaultHooks.map((h) => ({
					event: h.event,
					action: h.action,
					command: h.command,
					delaySeconds: h.delaySeconds,
					condition: h.condition
				})),
				metadata: metadataToMap(),
				defaultUid,
				defaultGid,
				defaultInitCommand,
				defaultInitCommandDelay,
				defaultRestartAfterInit
			};

			if (mode === 'edit' && template) {
				await rpcClient.module.updateModuleTemplate({ id: template.id, ...payload });
				toast.success(`Template "${name}" updated`);
			} else {
				await rpcClient.module.createModuleTemplate(payload);
				toast.success(`Template "${name}" created`);
			}

			open = false;
			onSuccess();
		} catch (error) {
			toast.error(
				`Failed to ${mode} template: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			submitting = false;
		}
	}
</script>

<Dialog bind:open>
	<DialogContent
		class="flex h-[85vh]! w-[95vw]! max-w-6xl! flex-col gap-0! overflow-hidden p-0!"
		showCloseButton={false}
	>
		<div class="flex h-full">
			<!-- Sidebar -->
			<div class="flex w-64 flex-col border-r bg-muted/30">
				<!-- Sidebar Header -->
				<div class="border-b p-6">
					<div class="flex items-center gap-3">
						<div class="flex h-12 w-12 items-center justify-center rounded-xl bg-primary/10">
							<Package class="h-6 w-6 text-primary" />
						</div>
						<div class="min-w-0 flex-1">
							<h3 class="truncate font-semibold">
								{name || (mode === 'create' ? 'New Template' : 'Edit Template')}
							</h3>
							<p class="text-sm text-muted-foreground">Custom template</p>
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
				<div class="border-t p-4">
					<div class="rounded-lg bg-muted/50 p-4">
						<p class="mb-2 text-sm font-medium">Template Aliases</p>
						<p class="mb-3 text-xs text-muted-foreground">
							Use aliases for dynamic values in any configuration field.
						</p>
						<AliasHelper showLabel />
					</div>
				</div>
			</div>

			<!-- Main Content -->
			<div class="flex min-w-0 flex-1 flex-col">
				<!-- Content Header -->
				<div class="flex items-center justify-between border-b bg-muted/30 px-8 py-6">
					<div>
						<h2 class="text-2xl font-semibold tracking-tight">
							{#if activeSection === 'basic'}Basic Information
							{:else if activeSection === 'docker'}Docker Configuration
							{:else if activeSection === 'ports'}Port Configuration
							{:else if activeSection === 'environment'}Environment Variables
							{:else if activeSection === 'volumes'}Volume Mounts
							{:else if activeSection === 'advanced'}Advanced Settings
							{/if}
						</h2>
						<p class="mt-1 text-muted-foreground">
							{#if activeSection === 'basic'}Template name, description, and appearance
							{:else if activeSection === 'docker'}Container image and health check settings
							{:else if activeSection === 'ports'}Default port mappings for the container
							{:else if activeSection === 'environment'}Default environment variables
							{:else if activeSection === 'volumes'}Default volume mount configurations
							{:else if activeSection === 'advanced'}Behavior, dependencies, hooks, and metadata
							{/if}
						</p>
					</div>
					<Button variant="ghost" size="icon" onclick={() => (open = false)} class="h-10 w-10">
						<X class="h-5 w-5" />
					</Button>
				</div>

				<!-- Scrollable Content Area -->
				<div class="flex-1 overflow-y-auto p-8">
					{#if activeSection === 'basic'}
						<!-- Basic Info Section -->
						<div class="max-w-2xl space-y-8">
							<div class="space-y-3">
								<Label for="name" class="text-base font-medium">Template Name *</Label>
								<Input
									id="name"
									bind:value={name}
									placeholder="My Custom Module"
									class="h-12 text-base"
								/>
								<p class="text-sm text-muted-foreground">
									A descriptive name for this module template
								</p>
							</div>

							<div class="space-y-3">
								<Label for="description" class="text-base font-medium">Description</Label>
								<Textarea
									id="description"
									bind:value={description}
									placeholder="What does this module do? Describe its purpose and features."
									rows={4}
									class="text-base"
								/>
							</div>

							<div class="grid grid-cols-2 gap-6">
								<div class="space-y-3">
									<Label for="category">Category</Label>
									<Input
										id="category"
										bind:value={category}
										placeholder="monitoring, maps, voice..."
										class="h-12"
									/>
									<p class="text-sm text-muted-foreground">Group similar templates</p>
								</div>
								<div class="space-y-3">
									<Label for="icon">Icon</Label>
									<Input
										id="icon"
										bind:value={icon}
										placeholder="chart-bar, map, microphone..."
										class="h-12"
									/>
									<p class="text-sm text-muted-foreground">
										Lucide icon name from <a
											href="https://lucide.dev"
											target="_blank"
											rel="noopener noreferrer"
											class="underline">lucide.dev</a
										>
									</p>
								</div>
							</div>
						</div>
					{:else if activeSection === 'docker'}
						<!-- Docker Section -->
						<div class="max-w-2xl space-y-8">
							<div class="space-y-3">
								<Label for="dockerImage" class="text-base font-medium">Docker Image *</Label>
								<Input
									id="dockerImage"
									bind:value={dockerImage}
									placeholder="nginx:latest, redis:alpine, myregistry/myimage:v1"
									class="h-12 font-mono text-base"
								/>
								<p class="text-sm text-muted-foreground">
									The Docker image to pull and run for this module
								</p>
							</div>

							<div class="space-y-4">
								<div>
									<h3 class="flex items-center gap-2 text-base font-medium">
										<Heart class="h-5 w-5" />
										Health Check
									</h3>
									<p class="mt-1 text-sm text-muted-foreground">
										Configure how to verify the container is healthy
									</p>
								</div>

								<div class="grid grid-cols-2 gap-6 rounded-lg border bg-card p-6">
									<div class="space-y-3">
										<Label for="healthCheckPath">Health Check Path</Label>
										<Input
											id="healthCheckPath"
											bind:value={healthCheckPath}
											placeholder="/health or /api/status"
											class="h-11"
										/>
										<p class="text-sm text-muted-foreground">HTTP endpoint to check</p>
									</div>
									<div class="space-y-3">
										<Label for="healthCheckPort">Health Check Port</Label>
										<Input
											id="healthCheckPort"
											type="number"
											bind:value={healthCheckPort}
											min={0}
											max={65535}
											class="h-11"
										/>
										<p class="text-sm text-muted-foreground">0 = use first configured port</p>
									</div>
								</div>
							</div>

							<div class="space-y-4">
								<div>
									<h3 class="text-base font-medium">Container User</h3>
									<p class="mt-1 text-sm text-muted-foreground">
										Default UID/GID for the container process
									</p>
								</div>
								<div class="grid grid-cols-2 gap-6 rounded-lg border bg-card p-6">
									<div class="space-y-3">
										<Label for="defaultUid">Default UID</Label>
										<Input
											id="defaultUid"
											bind:value={defaultUid}
											placeholder={'{{host.uid}}'}
											class="h-11 font-mono"
										/>
										<p class="text-sm text-muted-foreground">User ID or alias</p>
									</div>
									<div class="space-y-3">
										<Label for="defaultGid">Default GID</Label>
										<Input
											id="defaultGid"
											bind:value={defaultGid}
											placeholder={'{{host.gid}}'}
											class="h-11 font-mono"
										/>
										<p class="text-sm text-muted-foreground">Group ID or alias</p>
									</div>
								</div>
							</div>

							<div class="space-y-4">
								<h3 class="text-base font-medium">Behavior Flags</h3>
								<div class="space-y-4">
									<label
										class="flex cursor-pointer items-start gap-4 rounded-lg border p-4 transition-colors hover:bg-muted/50"
									>
										<Switch bind:checked={requiresServer} class="mt-0.5" />
										<div class="space-y-1">
											<span class="font-medium">Requires Server</span>
											<p class="text-sm text-muted-foreground">
												This module must be attached to a game server
											</p>
										</div>
									</label>

									<label
										class="flex cursor-pointer items-start gap-4 rounded-lg border p-4 transition-colors hover:bg-muted/50"
									>
										<Switch bind:checked={supportsProxy} class="mt-0.5" />
										<div class="space-y-1">
											<span class="font-medium">Supports Proxy</span>
											<p class="text-sm text-muted-foreground">
												Can be accessed through the server's proxy hostname
											</p>
										</div>
									</label>
								</div>
							</div>
						</div>
					{:else if activeSection === 'ports'}
						<!-- Ports Section -->
						<div class="space-y-6">
							<div class="flex items-center justify-between">
								<div>
									<p class="text-muted-foreground">
										{ports.length} port{ports.length !== 1 ? 's' : ''} configured
									</p>
									<p class="mt-1 text-sm text-muted-foreground">
										Host port 0 = auto-allocate when creating module instances
									</p>
								</div>
								<Button onclick={addPort} class="gap-2">
									<Plus class="h-4 w-4" />
									Add Port
								</Button>
							</div>

							{#if ports.length > 0}
								<div class="space-y-4">
									{#each ports as port, i (i)}
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

											<div class="space-y-2">
												<Label>Port Name</Label>
												<Input
													bind:value={port.name}
													placeholder="Web UI, API, Metrics..."
													class="h-11"
												/>
											</div>

											<div class="grid grid-cols-3 gap-4">
												<div class="space-y-2">
													<Label>Host Port</Label>
													<Input
														type="number"
														bind:value={port.hostPort}
														min={0}
														max={65535}
														placeholder="0 = auto"
														class="h-11"
													/>
												</div>
												<div class="space-y-2">
													<Label>Container Port</Label>
													<Input
														type="number"
														bind:value={port.containerPort}
														min={1}
														max={65535}
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
												<span class="text-sm">Route through proxy</span>
											</label>
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
									<p class="mt-1 text-sm text-muted-foreground">
										Use template aliases like {'{{server.data_path}}'} for dynamic values
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
												placeholder="value or {'{{alias}}'}"
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
										Add default variables for container configuration
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
										{volumes.length} volume{volumes.length !== 1 ? 's' : ''} configured
									</p>
									<p class="mt-1 text-sm text-muted-foreground">
										Use template aliases like {'{{module.data_path}}'} for dynamic paths
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
														placeholder="/host/path or {'{{alias}}'}"
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
															if (vol.readOnly) vol.createDir = false;
														}}
													/>
													<span class="text-sm">Read-only mount</span>
												</label>
												<label class="flex items-center gap-3">
													<Checkbox
														checked={vol.createDir}
														onCheckedChange={(checked) => {
															vol.createDir = !!checked;
															if (vol.createDir) vol.readOnly = false;
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
									<h3 class="mb-1 font-medium">No volumes configured</h3>
									<p class="mb-4 text-sm text-muted-foreground">
										Mount host directories for persistent data
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
							<!-- Suggested Dependencies -->
							<div class="space-y-4">
								<div>
									<h3 class="text-lg font-medium">Suggested Dependencies</h3>
									<p class="mt-1 text-sm text-muted-foreground">
										Template IDs this module commonly needs (comma-separated)
									</p>
								</div>
								<Input
									bind:value={suggestedDependencies}
									placeholder="redis, mysql, prometheus..."
									class="h-11 max-w-xl font-mono"
								/>
							</div>

							<!-- Event Hooks -->
							<div class="space-y-4">
								<div class="flex items-center justify-between">
									<div>
										<h3 class="text-lg font-medium">Default Event Hooks</h3>
										<p class="mt-1 text-sm text-muted-foreground">
											Pre-configured hooks for server lifecycle events
										</p>
									</div>
									<Button onclick={addDefaultHook} variant="outline" class="gap-2">
										<Plus class="h-4 w-4" />
										Add Hook
									</Button>
								</div>

								{#if defaultHooks.length > 0}
									<div class="space-y-4">
										{#each defaultHooks as hook, i (i)}
											<div class="space-y-4 rounded-xl border bg-card p-6">
												<div class="flex items-center justify-between">
													<span class="font-medium">Hook {i + 1}</span>
													<Button
														variant="ghost"
														size="icon"
														onclick={() => removeDefaultHook(i)}
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
															value={getEventTypeLabel(hook.event)}
															onValueChange={(v) => {
																if (v === 'Server Start')
																	hook.event = TriggeredEventType.SERVER_START;
																else if (v === 'Server Stop')
																	hook.event = TriggeredEventType.SERVER_STOP;
																else if (v === 'Server Healthy')
																	hook.event = TriggeredEventType.SERVER_HEALTHY;
																else if (v === 'Player Join')
																	hook.event = TriggeredEventType.PLAYER_JOIN;
																else if (v === 'Player Leave')
																	hook.event = TriggeredEventType.PLAYER_LEAVE;
															}}
														>
															<SelectTrigger class="h-11">
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
													<div class="space-y-2">
														<Label>Action</Label>
														<Select
															type="single"
															value={getEventActionLabel(hook.action)}
															onValueChange={(v) => {
																if (v === 'Start Module') hook.action = ModuleEventAction.START;
																else if (v === 'Stop Module') hook.action = ModuleEventAction.STOP;
																else if (v === 'Restart Module')
																	hook.action = ModuleEventAction.RESTART;
																else if (v === 'Execute Command')
																	hook.action = ModuleEventAction.EXEC;
																else if (v === 'RCON Command') hook.action = ModuleEventAction.RCON;
															}}
														>
															<SelectTrigger class="h-11">
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
															placeholder={hook.action === ModuleEventAction.RCON
																? 'say Hello'
																: '/bin/sh -c "..."'}
															class="h-11 font-mono"
														/>
													</div>
												{/if}

												<div class="space-y-2">
													<Label>Condition (optional)</Label>
													<Input
														bind:value={hook.condition}
														placeholder={'{{server.players_online}} == 0'}
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
										No default event hooks configured
									</div>
								{/if}
							</div>

							<!-- Init Command -->
							<div class="space-y-4">
								<div>
									<h3 class="text-lg font-medium">Default Init Command</h3>
									<p class="mt-1 text-sm text-muted-foreground">
										Command to exec inside the container after it starts
									</p>
								</div>

								<div class="space-y-4 rounded-lg border bg-card p-6">
									<div class="space-y-2">
										<Label>Command</Label>
										<Input
											bind:value={defaultInitCommand}
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
											<Input
												type="number"
												bind:value={defaultInitCommandDelay}
												min={0}
												class="h-11"
											/>
											<p class="text-xs text-muted-foreground">
												Seconds to wait after start before running
											</p>
										</div>
										<div class="flex items-center pt-6">
											<label class="flex items-center gap-3">
												<Checkbox bind:checked={defaultRestartAfterInit} />
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

							<!-- Metadata -->
							<div class="space-y-4">
								<div class="flex items-center justify-between">
									<div>
										<h3 class="flex items-center gap-2 text-lg font-medium">
											<Info class="h-5 w-5" />
											Default Metadata
										</h3>
										<p class="mt-1 text-sm text-muted-foreground">
											Custom key-value pairs for notes, instructions, or links
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

							<!-- Documentation -->
							<div class="space-y-4">
								<div>
									<h3 class="text-lg font-medium">Documentation</h3>
									<p class="mt-1 text-sm text-muted-foreground">
										Usage instructions, configuration notes, or helpful information
									</p>
								</div>
								<Textarea
									bind:value={documentation}
									placeholder="# Getting Started&#10;&#10;Describe how to configure and use this module..."
									rows={8}
									class="font-mono"
								/>
							</div>
						</div>
					{/if}
				</div>

				<!-- Footer -->
				<div class="flex items-center justify-between border-t bg-muted/20 p-4">
					<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
					<Button
						onclick={handleSubmit}
						disabled={!name.trim() || !dockerImage.trim() || submitting}
						class="min-w-[120px]"
					>
						{#if submitting}
							<Loader2 class="mr-2 h-4 w-4 animate-spin" />
							{mode === 'create' ? 'Creating...' : 'Saving...'}
						{:else}
							{mode === 'create' ? 'Create Template' : 'Save Changes'}
						{/if}
					</Button>
				</div>
			</div>
		</div>
	</DialogContent>
</Dialog>
