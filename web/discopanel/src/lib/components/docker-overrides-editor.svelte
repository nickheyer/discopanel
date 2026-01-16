<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Switch } from '$lib/components/ui/switch';
	import { Plus, AlertCircle, Code, ChevronDown, ChevronRight, X } from '@lucide/svelte';
	import type { DockerOverrides, VolumeMount } from '$lib/proto/discopanel/v1/common_pb';
	import { DockerOverridesSchema, VolumeMountSchema } from '$lib/proto/discopanel/v1/common_pb';
	import { create } from '@bufbuild/protobuf';
	import { Badge } from '$lib/components/ui/badge';

	interface Props {
		overrides?: DockerOverrides;
		disabled?: boolean;
		onchange?: (overrides: DockerOverrides | undefined) => void;
	}

	let { overrides = $bindable(), disabled = false, onchange }: Props = $props();

	let showAdvanced = $state(false);
	let jsonMode = $state(false);
	let jsonText = $state('');
	let jsonError = $state('');
	let envVarCounter = $state(0); // Counter for unique env var keys

	// Count active overrides for badge
	let activeCount = $derived(() => {
		if (!overrides) return 0;
		let count = 0;
		if (overrides.environment && Object.keys(overrides.environment).length > 0) count++;
		if (overrides.volumes && overrides.volumes.length > 0) count++;
		if (overrides.cpuLimit) count++;
		if (overrides.memoryLimit) count++;
		if (overrides.networkMode) count++;
		if (overrides.privileged) count++;
		if (overrides.user) count++;
		if (overrides.capAdd && overrides.capAdd.length > 0) count++;
		if (overrides.capDrop && overrides.capDrop.length > 0) count++;
		if (overrides.devices && overrides.devices.length > 0) count++;
		return count;
	});

	// Initialize JSON text when switching modes
	$effect(() => {
		if (jsonMode) {
			jsonText = JSON.stringify(overrides || {}, null, 2);
		}
	});

	function toggleJsonMode() {
		if (jsonMode) {
			// Parse JSON and update overrides
			try {
				const parsed = jsonText.trim() ? JSON.parse(jsonText) : {};
				overrides = Object.keys(parsed).length > 0 ? parsed : undefined;
				jsonError = '';
				jsonMode = false;
				onchange?.(overrides);
			} catch (e) {
				jsonError = `Invalid JSON: ${e instanceof Error ? e.message : 'Unknown error'}`;
			}
		} else {
			jsonMode = true;
		}
	}

	function updateJsonText(value: string) {
		jsonText = value;
		try {
			if (value.trim()) {
				JSON.parse(value);
			}
			jsonError = '';
		} catch (e) {
			jsonError = `Invalid JSON: ${e instanceof Error ? e.message : 'Unknown error'}`;
		}
	}

	function updateOverride<K extends keyof Omit<DockerOverrides, '$typeName' | '$unknown'>>(
		key: K,
		value: DockerOverrides[K] | undefined
	) {
		if (!overrides) {
			overrides = create(DockerOverridesSchema, {});
		}

		// Create a new instance with updated values
		const updates: any = {};

		// Copy existing values
		if (overrides.environment && Object.keys(overrides.environment).length > 0) updates.environment = { ...overrides.environment };
		if (overrides.volumes && overrides.volumes.length > 0) updates.volumes = [...overrides.volumes];
		if (overrides.capAdd && overrides.capAdd.length > 0) updates.capAdd = [...overrides.capAdd];
		if (overrides.capDrop && overrides.capDrop.length > 0) updates.capDrop = [...overrides.capDrop];
		if (overrides.devices && overrides.devices.length > 0) updates.devices = [...overrides.devices];
		if (overrides.networkMode) updates.networkMode = overrides.networkMode;
		if (overrides.privileged !== undefined) updates.privileged = overrides.privileged;
		if (overrides.user) updates.user = overrides.user;
		if (overrides.memoryLimit) updates.memoryLimit = overrides.memoryLimit;
		if (overrides.cpuLimit) updates.cpuLimit = overrides.cpuLimit;
		if (overrides.restartPolicy) updates.restartPolicy = overrides.restartPolicy;
		if (overrides.entrypoint && overrides.entrypoint.length > 0) updates.entrypoint = [...overrides.entrypoint];

		// Update the specific field
		if (value === undefined || value === null ||
		    (typeof value === 'string' && !value) ||
		    (Array.isArray(value) && value.length === 0) ||
		    (typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === 0) ||
		    (typeof value === 'number' && value === 0) ||
		    (typeof value === 'bigint' && value === 0n)) {
			delete updates[key];
		} else {
			updates[key] = value;
		}

		// Check if any values remain
		const hasValues = Object.values(updates).some(v =>
			v !== undefined && v !== null && v !== '' && v !== 0 && v !== 0n &&
			(!Array.isArray(v) || v.length > 0) &&
			(typeof v !== 'object' || Array.isArray(v) || Object.keys(v).length > 0)
		);

		if (hasValues) {
			overrides = create(DockerOverridesSchema, updates);
		} else {
			overrides = undefined;
		}

		onchange?.(overrides);
	}

	// Environment Variables (stored as map object)
	function addEnvVar() {
		const envMap = { ...(overrides?.environment || {}) };
		// Generate unique key name
		let newKey = `VAR_${envVarCounter}`;
		while (newKey in envMap) {
			envVarCounter++;
			newKey = `VAR_${envVarCounter}`;
		}
		envVarCounter++;
		envMap[newKey] = '';
		updateOverride('environment', envMap);
	}

	function updateEnvVar(oldKey: string, newKey: string, value: string) {
		const envMap = { ...(overrides?.environment || {}) };

		// If key changed, remove old key
		if (oldKey !== newKey) {
			delete envMap[oldKey];
		}

		// Add new key/value if key is present
		if (newKey) {
			envMap[newKey] = value;
		}

		const hasKeys = Object.keys(envMap).length > 0;
		updateOverride('environment', hasKeys ? envMap : undefined);
	}

	function removeEnvVar(key: string) {
		const envMap = { ...(overrides?.environment || {}) };
		delete envMap[key];
		const hasKeys = Object.keys(envMap).length > 0;
		updateOverride('environment', hasKeys ? envMap : undefined);
	}

	// Volumes (stored as VolumeMount objects)
	function addVolume() {
		const volumes = [...(overrides?.volumes || [])];
		const newVolume = create(VolumeMountSchema, {
			source: '',
			target: '',
			readOnly: false,
			type: 'bind'
		});
		volumes.push(newVolume);
		updateOverride('volumes', volumes);
	}

	function updateVolume(index: number, volume: VolumeMount | null) {
		const volumes = [...(overrides?.volumes || [])];
		if (volume) {
			volumes[index] = volume;
		} else {
			volumes.splice(index, 1);
		}
		updateOverride('volumes', volumes.length > 0 ? volumes : undefined);
	}

	function updateVolumeField(index: number, field: keyof VolumeMount, value: any) {
		const volumes = [...(overrides?.volumes || [])];
		if (volumes[index]) {
			volumes[index] = create(VolumeMountSchema, {
				...volumes[index],
				[field]: value
			});
			updateOverride('volumes', volumes);
		}
	}
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<button
			type="button"
			onclick={() => showAdvanced = !showAdvanced}
			disabled={disabled}
			class="flex items-center gap-2 text-sm font-medium hover:text-primary transition-colors"
		>
			{#if showAdvanced}
				<ChevronDown class="h-4 w-4" />
			{:else}
				<ChevronRight class="h-4 w-4" />
			{/if}
			<span>Docker Container Overrides</span>
			{#if activeCount() > 0}
				<Badge variant="secondary" class="ml-1">{activeCount()}</Badge>
			{/if}
			<span class="text-xs text-muted-foreground ml-1">(Advanced)</span>
		</button>

		{#if showAdvanced}
			<Button
				type="button"
				variant="ghost"
				size="sm"
				onclick={toggleJsonMode}
				disabled={disabled}
				class="gap-2 h-8"
			>
				<Code class="h-3 w-3" />
				{jsonMode ? 'Visual Editor' : 'JSON Editor'}
			</Button>
		{/if}
	</div>

	{#if showAdvanced}
		<Card class="overflow-hidden">
			{#if jsonMode}
				<CardContent class="pt-6">
					<div class="space-y-3">
						<Textarea
							bind:value={jsonText}
							oninput={(e) => updateJsonText(e.currentTarget.value)}
							disabled={disabled}
							placeholder={"{}"}
							class="font-mono text-xs min-h-[200px] {jsonError ? 'border-destructive' : ''}"
						/>
						{#if jsonError}
							<div class="flex items-center gap-2 text-destructive text-xs">
								<AlertCircle class="h-3 w-3" />
								{jsonError}
							</div>
						{/if}
						<div class="flex justify-end">
							<Button
								type="button"
								variant="default"
								size="sm"
								onclick={toggleJsonMode}
								disabled={disabled || !!jsonError}
							>
								Apply JSON
							</Button>
						</div>
					</div>
				</CardContent>
			{:else}
				<CardContent class="pt-6 space-y-6">
					<!-- Environment Variables -->
					<div class="space-y-3">
						<div class="flex items-center justify-between">
							<Label class="text-sm font-medium">Environment Variables</Label>
							<Button
								type="button"
								variant="ghost"
								size="sm"
								onclick={addEnvVar}
								disabled={disabled}
								class="h-7 text-xs gap-1"
							>
								<Plus class="h-3 w-3" />
								Add Variable
							</Button>
						</div>
						{#if overrides?.environment && Object.keys(overrides.environment).length > 0}
							<div class="rounded-lg border bg-muted/20 p-3">
								<div class="space-y-2">
									{#each Object.entries(overrides.environment) as [key, value]}
										<div class="flex items-center gap-2">
											<Input
												value={key}
												onchange={(e) => updateEnvVar(key, e.currentTarget.value, value)}
												placeholder="VARIABLE_NAME"
												disabled={disabled}
												class="h-8 font-mono text-xs flex-1"
											/>
											<span class="text-muted-foreground text-xs">=</span>
											<Input
												value={value}
												oninput={(e) => updateEnvVar(key, key, e.currentTarget.value)}
												placeholder="value"
												disabled={disabled}
												class="h-8 font-mono text-xs flex-1"
											/>
											<Button
												type="button"
												variant="ghost"
												size="icon"
												onclick={() => removeEnvVar(key)}
												disabled={disabled}
												class="h-8 w-8 hover:bg-destructive/10 hover:text-destructive"
											>
												<X class="h-3 w-3" />
											</Button>
										</div>
									{/each}
								</div>
							</div>
						{:else}
							<div class="text-xs text-muted-foreground italic">No environment variables configured</div>
						{/if}
					</div>

					<!-- Volume Mounts -->
					<div class="space-y-3">
						<div class="flex items-center justify-between">
							<Label class="text-sm font-medium">Volume Mounts</Label>
							<Button
								type="button"
								variant="ghost"
								size="sm"
								onclick={addVolume}
								disabled={disabled}
								class="h-7 text-xs gap-1"
							>
								<Plus class="h-3 w-3" />
								Add Volume
							</Button>
						</div>
						{#if overrides?.volumes && overrides.volumes.length > 0}
							<div class="rounded-lg border bg-muted/20 p-3">
								<div class="space-y-3">
									{#each overrides.volumes as volume, i}
										<div class="space-y-2 p-2 rounded border bg-background/50">
											<div class="flex items-center gap-2">
												<div class="flex-1 grid grid-cols-2 gap-2">
													<Input
														value={volume.source}
														onchange={(e) => updateVolumeField(i, 'source', e.currentTarget.value)}
														placeholder="/host/path or volume-name"
														disabled={disabled}
														class="h-8 text-xs font-mono"
													/>
													<Input
														value={volume.target}
														onchange={(e) => updateVolumeField(i, 'target', e.currentTarget.value)}
														placeholder="/container/path"
														disabled={disabled}
														class="h-8 text-xs font-mono"
													/>
												</div>
												<Button
													type="button"
													variant="ghost"
													size="icon"
													onclick={() => updateVolume(i, null)}
													disabled={disabled}
													class="h-8 w-8 hover:bg-destructive/10 hover:text-destructive"
												>
													<X class="h-3 w-3" />
												</Button>
											</div>
											<div class="flex items-center gap-4 pl-1">
												<label class="flex items-center gap-2">
													<input
														type="checkbox"
														checked={volume.readOnly}
														onchange={(e) => updateVolumeField(i, 'readOnly', e.currentTarget.checked)}
														disabled={disabled}
														class="h-3 w-3"
													/>
													<span class="text-xs text-muted-foreground">Read Only</span>
												</label>
												<select
													value={volume.type || 'bind'}
													onchange={(e) => updateVolumeField(i, 'type', e.currentTarget.value)}
													disabled={disabled}
													class="h-6 text-xs border rounded px-1 bg-background"
												>
													<option value="bind">Bind Mount</option>
													<option value="volume">Volume</option>
												</select>
											</div>
										</div>
									{/each}
								</div>
							</div>
						{:else}
							<div class="text-xs text-muted-foreground italic">No volume mounts configured</div>
						{/if}
					</div>

					<!-- Resource Limits -->
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-2">
							<Label for="cpu-limit" class="text-sm">CPU Limit (cores)</Label>
							<Input
								id="cpu-limit"
								type="number"
								step="0.5"
								min="0"
								placeholder="e.g., 2.5"
								value={overrides?.cpuLimit || ''}
								onchange={(e) => updateOverride('cpuLimit', e.currentTarget.value ? Number(e.currentTarget.value) : undefined)}
								disabled={disabled}
								class="h-8 text-xs"
							/>
						</div>
						<div class="space-y-2">
							<Label for="memory-limit" class="text-sm">Memory Limit (MB)</Label>
							<Input
								id="memory-limit"
								type="number"
								min="512"
								placeholder="e.g., 8192"
								value={overrides?.memoryLimit ? Number(overrides.memoryLimit) / 1024 / 1024 : ''}
								onchange={(e) => updateOverride('memoryLimit', e.currentTarget.value ? BigInt(Number(e.currentTarget.value) * 1024 * 1024) : undefined)}
								disabled={disabled}
								class="h-8 text-xs"
							/>
						</div>
					</div>

					<!-- Network & Restart -->
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-2">
							<Label for="network-mode" class="text-sm">Network Mode</Label>
							<Input
								id="network-mode"
								type="text"
								placeholder="bridge (default)"
								value={overrides?.networkMode || ''}
								onchange={(e) => updateOverride('networkMode', e.currentTarget.value || undefined)}
								disabled={disabled}
								class="h-8 text-xs"
							/>
						</div>
						<div class="space-y-2">
							<Label for="restart-policy" class="text-sm">Restart Policy</Label>
							<Input
								id="restart-policy"
								type="text"
								placeholder="unless-stopped"
								value={overrides?.restartPolicy || ''}
								onchange={(e) => updateOverride('restartPolicy', e.currentTarget.value || undefined)}
								disabled={disabled}
								class="h-8 text-xs"
							/>
						</div>
					</div>

					<!-- Security Options -->
					<div class="space-y-3">
						<Label class="text-sm font-medium">Security Options</Label>
						<div class="flex flex-wrap gap-4 pl-4">
							<label class="flex items-center gap-2">
								<Switch
									checked={overrides?.privileged || false}
									onCheckedChange={(checked) => updateOverride('privileged', checked)}
									disabled={disabled}
								/>
								<span class="text-sm">Privileged Mode</span>
							</label>
						</div>
					</div>

					<!-- User -->
					<div class="space-y-2">
						<Label for="user" class="text-sm">Run As User</Label>
						<Input
							id="user"
							type="text"
							placeholder="1000:1000 or username"
							value={overrides?.user || ''}
							onchange={(e) => updateOverride('user', e.currentTarget.value || undefined)}
							disabled={disabled}
							class="h-8 text-xs"
						/>
					</div>
					<!-- Entrypoint -->
					<div class="space-y-2">
						<Label for="entrypoint" class="text-sm">Entrypoint</Label>
						<Input
							id="entrypoint"
							type="text"
							placeholder='/bin/sh, -c, echo "hello"'
							value={overrides?.entrypoint?.join(', ') || ''}
							onchange={(e) => {
								const value = e.currentTarget.value;
								if (!value) {
									updateOverride('entrypoint', undefined);
								} else {
									const parts = value.split(',').map(s => s.trim()).filter(s => s);
									updateOverride('entrypoint', parts.length > 0 ? parts : undefined);
								}
							}}
							disabled={disabled}
							class="h-8 text-xs"
						/>
						<p class="text-xs text-muted-foreground">Comma-separated arguments</p>
					</div>
				</CardContent>
			{/if}
		</Card>
	{/if}
</div>