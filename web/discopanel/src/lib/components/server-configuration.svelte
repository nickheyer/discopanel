<script lang="ts">
	import { onMount } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import ScrollToTop from './scroll-to-top.svelte';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Button } from '$lib/components/ui/button';
	import { Switch } from '$lib/components/ui/switch';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { toast } from 'svelte-sonner';
	import { Save, RefreshCw, Loader2, Link, ArrowUp } from '@lucide/svelte';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import { ServerStatus } from '$lib/proto/discopanel/v1/common_pb';
	import type { ConfigCategory, ConfigProperty } from '$lib/proto/discopanel/v1/config_pb';
	import * as _ from 'lodash-es';

	let { server, config, onSave, saving: externalSaving = false }: { 
		server?: Server;
		config?: ConfigCategory[];
		onSave?: (updates: Record<string, any>) => Promise<void>;
		saving?: boolean;
	} = $props();

	let loading = $state(false);
	let saving = $state(false);
	let categories = $state<ConfigCategory[]>(config || []);
	let originalValues = $state<Record<string, any>>({});
	let currentValues = $state<Record<string, any>>({});
	let enabledFields = $state<Set<string>>(new Set());
	let modifiedProperties = $state<Set<string>>(new Set());
	let highlightedField = $state<string | null>(null);
	
	// Use external saving state if provided, otherwise use internal
	let isSaving = $derived(externalSaving !== undefined ? externalSaving : saving);

	onMount(() => {
		if (server && !config) {
			loadServerConfig();
		} else if (config) {
			processConfig(config);
		}
		
		// Check for hash in URL to scroll to section/field
		checkUrlHash();
		
		// Listen for hash changes
		window.addEventListener('hashchange', checkUrlHash);
		
		return () => {
			window.removeEventListener('hashchange', checkUrlHash);
		};
	});

	// Reload config when server changes
	let previousServerId = $state(server?.id);
	$effect(() => {
		if (server && server.id !== previousServerId) {
			previousServerId = server.id;
			loadServerConfig();
		}
	});

	async function loadServerConfig() {
		loading = true;
		try {
			const response = await rpcClient.config.getServerConfig({ serverId: server!.id });
			categories = response.categories;
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
		
		// Build originalValues and currentValues from categories
		originalValues = {};
		currentValues = {};
		enabledFields = new Set();
		categories.forEach(category => {
			category.properties.forEach((prop: ConfigProperty) => {
				// Skip internal fields
				if (prop.key === 'id' || prop.key === 'serverId' || prop.key === 'updatedAt') {
					return;
				}
				
				// Store the actual value (which might be null/undefined)
				originalValues[prop.key] = prop.value;
				currentValues[prop.key] = prop.value;
				
				// For global settings (when no server), only enable fields that have values
				// For server configs, enable required/system fields or fields with values
				if (!server) {
					// Global settings - only enable if there's a value
					if (!_.isEmpty(prop.value)) {
						enabledFields.add(prop.key);
					}
				} else {
					// Server config - enable required/system fields or fields with values
					if (!_.isEmpty(prop.value) || prop.required || prop.system) {
						enabledFields.add(prop.key);
					}
				}
			});
		});
		
		// Reset modified properties when loading new config
		modifiedProperties.clear();
	}

	async function saveServerConfig() {
		if (modifiedProperties.size === 0) {
			toast.info('No changes to save');
			return;
		}

		if (!onSave) {
			saving = true;
		}
		try {
			const changes: Record<string, any> = {};
			
			// Include all modified properties
			modifiedProperties.forEach(key => {
				if (enabledFields.has(key)) {
					// Field is enabled - send its value (null/undefined means unset)
					const value = currentValues[key];
					// Convert undefined to null for consistency
					changes[key] = value === undefined ? null : value;
				} else {
					// Field is disabled - send null to unset it
					changes[key] = null;
				}
			});

			if (onSave) {
				// Custom save handler (for global settings)
				await onSave(changes);
			} else if (server) {
				// Default server config save
				const response = await rpcClient.config.updateServerConfig({ serverId: server!.id, updates: changes });
				categories = response.categories;
				processConfig(response.categories);
			}
			enabledFields = new Set(enabledFields); // Trigger reactivity
			
			toast.success('Configuration saved successfully');
			modifiedProperties.clear();
			modifiedProperties = new Set(); // Trigger reactivity
			
			if (server && server?.status === ServerStatus.RUNNING) {
				toast.info('Restart the server for changes to take effect');
			}
		} catch (error) {
			toast.error('Failed to save configuration');
			console.error('Failed to save configuration:', error);
		} finally {
			if (!onSave) {
				saving = false;
			}
		}
	}

	function handlePropertyChange(key: string, value: any) {
		// Treat empty strings as undefined (unset)
		if (value === '') {
			currentValues[key] = undefined;
		} else {
			currentValues[key] = value;
		}
		
		// Track modifications
		updateModifiedProperties();
	}

	function toggleFieldEnabled(key: string, enabled: boolean) {
		if (enabled) {
			enabledFields.add(key);
			// Set to default value if currently null
			if (currentValues[key] === null || currentValues[key] === undefined) {
				const prop = categories.flatMap(c => c.properties).find(p => p.key === key);
				if (prop) {
					currentValues[key] = prop.defaultValue ?? getDefaultForType(prop.type);
				}
			}
		} else {
			enabledFields.delete(key);
			currentValues[key] = null;
		}
		enabledFields = new Set(enabledFields);
		
		// Track modifications
		updateModifiedProperties();
	}

	function updateModifiedProperties() {
		modifiedProperties.clear();
		
		// Check for changes in enabled fields
		categories.forEach(category => {
			category.properties.forEach((prop: ConfigProperty) => {
				const wasEnabled = originalValues[prop.key] !== null && originalValues[prop.key] !== undefined;
				const isEnabled = enabledFields.has(prop.key);
				
				if (wasEnabled !== isEnabled) {
					modifiedProperties.add(prop.key);
				} else if (isEnabled && currentValues[prop.key] !== originalValues[prop.key]) {
					modifiedProperties.add(prop.key);
				}
			});
		});
		
		modifiedProperties = new Set(modifiedProperties);
	}

	function getDefaultForType(type: string): any {
		switch (type) {
			case 'number': return 0;
			case 'checkbox': return false;
			case 'text':
			case 'password':
			case 'select':
			default: return '';
		}
	}
	
	function checkUrlHash() {
		const hash = window.location.hash.slice(1); // Remove #
		if (!hash) return;
		
		// Hash can be either section name or field key
		setTimeout(() => {
			const element = document.getElementById(hash);
			if (element) {
				element.scrollIntoView({ behavior: 'smooth', block: 'center' });
				
				// If it's a field, highlight it
				if (element.hasAttribute('data-field')) {
					highlightedField = hash;
					// Remove highlight after 3 seconds
					setTimeout(() => {
						highlightedField = null;
					}, 3000);
				}
			}
		}, 100);
	}
	
	function copyLinkToClipboard(anchor: string) {
		const url = new URL(window.location.href);
		url.hash = anchor;
		navigator.clipboard.writeText(url.toString());
		toast.success('Link copied to clipboard');
	}

	function resetChanges() {
		modifiedProperties.clear();
		modifiedProperties = modifiedProperties;
		enabledFields.clear();
		enabledFields = enabledFields;
		loadServerConfig();
	}

	function getInputType(type: string): 'text' | 'number' | 'checkbox' | 'select' | 'password' {
		switch (type) {
			case 'text':
			case 'number':
			case 'checkbox':
			case 'select':
			case 'password':
				return type;
			default:
				return 'text';
		}
	}
</script>

<Card class="h-full flex flex-col">
	<CardHeader class="flex-shrink-0">
		<div class="flex items-center justify-between">
			<div class="flex items-center gap-2">
				<CardTitle>Server Configuration</CardTitle>
				<CardDescription>
					Configure Minecraft server settings
				</CardDescription>
			</div>
			<div class="flex items-center gap-2">
				{#if modifiedProperties.size > 0}
					<span class="text-sm text-muted-foreground">
						{modifiedProperties.size} unsaved {modifiedProperties.size === 1 ? 'change' : 'changes'}
					</span>
				{/if}
				<Button
					variant="outline"
					size="sm"
					onclick={resetChanges}
					disabled={loading || (server && server?.status === ServerStatus.RUNNING) || modifiedProperties.size === 0}
				>
					<RefreshCw class="h-4 w-4 mr-2" />
					Reset
				</Button>
				<Button
					size="sm"
					onclick={saveServerConfig}
					disabled={loading || isSaving || (server && server?.status === ServerStatus.RUNNING) || modifiedProperties.size === 0}
				>
					{#if isSaving}
						<Loader2 class="h-4 w-4 mr-2 animate-spin" />
					{:else}
						<Save class="h-4 w-4 mr-2" />
					{/if}
					Save Changes
				</Button>
			</div>
		</div>
	</CardHeader>
	
	<CardContent class="flex-1 flex flex-col min-h-0">
		{#if loading}
			<div class="flex items-center justify-center py-8">
				<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
			</div>
		{:else}
			{#if categories.length === 0}
				<div class="flex flex-col items-center justify-center py-12 text-muted-foreground">
					<p class="text-lg mb-2">No configuration found</p>
					<p class="text-sm">Unable to load server configuration</p>
				</div>
			{:else}
				<div class="space-y-6 overflow-y-auto pr-4">
					{#each categories as category}
						{@const filteredProps = !server ?
							category.properties.filter((p: ConfigProperty) => !p.system) :
							category.properties}
						{#if filteredProps.length > 0}
							<div class="space-y-4" id={category.name.toLowerCase().replace(/\s+/g, '-')}>
								<div class="flex items-center justify-between border-b pb-2 group">
									<h3 class="text-lg font-semibold text-foreground/90">{category.name}</h3>
									<Button
										variant="ghost"
										size="icon"
										class="h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity"
										onclick={() => copyLinkToClipboard(category.name.toLowerCase().replace(/\s+/g, '-'))}
									>
										<Link class="h-3 w-3" />
									</Button>
								</div>
								<div class="grid gap-4 md:grid-cols-2">
									{#each filteredProps as prop}
									{@const inputType = getInputType(prop.type)}
									<div 
										id={prop.key}
										data-field="true"
										class="relative p-4 rounded-lg border bg-card hover:bg-accent/5 transition-all duration-300 {highlightedField === prop.key ? 'ring-2 ring-primary ring-offset-2 animate-pulse' : ''}">
										<div class="flex gap-3">
											<Checkbox
												checked={enabledFields.has(prop.key)}
												onCheckedChange={(checked) => toggleFieldEnabled(prop.key, checked)}
												disabled={prop.required || prop.system || server?.status === ServerStatus.RUNNING}
												class="mt-1"
											/>
											<div class="flex-1 space-y-2">
												<div class="flex items-start justify-between gap-2">
													<div class="flex-1">
														<div class="flex items-center gap-2">
															<Label for={prop.key} class="text-sm font-medium">
															{prop.label}
															{#if prop.required}
																<span class="text-xs text-red-500 ml-1">*</span>
															{/if}
															{#if prop.system}
																<span class="text-xs text-blue-500 ml-1">• system</span>
															{/if}
															{#if modifiedProperties.has(prop.key)}
																<span class="text-xs text-orange-500 ml-1">• modified</span>
															{/if}
														</Label>
														<Button
															variant="ghost"
															size="icon"
															class="h-4 w-4 opacity-0 hover:opacity-100 transition-opacity"
															onclick={() => copyLinkToClipboard(prop.key)}
														>
															<Link class="h-3 w-3" />
														</Button>
													</div>
														<div class="text-xs text-muted-foreground font-mono">{prop.envVar}</div>
														{#if prop.description}
															<p class="text-xs text-muted-foreground mt-1">{prop.description}</p>
														{/if}
													</div>
												</div>
										
										{#if inputType === 'checkbox'}
											<div class="flex items-center space-x-2">
												<Switch
													id={prop.key}
													checked={enabledFields.has(prop.key) ? (currentValues[prop.key] ?? prop.defaultValue ?? false) : (prop.defaultValue ?? false)}
													onCheckedChange={(checked) => handlePropertyChange(prop.key, checked)}
													disabled={prop.system || !enabledFields.has(prop.key) || server?.status === ServerStatus.RUNNING}
													class=""
												/>
												<span class="text-sm text-muted-foreground">
													{(currentValues[prop.key] ?? prop.defaultValue ?? false) ? 'Enabled' : 'Disabled'}
													{#if currentValues[prop.key] === null || currentValues[prop.key] === undefined}
														<span class="text-xs ml-1">(default)</span>
													{/if}
												</span>
											</div>
										{:else if inputType === 'select' && prop.options}
											<Select
												type="single"
												value={String(currentValues[prop.key] ?? prop.defaultValue ?? '')}
												onValueChange={(value) => handlePropertyChange(prop.key, value || undefined)}
												disabled={prop.system || !enabledFields.has(prop.key) || server?.status === ServerStatus.RUNNING}
											>
												<SelectTrigger class="h-9">
													<span>
														{currentValues[prop.key] || prop.defaultValue || 'Select an option'}
														{#if currentValues[prop.key] === undefined && prop.defaultValue}
															<span class="text-xs text-muted-foreground ml-1">(default)</span>
														{/if}
													</span>
												</SelectTrigger>
												<SelectContent>
													{#each prop.options as option}
														<SelectItem value={option}>{option}</SelectItem>
													{/each}
												</SelectContent>
											</Select>
										{:else if inputType === 'number'}
											<Input
												id={prop.key}
												type="number"
												value={enabledFields.has(prop.key) ? (currentValues[prop.key] ?? '') : ''}
												placeholder={prop.defaultValue !== undefined ? String(prop.defaultValue) : ''}
												oninput={(e) => handlePropertyChange(prop.key, e.currentTarget.value ? parseInt(e.currentTarget.value) : undefined)}
												disabled={prop.system || !enabledFields.has(prop.key) || server?.status === ServerStatus.RUNNING}
												class="h-9"
											/>
										{:else if inputType === 'password'}
											<Input
												id={prop.key}
												type="password"
												value={enabledFields.has(prop.key) ? (currentValues[prop.key] ?? '') : ''}
												placeholder={prop.defaultValue !== undefined ? String(prop.defaultValue) : ''}
												oninput={(e) => handlePropertyChange(prop.key, e.currentTarget.value || undefined)}
												disabled={prop.system || !enabledFields.has(prop.key) || server?.status === ServerStatus.RUNNING}
												class="h-9"
											/>
										{:else}
											<Input
												id={prop.key}
												type="text"
												value={enabledFields.has(prop.key) ? (currentValues[prop.key] ?? '') : ''}
												placeholder={prop.defaultValue !== undefined ? String(prop.defaultValue) : ''}
												oninput={(e) => handlePropertyChange(prop.key, e.currentTarget.value || undefined)}
												disabled={prop.system || !enabledFields.has(prop.key) || server?.status === ServerStatus.RUNNING}
												class="h-9"
											/>
										{/if}
										
										{#if prop.defaultValue !== undefined}
											<p class="text-xs text-muted-foreground">
												Default: {String(prop.defaultValue)}
											</p>
										{/if}
											</div>
										</div>
									</div>
								{/each}
							</div>
						</div>
						{/if}
					{/each}
				</div>
			{/if}

			{#if server?.status === ServerStatus.RUNNING}
				<div class="mt-4 p-4 bg-yellow-50 dark:bg-yellow-950 rounded-lg border border-yellow-200 dark:border-yellow-800">
					<p class="text-sm text-yellow-800 dark:text-yellow-200">
						⚠️ Server must be stopped to modify configuration. Changes will take effect after restart.
					</p>
				</div>
			{/if}
		{/if}
		<ScrollToTop />
	</CardContent>
</Card>
