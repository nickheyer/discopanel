<script lang="ts">
	import { onMount } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { Input } from '$lib/components/ui/input';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Switch } from '$lib/components/ui/switch';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Loader2, Plus, Trash2, Webhook, Play, Copy, Edit2, ChevronDown } from '@lucide/svelte';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { Webhook as WebhookType } from '$lib/proto/discopanel/v1/webhook_pb';
	import { WebhookEventType, WebhookFormat, CreateWebhookRequestSchema, UpdateWebhookRequestSchema, ToggleWebhookRequestSchema, TestWebhookRequestSchema, DeleteWebhookRequestSchema, ListWebhooksRequestSchema } from '$lib/proto/discopanel/v1/webhook_pb';
	import { create } from '@bufbuild/protobuf';
	import { EditorView } from '@codemirror/view';
	import { EditorState, Compartment } from '@codemirror/state';
	import { json } from '@codemirror/lang-json';
	import { oneDark } from '@codemirror/theme-one-dark';
	import { basicSetup } from 'codemirror';

	let { server, active }: { server: Server, active?: boolean } = $props();

	let loading = $state(true);
	let webhooks = $state<WebhookType[]>([]);
	let initialized = $state(false);
	let previousServerId = $state(server.id);

	// Dialog state
	let showCreateDialog = $state(false);
	let selectedWebhook = $state<WebhookType | null>(null);
	let creating = $state(false);
	let testing = $state<string | null>(null);

	// Form state
	let webhookName = $state('');
	let webhookUrl = $state('');
	let webhookSecret = $state('');
	let webhookFormat = $state<WebhookFormat>(WebhookFormat.GENERIC);
	let selectedEvents = $state<WebhookEventType[]>([]);
	let payloadTemplate = $state('');
	let customizePayload = $state(false);
	let maxRetries = $state(3);
	let retryDelayMs = $state(1000);
	let timeoutMs = $state(5000);
	let showAdvanced = $state(false);

	// Template presets loaded from backend (keyed by name)
	let templatePresets = $state<Record<string, string>>({});

	// CodeMirror editor
	let editorContainer = $state<HTMLDivElement>();
	let editorView = $state<EditorView | null>(null);
	const editableCompartment = new Compartment();

	// Preset display names (order matters for rendering)
	const presetLabels: Record<string, string> = {
		discord: 'Discord',
		slack: 'Slack',
		teams: 'Teams',
		ntfy: 'ntfy',
		generic: 'Generic',
	};

	// Auto-detect format from URL
	function isDiscordUrl(url: string): boolean {
		return url.includes('discord.com/api/webhooks') || url.includes('discordapp.com/api/webhooks');
	}

	function getDefaultPresetKey(url: string): string {
		return isDiscordUrl(url) ? 'discord' : 'generic';
	}

	function getDefaultTemplate(url: string): string {
		return templatePresets[getDefaultPresetKey(url)] || '';
	}

	// All available events
	const allEvents: { type: WebhookEventType; label: string; description: string }[] = [
		{ type: WebhookEventType.SERVER_START, label: 'Server Start', description: 'When server starts' },
		{ type: WebhookEventType.SERVER_STOP, label: 'Server Stop', description: 'When server stops' },
		{ type: WebhookEventType.SERVER_RESTART, label: 'Server Restart', description: 'When server restarts' },
	];

	onMount(() => {
		if (server && !initialized) {
			initialized = true;
			loadWebhooks();
		}
	});

	// Reset state when server changes
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			loading = true;
			webhooks = [];
			initialized = false;
			loadWebhooks();
		}
	});

	$effect(() => {
		if (server && !initialized && active) {
			initialized = true;
			loadWebhooks();
		}
	});

	// Create/destroy editor when dialog opens/closes
	$effect(() => {
		if (showCreateDialog && editorContainer && !editorView) {
			createEditor();
		}
		if (!showCreateDialog && editorView) {
			destroyEditor();
		}
	});

	// Sync editor content and editable state when customizePayload or URL changes
	$effect(() => {
		if (!editorView) return;
		const displayValue = customizePayload ? payloadTemplate : getDefaultTemplate(webhookUrl);
		const currentValue = editorView.state.doc.toString();
		const transactions: any[] = [];
		if (displayValue !== currentValue) {
			transactions.push({ changes: { from: 0, to: editorView.state.doc.length, insert: displayValue } });
		}
		transactions.push({ effects: editableCompartment.reconfigure(EditorView.editable.of(customizePayload)) });
		for (const t of transactions) {
			editorView.dispatch(t);
		}
	});

	// Cleanup on unmount
	$effect(() => {
		return () => {
			destroyEditor();
		};
	});

	function createEditor() {
		if (!editorContainer) return;

		const displayValue = customizePayload ? payloadTemplate : getDefaultTemplate(webhookUrl);

		editorView = new EditorView({
			parent: editorContainer,
			state: EditorState.create({
				doc: displayValue,
				extensions: [
					basicSetup,
					json(),
					oneDark,
					editableCompartment.of(EditorView.editable.of(customizePayload)),
					EditorView.updateListener.of((update) => {
						if (update.docChanged && customizePayload) {
							payloadTemplate = update.state.doc.toString();
						}
					}),
					EditorView.theme({
						'&': { fontSize: '12px', height: '100%' },
						'.cm-scroller': { overflow: 'auto', fontFamily: "'JetBrains Mono', 'Fira Code', monospace" },
						'.cm-content': { padding: '8px 0' },
						'&.cm-focused': { outline: 'none' },
					}),
				],
			}),
		});
	}

	function destroyEditor() {
		if (editorView) {
			editorView.destroy();
			editorView = null;
		}
	}

	function applyPreset(key: string) {
		const template = templatePresets[key];
		if (template) {
			payloadTemplate = template;
			if (editorView) {
				editorView.dispatch({
					changes: { from: 0, to: editorView.state.doc.length, insert: template }
				});
			}
		}
	}

	async function loadWebhooks() {
		try {
			loading = true;
			const request = create(ListWebhooksRequestSchema, { serverId: server.id });
			const response = await rpcClient.webhook.listWebhooks(request);
			webhooks = response.webhooks;
			if (response.templatePresets) {
				templatePresets = { ...response.templatePresets };
			}
		} catch (error) {
			toast.error('Failed to load webhooks');
		} finally {
			loading = false;
		}
	}

	function resetForm() {
		webhookName = '';
		webhookUrl = '';
		webhookSecret = '';
		webhookFormat = WebhookFormat.GENERIC;
		selectedEvents = [];
		payloadTemplate = '';
		customizePayload = false;
		maxRetries = 3;
		retryDelayMs = 1000;
		timeoutMs = 5000;
		selectedWebhook = null;
		showAdvanced = false;
	}

	function openCreateDialog() {
		resetForm();
		showCreateDialog = true;
	}

	function openEditDialog(webhook: WebhookType) {
		selectedWebhook = webhook;
		webhookName = webhook.name;
		webhookUrl = webhook.url;
		webhookSecret = '';
		webhookFormat = webhook.format;
		selectedEvents = [...webhook.events];
		payloadTemplate = webhook.payloadTemplate;
		customizePayload = !!webhook.payloadTemplate;
		maxRetries = webhook.maxRetries;
		retryDelayMs = webhook.retryDelayMs;
		timeoutMs = webhook.timeoutMs;
		showCreateDialog = true;
	}

	async function saveWebhook() {
		if (!webhookName.trim()) {
			toast.error('Webhook name is required');
			return;
		}
		if (!webhookUrl.trim()) {
			toast.error('Webhook URL is required');
			return;
		}
		if (selectedEvents.length === 0) {
			toast.error('At least one event must be selected');
			return;
		}

		creating = true;
		try {
			const effectiveFormat = isDiscordUrl(webhookUrl) ? WebhookFormat.DISCORD : WebhookFormat.GENERIC;
			const effectiveTemplate = customizePayload ? payloadTemplate : '';

			if (selectedWebhook) {
				const request = create(UpdateWebhookRequestSchema, {
					id: selectedWebhook.id,
					name: webhookName,
					url: webhookUrl,
					secret: webhookSecret || undefined,
					events: selectedEvents,
					format: effectiveFormat,
					payloadTemplate: effectiveTemplate,
					maxRetries: maxRetries,
					retryDelayMs: retryDelayMs,
					timeoutMs: timeoutMs,
				});
				await rpcClient.webhook.updateWebhook(request);
				toast.success('Webhook updated successfully');
			} else {
				const request = create(CreateWebhookRequestSchema, {
					serverId: server.id,
					name: webhookName,
					url: webhookUrl,
					secret: webhookSecret || undefined,
					events: selectedEvents,
					format: effectiveFormat,
					payloadTemplate: effectiveTemplate,
					maxRetries: maxRetries,
					retryDelayMs: retryDelayMs,
					timeoutMs: timeoutMs,
				});
				await rpcClient.webhook.createWebhook(request);
				toast.success('Webhook created successfully');
			}
			showCreateDialog = false;
			resetForm();
			await loadWebhooks();
		} catch (error: any) {
			toast.error(error.message || 'Failed to save webhook');
		} finally {
			creating = false;
		}
	}

	async function toggleWebhook(webhook: WebhookType) {
		try {
			const request = create(ToggleWebhookRequestSchema, { id: webhook.id, enabled: !webhook.enabled });
			await rpcClient.webhook.toggleWebhook(request);
			toast.success(`Webhook ${!webhook.enabled ? 'enabled' : 'disabled'}`);
			await loadWebhooks();
		} catch (error) {
			toast.error('Failed to toggle webhook');
		}
	}

	async function testWebhook(webhook: WebhookType) {
		testing = webhook.id;
		try {
			const request = create(TestWebhookRequestSchema, { id: webhook.id });
			const response = await rpcClient.webhook.testWebhook(request);
			if (response.success) {
				toast.success(`Test successful! Response: ${response.responseCode} (${response.durationMs}ms)`);
			} else {
				toast.error(`Test failed: ${response.errorMessage || `HTTP ${response.responseCode}`}`);
			}
		} catch (error: any) {
			toast.error(error.message || 'Failed to test webhook');
		} finally {
			testing = null;
		}
	}

	async function deleteWebhook(webhook: WebhookType) {
		if (!confirm(`Are you sure you want to delete the webhook "${webhook.name}"?`)) {
			return;
		}
		try {
			const request = create(DeleteWebhookRequestSchema, { id: webhook.id });
			await rpcClient.webhook.deleteWebhook(request);
			toast.success('Webhook deleted successfully');
			await loadWebhooks();
		} catch (error) {
			toast.error('Failed to delete webhook');
		}
	}

	function getEventLabel(eventType: WebhookEventType): string {
		return allEvents.find(e => e.type === eventType)?.label || 'Unknown';
	}

	function getFormatLabel(format: WebhookFormat | number): string {
		const val = Number(format);
		if (val === WebhookFormat.GENERIC) return 'Generic JSON';
		if (val === WebhookFormat.DISCORD) return 'Discord';
		return 'Unknown';
	}

	function toggleEvent(eventType: WebhookEventType) {
		if (selectedEvents.includes(eventType)) {
			selectedEvents = selectedEvents.filter(e => e !== eventType);
		} else {
			selectedEvents = [...selectedEvents, eventType];
		}
	}

	function copyUrl(url: string) {
		navigator.clipboard.writeText(url);
		toast.success('URL copied to clipboard');
	}
