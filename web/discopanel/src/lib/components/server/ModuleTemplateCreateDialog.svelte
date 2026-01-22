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
	import { ModuleEventType, ModuleEventAction } from '$lib/proto/discopanel/v1/module_pb';
	import {
		Loader2, Plus, Trash2, Package, X,
		FileText, Container, Network, Variable, HardDrive, Wrench, Heart, Info
	} from '@lucide/svelte';

	interface Props {
		open: boolean;
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

	type ConfigSection = 'basic' | 'docker' | 'ports' | 'environment' | 'volumes' | 'advanced';

	let { open = $bindable(), onCreated }: Props = $props();

	let creating = $state(false);
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
					read_only: v.readOnly
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
		volumes = [...volumes, { hostPath: '', containerPath: '', readOnly: false }];
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
				event: ModuleEventType.SERVER_START,
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

	function getEventTypeLabel(event: ModuleEventType): string {
		switch (event) {
			case ModuleEventType.SERVER_START:
				return 'Server Start';
			case ModuleEventType.SERVER_STOP:
				return 'Server Stop';
			case ModuleEventType.SERVER_HEALTHY:
				return 'Server Healthy';
			case ModuleEventType.PLAYER_JOIN:
				return 'Player Join';
			case ModuleEventType.PLAYER_LEAVE:
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
		if (!open) {
			resetForm();
		}
	});

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
		envVars = [];
		volumes = [];
		ports = [];
		suggestedDependencies = '';
		defaultHooks = [];
		metadata = [];
		activeSection = 'basic';
	}

	async function handleCreate() {
		if (!name.trim() || !dockerImage.trim()) return;

		creating = true;
		try {
			await rpcClient.module.createModuleTemplate({
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
				metadata: metadataToMap()
			});
			toast.success(`Template "${name}" created`);
			open = false;
			onCreated();
		} catch (error) {
			toast.error(`Failed to create template: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			creating = false;
		}
	}
</script>

<Dialog bind:open>
	<DialogContent class="!max-w-6xl !w-[95vw] !h-[85vh] !p-0 !gap-0 overflow-hidden flex flex-col" showCloseButton={false}>
		<div class="flex h-full">
			<!-- Sidebar -->
			<div class="w-64 border-r bg-muted/30 flex flex-col">
				<!-- Sidebar Header -->
				<div class="p-6 border-b">
					<div class="flex items-center gap-3">
						<div class="h-12 w-12 rounded-xl bg-primary/10 flex items-center justify-center">
							<Package class="h-6 w-6 text-primary" />
						</div>
						<div class="flex-1 min-w-0">
							<h3 class="font-semibold truncate">
								{name || 'New Template'}
							</h3>
							<p class="text-sm text-muted-foreground">Custom template</p>
						</div>
					</div>
				</div>

				<!-- Navigation -->
				<nav class="flex-1 p-4 space-y-1">
					{#each navItems as item}
						{@const Icon = item.icon}
						<button
							onclick={() => (activeSection = item.id)}
							class="w-full flex items-center gap-3 px-4 py-3 rounded-lg text-left transition-colors {activeSection ===
							item.id
								? 'bg-primary text-primary-foreground'
								: 'hover:bg-muted text-muted-foreground hover:text-foreground'}"
						>
							<Icon class="h-5 w-5" />
							<span class="font-medium">{item.label}</span>
						</button>
					{/each}
				</nav>

				<!-- Sidebar Footer -->
				<div class="p-4 border-t">
					<div class="p-4 rounded-lg bg-muted/50">
						<p class="text-sm font-medium mb-2">Template Aliases</p>
						<p class="text-xs text-muted-foreground mb-3">
							Use aliases for dynamic values in any configuration field.
						</p>
						<AliasHelper showLabel />
					</div>
				</div>
			</div>

			<!-- Main Content -->
			<div class="flex-1 flex flex-col min-w-0">
				<!-- Content Header -->
				<div class="flex items-center justify-between px-8 py-6 border-b bg-muted/30">
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
						<p class="text-muted-foreground mt-1">
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
										Lucide icon name from <a href="https://lucide.dev" target="_blank" class="underline">lucide.dev</a>
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
									class="h-12 text-base font-mono"
								/>
								<p class="text-sm text-muted-foreground">
									The Docker image to pull and run for this module
								</p>
							</div>

							<div class="space-y-4">
								<div>
									<h3 class="text-base font-medium flex items-center gap-2">
										<Heart class="h-5 w-5" />
										Health Check
									</h3>
									<p class="text-sm text-muted-foreground mt-1">
										Configure how to verify the container is healthy
									</p>
								</div>

								<div class="grid grid-cols-2 gap-6 p-6 border rounded-lg bg-card">
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
								<h3 class="text-base font-medium">Behavior Flags</h3>
								<div class="space-y-4">
									<label class="flex items-start gap-4 p-4 border rounded-lg cursor-pointer hover:bg-muted/50 transition-colors">
										<Switch bind:checked={requiresServer} class="mt-0.5" />
										<div class="space-y-1">
											<span class="font-medium">Requires Server</span>
											<p class="text-sm text-muted-foreground">
												This module must be attached to a game server
											</p>
										</div>
									</label>

									<label class="flex items-start gap-4 p-4 border rounded-lg cursor-pointer hover:bg-muted/50 transition-colors">
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
									<p class="text-sm text-muted-foreground mt-1">
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
									{#each ports as port, i}
										<div class="p-6 border rounded-xl bg-card space-y-4">
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
								<div class="flex flex-col items-center justify-center py-16 text-center border rounded-xl border-dashed">
									<Network class="h-12 w-12 text-muted-foreground/50 mb-4" />
									<h3 class="font-medium mb-1">No ports configured</h3>
									<p class="text-sm text-muted-foreground mb-4">
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
									<p class="text-sm text-muted-foreground mt-1">
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
									{#each envVars as env, i}
										<div class="flex items-center gap-3 p-4 border rounded-lg bg-card">
											<Input
												bind:value={env.key}
												placeholder="VARIABLE_NAME"
												class="w-56 font-mono h-11"
											/>
											<span class="text-muted-foreground text-xl">=</span>
											<Input
												bind:value={env.value}
												placeholder="value or {'{{alias}}'}"
												class="flex-1 font-mono h-11"
											/>
											<Button
												variant="ghost"
												size="icon"
												onclick={() => removeEnvVar(i)}
												class="h-10 w-10 text-destructive hover:text-destructive shrink-0"
											>
												<Trash2 class="h-4 w-4" />
											</Button>
										</div>
									{/each}
								</div>
							{:else}
								<div class="flex flex-col items-center justify-center py-16 text-center border rounded-xl border-dashed">
									<Variable class="h-12 w-12 text-muted-foreground/50 mb-4" />
									<h3 class="font-medium mb-1">No environment variables</h3>
									<p class="text-sm text-muted-foreground mb-4">
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
									<p class="text-sm text-muted-foreground mt-1">
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
									{#each volumes as vol, i}
										<div class="p-6 border rounded-xl bg-card space-y-4">
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
														class="font-mono h-11"
													/>
												</div>
												<div class="space-y-2">
													<Label>Container Path</Label>
													<Input
														bind:value={vol.containerPath}
														placeholder="/container/path"
														class="font-mono h-11"
													/>
												</div>
											</div>

											<label class="flex items-center gap-3 pt-2">
												<Checkbox bind:checked={vol.readOnly} />
												<span class="text-sm">Read-only mount</span>
											</label>
										</div>
									{/each}
								</div>
							{:else}
								<div class="flex flex-col items-center justify-center py-16 text-center border rounded-xl border-dashed">
									<HardDrive class="h-12 w-12 text-muted-foreground/50 mb-4" />
									<h3 class="font-medium mb-1">No volumes configured</h3>
									<p class="text-sm text-muted-foreground mb-4">
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
									<p class="text-sm text-muted-foreground mt-1">
										Template IDs this module commonly needs (comma-separated)
									</p>
								</div>
								<Input
									bind:value={suggestedDependencies}
									placeholder="redis, mysql, prometheus..."
									class="h-11 font-mono max-w-xl"
								/>
							</div>

							<!-- Event Hooks -->
							<div class="space-y-4">
								<div class="flex items-center justify-between">
									<div>
										<h3 class="text-lg font-medium">Default Event Hooks</h3>
										<p class="text-sm text-muted-foreground mt-1">
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
										{#each defaultHooks as hook, i}
											<div class="p-6 border rounded-xl bg-card space-y-4">
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
																if (v === 'Server Start') hook.event = ModuleEventType.SERVER_START;
																else if (v === 'Server Stop') hook.event = ModuleEventType.SERVER_STOP;
																else if (v === 'Server Healthy') hook.event = ModuleEventType.SERVER_HEALTHY;
																else if (v === 'Player Join') hook.event = ModuleEventType.PLAYER_JOIN;
																else if (v === 'Player Leave') hook.event = ModuleEventType.PLAYER_LEAVE;
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
																else if (v === 'Restart Module') hook.action = ModuleEventAction.RESTART;
																else if (v === 'Execute Command') hook.action = ModuleEventAction.EXEC;
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
															placeholder={hook.action === ModuleEventAction.RCON ? 'say Hello' : '/bin/sh -c "..."'}
															class="font-mono h-11"
														/>
													</div>
												{/if}

												<div class="space-y-2">
													<Label>Condition (optional)</Label>
													<Input
														bind:value={hook.condition}
														placeholder={'{{server.players_online}} == 0'}
														class="font-mono h-11"
													/>
												</div>
											</div>
										{/each}
									</div>
								{:else}
									<div class="p-6 border rounded-lg border-dashed text-center text-muted-foreground">
										No default event hooks configured
									</div>
								{/if}
							</div>

							<!-- Metadata -->
							<div class="space-y-4">
								<div class="flex items-center justify-between">
									<div>
										<h3 class="text-lg font-medium flex items-center gap-2">
											<Info class="h-5 w-5" />
											Default Metadata
										</h3>
										<p class="text-sm text-muted-foreground mt-1">
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
										{#each metadata as entry, i}
											<div class="flex items-center gap-3 p-4 border rounded-lg bg-card">
												<Input
													bind:value={entry.key}
													placeholder="key"
													class="w-48 font-mono h-11"
												/>
												<span class="text-muted-foreground text-xl">:</span>
												<Input
													bind:value={entry.value}
													placeholder="value"
													class="flex-1 font-mono h-11"
												/>
												<Button
													variant="ghost"
													size="icon"
													onclick={() => removeMetadataEntry(i)}
													class="h-10 w-10 text-destructive hover:text-destructive shrink-0"
												>
													<Trash2 class="h-4 w-4" />
												</Button>
											</div>
										{/each}
									</div>
								{:else}
									<div class="p-6 border rounded-lg border-dashed text-center text-muted-foreground">
										No metadata entries
									</div>
								{/if}
							</div>

							<!-- Documentation -->
							<div class="space-y-4">
								<div>
									<h3 class="text-lg font-medium">Documentation</h3>
									<p class="text-sm text-muted-foreground mt-1">
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
				<div class="flex items-center justify-end gap-3 px-8 py-5 border-t bg-muted/30">
					<Button variant="outline" onclick={() => (open = false)} class="h-11 px-6">
						Cancel
					</Button>
					<Button
						onclick={handleCreate}
						disabled={creating || !name.trim() || !dockerImage.trim()}
						class="h-11 px-8 gap-2"
					>
						{#if creating}
							<Loader2 class="h-4 w-4 animate-spin" />
							Creating...
						{:else}
							<Package class="h-4 w-4" />
							Create Template
						{/if}
					</Button>
				</div>
			</div>
		</div>
	</DialogContent>
</Dialog>
