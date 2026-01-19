<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import AliasHelper from '$lib/components/ui/AliasHelper.svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { ModulePort, ModuleEventHook } from '$lib/proto/discopanel/v1/module_pb';
	import { ModuleEventType, ModuleEventAction } from '$lib/proto/discopanel/v1/module_pb';
	import { Loader2, Plus, Trash2, Package, ChevronDown, ChevronUp } from '@lucide/svelte';

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

	let { open = $bindable(), onCreated }: Props = $props();

	let creating = $state(false);

	// Form state
	let name = $state('');
	let description = $state('');
	let dockerImage = $state('');
	let configSchema = $state('{}');
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

	// Port configuration (consolidated - no more separate defaultPort/defaultProtocol)
	let ports = $state<PortConfig[]>([]);
	let suggestedDependencies = $state('');
	let defaultHooks = $state<EventHook[]>([]);
	let metadata = $state<MetadataEntry[]>([]);

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
		ports = [...ports, { name: '', containerPort: 0, hostPort: 0, protocol: 'tcp', proxyEnabled: supportsProxy }];
	}

	function removePort(index: number) {
		ports = ports.filter((_, i) => i !== index);
	}

	// Event hooks helpers
	function addDefaultHook() {
		defaultHooks = [...defaultHooks, {
			event: ModuleEventType.SERVER_START,
			action: ModuleEventAction.START,
			command: '',
			delaySeconds: 0,
			condition: ''
		}];
	}

	function removeDefaultHook(index: number) {
		defaultHooks = defaultHooks.filter((_, i) => i !== index);
	}

	// Metadata helpers
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

	// Reset form when dialog closes
	$effect(() => {
		if (!open) {
			resetForm();
		}
	});

	function resetForm() {
		name = '';
		description = '';
		dockerImage = '';
		configSchema = '{}';
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
		advancedExpanded = false;
	}

	async function handleCreate() {
		if (!name.trim() || !dockerImage.trim()) return;

		creating = true;
		try {
			await rpcClient.module.createModuleTemplate({
				name: name.trim(),
				description: description.trim(),
				dockerImage: dockerImage.trim(),
				configSchema: configSchema.trim() || '{}',
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
					.filter(p => p.containerPort > 0)
					.map(p => ({
						name: p.name,
						containerPort: p.containerPort,
						hostPort: p.hostPort,
						protocol: p.protocol,
						proxyEnabled: p.proxyEnabled
					})),
				suggestedDependencies: suggestedDependencies.trim()
					? suggestedDependencies.split(',').map(s => s.trim()).filter(s => s)
					: [],
				defaultHooks: defaultHooks.map(h => ({
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
	<DialogContent class="max-w-2xl max-h-[90vh] overflow-y-auto">
		<DialogHeader>
			<DialogTitle>Create Custom Template</DialogTitle>
			<DialogDescription>Define a new module template for reusable container configurations</DialogDescription>
		</DialogHeader>

		<div class="flex items-center gap-4 py-2 px-1 text-xs text-muted-foreground border-b mb-2">
			<span class="flex-1">Use template aliases for dynamic values in env vars, volumes, and metadata.</span>
			<AliasHelper showLabel />
		</div>

		<div class="space-y-6 py-4">
			<!-- Basic Info -->
			<div class="space-y-4">
				<h4 class="text-sm font-medium">Basic Information</h4>
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-2">
						<Label for="name">Template Name *</Label>
						<Input id="name" bind:value={name} placeholder="My Custom Module" />
					</div>
					<div class="space-y-2">
						<Label for="category">Category</Label>
						<Input id="category" bind:value={category} placeholder="monitoring, maps, voice, etc." />
					</div>
				</div>
				<div class="space-y-2">
					<Label for="description">Description</Label>
					<Textarea id="description" bind:value={description} placeholder="What does this module do?" rows={2} />
				</div>
				<div class="space-y-2">
					<Label for="icon">Icon (Lucide icon name)</Label>
					<Input id="icon" bind:value={icon} placeholder="chart-bar, map, microphone, etc." />
					<p class="text-xs text-muted-foreground">See lucide.dev for icon names</p>
				</div>
			</div>

			<!-- Docker Configuration -->
			<div class="space-y-4">
				<h4 class="text-sm font-medium">Docker Configuration</h4>
				<div class="space-y-2">
					<Label for="dockerImage">Docker Image *</Label>
					<Input id="dockerImage" bind:value={dockerImage} placeholder="nginx:latest, redis:alpine, etc." class="font-mono" />
					<p class="text-xs text-muted-foreground">The Docker image to use for this module</p>
				</div>
			</div>

			<!-- Port Configuration -->
			<div class="space-y-4">
				<div class="flex items-center justify-between">
					<div>
						<h4 class="text-sm font-medium">Ports</h4>
						<p class="text-xs text-muted-foreground">Configure port mappings. Host port 0 = auto-allocate at instance creation.</p>
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

			<!-- Health Check -->
			<div class="space-y-4">
				<h4 class="text-sm font-medium">Health Check (optional)</h4>
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-2">
						<Label for="healthCheckPath">Health Check Path</Label>
						<Input id="healthCheckPath" bind:value={healthCheckPath} placeholder="/health" />
					</div>
					<div class="space-y-2">
						<Label for="healthCheckPort">Health Check Port</Label>
						<Input id="healthCheckPort" type="number" bind:value={healthCheckPort} min={0} max={65535} />
						<p class="text-xs text-muted-foreground">0 = use first port</p>
					</div>
				</div>
			</div>

			<!-- Behavior -->
			<div class="space-y-4">
				<h4 class="text-sm font-medium">Behavior</h4>
				<div class="space-y-3">
					<div class="flex items-center justify-between">
						<div>
							<Label for="requiresServer">Requires Server</Label>
							<p class="text-xs text-muted-foreground">Module must be attached to a server</p>
						</div>
						<Switch id="requiresServer" bind:checked={requiresServer} />
					</div>
					<div class="flex items-center justify-between">
						<div>
							<Label for="supportsProxy">Supports Proxy</Label>
							<p class="text-xs text-muted-foreground">Can be proxied through server hostname</p>
						</div>
						<Switch id="supportsProxy" bind:checked={supportsProxy} />
					</div>
				</div>
			</div>

			<!-- Default Environment Variables -->
			<div class="space-y-4">
				<div class="flex items-center justify-between">
					<div>
						<h4 class="text-sm font-medium">Default Environment Variables</h4>
						<p class="text-xs text-muted-foreground">Pre-configured environment variables. Use aliases for dynamic values.</p>
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
									class="flex-1 font-mono text-sm"
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
					<p class="text-xs text-muted-foreground italic">No default environment variables</p>
				{/if}
			</div>

			<!-- Default Volume Mounts -->
			<div class="space-y-4">
				<div class="flex items-center justify-between">
					<div>
						<h4 class="text-sm font-medium">Default Volume Mounts</h4>
						<p class="text-xs text-muted-foreground">Pre-configured volume mounts. Use aliases for dynamic paths.</p>
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
					<p class="text-xs text-muted-foreground italic">No default volume mounts</p>
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
						<h4 class="text-sm font-medium">Advanced Defaults</h4>
						<p class="text-xs text-muted-foreground">Suggested dependencies, event hooks, and metadata</p>
					</div>
					{#if advancedExpanded}
						<ChevronUp class="h-4 w-4 text-muted-foreground" />
					{:else}
						<ChevronDown class="h-4 w-4 text-muted-foreground" />
					{/if}
				</button>

				{#if advancedExpanded}
					<div class="p-4 space-y-6 border-t">
						<!-- Suggested Dependencies -->
						<div class="space-y-4">
							<div>
								<h4 class="text-sm font-medium">Suggested Dependencies</h4>
								<p class="text-xs text-muted-foreground">Template IDs this module commonly needs (comma-separated)</p>
							</div>
							<Input
								bind:value={suggestedDependencies}
								placeholder="redis, mysql, etc."
								class="font-mono text-sm"
							/>
						</div>

						<!-- Default Event Hooks -->
						<div class="space-y-4">
							<div class="flex items-center justify-between">
								<div>
									<h4 class="text-sm font-medium">Default Event Hooks</h4>
									<p class="text-xs text-muted-foreground">Pre-configured hooks for server lifecycle events</p>
								</div>
								<Button variant="outline" size="sm" onclick={addDefaultHook}>
									<Plus class="h-3 w-3 mr-1" />
									Add
								</Button>
							</div>
							{#if defaultHooks.length > 0}
								<div class="space-y-3">
									{#each defaultHooks as hook, index}
										<div class="p-3 border rounded-lg space-y-2">
											<div class="flex items-center justify-between">
												<span class="text-xs font-medium text-muted-foreground">Hook {index + 1}</span>
												<Button
													variant="ghost"
													size="icon"
													onclick={() => removeDefaultHook(index)}
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
								<p class="text-xs text-muted-foreground italic">No default event hooks</p>
							{/if}
						</div>

						<!-- Default Metadata -->
						<div class="space-y-4">
							<div class="flex items-center justify-between">
								<div>
									<h4 class="text-sm font-medium">Default Metadata</h4>
									<p class="text-xs text-muted-foreground">Custom key-value pairs for notes, instructions, links. Values support aliases.</p>
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
								<p class="text-xs text-muted-foreground italic">No default metadata</p>
							{/if}
						</div>
					</div>
				{/if}
			</div>

			<!-- Documentation -->
			<div class="space-y-2">
				<Label for="documentation">Documentation (optional)</Label>
				<Textarea id="documentation" bind:value={documentation} placeholder="Usage instructions, configuration notes, etc." rows={3} />
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => (open = false)}>Cancel</Button>
			<Button onclick={handleCreate} disabled={creating || !name.trim() || !dockerImage.trim()}>
				{#if creating}
					<Loader2 class="h-4 w-4 mr-2 animate-spin" />
				{:else}
					<Package class="h-4 w-4 mr-2" />
				{/if}
				Create Template
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