</script>

<Card class="border-border/50 shadow-sm">
	<CardHeader class="flex flex-row items-center justify-between pb-4">
		<div>
			<CardTitle class="text-xl flex items-center gap-2">
				<Webhook class="h-5 w-5" />
				Webhooks
			</CardTitle>
			<CardDescription>Configure webhook notifications for server events</CardDescription>
		</div>
		<Button onclick={openCreateDialog} size="sm">
			<Plus class="h-4 w-4 mr-2" />
			Add Webhook
		</Button>
	</CardHeader>
	<CardContent>
		{#if loading}
			<div class="flex items-center justify-center py-8">
				<Loader2 class="h-6 w-6 animate-spin text-muted-foreground" />
			</div>
		{:else if webhooks.length === 0}
			<div class="text-center py-8 text-muted-foreground">
				<Webhook class="h-12 w-12 mx-auto mb-4 opacity-50" />
				<p>No webhooks configured</p>
				<p class="text-sm mt-1">Add a webhook to receive notifications for server events</p>
			</div>
		{:else}
			<div class="space-y-4">
				{#each webhooks as webhook}
					<div class="flex items-center justify-between p-4 rounded-lg border border-border/50 bg-muted/20 hover:bg-muted/40 transition-colors">
						<div class="flex-1 min-w-0">
							<div class="flex items-center gap-2 mb-1">
								<h4 class="font-medium truncate">{webhook.name}</h4>
								<Badge variant={webhook.enabled ? 'default' : 'secondary'} class="text-xs">
									{webhook.enabled ? 'Enabled' : 'Disabled'}
								</Badge>
								<Badge variant="outline" class="text-xs">
									{webhook.payloadTemplate ? 'Custom' : (isDiscordUrl(webhook.url) ? 'Discord' : getFormatLabel(webhook.format))}
								</Badge>
							</div>
							<div class="flex items-center gap-2 text-sm text-muted-foreground">
								<span class="truncate max-w-[300px] font-mono text-xs">{webhook.url}</span>
								<Button variant="ghost" size="icon" class="h-5 w-5" onclick={() => copyUrl(webhook.url)}>
									<Copy class="h-3 w-3" />
								</Button>
							</div>
							<div class="flex flex-wrap gap-1 mt-2">
								{#each webhook.events as event}
									<Badge variant="outline" class="text-xs px-1.5 py-0">
										{getEventLabel(event)}
									</Badge>
								{/each}
							</div>
						</div>
						<div class="flex items-center gap-2 ml-4">
							<Switch
								checked={webhook.enabled}
								onCheckedChange={() => toggleWebhook(webhook)}
							/>
							<Button
								variant="ghost"
								size="icon"
								onclick={() => testWebhook(webhook)}
								disabled={testing === webhook.id}
								title="Test webhook"
							>
								{#if testing === webhook.id}
									<Loader2 class="h-4 w-4 animate-spin" />
								{:else}
									<Play class="h-4 w-4" />
								{/if}
							</Button>
							<Button
								variant="ghost"
								size="icon"
								onclick={() => openEditDialog(webhook)}
								title="Edit webhook"
							>
								<Edit2 class="h-4 w-4" />
							</Button>
							<Button
								variant="ghost"
								size="icon"
								onclick={() => deleteWebhook(webhook)}
								title="Delete webhook"
								class="text-destructive hover:text-destructive"
							>
								<Trash2 class="h-4 w-4" />
							</Button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</CardContent>
</Card>

<!-- Create/Edit Dialog -->
<Dialog.Root bind:open={showCreateDialog}>
	<Dialog.Content class="sm:max-w-[1060px]">
		<Dialog.Header>
			<Dialog.Title>{selectedWebhook ? 'Edit Webhook' : 'Create Webhook'}</Dialog.Title>
			<Dialog.Description>
				{selectedWebhook ? 'Update your webhook configuration' : 'Configure a new webhook to receive server event notifications'}
			</Dialog.Description>
		</Dialog.Header>

		<div class="flex gap-6 py-4">
			<!-- Left: Configuration -->
			<div class="flex-1 space-y-4 min-w-0">
				<div class="space-y-2">
					<Label for="name">Name</Label>
					<Input id="name" bind:value={webhookName} placeholder="My Webhook" />
				</div>

				<div class="space-y-2">
					<Label for="url">URL</Label>
					<Input id="url" bind:value={webhookUrl} placeholder={isDiscordUrl(webhookUrl) ? 'https://discord.com/api/webhooks/...' : 'https://example.com/webhook'} />
				</div>

				<div class="space-y-2">
					<Label>Events</Label>
					<div class="grid grid-cols-2 gap-2 p-3 rounded-lg border border-border/50 bg-muted/20">
						{#each allEvents as event}
							<label class="flex items-center gap-2 cursor-pointer hover:bg-muted/40 p-2 rounded">
								<Checkbox
									checked={selectedEvents.includes(event.type)}
									onCheckedChange={() => toggleEvent(event.type)}
								/>
								<div>
									<span class="text-sm font-medium">{event.label}</span>
									<p class="text-xs text-muted-foreground">{event.description}</p>
								</div>
							</label>
						{/each}
					</div>
				</div>

				<!-- Advanced Settings -->
				<Collapsible.Root bind:open={showAdvanced}>
					<Collapsible.Trigger class="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors w-full py-2">
						<ChevronDown class="h-4 w-4 transition-transform {showAdvanced ? 'rotate-180' : ''}" />
						Advanced Settings
					</Collapsible.Trigger>
					<Collapsible.Content>
						<div class="space-y-4 pt-2">
							<div class="space-y-2">
								<Label for="secret">Secret (optional)</Label>
								<Input id="secret" type="password" bind:value={webhookSecret} placeholder={selectedWebhook?.hasSecret ? '(unchanged)' : 'HMAC signing secret'} />
								<p class="text-xs text-muted-foreground">Used to sign the webhook payload with HMAC-SHA256</p>
							</div>

							<div class="grid grid-cols-3 gap-4">
								<div class="space-y-2">
									<Label for="maxRetries">Max Retries</Label>
									<Input id="maxRetries" type="number" bind:value={maxRetries} min={0} max={10} />
								</div>
								<div class="space-y-2">
									<Label for="retryDelay">Retry Delay (ms)</Label>
									<Input id="retryDelay" type="number" bind:value={retryDelayMs} min={100} max={60000} />
								</div>
								<div class="space-y-2">
									<Label for="timeout">Timeout (ms)</Label>
									<Input id="timeout" type="number" bind:value={timeoutMs} min={1000} max={30000} />
								</div>
							</div>
						</div>
					</Collapsible.Content>
				</Collapsible.Root>
			</div>

			<!-- Right: Payload Template (always visible) -->
			<div class="w-[520px] shrink-0 flex flex-col gap-3 border-l border-border/50 pl-6">
				<div class="flex items-center justify-between">
					<div>
						<Label class="text-sm">Customize Payload</Label>
						<p class="text-xs text-muted-foreground mt-0.5">
							{#if customizePayload}
								Using custom payload template
							{:else}
								Using default {presetLabels[getDefaultPresetKey(webhookUrl)] || 'Generic'} preset
							{/if}
						</p>
					</div>
					<Switch
						checked={customizePayload}
						onCheckedChange={(checked) => {
							customizePayload = checked;
							if (checked && !payloadTemplate) {
								payloadTemplate = getDefaultTemplate(webhookUrl);
							}
						}}
					/>
				</div>

				<!-- Presets -->
				<div class="{!customizePayload ? 'opacity-40 pointer-events-none' : ''}">
					<p class="text-xs font-medium text-muted-foreground mb-1.5">Presets</p>
					<div class="flex flex-wrap gap-1">
						{#each Object.keys(presetLabels) as key}
							{#if templatePresets[key]}
								<Button variant="outline" size="sm" class="h-7 text-xs" onclick={() => applyPreset(key)}>
									{presetLabels[key]}
								</Button>
							{/if}
						{/each}
					</div>
				</div>

				<!-- CodeMirror Editor -->
				<div
					bind:this={editorContainer}
					class="h-[300px] rounded-md border border-border/50 overflow-hidden {!customizePayload ? 'opacity-50 pointer-events-none' : ''}"
				></div>

				<!-- Variables reference (always visible) -->
				<div class="{!customizePayload ? 'opacity-40' : ''}">
					<p class="text-xs font-medium text-muted-foreground mb-1">Available variables</p>
					<div class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs text-muted-foreground font-mono p-2 rounded border border-border/50 bg-muted/20">
						{#each [
							['{{.event}}', 'Event name'],
							['{{.timestamp}}', 'ISO 8601 timestamp'],
							['{{.title}}', 'Event title'],
							['{{.color}}', 'Color (int, for Discord)'],
							['{{.server_id}}', 'Server ID'],
							['{{.server_name}}', 'Server name'],
							['{{.server_status}}', 'Server status'],
							['{{.server_mc_version}}', 'MC version'],
							['{{.server_mod_loader}}', 'Mod loader'],
							['{{.server_players}}', 'Player count'],
							['{{.server_max_players}}', 'Max players'],
							['{{.server_port}}', 'Server port'],
						] as [variable, description]}
							<button
								class="text-left hover:text-foreground transition-colors cursor-pointer"
								title="Copy {variable}"
								onclick={() => { navigator.clipboard.writeText(variable); toast.success(`Copied ${variable}`); }}
							>{variable}</button>
							<span class="font-sans">{description}</span>
						{/each}
					</div>
				</div>
			</div>
		</div>

		<Dialog.Footer>
			<Button variant="outline" onclick={() => { showCreateDialog = false; resetForm(); }}>
				Cancel
			</Button>
			<Button onclick={saveWebhook} disabled={creating}>
				{#if creating}
					<Loader2 class="h-4 w-4 mr-2 animate-spin" />
				{/if}
				{selectedWebhook ? 'Update' : 'Create'}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
