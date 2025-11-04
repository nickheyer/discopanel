<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Switch } from '$lib/components/ui/switch';
	import { Plus, AlertCircle, Code, ChevronDown, ChevronRight, X } from '@lucide/svelte';
	import type { ContainerOverrides, VolumeMount } from '$lib/api/types';
	import { Badge } from '$lib/components/ui/badge';

	interface Props {
		overrides?: ContainerOverrides;
		disabled?: boolean;
		onchange?: (overrides: ContainerOverrides | undefined) => void;
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
		if (overrides.cpu_limit) count++;
		if (overrides.memory_override) count++;
		if (overrides.restart_policy) count++;
		if (overrides.network_mode) count++;
		if (overrides.privileged) count++;
		if (overrides.user) count++;
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

	function updateOverride<K extends keyof ContainerOverrides>(key: K, value: ContainerOverrides[K]) {
		if (!overrides) overrides = {};

		if (value === undefined || value === null ||
		    (typeof value === 'string' && !value) ||
		    (Array.isArray(value) && value.length === 0) ||
		    (typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === 0)) {
			delete overrides[key];
		} else {
			overrides[key] = value;
		}

		// If no overrides left, set to undefined
		if (Object.keys(overrides).length === 0) {
			overrides = undefined;
		} else {
			overrides = { ...overrides };
		}

		onchange?.(overrides);
	}

	// Environment Variables
	function addEnvVar() {
		const env = { ...(overrides?.environment || {}) };
		// Generate unique key name
		let newKey = `VAR_${envVarCounter}`;
		while (env[newKey]) {
			envVarCounter++;
			newKey = `VAR_${envVarCounter}`;
		}
		envVarCounter++;
		env[newKey] = '';
		updateOverride('environment', env);
	}

	function updateEnvVar(oldKey: string, newKey: string, value: string) {
		const env = { ...(overrides?.environment || {}) };

		// If key changed, delete old key
		if (oldKey !== newKey) {
			delete env[oldKey];
		}

		// Add new key/value if both are present
		if (newKey) {
			env[newKey] = value;
		}

		updateOverride('environment', Object.keys(env).length > 0 ? env : undefined);
	}

	function removeEnvVar(key: string) {
		const env = { ...(overrides?.environment || {}) };
		delete env[key];
		updateOverride('environment', Object.keys(env).length > 0 ? env : undefined);
	}

	// Volumes
	function addVolume() {
		const volumes = [...(overrides?.volumes || [])];
		volumes.push({ source: '', target: '', type: 'bind' });
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
			<span>Container Overrides</span>
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
								<div class="space-y-2">
									{#each overrides.volumes as volume, i}
										<div class="flex items-center gap-2">
											<Input
												bind:value={volume.source}
												onchange={() => updateVolume(i, volume)}
												placeholder="/host/path"
												disabled={disabled}
												class="h-8 text-xs flex-1"
											/>
											<span class="text-muted-foreground text-xs">→</span>
											<Input
												bind:value={volume.target}
												onchange={() => updateVolume(i, volume)}
												placeholder="/container/path"
												disabled={disabled}
												class="h-8 text-xs flex-1"
											/>
											<label class="flex items-center gap-1">
												<Switch
													checked={volume.read_only || false}
													onCheckedChange={(checked) => {
														volume.read_only = checked;
														updateVolume(i, volume);
													}}
													disabled={disabled}
													class="scale-75"
												/>
												<span class="text-xs">RO</span>
											</label>
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
								value={overrides?.cpu_limit || ''}
								onchange={(e) => updateOverride('cpu_limit', e.currentTarget.value ? Number(e.currentTarget.value) : undefined)}
								disabled={disabled}
								class="h-8 text-xs"
							/>
						</div>
						<div class="space-y-2">
							<Label for="memory-override" class="text-sm">Memory Override (MB)</Label>
							<Input
								id="memory-override"
								type="number"
								min="512"
								placeholder="e.g., 8192"
								value={overrides?.memory_override || ''}
								onchange={(e) => updateOverride('memory_override', e.currentTarget.value ? Number(e.currentTarget.value) : undefined)}
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
								value={overrides?.network_mode || ''}
								onchange={(e) => updateOverride('network_mode', e.currentTarget.value || undefined)}
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
								value={overrides?.restart_policy || ''}
								onchange={(e) => updateOverride('restart_policy', e.currentTarget.value || undefined)}
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
									onCheckedChange={(checked) => updateOverride('privileged', checked || undefined)}
									disabled={disabled}
								/>
								<span class="text-sm">Privileged Mode</span>
							</label>
							<label class="flex items-center gap-2">
								<Switch
									checked={overrides?.read_only || false}
									onCheckedChange={(checked) => updateOverride('read_only', checked || undefined)}
									disabled={disabled}
								/>
								<span class="text-sm">Read-only Root FS</span>
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
				</CardContent>
			{/if}
		</Card>
	{/if}
</div>