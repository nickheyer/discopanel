<script lang="ts">
	import { onMount } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { Input } from '$lib/components/ui/input';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Switch } from '$lib/components/ui/switch';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as Select from '$lib/components/ui/select';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import { Loader2, Plus, Play, Pause, Trash2, Clock, CheckCircle2, XCircle, AlertCircle, RefreshCw, Terminal, RotateCcw, Square, Power, FileText, History, Pencil, Webhook as WebhookIcon, Zap, ChevronDown, Copy } from '@lucide/svelte';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { ScheduledTask, TaskExecution } from '$lib/proto/discopanel/v1/task_pb';
	import { TaskType, TaskStatus, ScheduleType, ExecutionStatus, TaskEventTrigger, CreateTaskRequestSchema, UpdateTaskRequestSchema, ToggleTaskRequestSchema, TriggerTaskRequestSchema, DeleteTaskRequestSchema, ListTasksRequestSchema, ListTaskExecutionsRequestSchema } from '$lib/proto/discopanel/v1/task_pb';
	import { create } from '@bufbuild/protobuf';
	import { EditorView } from '@codemirror/view';
	import { EditorState, Compartment } from '@codemirror/state';
	import { json } from '@codemirror/lang-json';
	import { oneDark } from '@codemirror/theme-one-dark';
	import { basicSetup } from 'codemirror';

	let { server, active }: { server: Server, active?: boolean } = $props();

	let loading = $state(true);
	let tasks = $state<ScheduledTask[]>([]);
	let initialized = $state(false);
	let previousServerId = $state(server.id);

	// Dialog state
	let showCreateDialog = $state(false);
	let showHistoryDialog = $state(false);
	let selectedTask = $state<ScheduledTask | null>(null);
	let taskHistory = $state<TaskExecution[]>([]);
	let historyLoading = $state(false);
	let creating = $state(false);

	// Form state — common
	let taskName = $state('');
	let taskDescription = $state('');
	let taskType = $state<TaskType>(TaskType.COMMAND);
	let scheduleType = $state<ScheduleType>(ScheduleType.CRON);
	let cronExpr = $state('');
	let intervalSecs = $state(3600);
	let runAt = $state('');
	let timezone = $state('UTC');
	let timeout = $state(300);
	let requireOnline = $state(true);
	let taskConfig = $state('');
	let eventTriggers = $state<TaskEventTrigger[]>([TaskEventTrigger.SERVER_START]);

	// Form state — webhook
	let webhookUrl = $state('');
	let webhookSecret = $state('');
	let payloadTemplate = $state('');
	let customizePayload = $state(false);
	let webhookMaxRetries = $state(3);
	let webhookRetryDelayMs = $state(1000);
	let webhookTimeoutMs = $state(5000);
	let showWebhookAdvanced = $state(false);
	let originalWebhookHasSecret = $state(false); // for placeholder display when editing

	// Webhook template presets fetched from backend
	let webhookTemplatePresets = $state<Record<string, string>>({});

	// CodeMirror editor for payload template
	let editorContainer = $state<HTMLDivElement>();
	let editorView = $state<EditorView | null>(null);
	const editableCompartment = new Compartment();

	const presetLabels: Record<string, string> = {
		discord: 'Discord',
		slack: 'Slack',
		teams: 'Teams',
		ntfy: 'ntfy',
		generic: 'Generic',
	};

	function isDiscordUrl(url: string): boolean {
		return url.includes('discord.com/api/webhooks') || url.includes('discordapp.com/api/webhooks');
	}

	function getDefaultPresetKey(url: string): string {
		return isDiscordUrl(url) ? 'discord' : 'generic';
	}

	function getDefaultTemplate(url: string): string {
		return webhookTemplatePresets[getDefaultPresetKey(url)] || '';
	}

	onMount(() => {
		if (server && !initialized) {
			initialized = true;
			loadTasks();
		}
	});

	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			loading = true;
			tasks = [];
			initialized = false;
			loadTasks();
		}
	});

	$effect(() => {
		if (server && !initialized && active) {
			initialized = true;
			loadTasks();
		}
	});

	// Create/destroy editor when dialog opens with a webhook task
	$effect(() => {
		if (showCreateDialog && taskType === TaskType.WEBHOOK && editorContainer && !editorView) {
			createEditor();
		}
		if ((!showCreateDialog || taskType !== TaskType.WEBHOOK) && editorView) {
			destroyEditor();
		}
	});

	// Sync editor content/editable when toggle or URL changes
	$effect(() => {
		if (!editorView) return;
		const displayValue = customizePayload ? payloadTemplate : getDefaultTemplate(webhookUrl);
		const currentValue = editorView.state.doc.toString();
		const transactions: any[] = [];
		if (displayValue !== currentValue) {
			transactions.push({ changes: { from: 0, to: editorView.state.doc.length, insert: displayValue } });
		}
		transactions.push({ effects: editableCompartment.reconfigure(EditorView.editable.of(customizePayload)) });
		for (const t of transactions) editorView.dispatch(t);
	});

	$effect(() => {
		return () => destroyEditor();
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

	function toggleEventTrigger(trigger: TaskEventTrigger) {
		if (eventTriggers.includes(trigger)) {
			eventTriggers = eventTriggers.filter((t) => t !== trigger);
		} else {
			eventTriggers = [...eventTriggers, trigger];
		}
	}

	function applyPreset(key: string) {
		const template = webhookTemplatePresets[key];
		if (template) {
			payloadTemplate = template;
			if (editorView) {
				editorView.dispatch({
					changes: { from: 0, to: editorView.state.doc.length, insert: template },
				});
			}
		}
	}

	async function loadTasks() {
		try {
			loading = true;
			const request = create(ListTasksRequestSchema, { serverId: server.id });
			const response = await rpcClient.task.listTasks(request);
			tasks = response.tasks;
			if (response.webhookTemplatePresets) {
				webhookTemplatePresets = { ...response.webhookTemplatePresets };
			}
		} catch (error) {
			toast.error('Failed to load tasks');
		} finally {
			loading = false;
		}
	}

	function resetForm() {
		taskName = '';
		taskDescription = '';
		taskType = TaskType.COMMAND;
		scheduleType = ScheduleType.CRON;
		cronExpr = '';
		intervalSecs = 3600;
		runAt = '';
		timezone = 'UTC';
		timeout = 300;
		requireOnline = true;
		taskConfig = '';
		eventTriggers = [TaskEventTrigger.SERVER_START];
		webhookUrl = '';
		webhookSecret = '';
		payloadTemplate = '';
		customizePayload = false;
		webhookMaxRetries = 3;
		webhookRetryDelayMs = 1000;
		webhookTimeoutMs = 5000;
		showWebhookAdvanced = false;
		originalWebhookHasSecret = false;
		selectedTask = null;
	}

	function openCreateDialog() {
		resetForm();
		showCreateDialog = true;
	}

	function openCreateWebhookDialog() {
		resetForm();
		taskType = TaskType.WEBHOOK;
		scheduleType = ScheduleType.EVENT;
		requireOnline = false;
		timeout = 60;
		showCreateDialog = true;
	}

	function openEditDialog(task: ScheduledTask) {
		selectedTask = task;
		taskName = task.name;
		taskDescription = task.description;
		taskType = task.taskType;
		scheduleType = task.schedule;
		cronExpr = task.cronExpr;
		intervalSecs = task.intervalSecs;
		if (task.runAt) {
			runAt = new Date(Number(task.runAt.seconds) * 1000).toISOString().slice(0, 16);
		}
		timezone = task.timezone || 'UTC';
		timeout = task.timeout;
		requireOnline = task.requireOnline;
		taskConfig = task.config;
		eventTriggers = task.eventTriggers && task.eventTriggers.length > 0 ? [...task.eventTriggers] : [TaskEventTrigger.SERVER_START];

		// Webhook-specific: parse JSON config
		if (task.taskType === TaskType.WEBHOOK) {
			try {
				const cfg = JSON.parse(task.config || '{}');
				webhookUrl = cfg.url || '';
				webhookSecret = '';
				originalWebhookHasSecret = !!cfg.secret;
				payloadTemplate = cfg.payload_template || '';
				customizePayload = !!cfg.payload_template;
				webhookMaxRetries = cfg.max_retries ?? 3;
				webhookRetryDelayMs = cfg.retry_delay_ms ?? 1000;
				webhookTimeoutMs = cfg.timeout_ms ?? 5000;
			} catch {
				// Invalid config — leave defaults from resetForm
			}
		}
		showCreateDialog = true;
	}

	function buildWebhookConfig(): string {
		// Format is only consulted by the backend when no custom template is set;
		// derive it from the URL so the right preset (Discord/Slack/Teams/ntfy/generic) fires.
		const cfg: Record<string, any> = {
			url: webhookUrl,
			format: getDefaultPresetKey(webhookUrl),
			payload_template: customizePayload ? payloadTemplate : '',
			max_retries: webhookMaxRetries,
			retry_delay_ms: webhookRetryDelayMs,
			timeout_ms: webhookTimeoutMs,
		};
		// Preserve existing secret on edit unless user typed a new one
		if (webhookSecret) {
			cfg.secret = webhookSecret;
		} else if (selectedTask && originalWebhookHasSecret) {
			try {
				const prev = JSON.parse(selectedTask.config || '{}');
				if (prev.secret) cfg.secret = prev.secret;
			} catch {}
		}
		return JSON.stringify(cfg);
	}

	async function saveTask() {
		if (!taskName.trim()) {
			toast.error('Task name is required');
			return;
		}

		// Validate by task type
		if (taskType === TaskType.WEBHOOK) {
			if (!webhookUrl.trim()) {
				toast.error('Webhook URL is required');
				return;
			}
		}

		// Validate by schedule
		if (scheduleType === ScheduleType.CRON && !cronExpr.trim()) {
			toast.error('Cron expression is required');
			return;
		}
		if (scheduleType === ScheduleType.EVENT && eventTriggers.length === 0) {
			toast.error('At least one event trigger is required');
			return;
		}

		creating = true;
		try {
			let config = taskConfig;
			if (taskType === TaskType.WEBHOOK) {
				config = buildWebhookConfig();
			} else if (!config && taskType === TaskType.COMMAND) {
				config = JSON.stringify({ command: '' });
			}

			const isEventScheduled = scheduleType === ScheduleType.EVENT;
			if (selectedTask) {
				const request = create(UpdateTaskRequestSchema, {
					id: selectedTask.id,
					name: taskName,
					description: taskDescription,
					taskType: taskType,
					schedule: scheduleType,
					cronExpr: scheduleType === ScheduleType.CRON ? cronExpr : undefined,
					intervalSecs: scheduleType === ScheduleType.INTERVAL ? intervalSecs : undefined,
					timezone: timezone,
					config: config,
					timeout: timeout,
					requireOnline: requireOnline,
					eventTriggers: isEventScheduled ? eventTriggers : [],
					clearEventTriggers: !isEventScheduled,
				});
				await rpcClient.task.updateTask(request);
				toast.success('Task updated successfully');
			} else {
				const request = create(CreateTaskRequestSchema, {
					serverId: server.id,
					name: taskName,
					description: taskDescription,
					taskType: taskType,
					schedule: scheduleType,
					cronExpr: scheduleType === ScheduleType.CRON ? cronExpr : undefined,
					intervalSecs: scheduleType === ScheduleType.INTERVAL ? intervalSecs : 0,
					timezone: timezone,
					config: config,
					timeout: timeout,
					requireOnline: requireOnline,
					eventTriggers: isEventScheduled ? eventTriggers : [],
				});
				await rpcClient.task.createTask(request);
				toast.success('Task created successfully');
			}
			showCreateDialog = false;
			resetForm();
			await loadTasks();
		} catch (error: any) {
			toast.error(error.message || 'Failed to save task');
		} finally {
			creating = false;
		}
	}

	async function toggleTask(task: ScheduledTask) {
		try {
			const newStatus = task.status === TaskStatus.ENABLED ? TaskStatus.DISABLED : TaskStatus.ENABLED;
			const request = create(ToggleTaskRequestSchema, { id: task.id, status: newStatus });
			await rpcClient.task.toggleTask(request);
			toast.success(`Task ${newStatus === TaskStatus.ENABLED ? 'enabled' : 'disabled'}`);
			await loadTasks();
		} catch (error) {
			toast.error('Failed to toggle task');
		}
	}

	async function triggerTask(task: ScheduledTask) {
		try {
			const request = create(TriggerTaskRequestSchema, { id: task.id });
			await rpcClient.task.triggerTask(request);
			toast.success('Task triggered successfully');
			await loadTasks();
		} catch (error: any) {
			toast.error(error.message || 'Failed to trigger task');
		}
	}

	async function deleteTask(task: ScheduledTask) {
		if (!confirm(`Are you sure you want to delete the task "${task.name}"?`)) return;
		try {
			const request = create(DeleteTaskRequestSchema, { id: task.id });
			await rpcClient.task.deleteTask(request);
			toast.success('Task deleted successfully');
			await loadTasks();
		} catch (error) {
			toast.error('Failed to delete task');
		}
	}

	async function viewHistory(task: ScheduledTask) {
		selectedTask = task;
		historyLoading = true;
		showHistoryDialog = true;
		try {
			const request = create(ListTaskExecutionsRequestSchema, { taskId: task.id, limit: 50 });
			const response = await rpcClient.task.listTaskExecutions(request);
			taskHistory = response.executions;
		} catch (error) {
			toast.error('Failed to load task history');
		} finally {
			historyLoading = false;
		}
	}

	function getTaskTypeLabel(type: TaskType): string {
		switch (type) {
			case TaskType.COMMAND: return 'Command';
			case TaskType.BACKUP: return 'Backup';
			case TaskType.RESTART: return 'Restart';
			case TaskType.START: return 'Start';
			case TaskType.STOP: return 'Stop';
			case TaskType.SCRIPT: return 'Script';
			case TaskType.WEBHOOK: return 'Webhook';
			default: return 'Unknown';
		}
	}

	function getTaskTypeIcon(type: TaskType) {
		switch (type) {
			case TaskType.COMMAND: return Terminal;
			case TaskType.BACKUP: return FileText;
			case TaskType.RESTART: return RotateCcw;
			case TaskType.START: return Power;
			case TaskType.STOP: return Square;
			case TaskType.SCRIPT: return FileText;
			case TaskType.WEBHOOK: return WebhookIcon;
			default: return Clock;
		}
	}

	function getScheduleTypeLabel(s: ScheduleType): string {
		switch (s) {
			case ScheduleType.CRON: return 'Cron Expression';
			case ScheduleType.INTERVAL: return 'Fixed Interval';
			case ScheduleType.ONCE: return 'Run Once';
			case ScheduleType.EVENT: return 'On Event';
			default: return 'Unknown';
		}
	}

	function getEventTriggerLabel(e: TaskEventTrigger): string {
		switch (e) {
			case TaskEventTrigger.SERVER_START: return 'Server Start';
			case TaskEventTrigger.SERVER_STOP: return 'Server Stop';
			case TaskEventTrigger.SERVER_RESTART: return 'Server Restart';
			default: return 'Unknown';
		}
	}

	function getScheduleLabel(task: ScheduledTask): string {
		switch (task.schedule) {
			case ScheduleType.CRON: return `Cron: ${task.cronExpr}`;
			case ScheduleType.INTERVAL: return `Every ${formatInterval(task.intervalSecs)}`;
			case ScheduleType.ONCE: return task.runAt ? `Once at ${new Date(Number(task.runAt.seconds) * 1000).toLocaleString()}` : 'Once';
			case ScheduleType.EVENT: return `On ${(task.eventTriggers || []).map(getEventTriggerLabel).join(', ') || 'none'}`;
			default: return 'Unknown';
		}
	}

	function formatInterval(seconds: number): string {
		if (seconds < 60) return `${seconds}s`;
		if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
		if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
		return `${Math.floor(seconds / 86400)}d`;
	}

	function getExecutionStatusBadge(status: ExecutionStatus) {
		switch (status) {
			case ExecutionStatus.COMPLETED:
				return { variant: 'default' as const, icon: CheckCircle2, class: 'bg-green-500/10 text-green-500 border-green-500/20' };
			case ExecutionStatus.FAILED:
				return { variant: 'destructive' as const, icon: XCircle, class: '' };
			case ExecutionStatus.RUNNING:
				return { variant: 'secondary' as const, icon: Loader2, class: 'animate-pulse' };
			case ExecutionStatus.SKIPPED:
				return { variant: 'outline' as const, icon: AlertCircle, class: '' };
			case ExecutionStatus.TIMEOUT:
				return { variant: 'destructive' as const, icon: Clock, class: '' };
			case ExecutionStatus.CANCELLED:
				return { variant: 'outline' as const, icon: XCircle, class: '' };
			default:
				return { variant: 'outline' as const, icon: Clock, class: '' };
		}
	}

	function getExecutionStatusLabel(status: ExecutionStatus): string {
		switch (status) {
			case ExecutionStatus.PENDING: return 'Pending';
			case ExecutionStatus.RUNNING: return 'Running';
			case ExecutionStatus.COMPLETED: return 'Completed';
			case ExecutionStatus.FAILED: return 'Failed';
			case ExecutionStatus.SKIPPED: return 'Skipped';
			case ExecutionStatus.CANCELLED: return 'Cancelled';
			case ExecutionStatus.TIMEOUT: return 'Timeout';
			default: return 'Unknown';
		}
	}

	function formatDuration(ms: bigint): string {
		const seconds = Number(ms) / 1000;
		if (seconds < 1) return `${Number(ms)}ms`;
		if (seconds < 60) return `${seconds.toFixed(1)}s`;
		return `${Math.floor(seconds / 60)}m ${Math.floor(seconds % 60)}s`;
	}

	function formatNextRun(task: ScheduledTask): string {
		if (!task.nextRun) return 'Not scheduled';
		const date = new Date(Number(task.nextRun.seconds) * 1000);
		const now = new Date();
		const diff = date.getTime() - now.getTime();
		if (diff < 0) return 'Overdue';
		if (diff < 60000) return 'Less than a minute';
		if (diff < 3600000) return `${Math.floor(diff / 60000)}m`;
		if (diff < 86400000) return `${Math.floor(diff / 3600000)}h`;
		return date.toLocaleString();
	}

	function getWebhookUrlForDisplay(task: ScheduledTask): string {
		if (task.taskType !== TaskType.WEBHOOK) return '';
		try {
			const cfg = JSON.parse(task.config || '{}');
			return cfg.url || '';
		} catch {
			return '';
		}
	}

	function copyText(text: string) {
		navigator.clipboard.writeText(text);
		toast.success('Copied to clipboard');
	}
</script>

{#if loading}
	<div class="flex items-center justify-center py-8">
		<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
	</div>
{:else}
	<div class="space-y-4">
		<!-- Header -->
		<div class="flex items-center justify-between">
			<div>
				<h3 class="text-lg font-semibold">Tasks</h3>
				<p class="text-sm text-muted-foreground">Schedule operations or run them on server events (including webhooks).</p>
			</div>
			<div class="flex gap-2">
				<Button variant="outline" size="sm" onclick={loadTasks}>
					<RefreshCw class="h-4 w-4 mr-2" />
					Refresh
				</Button>
				<Button variant="outline" size="sm" onclick={openCreateWebhookDialog}>
					<WebhookIcon class="h-4 w-4 mr-2" />
					New Webhook
				</Button>
				<Button size="sm" onclick={openCreateDialog}>
					<Plus class="h-4 w-4 mr-2" />
					New Task
				</Button>
			</div>
		</div>

		<!-- Tasks List -->
		{#if tasks.length === 0}
			<Card>
				<CardContent class="py-8">
					<div class="text-center space-y-4">
						<Clock class="h-12 w-12 mx-auto text-muted-foreground/50" />
						<div>
							<p class="text-lg font-medium">No tasks yet</p>
							<p class="text-sm text-muted-foreground">Create a scheduled task or a webhook to react to server events.</p>
						</div>
						<div class="flex gap-2 justify-center">
							<Button variant="outline" onclick={openCreateWebhookDialog}>
								<WebhookIcon class="h-4 w-4 mr-2" />
								New Webhook
							</Button>
							<Button onclick={openCreateDialog}>
								<Plus class="h-4 w-4 mr-2" />
								New Task
							</Button>
						</div>
					</div>
				</CardContent>
			</Card>
		{:else}
			<div class="space-y-3">
				{#each tasks as task (task.id)}
					{@const TaskIcon = getTaskTypeIcon(task.taskType)}
					{@const webhookUrlDisplay = getWebhookUrlForDisplay(task)}
					<Card class="hover:shadow-md transition-shadow">
						<CardContent class="p-4">
							<div class="flex items-start gap-4">
								<div class="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center flex-shrink-0">
									<TaskIcon class="h-5 w-5 text-primary" />
								</div>
								<div class="flex-1 min-w-0">
									<div class="flex items-center gap-2 mb-1">
										<h4 class="font-medium truncate">{task.name}</h4>
										<Badge variant={task.status === TaskStatus.ENABLED ? 'default' : 'secondary'} class="text-xs">
											{task.status === TaskStatus.ENABLED ? 'Enabled' : 'Disabled'}
										</Badge>
										<Badge variant="outline" class="text-xs">
											{getTaskTypeLabel(task.taskType)}
										</Badge>
										{#if task.schedule === ScheduleType.EVENT}
											{#each task.eventTriggers as trigger}
												<Badge variant="outline" class="text-xs flex items-center gap-1">
													<Zap class="h-3 w-3" />
													{getEventTriggerLabel(trigger)}
												</Badge>
											{/each}
										{/if}
									</div>
									{#if task.description}
										<p class="text-sm text-muted-foreground mb-2 truncate">{task.description}</p>
									{/if}
									{#if webhookUrlDisplay}
										<div class="flex items-center gap-2 text-xs text-muted-foreground mb-2">
											<span class="font-mono truncate max-w-[400px]">{webhookUrlDisplay}</span>
											<Button variant="ghost" size="icon" class="h-5 w-5" onclick={() => copyText(webhookUrlDisplay)}>
												<Copy class="h-3 w-3" />
											</Button>
										</div>
									{/if}
									<div class="flex flex-wrap gap-2 text-xs text-muted-foreground">
										<span class="flex items-center gap-1">
											<Clock class="h-3 w-3" />
											{getScheduleLabel(task)}
										</span>
										{#if task.nextRun && task.status === TaskStatus.ENABLED && task.schedule !== ScheduleType.EVENT}
											<span class="flex items-center gap-1">
												Next: {formatNextRun(task)}
											</span>
										{/if}
										{#if task.lastRun}
											<span>
												Last: {new Date(Number(task.lastRun.seconds) * 1000).toLocaleString()}
											</span>
										{/if}
									</div>
								</div>
								<div class="flex items-center gap-1 flex-shrink-0">
									<Button variant="ghost" size="icon" onclick={() => viewHistory(task)} title="View History">
										<History class="h-4 w-4" />
									</Button>
									<Button
										variant="ghost"
										size="icon"
										onclick={() => triggerTask(task)}
										title="Run Now"
										disabled={task.status !== TaskStatus.ENABLED}
									>
										<Play class="h-4 w-4" />
									</Button>
									<Button variant="ghost" size="icon" onclick={() => toggleTask(task)} title={task.status === TaskStatus.ENABLED ? 'Disable' : 'Enable'}>
										{#if task.status === TaskStatus.ENABLED}
											<Pause class="h-4 w-4" />
										{:else}
											<Play class="h-4 w-4" />
										{/if}
									</Button>
									<Button variant="ghost" size="icon" onclick={() => openEditDialog(task)} title="Edit">
										<Pencil class="h-4 w-4" />
									</Button>
									<Button variant="ghost" size="icon" class="text-destructive hover:text-destructive" onclick={() => deleteTask(task)} title="Delete">
										<Trash2 class="h-4 w-4" />
									</Button>
								</div>
							</div>
						</CardContent>
					</Card>
				{/each}
			</div>
		{/if}
	</div>

	<!-- Create/Edit Dialog -->
	<Dialog.Root bind:open={showCreateDialog}>
		<Dialog.Content class={taskType === TaskType.WEBHOOK ? 'sm:max-w-[1060px]' : 'max-w-lg max-h-[90vh] overflow-y-auto'}>
			<Dialog.Header>
				<Dialog.Title>{selectedTask ? 'Edit Task' : 'Create New Task'}</Dialog.Title>
				<Dialog.Description>
					{selectedTask ? 'Update the task configuration' : 'Configure a scheduled or event-triggered task for your server'}
				</Dialog.Description>
			</Dialog.Header>

			{#if taskType === TaskType.WEBHOOK}
				<!-- Two-column webhook layout -->
				<div class="flex gap-6 py-4">
					<!-- Left: Configuration -->
					<div class="flex-1 space-y-4 min-w-0">
						<div class="space-y-2">
							<Label for="taskName">Name</Label>
							<Input id="taskName" bind:value={taskName} placeholder="My Webhook" />
						</div>

						<div class="space-y-2">
							<Label for="taskDescription">Description (optional)</Label>
							<Input id="taskDescription" bind:value={taskDescription} placeholder="Notify Discord on server start" />
						</div>

						<div class="space-y-2">
							<Label>Task Type</Label>
							<Select.Root type="single" name="taskType" value={taskType.toString()} onValueChange={(v) => { if (v) taskType = parseInt(v) as TaskType; }}>
								<Select.Trigger class="w-full">{getTaskTypeLabel(taskType)}</Select.Trigger>
								<Select.Content>
									<Select.Item value={TaskType.COMMAND.toString()} label="Command">Command</Select.Item>
									<Select.Item value={TaskType.RESTART.toString()} label="Restart Server">Restart Server</Select.Item>
									<Select.Item value={TaskType.START.toString()} label="Start Server">Start Server</Select.Item>
									<Select.Item value={TaskType.STOP.toString()} label="Stop Server">Stop Server</Select.Item>
									<Select.Item value={TaskType.SCRIPT.toString()} label="Script">Script</Select.Item>
									<Select.Item value={TaskType.WEBHOOK.toString()} label="Webhook">Webhook</Select.Item>
									<Select.Item value={TaskType.BACKUP.toString()} label="Backup (Coming Soon)" disabled>Backup (Coming Soon)</Select.Item>
								</Select.Content>
							</Select.Root>
						</div>

						<div class="space-y-2">
							<Label for="url">URL</Label>
							<Input id="url" bind:value={webhookUrl} placeholder={isDiscordUrl(webhookUrl) ? 'https://discord.com/api/webhooks/...' : 'https://example.com/webhook'} />
						</div>

						<div class="space-y-2">
							<Label>Trigger</Label>
							<Select.Root type="single" name="scheduleType" value={scheduleType.toString()} onValueChange={(v) => { if (v) scheduleType = parseInt(v) as ScheduleType; }}>
								<Select.Trigger class="w-full">{getScheduleTypeLabel(scheduleType)}</Select.Trigger>
								<Select.Content>
									<Select.Item value={ScheduleType.EVENT.toString()} label="On Event">On Event</Select.Item>
									<Select.Item value={ScheduleType.CRON.toString()} label="Cron Expression">Cron Expression</Select.Item>
									<Select.Item value={ScheduleType.INTERVAL.toString()} label="Fixed Interval">Fixed Interval</Select.Item>
									<Select.Item value={ScheduleType.ONCE.toString()} label="Run Once">Run Once</Select.Item>
								</Select.Content>
							</Select.Root>
						</div>

						{#if scheduleType === ScheduleType.EVENT}
							<div class="space-y-2">
								<Label>Events</Label>
								<div class="grid grid-cols-1 gap-2 p-3 rounded-lg border border-border/50 bg-muted/20">
									{#each [
										{ trigger: TaskEventTrigger.SERVER_START, label: 'Server Start', description: 'When the server starts' },
										{ trigger: TaskEventTrigger.SERVER_STOP, label: 'Server Stop', description: 'When the server stops' },
										{ trigger: TaskEventTrigger.SERVER_RESTART, label: 'Server Restart', description: 'When the server restarts' },
									] as { trigger, label, description }}
										<label class="flex items-center gap-2 cursor-pointer hover:bg-muted/40 p-2 rounded">
											<Checkbox
												checked={eventTriggers.includes(trigger)}
												onCheckedChange={() => toggleEventTrigger(trigger)}
											/>
											<div>
												<span class="text-sm font-medium">{label}</span>
												<p class="text-xs text-muted-foreground">{description}</p>
											</div>
										</label>
									{/each}
								</div>
							</div>
						{:else if scheduleType === ScheduleType.CRON}
							<div class="space-y-2">
								<Label for="cronExpr">Cron Expression</Label>
								<Input id="cronExpr" bind:value={cronExpr} placeholder="0 0 * * *" />
								<p class="text-xs text-muted-foreground">Format: minute hour day month weekday</p>
							</div>
						{:else if scheduleType === ScheduleType.INTERVAL}
							<div class="space-y-2">
								<Label for="intervalSecs">Interval (seconds)</Label>
								<Input id="intervalSecs" type="number" bind:value={intervalSecs} min={60} />
								<p class="text-xs text-muted-foreground">Minimum 60 seconds. Current: {formatInterval(intervalSecs)}</p>
							</div>
						{:else if scheduleType === ScheduleType.ONCE}
							<div class="space-y-2">
								<Label for="runAt">Run At</Label>
								<Input id="runAt" type="datetime-local" bind:value={runAt} />
							</div>
						{/if}

						<!-- Advanced webhook settings -->
						<Collapsible.Root bind:open={showWebhookAdvanced}>
							<Collapsible.Trigger class="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors w-full py-2">
								<ChevronDown class="h-4 w-4 transition-transform {showWebhookAdvanced ? 'rotate-180' : ''}" />
								Advanced Settings
							</Collapsible.Trigger>
							<Collapsible.Content>
								<div class="space-y-4 pt-2">
									<div class="space-y-2">
										<Label for="secret">Secret (optional)</Label>
										<Input id="secret" type="password" bind:value={webhookSecret} placeholder={originalWebhookHasSecret ? '(unchanged)' : 'HMAC signing secret'} />
										<p class="text-xs text-muted-foreground">Used to sign the webhook payload with HMAC-SHA256</p>
									</div>
									<div class="grid grid-cols-3 gap-4">
										<div class="space-y-2">
											<Label for="maxRetries">Max Retries</Label>
											<Input id="maxRetries" type="number" bind:value={webhookMaxRetries} min={0} max={10} />
										</div>
										<div class="space-y-2">
											<Label for="retryDelay">Retry Delay (ms)</Label>
											<Input id="retryDelay" type="number" bind:value={webhookRetryDelayMs} min={100} max={60000} />
										</div>
										<div class="space-y-2">
											<Label for="webhookTimeout">Timeout (ms)</Label>
											<Input id="webhookTimeout" type="number" bind:value={webhookTimeoutMs} min={1000} max={30000} />
										</div>
									</div>
								</div>
							</Collapsible.Content>
						</Collapsible.Root>
					</div>

					<!-- Right: Payload Template -->
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

						<div class={!customizePayload ? 'opacity-40 pointer-events-none' : ''}>
							<p class="text-xs font-medium text-muted-foreground mb-1.5">Presets</p>
							<div class="flex flex-wrap gap-1">
								{#each Object.keys(presetLabels) as key}
									{#if webhookTemplatePresets[key]}
										<Button variant="outline" size="sm" class="h-7 text-xs" onclick={() => applyPreset(key)}>
											{presetLabels[key]}
										</Button>
									{/if}
								{/each}
							</div>
						</div>

						<div
							bind:this={editorContainer}
							class="h-[300px] rounded-md border border-border/50 overflow-hidden {!customizePayload ? 'opacity-50 pointer-events-none' : ''}"
						></div>

						<div class={!customizePayload ? 'opacity-40' : ''}>
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
			{:else}
				<!-- Single-column scheduled task layout -->
				<div class="space-y-4 py-4">
					<div class="space-y-2">
						<Label for="taskName">Task Name</Label>
						<Input id="taskName" bind:value={taskName} placeholder="Daily Backup" />
					</div>

					<div class="space-y-2">
						<Label for="taskDescription">Description (optional)</Label>
						<Input id="taskDescription" bind:value={taskDescription} placeholder="Runs every day at midnight" />
					</div>

					<div class="space-y-2">
						<Label>Task Type</Label>
						<Select.Root type="single" name="taskType" value={taskType.toString()} onValueChange={(v) => { if (v) taskType = parseInt(v) as TaskType; }}>
							<Select.Trigger class="w-full">{getTaskTypeLabel(taskType)}</Select.Trigger>
							<Select.Content>
								<Select.Item value={TaskType.COMMAND.toString()} label="Command">Command</Select.Item>
								<Select.Item value={TaskType.RESTART.toString()} label="Restart Server">Restart Server</Select.Item>
								<Select.Item value={TaskType.START.toString()} label="Start Server">Start Server</Select.Item>
								<Select.Item value={TaskType.STOP.toString()} label="Stop Server">Stop Server</Select.Item>
								<Select.Item value={TaskType.SCRIPT.toString()} label="Script">Script</Select.Item>
								<Select.Item value={TaskType.WEBHOOK.toString()} label="Webhook">Webhook</Select.Item>
								<Select.Item value={TaskType.BACKUP.toString()} label="Backup (Coming Soon)" disabled>Backup (Coming Soon)</Select.Item>
							</Select.Content>
						</Select.Root>
					</div>

					{#if taskType === TaskType.COMMAND}
						<div class="space-y-2">
							<Label for="command">RCON Command</Label>
							<Input
								id="command"
								value={(() => { try { return JSON.parse(taskConfig || '{}').command || ''; } catch { return ''; } })()}
								oninput={(e) => taskConfig = JSON.stringify({ command: e.currentTarget.value })}
								placeholder="say Hello World!"
							/>
							<p class="text-xs text-muted-foreground">The command to execute via RCON</p>
						</div>
					{:else if taskType === TaskType.SCRIPT}
						<div class="space-y-2">
							<Label for="scriptPath">Script Path or Executable</Label>
							<Input
								id="scriptPath"
								value={(() => { try { return JSON.parse(taskConfig || '{}').script_path || ''; } catch { return ''; } })()}
								oninput={(e) => {
									const current = (() => { try { return JSON.parse(taskConfig || '{}'); } catch { return {}; } })();
									taskConfig = JSON.stringify({ ...current, script_path: e.currentTarget.value });
								}}
								placeholder="/data/scripts/backup.sh"
							/>
							<p class="text-xs text-muted-foreground">Path to the script/executable inside the container</p>
						</div>
						<div class="space-y-2">
							<Label for="scriptArgs">Arguments (optional)</Label>
							<Input
								id="scriptArgs"
								value={(() => { try { return (JSON.parse(taskConfig || '{}').args || []).join(' '); } catch { return ''; } })()}
								oninput={(e) => {
									const current = (() => { try { return JSON.parse(taskConfig || '{}'); } catch { return {}; } })();
									const args = e.currentTarget.value.split(' ').filter((a: string) => a.trim());
									taskConfig = JSON.stringify({ ...current, args });
								}}
								placeholder="--verbose --output /data/backups"
							/>
							<p class="text-xs text-muted-foreground">Space-separated arguments to pass to the script/executable</p>
						</div>
					{/if}

					<div class="space-y-2">
						<Label>Schedule Type</Label>
						<Select.Root type="single" name="scheduleType" value={scheduleType.toString()} onValueChange={(v) => { if (v) scheduleType = parseInt(v) as ScheduleType; }}>
							<Select.Trigger class="w-full">{getScheduleTypeLabel(scheduleType)}</Select.Trigger>
							<Select.Content>
								<Select.Item value={ScheduleType.CRON.toString()} label="Cron Expression">Cron Expression</Select.Item>
								<Select.Item value={ScheduleType.INTERVAL.toString()} label="Fixed Interval">Fixed Interval</Select.Item>
								<Select.Item value={ScheduleType.ONCE.toString()} label="Run Once">Run Once</Select.Item>
								<Select.Item value={ScheduleType.EVENT.toString()} label="On Event">On Event</Select.Item>
							</Select.Content>
						</Select.Root>
					</div>

					{#if scheduleType === ScheduleType.CRON}
						<div class="space-y-2">
							<Label for="cronExpr">Cron Expression</Label>
							<Input id="cronExpr" bind:value={cronExpr} placeholder="0 0 * * *" />
							<p class="text-xs text-muted-foreground">
								Format: minute hour day month weekday (e.g., "0 0 * * *" for daily at midnight)
							</p>
						</div>
					{:else if scheduleType === ScheduleType.INTERVAL}
						<div class="space-y-2">
							<Label for="intervalSecs">Interval (seconds)</Label>
							<Input id="intervalSecs" type="number" bind:value={intervalSecs} min={60} />
							<p class="text-xs text-muted-foreground">
								Minimum 60 seconds. Current: {formatInterval(intervalSecs)}
							</p>
						</div>
					{:else if scheduleType === ScheduleType.ONCE}
						<div class="space-y-2">
							<Label for="runAt">Run At</Label>
							<Input id="runAt" type="datetime-local" bind:value={runAt} />
						</div>
					{:else if scheduleType === ScheduleType.EVENT}
						<div class="space-y-2">
							<Label>Events</Label>
							<div class="grid grid-cols-1 gap-2 p-3 rounded-lg border border-border/50 bg-muted/20">
								{#each [
									{ trigger: TaskEventTrigger.SERVER_START, label: 'Server Start', description: 'When the server starts' },
									{ trigger: TaskEventTrigger.SERVER_STOP, label: 'Server Stop', description: 'When the server stops' },
									{ trigger: TaskEventTrigger.SERVER_RESTART, label: 'Server Restart', description: 'When the server restarts' },
								] as { trigger, label, description }}
									<label class="flex items-center gap-2 cursor-pointer hover:bg-muted/40 p-2 rounded">
										<Checkbox
											checked={eventTriggers.includes(trigger)}
											onCheckedChange={() => toggleEventTrigger(trigger)}
										/>
										<div>
											<span class="text-sm font-medium">{label}</span>
											<p class="text-xs text-muted-foreground">{description}</p>
										</div>
									</label>
								{/each}
							</div>
						</div>
					{/if}

					<div class="space-y-2">
						<Label for="timeout">Timeout (seconds)</Label>
						<Input id="timeout" type="number" bind:value={timeout} min={10} max={3600} />
						<p class="text-xs text-muted-foreground">Maximum execution time before task is cancelled</p>
					</div>

					<div class="flex items-center justify-between">
						<div>
							<Label for="requireOnline">Require Server Online</Label>
							<p class="text-xs text-muted-foreground">Skip task if server is offline</p>
						</div>
						<Switch id="requireOnline" bind:checked={requireOnline} />
					</div>
				</div>
			{/if}

			<Dialog.Footer>
				<Button variant="outline" onclick={() => { showCreateDialog = false; resetForm(); }}>Cancel</Button>
				<Button onclick={saveTask} disabled={creating}>
					{#if creating}
						<Loader2 class="h-4 w-4 mr-2 animate-spin" />
					{/if}
					{selectedTask ? 'Save Changes' : 'Create Task'}
				</Button>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>

	<!-- History Dialog -->
	<Dialog.Root bind:open={showHistoryDialog}>
		<Dialog.Content class="max-w-2xl max-h-[80vh] overflow-y-auto">
			<Dialog.Header>
				<Dialog.Title>Task History: {selectedTask?.name}</Dialog.Title>
				<Dialog.Description>Recent execution history for this task</Dialog.Description>
			</Dialog.Header>

			{#if historyLoading}
				<div class="flex items-center justify-center py-8">
					<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
				</div>
			{:else if taskHistory.length === 0}
				<div class="text-center py-8 text-muted-foreground">
					<History class="h-12 w-12 mx-auto mb-4 opacity-50" />
					<p>No execution history yet</p>
				</div>
			{:else}
				<div class="space-y-2 max-h-96 overflow-y-auto">
					{#each taskHistory as execution (execution.id)}
						{@const statusInfo = getExecutionStatusBadge(execution.status)}
						{@const StatusIcon = statusInfo.icon}
						<div class="p-3 rounded-lg border bg-card">
							<div class="flex items-start justify-between mb-2">
								<div class="flex items-center gap-2">
									<Badge variant={statusInfo.variant} class={statusInfo.class}>
										<StatusIcon class="h-3 w-3 mr-1 {execution.status === ExecutionStatus.RUNNING ? 'animate-spin' : ''}" />
										{getExecutionStatusLabel(execution.status)}
									</Badge>
									<span class="text-xs text-muted-foreground">
										{execution.trigger === 'manual' ? 'Manual' : execution.trigger === 'event' ? 'Event' : 'Scheduled'}
									</span>
								</div>
								{#if execution.duration}
									<span class="text-xs text-muted-foreground">{formatDuration(execution.duration)}</span>
								{/if}
							</div>
							<div class="text-xs text-muted-foreground mb-1">
								{new Date(Number(execution.startedAt?.seconds || 0) * 1000).toLocaleString()}
							</div>
							{#if execution.output}
								<div class="mt-2 p-2 bg-muted rounded text-xs font-mono whitespace-pre-wrap max-h-24 overflow-y-auto">
									{execution.output}
								</div>
							{/if}
							{#if execution.error}
								<div class="mt-2 p-2 bg-destructive/10 rounded text-xs text-destructive">
									{execution.error}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}

			<Dialog.Footer>
				<Button variant="outline" onclick={() => { showHistoryDialog = false; selectedTask = null; taskHistory = []; }}>Close</Button>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>
{/if}
