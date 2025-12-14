<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { replaceState } from '$app/navigation';
	import { rpcClient } from '$lib/api/rpc-client';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Button } from '$lib/components/ui/button';
	import { Switch } from '$lib/components/ui/switch';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { toast } from 'svelte-sonner';
	import { Save, RefreshCw, Loader2, Link, CircleDot, Circle } from '@lucide/svelte';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import { ServerStatus } from '$lib/proto/discopanel/v1/common_pb';
	import type { ConfigCategory, ConfigProperty } from '$lib/proto/discopanel/v1/config_pb';
	import ScrollToTop from './scroll-to-top.svelte';

	interface Props {
		server?: Server;
		config?: ConfigCategory[];
		onSave?: (updates: Record<string, string>) => Promise<void>;
		saving?: boolean;
	}

	let { server, config, onSave, saving: externalSaving = false }: Props = $props();

	// State
	let loading = $state(false);
	let saving = $state(false);
	let categories = $state<ConfigCategory[]>([]);
	let activeCategory = $state<string>('');
	let highlightedField = $state<string | null>(null);

	// Track original and current values
	let originalValues = $state<Map<string, string | null>>(new Map());
	let currentValues = $state<Map<string, string | null>>(new Map());
	let originalEnabled = $state<Set<string>>(new Set());
	let currentEnabled = $state<Set<string>>(new Set());

	// Derived state
	let isSaving = $derived(externalSaving || saving);
	let isServerRunning = $derived(server?.status === ServerStatus.RUNNING);

	// Get filtered categories (hide empty ones and filter system fields for global)
	let filteredCategories = $derived.by(() => {
		return categories
			.map(cat => ({
				...cat,
				properties: !server
					? cat.properties.filter(p => !p.system)
					: cat.properties
			}))
			.filter(cat => cat.properties.length > 0);
	});

	// Get current category's properties
	let currentCategoryProps = $derived.by(() => {
		const cat = filteredCategories.find(c => getCategoryId(c.name) === activeCategory);
		return cat?.properties ?? [];
	});

	let currentCategoryName = $derived.by(() => {
		const cat = filteredCategories.find(c => getCategoryId(c.name) === activeCategory);
		return cat?.name ?? '';
	});

	// Calculate modified fields
	let modifiedFields = $derived.by(() => {
		const modified = new Set<string>();
		for (const [key, value] of currentValues) {
			const origValue = originalValues.get(key);
			const wasEnabled = originalEnabled.has(key);
			const isEnabled = currentEnabled.has(key);

			if (wasEnabled !== isEnabled) {
				modified.add(key);
				continue;
			}

			if (isEnabled && value !== origValue) {
				modified.add(key);
			}
		}
		return modified;
	});

	// Count modified fields per category
	let modifiedCountByCategory = $derived.by(() => {
		const counts = new Map<string, number>();
		for (const cat of filteredCategories) {
			const catId = getCategoryId(cat.name);
			let count = 0;
			for (const prop of cat.properties) {
				if (modifiedFields.has(prop.key)) count++;
			}
			counts.set(catId, count);
		}
		return counts;
	});

	let hasChanges = $derived(modifiedFields.size > 0);

	// Initialize URL hash handling
	onMount(() => {
		checkUrlHash();
		window.addEventListener('hashchange', checkUrlHash);
		return () => window.removeEventListener('hashchange', checkUrlHash);
	});

	// React to config prop changes (for global settings)
	let previousConfig = $state<ConfigCategory[] | undefined>(undefined);
	$effect(() => {
		if (config && config !== previousConfig) {
			untrack(() => {
				previousConfig = config;
				processConfig(config);
			});
		}
	});

	// Reload when server changes
	let previousServerId = $state<string | undefined>(undefined);
	$effect(() => {
		if (server && server.id !== previousServerId) {
			untrack(() => {
				previousServerId = server!.id;
				loadServerConfig();
			});
		}
	});

	// Set initial active category when categories load
	$effect(() => {
		if (filteredCategories.length > 0 && !activeCategory) {
			untrack(() => {
				activeCategory = getCategoryId(filteredCategories[0].name);
			});
		}
	});

	async function loadServerConfig() {
		if (!server) return;
		loading = true;
		try {
			const response = await rpcClient.config.getServerConfig({ serverId: server.id });
			processConfig(response.categories);
		} catch (error) {
			toast.error('Failed to load server configuration');
			console.error(error);
		} finally {
			loading = false;
		}
	}

	function processConfig(configData: ConfigCategory[]) {
		categories = configData;

		const newOriginalValues = new Map<string, string | null>();
		const newCurrentValues = new Map<string, string | null>();
		const newOriginalEnabled = new Set<string>();
		const newCurrentEnabled = new Set<string>();

		const isGlobal = !server;

		for (const category of configData) {
			for (const prop of category.properties) {
				if (['id', 'serverId', 'updatedAt'].includes(prop.key)) continue;

				const value = prop.value || null;
				newOriginalValues.set(prop.key, value);
				newCurrentValues.set(prop.key, value);

				const hasValue = value !== null && value !== '';
				const shouldEnable = isGlobal
					? hasValue || prop.required
					: hasValue || prop.required || prop.system;

				if (shouldEnable) {
					newOriginalEnabled.add(prop.key);
					newCurrentEnabled.add(prop.key);
				}
			}
		}

		originalValues = newOriginalValues;
		currentValues = newCurrentValues;
		originalEnabled = newOriginalEnabled;
		currentEnabled = newCurrentEnabled;
	}

	async function handleSave() {
		if (!hasChanges) {
			toast.info('No changes to save');
			return;
		}

		saving = true;
		try {
			const updates: Record<string, string> = {};

			for (const key of modifiedFields) {
				if (currentEnabled.has(key)) {
					const value = currentValues.get(key);
					updates[key] = value ?? '';
				} else {
					updates[key] = '';
				}
			}

			if (onSave) {
				await onSave(updates);
			} else if (server) {
				const response = await rpcClient.config.updateServerConfig({
					serverId: server.id,
					updates
				});
				processConfig(response.categories);
			}

			toast.success('Configuration saved');

			if (isServerRunning) {
				toast.info('Restart the server for changes to take effect');
			}
		} catch (error) {
			toast.error('Failed to save configuration');
			console.error(error);
		} finally {
			saving = false;
		}
	}

	function handleReset() {
		currentValues = new Map(originalValues);
		currentEnabled = new Set(originalEnabled);
	}

	function toggleFieldEnabled(key: string, enabled: boolean, prop: ConfigProperty) {
		const newEnabled = new Set(currentEnabled);
		const newValues = new Map(currentValues);

		if (enabled) {
			newEnabled.add(key);
			if (!newValues.get(key)) {
				newValues.set(key, prop.defaultValue ?? getDefaultForType(prop.type));
			}
		} else {
			newEnabled.delete(key);
		}

		currentEnabled = newEnabled;
		currentValues = newValues;
	}

	function updateValue(key: string, value: string | boolean) {
		const newValues = new Map(currentValues);
		const strValue = typeof value === 'boolean' ? String(value) : value;
		newValues.set(key, strValue || null);
		currentValues = newValues;
	}

	function getDefaultForType(type: string): string {
		switch (type) {
			case 'number': return '0';
			case 'checkbox': return 'false';
			default: return '';
		}
	}

	function getDisplayValue(prop: ConfigProperty): string {
		const value = currentValues.get(prop.key);
		const isEnabled = currentEnabled.has(prop.key);

		if (isEnabled && value !== null && value !== undefined) {
			return value;
		}
		return prop.defaultValue ?? '';
	}

	function getBooleanValue(prop: ConfigProperty): boolean {
		const value = getDisplayValue(prop);
		return value.toLowerCase() === 'true';
	}

	function getCategoryId(name: string): string {
		return name.toLowerCase().replace(/\s+/g, '-');
	}

	function selectCategory(categoryId: string) {
		activeCategory = categoryId;
		const url = new URL(window.location.href);
		url.hash = categoryId;
		replaceState(url.toString(), {});
	}

	function copyLinkToClipboard(anchor: string) {
		const url = new URL(window.location.href);
		url.hash = anchor;
		navigator.clipboard.writeText(url.toString());
		toast.success('Link copied to clipboard');
	}

	function checkUrlHash() {
		const hash = window.location.hash.slice(1);
		if (!hash) return;

		setTimeout(() => {
			const matchingCategory = filteredCategories.find(c => getCategoryId(c.name) === hash);
			if (matchingCategory) {
				activeCategory = hash;
				return;
			}

			for (const cat of filteredCategories) {
				const matchingProp = cat.properties.find(p => p.key === hash);
				if (matchingProp) {
					activeCategory = getCategoryId(cat.name);
					setTimeout(() => {
						const element = document.getElementById(hash);
						if (element) {
							element.scrollIntoView({ behavior: 'smooth', block: 'center' });
							highlightedField = hash;
							setTimeout(() => {
								highlightedField = null;
							}, 3000);
						}
					}, 100);
					return;
				}
			}
		}, 50);
	}

	function canToggleField(prop: ConfigProperty): boolean {
		if (isServerRunning) return false;
		if (prop.required) return false;
		if (prop.system) return false;
		return true;
	}
</script>

<Card class="h-full flex flex-col pb-0 gap-0">
	<CardHeader class="flex-shrink-0 border-b pb-0 mb-0">
		<div class="flex flex-col sm:flex-row sm:items-center justify-between">
			<div>
				<CardTitle>
					{!server ? 'Default Server Configuration' : 'Server Configuration'}
				</CardTitle>
				<p class="text-sm text-muted-foreground mt-1">
					{!server
						? 'Configure default values for new servers'
						: 'Configure Minecraft server environment variables'}
				</p>
			</div>
			<div class="flex items-center gap-3">
				{#if hasChanges}
					<span class="text-sm text-muted-foreground whitespace-nowrap">
						{modifiedFields.size} unsaved {modifiedFields.size === 1 ? 'change' : 'changes'}
					</span>
				{/if}
				<Button
					variant="outline"
					size="sm"
					onclick={handleReset}
					disabled={loading || isServerRunning || !hasChanges}
				>
					<RefreshCw class="h-4 w-4 mr-2" />
					Reset
				</Button>
				<Button
					size="sm"
					onclick={handleSave}
					disabled={loading || isSaving || isServerRunning || !hasChanges}
				>
					{#if isSaving}
						<Loader2 class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<Save class="h-4 w-4 mr-2" />
					{/if}
					Save
				</Button>
			</div>
		</div>
	</CardHeader>

	<CardContent class="flex-1 overflow-hidden p-0 my-0">
		{#if loading}
			<div class="flex items-center justify-center py-12">
				<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
			</div>
		{:else if filteredCategories.length === 0}
			<div class="flex flex-col items-center justify-center py-12 text-muted-foreground">
				<p class="text-lg mb-2">No configuration found</p>
				<p class="text-sm">Unable to load server configuration</p>
			</div>
		{:else}
			<div class="flex h-full">
				<!-- Category Sidebar -->
				<div class="w-48 flex-shrink-0 border-r bg-muted/20 overflow-y-auto">
					<nav class="p-2 space-y-1">
						{#each filteredCategories as category}
							{@const categoryId = getCategoryId(category.name)}
							{@const isActive = activeCategory === categoryId}
							{@const modCount = modifiedCountByCategory.get(categoryId) ?? 0}
							<button
								class="w-full flex items-center justify-between px-3 py-2 rounded-md text-sm transition-colors text-left
									{isActive
										? 'bg-primary text-primary-foreground'
										: 'text-muted-foreground hover:bg-muted hover:text-foreground'}"
								onclick={() => selectCategory(categoryId)}
							>
								<span class="truncate">{category.name}</span>
								{#if modCount > 0}
									<span class="ml-2 inline-flex items-center justify-center h-5 min-w-5 px-1.5 text-xs font-medium rounded-full
										{isActive ? 'bg-primary-foreground/20 text-primary-foreground' : 'bg-orange-500 text-white'}">
										{modCount}
									</span>
								{/if}
							</button>
						{/each}
					</nav>
				</div>

				<!-- Fields Panel -->
				<div class="flex-1 flex flex-col min-w-0">
					<!-- Category Header -->
					<div class="flex-shrink-0 px-4 py-3 border-b bg-muted/10">
						<h3 class="font-semibold">{currentCategoryName}</h3>
						<p class="text-xs text-muted-foreground">{currentCategoryProps.length} fields</p>
					</div>

					<!-- Fields Grid -->
					<div class="flex-1 overflow-y-auto p-4">
						<div class="grid gap-4 lg:grid-cols-2">
							{#each currentCategoryProps as prop (prop.key)}
								{@const isEnabled = currentEnabled.has(prop.key)}
								{@const isModified = modifiedFields.has(prop.key)}
								{@const isHighlighted = highlightedField === prop.key}
								{@const canToggle = canToggleField(prop)}

								<div
									id={prop.key}
									data-field="true"
									class="group p-4 rounded-lg border transition-all duration-300
										{isHighlighted ? 'ring-2 ring-primary ring-offset-2' : ''}
										{isModified ? 'border-orange-500/50 bg-orange-500/5' : 'bg-card'}
										{!isEnabled ? 'bg-muted/30' : ''}"
								>
									<!-- Field Header -->
									<div class="flex items-start justify-between gap-2 mb-3">
										<div class="flex-1 min-w-0">
											<div class="flex items-center gap-2 flex-wrap mb-1">
												<Label for={prop.key} class="font-medium text-sm {!isEnabled ? 'text-muted-foreground' : ''}">
													{prop.label}
												</Label>
												{#if prop.required}
													<span class="text-xs text-red-500 font-medium">required</span>
												{/if}
												{#if prop.system}
													<span class="text-xs text-blue-500 font-medium">system</span>
												{/if}
												{#if isModified}
													<span class="text-xs text-orange-500 font-medium">modified</span>
												{/if}
												{#if !isEnabled}
													<span class="text-xs text-muted-foreground">(unset)</span>
												{/if}
											</div>
											{#if prop.envVar}
												<code class="text-xs text-muted-foreground font-mono">{prop.envVar}</code>
											{/if}
											{#if prop.description}
												<p class="text-xs text-muted-foreground mt-1">{prop.description}</p>
											{/if}
										</div>
										<div class="flex items-center gap-1">
											<!-- Set/Unset Toggle -->
											<button
												class="p-1 rounded transition-colors
													{canToggle ? 'hover:bg-muted cursor-pointer' : 'opacity-50 cursor-not-allowed'}"
												onclick={() => canToggle && toggleFieldEnabled(prop.key, !isEnabled, prop)}
												disabled={!canToggle}
												title={isEnabled ? 'Click to unset (use default)' : 'Click to set a custom value'}
											>
												{#if isEnabled}
													<CircleDot class="h-4 w-4 text-green-500" />
												{:else}
													<Circle class="h-4 w-4 text-muted-foreground" />
												{/if}
											</button>
											<Button
												variant="ghost"
												size="icon"
												class="h-6 w-6 opacity-0 group-hover:opacity-100"
												onclick={() => copyLinkToClipboard(prop.key)}
											>
												<Link class="h-3 w-3" />
											</Button>
										</div>
									</div>

									<!-- Field Input -->
									{#if prop.type === 'checkbox'}
										<div class="flex items-center gap-3 py-1">
											<Switch
												id={prop.key}
												checked={getBooleanValue(prop)}
												onCheckedChange={(checked) => updateValue(prop.key, checked)}
												disabled={prop.system || !isEnabled || isServerRunning}
											/>
											<span class="text-sm {!isEnabled ? 'text-muted-foreground' : ''}">
												{getBooleanValue(prop) ? 'Enabled' : 'Disabled'}
											</span>
										</div>
									{:else if prop.type === 'select' && prop.options?.length}
										<Select
											type="single"
											value={getDisplayValue(prop)}
											onValueChange={(value) => updateValue(prop.key, value ?? '')}
											disabled={prop.system || !isEnabled || isServerRunning}
										>
											<SelectTrigger class="h-9 {!isEnabled ? 'opacity-60' : ''}">
												<span class="truncate">
													{getDisplayValue(prop) || 'Select...'}
												</span>
											</SelectTrigger>
											<SelectContent>
												{#each prop.options as option}
													<SelectItem value={option}>{option || '(empty)'}</SelectItem>
												{/each}
											</SelectContent>
										</Select>
									{:else if prop.type === 'number'}
										<Input
											id={prop.key}
											type="number"
											value={getDisplayValue(prop)}
											placeholder={prop.defaultValue ?? ''}
											oninput={(e) => updateValue(prop.key, e.currentTarget.value)}
											disabled={prop.system || !isEnabled || isServerRunning}
											class="h-9 {!isEnabled ? 'opacity-60' : ''}"
										/>
									{:else if prop.type === 'password'}
										<Input
											id={prop.key}
											type="password"
											value={getDisplayValue(prop)}
											placeholder={prop.defaultValue ?? ''}
											oninput={(e) => updateValue(prop.key, e.currentTarget.value)}
											disabled={prop.system || !isEnabled || isServerRunning}
											class="h-9 {!isEnabled ? 'opacity-60' : ''}"
										/>
									{:else}
										<Input
											id={prop.key}
											type="text"
											value={getDisplayValue(prop)}
											placeholder={prop.defaultValue ?? ''}
											oninput={(e) => updateValue(prop.key, e.currentTarget.value)}
											disabled={prop.system || !isEnabled || isServerRunning}
											class="h-9 {!isEnabled ? 'opacity-60' : ''}"
										/>
									{/if}

									{#if prop.defaultValue !== undefined && prop.defaultValue !== ''}
										<p class="text-xs text-muted-foreground mt-2">
											Default: <code class="bg-muted px-1 py-0.5 rounded">{prop.defaultValue}</code>
										</p>
									{/if}
								</div>
							{/each}
						</div>
					</div>
				</div>
			</div>

			{#if isServerRunning}
				<div class="p-4 bg-yellow-50 dark:bg-yellow-950 border-t border-yellow-200 dark:border-yellow-800">
					<p class="text-sm text-yellow-800 dark:text-yellow-200">
						Server must be stopped to modify configuration. Changes will take effect after restart.
					</p>
				</div>
			{/if}
		{/if}
	</CardContent>
</Card>

<ScrollToTop />
