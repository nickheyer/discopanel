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
	import {
		Loader2,
		Plus,
		Play,
		Pause,
		Trash2,
		Clock,
		CheckCircle2,
		XCircle,
		AlertCircle,
		RefreshCw,
		Terminal,
		RotateCcw,
		Square,
		Power,
		FileText,
		History,
		Archive,
		Wrench,
		X,
		Pencil,
		Webhook as WebhookIcon,
		Zap,
		Copy
	} from '@lucide/svelte';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { ScheduledTask, TaskExecution } from '$lib/proto/discopanel/v1/task_pb';
	import {
		TaskType,
		TaskStatus,
		ScheduleType,
		ExecutionStatus,
		CreateTaskRequestSchema,
		UpdateTaskRequestSchema,
		ToggleTaskRequestSchema,
		TriggerTaskRequestSchema,
		DeleteTaskRequestSchema,
		ListTasksRequestSchema,
		ListTaskExecutionsRequestSchema
	} from '$lib/proto/discopanel/v1/task_pb';
	import { TriggeredEventType } from '$lib/proto/discopanel/v1/event_pb';
	import { SERVER_EVENT_TYPES, getEventTypeLabel } from '$lib/utils/events';
	import { create } from '@bufbuild/protobuf';
	import { timestampFromDate } from '@bufbuild/protobuf/wkt';
	import CodeEditor from '$lib/components/ui/code-editor.svelte';

	let { server, active }: { server: Server; active?: boolean } = $props();

	let loading = $state(true);
	let tasks = $state<ScheduledTask[]>([]);
	let initialized = $state(false);
	// svelte-ignore state_referenced_locally
	let previousServerId = $state(server.id);

	// Dialog state
	type DialogSection = 'general' | 'payload' | 'schedule' | 'advanced';
	let showCreateDialog = $state(false);
	let showHistoryDialog = $state(false);
	let selectedTask = $state<ScheduledTask | null>(null);
	let taskHistory = $state<TaskExecution[]>([]);
	let historyLoading = $state(false);
	let creating = $state(false);
	let activeSection = $state<DialogSection>('general');

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
	let retryCount = $state(0);
	let retryDelay = $state(60);
	let requireOnline = $state(true);

	// Type-specific config state
	let command = $state('');
	let scriptPath = $state('');
	let scriptArgs = $state('');
	let backupName = $state('');
	let backupPaths = $state('');
	let backupCompress = $state(true);
	let backupRetentionDays = $state(7);
	let backupMinBackups = $state(3);
	let backupMaxBackups = $state(0);

	const dialogSections = $derived<
		{
			id: DialogSection;
			label: string;
			icon: typeof FileText;
			title: string;
			description: string;
		}[]
	>([
		{
			id: 'general',
			label: 'General',
			icon: FileText,
			title: 'General',
			description: 'Task name, type, and configuration'
		},
		...(taskType === TaskType.WEBHOOK
			? [
					{
						id: 'payload' as DialogSection,
						label: 'Payload',
						icon: WebhookIcon,
						title: 'Payload',
						description: 'Customize the request body sent to the webhook'
					}
				]
			: []),
		{
			id: 'schedule',
			label: 'Schedule',
			icon: Clock,
			title: 'Schedule',
			description: 'When and how often the task runs'
		},
		{
			id: 'advanced',
			label: 'Advanced',
			icon: Wrench,
			title: 'Advanced',
			description: 'Timeouts, retries, and execution conditions'
		}
	]);

	const currentSection = $derived(
		dialogSections.find((s) => s.id === activeSection) ?? dialogSections[0]
	);
	const DialogTaskIcon = $derived(getTaskTypeIcon(taskType));

	// Keep the active section valid when the available sections change (e.g. switching task type)
	$effect(() => {
		if (!dialogSections.some((s) => s.id === activeSection)) {
			activeSection = 'general';
		}
	});

	let taskConfig = $state('');
	let eventTriggers = $state<TriggeredEventType[]>([TriggeredEventType.SERVER_START]);

	// Form state — webhook
	let webhookUrl = $state('');
	let webhookSecret = $state('');
	let payloadTemplate = $state('');
	let customizePayload = $state(false);
	let webhookMaxRetries = $state(3);
	let webhookRetryDelayMs = $state(1000);
	let webhookTimeoutMs = $state(5000);
	let originalWebhookHasSecret = $state(false); // for placeholder display when editing

	// Built-in webhook payload presets. These are static, so they live in the
	// UI rather than being fetched over RPC. When a webhook isn't customized,
	// the resolved preset is sent to the backend as payload_template.
	const webhookTemplatePresets: Record<string, string> = {
		generic: `{
  "event": "{{.event}}",
  "timestamp": "{{.timestamp}}",
  "server": {
    "id": "{{.server_id}}",
    "name": "{{.server_name}}",
    "status": "{{.server_status}}",
    "mc_version": "{{.server_mc_version}}",
    "mod_loader": "{{.server_mod_loader}}",
    "players_online": {{.server_players}},
    "max_players": {{.server_max_players}},
    "port": {{.server_port}}
  }
}`,
		discord: `{
  "embeds": [{
    "title": "{{.title}}",
    "description": "**{{.server_name}}** - {{.server_status}}",
    "color": {{.color}},
    "timestamp": "{{.timestamp}}",
    "fields": [
      {"name": "Version", "value": "{{.server_mc_version}}", "inline": true},
      {"name": "Players", "value": "{{.server_players}}/{{.server_max_players}}", "inline": true},
      {"name": "Mod Loader", "value": "{{.server_mod_loader}}", "inline": true}
    ],
    "footer": {"text": "DiscoPanel"}
  }]
}`,
		slack: `{
  "blocks": [
    {
      "type": "header",
      "text": {"type": "plain_text", "text": "{{.title}}"}
    },
    {
      "type": "section",
      "text": {"type": "mrkdwn", "text": "*{{.server_name}}* — {{.server_status}}"}
    },
    {
      "type": "section",
      "fields": [
        {"type": "mrkdwn", "text": "*Version:*\\n{{.server_mc_version}}"},
        {"type": "mrkdwn", "text": "*Players:*\\n{{.server_players}}/{{.server_max_players}}"},
        {"type": "mrkdwn", "text": "*Mod Loader:*\\n{{.server_mod_loader}}"},
        {"type": "mrkdwn", "text": "*Port:*\\n{{.server_port}}"}
      ]
    },
    {
      "type": "context",
      "elements": [{"type": "mrkdwn", "text": "DiscoPanel • {{.timestamp}}"}]
    }
  ]
}`,
		teams: `{
  "type": "message",
  "attachments": [{
    "contentType": "application/vnd.microsoft.card.adaptive",
    "content": {
      "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
      "type": "AdaptiveCard",
      "version": "1.4",
      "body": [
        {
          "type": "TextBlock",
          "size": "medium",
          "weight": "bolder",
          "text": "{{.title}}"
        },
        {
          "type": "TextBlock",
          "text": "**{{.server_name}}** — {{.server_status}}",
          "wrap": true
        },
        {
          "type": "FactSet",
          "facts": [
            {"title": "Version", "value": "{{.server_mc_version}}"},
            {"title": "Players", "value": "{{.server_players}}/{{.server_max_players}}"},
            {"title": "Mod Loader", "value": "{{.server_mod_loader}}"},
            {"title": "Port", "value": "{{.server_port}}"}
          ]
        },
        {
          "type": "TextBlock",
          "text": "DiscoPanel • {{.timestamp}}",
          "size": "small",
          "isSubtle": true
        }
      ]
    }
  }]
}`,
		ntfy: `{
  "topic": "discopanel",
  "title": "{{.title}}",
  "message": "{{.server_name}} — {{.server_status}}",
  "tags": ["video_game"],
  "priority": 3
}`
	};

	// Payload shown in the editor
	let displayValue = $derived(customizePayload ? payloadTemplate : getDefaultTemplate(webhookUrl));

	const presetLabels: Record<string, string> = {
		discord: 'Discord',
		slack: 'Slack',
		teams: 'Teams',
		ntfy: 'ntfy',
		generic: 'Generic'
	};

	function isDiscordUrl(url: string): boolean {
		return url.includes('discord.com/api/webhooks') || url.includes('discordapp.com/api/webhooks');
	}

	function getDefaultPresetKey(url: string): string {
		if (isDiscordUrl(url)) return 'discord';
		if (url.includes('hooks.slack.com')) return 'slack';
		if (url.includes('.webhook.office.com') || url.includes('outlook.office.com/webhook'))
			return 'teams';
		if (url.includes('ntfy.sh')) return 'ntfy';
		return 'generic';
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

	function toggleEventTrigger(trigger: TriggeredEventType) {
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
		}
	}

	async function loadTasks() {
		try {
			loading = true;
			const request = create(ListTasksRequestSchema, { serverId: server.id });
			const response = await rpcClient.task.listTasks(request);
			tasks = response.tasks;
		} catch (_e) {
			toast.error('Failed to load tasks');
		} finally {
			loading = false;
		}
	}

	function resetForm() {
		taskName = '';
		taskDescription = '';
		scheduleType = ScheduleType.CRON;
		cronExpr = '';
		intervalSecs = 3600;
		runAt = '';
		timezone = 'UTC';
		timeout = 300;
		retryCount = 0;
		retryDelay = 60;
		requireOnline = true;
		command = '';
		scriptPath = '';
		scriptArgs = '';
		backupName = '';
		backupPaths = '';
		backupCompress = true;
		backupRetentionDays = 7;
		backupMinBackups = 3;
		backupMaxBackups = 0;
		activeSection = 'general';
		taskConfig = '';
		eventTriggers = [TriggeredEventType.SERVER_START];
		webhookUrl = '';
		webhookSecret = '';
		payloadTemplate = '';
		customizePayload = false;
		webhookMaxRetries = 3;
		webhookRetryDelayMs = 1000;
		webhookTimeoutMs = 5000;
		originalWebhookHasSecret = false;
		selectedTask = null;
	}

	function openCreateDialog() {
		resetForm();
		taskType = TaskType.COMMAND;
		showCreateDialog = true;
	}

	function openEditDialog(task: ScheduledTask) {
		resetForm();
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
		retryCount = task.retryCount;
		retryDelay = task.retryDelay;
		requireOnline = task.requireOnline;

		let parsed: Record<string, unknown> = {};
		try {
			parsed = JSON.parse(task.config || '{}');
		} catch {
			parsed = {};
		}
		command = typeof parsed.command === 'string' ? parsed.command : '';
		scriptPath = typeof parsed.script_path === 'string' ? parsed.script_path : '';
		scriptArgs = Array.isArray(parsed.args) ? parsed.args.join(' ') : '';
		backupName = typeof parsed.backup_name === 'string' ? parsed.backup_name : '';
		backupPaths = Array.isArray(parsed.paths) ? parsed.paths.join(', ') : '';
		backupCompress = typeof parsed.compress === 'boolean' ? parsed.compress : true;
		backupRetentionDays = typeof parsed.retention_days === 'number' ? parsed.retention_days : 0;
		backupMinBackups = typeof parsed.min_backups === 'number' ? parsed.min_backups : 0;
		backupMaxBackups = typeof parsed.max_backups === 'number' ? parsed.max_backups : 0;

		taskConfig = task.config;
		eventTriggers =
			task.eventTriggers && task.eventTriggers.length > 0
				? [...task.eventTriggers]
				: [TriggeredEventType.SERVER_START];

		// Webhook-specific: parse JSON config
		if (task.taskType === TaskType.WEBHOOK) {
			try {
				const cfg = JSON.parse(task.config || '{}');
				webhookUrl = cfg.url || '';
				webhookSecret = '';
				originalWebhookHasSecret = !!cfg.secret;
				payloadTemplate = cfg.payload_template || '';
				// A stored template that matches the URL's default preset counts as "not customized".
				customizePayload =
					!!payloadTemplate && payloadTemplate.trim() !== getDefaultTemplate(webhookUrl).trim();
				webhookMaxRetries = cfg.max_retries ?? 3;
				webhookRetryDelayMs = cfg.retry_delay_ms ?? 1000;
				webhookTimeoutMs = cfg.timeout_ms ?? 5000;
			} catch {
				// Invalid config — leave defaults from resetForm
			}
		}

		showCreateDialog = true;
	}

	function closeDialog() {
		showCreateDialog = false;
		resetForm();
	}

	function buildTaskConfig(): string {
		switch (taskType) {
			case TaskType.COMMAND:
				return JSON.stringify({ command: command.trim() });
			case TaskType.SCRIPT:
				return JSON.stringify({
					script_path: scriptPath.trim(),
					args: scriptArgs
						.split(' ')
						.map((a) => a.trim())
						.filter(Boolean)
				});
			case TaskType.BACKUP:
				return JSON.stringify({
					backup_name: backupName.trim(),
					paths: backupPaths
						.split(',')
						.map((p) => p.trim())
						.filter(Boolean),
					compress: backupCompress,
					retention_days: backupRetentionDays,
					min_backups: backupMinBackups,
					max_backups: backupMaxBackups
				});
			default:
				return '';
		}
	}

	function buildWebhookConfig(): string {
		// The backend just renders payload_template, so always send a concrete
		// template: the user's custom one, or the preset resolved from the URL.
		const cfg: Record<string, any> = {
			url: webhookUrl,
			payload_template: customizePayload ? payloadTemplate : getDefaultTemplate(webhookUrl),
			max_retries: webhookMaxRetries,
			retry_delay_ms: webhookRetryDelayMs,
			timeout_ms: webhookTimeoutMs
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
		if (taskType === TaskType.COMMAND && !command.trim()) {
			toast.error('A command is required for command tasks');
			return;
		}
		if (taskType === TaskType.SCRIPT && !scriptPath.trim()) {
			toast.error('A script path is required for script tasks');
			return;
		}
		if (scheduleType === ScheduleType.CRON && !cronExpr.trim()) {
			toast.error('A cron expression is required');
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
			const config = taskType === TaskType.WEBHOOK ? buildWebhookConfig() : buildTaskConfig();

			const runAtTimestamp =
				scheduleType === ScheduleType.ONCE && runAt
					? timestampFromDate(new Date(runAt))
					: undefined;

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
					runAt: runAtTimestamp,
					timezone: timezone,
					config: config,
					timeout: timeout,
					retryCount: retryCount,
					retryDelay: retryDelay,
					requireOnline: requireOnline,
					eventTriggers: isEventScheduled ? eventTriggers : [],
					clearEventTriggers: !isEventScheduled
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
					runAt: runAtTimestamp,
					timezone: timezone,
					config: config,
					timeout: timeout,
					retryCount: retryCount,
					retryDelay: retryDelay,
					requireOnline: requireOnline,
					eventTriggers: isEventScheduled ? eventTriggers : []
				});
				await rpcClient.task.createTask(request);
				toast.success('Task created successfully');
			}
			showCreateDialog = false;
			resetForm();
			await loadTasks();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to save task');
		} finally {
			creating = false;
		}
	}

	async function toggleTask(task: ScheduledTask) {
		try {
			const newStatus =
				task.status === TaskStatus.ENABLED ? TaskStatus.DISABLED : TaskStatus.ENABLED;
			const request = create(ToggleTaskRequestSchema, { id: task.id, status: newStatus });
			await rpcClient.task.toggleTask(request);
			toast.success(`Task ${newStatus === TaskStatus.ENABLED ? 'enabled' : 'disabled'}`);
			await loadTasks();
		} catch (_e) {
			toast.error('Failed to toggle task');
		}
	}

	async function triggerTask(task: ScheduledTask) {
		try {
			const request = create(TriggerTaskRequestSchema, { id: task.id });
			await rpcClient.task.triggerTask(request);
			toast.success('Task triggered successfully');
			await loadTasks();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to trigger task');
		}
	}

	async function deleteTask(task: ScheduledTask) {
		if (!confirm(`Are you sure you want to delete the task "${task.name}"?`)) return;
		try {
			const request = create(DeleteTaskRequestSchema, { id: task.id });
			await rpcClient.task.deleteTask(request);
			toast.success('Task deleted successfully');
			await loadTasks();
		} catch (_e) {
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
		} catch (_e) {
			toast.error('Failed to load task history');
		} finally {
			historyLoading = false;
		}
	}

	function getTaskTypeLabel(type: TaskType): string {
		switch (type) {
			case TaskType.COMMAND:
				return 'Command';
			case TaskType.BACKUP:
				return 'Backup';
			case TaskType.RESTART:
				return 'Restart';
			case TaskType.START:
				return 'Start';
			case TaskType.STOP:
				return 'Stop';
			case TaskType.SCRIPT:
				return 'Script';
			case TaskType.WEBHOOK:
				return 'Webhook';
			default:
				return 'Unknown';
		}
	}

	function getTaskTypeIcon(type: TaskType) {
		switch (type) {
			case TaskType.COMMAND:
				return Terminal;
			case TaskType.BACKUP:
				return Archive;
			case TaskType.RESTART:
				return RotateCcw;
			case TaskType.START:
				return Power;
			case TaskType.STOP:
				return Square;
			case TaskType.SCRIPT:
				return FileText;
			case TaskType.WEBHOOK:
				return WebhookIcon;
			default:
				return Clock;
		}
	}

	function getScheduleTypeLabel(s: ScheduleType): string {
		switch (s) {
			case ScheduleType.CRON:
				return 'Cron Expression';
			case ScheduleType.INTERVAL:
				return 'Fixed Interval';
			case ScheduleType.ONCE:
				return 'Run Once';
			case ScheduleType.EVENT:
				return 'On Event';
			default:
				return 'Unknown';
		}
	}

	function getScheduleLabel(task: ScheduledTask): string {
		switch (task.schedule) {
			case ScheduleType.CRON:
				return `Cron: ${task.cronExpr}`;
			case ScheduleType.INTERVAL:
				return `Every ${formatInterval(task.intervalSecs)}`;
			case ScheduleType.ONCE:
				return task.runAt
					? `Once at ${new Date(Number(task.runAt.seconds) * 1000).toLocaleString()}`
					: 'Once';
			case ScheduleType.EVENT:
				return `On ${(task.eventTriggers || []).map(getEventTypeLabel).join(', ') || 'none'}`;
			default:
				return 'Unknown';
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
				return {
					variant: 'default' as const,
					icon: CheckCircle2,
					class: 'bg-green-500/10 text-green-500 border-green-500/20'
				};
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
			case ExecutionStatus.PENDING:
				return 'Pending';
			case ExecutionStatus.RUNNING:
				return 'Running';
			case ExecutionStatus.COMPLETED:
				return 'Completed';
			case ExecutionStatus.FAILED:
				return 'Failed';
			case ExecutionStatus.SKIPPED:
				return 'Skipped';
			case ExecutionStatus.CANCELLED:
				return 'Cancelled';
			case ExecutionStatus.TIMEOUT:
				return 'Timeout';
			default:
				return 'Unknown';
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
				<p class="text-sm text-muted-foreground">
					Schedule operations or run them on server events (including webhooks).
				</p>
			</div>
			<div class="flex gap-2">
				<Button variant="outline" size="sm" onclick={loadTasks}>
					<RefreshCw class="mr-2 h-4 w-4" />
					Refresh
				</Button>
				<Button size="sm" onclick={openCreateDialog}>
					<Plus class="mr-2 h-4 w-4" />
					New Task
				</Button>
			</div>
		</div>

		<!-- Tasks List -->
		{#if tasks.length === 0}
			<Card>
				<CardContent class="py-8">
					<div class="space-y-4 text-center">
						<Clock class="mx-auto h-12 w-12 text-muted-foreground/50" />
						<div>
							<p class="text-lg font-medium">No tasks yet</p>
							<p class="text-sm text-muted-foreground">
								Create a scheduled task or a webhook to react to server events.
							</p>
						</div>
						<div class="flex justify-center gap-2">
							<Button onclick={openCreateDialog}>
								<Plus class="mr-2 h-4 w-4" />
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
					<Card class="transition-shadow hover:shadow-md">
						<CardContent class="p-4">
							<div class="flex items-start gap-4">
								<div
									class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10"
								>
									<TaskIcon class="h-5 w-5 text-primary" />
								</div>
								<div class="min-w-0 flex-1">
									<div class="mb-1 flex items-center gap-2">
										<h4 class="truncate font-medium">{task.name}</h4>
										<Badge
											variant={task.status === TaskStatus.ENABLED ? 'default' : 'secondary'}
											class="text-xs"
										>
											{task.status === TaskStatus.ENABLED ? 'Enabled' : 'Disabled'}
										</Badge>
										<Badge variant="outline" class="text-xs">
											{getTaskTypeLabel(task.taskType)}
										</Badge>
										{#if task.schedule === ScheduleType.EVENT}
											{#each task.eventTriggers as trigger}
												<Badge variant="outline" class="flex items-center gap-1 text-xs">
													<Zap class="h-3 w-3" />
													{getEventTypeLabel(trigger)}
												</Badge>
											{/each}
										{/if}
									</div>
									{#if task.description}
										<p class="mb-2 truncate text-sm text-muted-foreground">{task.description}</p>
									{/if}
									{#if webhookUrlDisplay}
										<div class="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
											<span class="max-w-[400px] truncate font-mono">{webhookUrlDisplay}</span>
											<Button
												variant="ghost"
												size="icon"
												class="h-5 w-5"
												onclick={() => copyText(webhookUrlDisplay)}
											>
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
								<div class="flex shrink-0 items-center gap-1">
									<Button
										variant="ghost"
										size="icon"
										onclick={() => viewHistory(task)}
										title="View History"
									>
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
									<Button
										variant="ghost"
										size="icon"
										onclick={() => toggleTask(task)}
										title={task.status === TaskStatus.ENABLED ? 'Disable' : 'Enable'}
									>
										{#if task.status === TaskStatus.ENABLED}
											<Pause class="h-4 w-4" />
										{:else}
											<Play class="h-4 w-4" />
										{/if}
									</Button>
									<Button
										variant="ghost"
										size="icon"
										onclick={() => openEditDialog(task)}
										title="Edit"
									>
										<Pencil class="h-4 w-4" />
									</Button>
									<Button
										variant="ghost"
										size="icon"
										class="text-destructive hover:text-destructive"
										onclick={() => deleteTask(task)}
										title="Delete"
									>
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
		<Dialog.Content
			class="flex h-[80vh]! w-[95vw]! max-w-4xl! flex-col gap-0! overflow-hidden p-0!"
			showCloseButton={false}
		>
			<div class="flex h-full">
				<!-- Sidebar -->
				<div class="flex w-56 shrink-0 flex-col border-r bg-muted/30">
					<div class="border-b p-6">
						<div class="flex items-center gap-3">
							<div
								class="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-primary/10"
							>
								<DialogTaskIcon class="h-6 w-6 text-primary" />
							</div>
							<div class="min-w-0 flex-1">
								<h3 class="truncate font-semibold">
									{taskName || (selectedTask ? 'Edit Task' : 'New Task')}
								</h3>
								<p class="truncate text-sm text-muted-foreground">{getTaskTypeLabel(taskType)}</p>
							</div>
						</div>
					</div>

					<nav class="flex-1 space-y-1 p-4">
						{#each dialogSections as section (section.id)}
							{@const SectionIcon = section.icon}
							<button
								onclick={() => (activeSection = section.id)}
								class="flex w-full items-center gap-3 rounded-lg px-4 py-3 text-left transition-colors {activeSection ===
								section.id
									? 'bg-primary text-primary-foreground'
									: 'text-muted-foreground hover:bg-muted hover:text-foreground'}"
							>
								<SectionIcon class="h-5 w-5" />
								<span class="font-medium">{section.label}</span>
							</button>
						{/each}
					</nav>
				</div>

				<!-- Main Content -->
				<div class="flex min-w-0 flex-1 flex-col">
					<!-- Content Header -->
					<div class="flex items-center justify-between border-b bg-muted/30 px-8 py-6">
						<div>
							<h2 class="text-2xl font-semibold tracking-tight">{currentSection.title}</h2>
							<p class="mt-1 text-muted-foreground">{currentSection.description}</p>
						</div>
						<Button variant="ghost" size="icon" onclick={closeDialog} class="h-10 w-10">
							<X class="h-5 w-5" />
						</Button>
					</div>

					<!-- Scrollable Content -->
					<div class="flex-1 overflow-y-auto p-8">
						<div class="max-w-2xl space-y-6">
							{#if activeSection === 'general'}
								<div class="space-y-3">
									<Label for="taskName">Task Name *</Label>
									<Input
										id="taskName"
										bind:value={taskName}
										placeholder="Daily Backup"
										class="h-11"
									/>
								</div>

								<div class="space-y-3">
									<Label for="taskDescription">Description</Label>
									<Input
										id="taskDescription"
										bind:value={taskDescription}
										placeholder="Runs every day at midnight"
										class="h-11"
									/>
								</div>

								<div class="space-y-3">
									<Label>Task Type</Label>
									<Select.Root
										type="single"
										name="taskType"
										value={taskType.toString()}
										onValueChange={(v) => {
											if (v) taskType = parseInt(v) as TaskType;
										}}
									>
										<Select.Trigger class="h-11! w-full">
											{getTaskTypeLabel(taskType)}
										</Select.Trigger>
										<Select.Content>
											<Select.Item value={TaskType.COMMAND.toString()} label="Command"
												>Command</Select.Item
											>
											<Select.Item value={TaskType.BACKUP.toString()} label="Backup"
												>Backup</Select.Item
											>
											<Select.Item value={TaskType.RESTART.toString()} label="Restart Server"
												>Restart Server</Select.Item
											>
											<Select.Item value={TaskType.START.toString()} label="Start Server"
												>Start Server</Select.Item
											>
											<Select.Item value={TaskType.STOP.toString()} label="Stop Server"
												>Stop Server</Select.Item
											>
											<Select.Item value={TaskType.SCRIPT.toString()} label="Script"
												>Script</Select.Item
											>
											<Select.Item value={TaskType.WEBHOOK.toString()} label="Webhook"
												>Webhook</Select.Item
											>
										</Select.Content>
									</Select.Root>
								</div>

								{#if taskType === TaskType.COMMAND}
									<div class="space-y-3">
										<Label for="command">RCON Command *</Label>
										<Input
											id="command"
											bind:value={command}
											placeholder="say Hello World!"
											class="h-11 font-mono"
										/>
										<p class="text-sm text-muted-foreground">The command to execute via RCON</p>
									</div>
								{:else if taskType === TaskType.SCRIPT}
									<div class="space-y-3">
										<Label for="scriptPath">Script Path or Executable *</Label>
										<Input
											id="scriptPath"
											bind:value={scriptPath}
											placeholder="/data/scripts/cleanup.sh"
											class="h-11 font-mono"
										/>
										<p class="text-sm text-muted-foreground">
											Path to the script/executable inside the container
										</p>
									</div>
									<div class="space-y-3">
										<Label for="scriptArgs">Arguments</Label>
										<Input
											id="scriptArgs"
											bind:value={scriptArgs}
											placeholder="--verbose --level 2"
											class="h-11 font-mono"
										/>
										<p class="text-sm text-muted-foreground">
											Space-separated arguments to pass to the script/executable
										</p>
									</div>
								{:else if taskType === TaskType.BACKUP}
									<div class="space-y-3">
										<Label for="backupName">Backup Name</Label>
										<Input
											id="backupName"
											bind:value={backupName}
											placeholder={taskName || 'Daily Backup'}
											class="h-11"
										/>
										<p class="text-sm text-muted-foreground">
											Used as the archive filename prefix. Defaults to the task name.
										</p>
									</div>
									<div class="space-y-3">
										<Label for="backupPaths">Paths to Include</Label>
										<Input
											id="backupPaths"
											bind:value={backupPaths}
											placeholder="world, world_nether, world_the_end"
											class="h-11 font-mono"
										/>
										<p class="text-sm text-muted-foreground">
											Comma-separated paths relative to the server directory. Leave empty to back up
											the world directory.
										</p>
									</div>
									<label
										class="flex cursor-pointer items-start gap-4 rounded-lg border p-4 transition-colors hover:bg-muted/50"
									>
										<Switch bind:checked={backupCompress} class="mt-0.5" />
										<div class="space-y-1">
											<span class="font-medium">Compress Archive</span>
											<p class="text-sm text-muted-foreground">
												Smaller backups at the cost of more CPU while archiving
											</p>
										</div>
									</label>
									<div class="grid grid-cols-3 gap-6">
										<div class="space-y-3">
											<Label for="retentionDays">Retention (days)</Label>
											<Input
												id="retentionDays"
												type="number"
												bind:value={backupRetentionDays}
												min={0}
												class="h-11"
											/>
											<p class="text-sm text-muted-foreground">
												Delete backups older than this. 0 = keep forever
											</p>
										</div>
										<div class="space-y-3">
											<Label for="minBackups">Min Backups</Label>
											<Input
												id="minBackups"
												type="number"
												bind:value={backupMinBackups}
												min={0}
												disabled={backupRetentionDays <= 0}
												class="h-11"
											/>
											<p class="text-sm text-muted-foreground">
												Never expire by age below this many, even past retention
											</p>
										</div>
										<div class="space-y-3">
											<Label for="maxBackups">Max Backups</Label>
											<Input
												id="maxBackups"
												type="number"
												bind:value={backupMaxBackups}
												min={0}
												class="h-11"
											/>
											<p class="text-sm text-muted-foreground">
												Hard cap, oldest deleted first. 0 = unlimited
											</p>
										</div>
									</div>
									<p class="text-sm text-muted-foreground">
										World saving is automatically paused and flushed while the backup runs, then
										re-enabled.
									</p>
								{:else if taskType === TaskType.WEBHOOK}
									<div class="space-y-3">
										<Label for="url">Webhook URL *</Label>
										<Input
											id="url"
											bind:value={webhookUrl}
											placeholder={isDiscordUrl(webhookUrl)
												? 'https://discord.com/api/webhooks/...'
												: 'https://example.com/webhook'}
											class="h-11 font-mono"
										/>
										<p class="text-sm text-muted-foreground">
											The endpoint the request is sent to. Discord/Slack/Teams/ntfy URLs are
											auto-detected for the default payload preset.
										</p>
									</div>
								{:else}
									<div class="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
										No additional configuration required for this task type.
									</div>
								{/if}
							{:else if activeSection === 'payload' && taskType === TaskType.WEBHOOK}
								<div class="flex items-center justify-between">
									<div>
										<Label class="text-base">Customize Payload</Label>
										<p class="mt-0.5 text-sm text-muted-foreground">
											{#if customizePayload}
												Using a custom payload template
											{:else}
												Using the default {presetLabels[getDefaultPresetKey(webhookUrl)] ||
													'Generic'} preset
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

								<div class={!customizePayload ? 'pointer-events-none opacity-40' : ''}>
									<p class="mb-1.5 text-sm font-medium text-muted-foreground">Presets</p>
									<div class="flex flex-wrap gap-1">
										{#each Object.keys(presetLabels) as key}
											{#if webhookTemplatePresets[key]}
												<Button
													variant="outline"
													size="sm"
													class="h-7 text-xs"
													onclick={() => applyPreset(key)}
												>
													{presetLabels[key]}
												</Button>
											{/if}
										{/each}
									</div>
								</div>

								<div class={!customizePayload ? 'pointer-events-none opacity-50' : ''}>
									<CodeEditor
										value={displayValue}
										language="json-template"
										readOnly={!customizePayload}
										height="340px"
										onChange={(v) => {
											if (customizePayload) payloadTemplate = v;
										}}
									/>
								</div>

								<div class={!customizePayload ? 'opacity-40' : ''}>
									<p class="mb-1 text-sm font-medium text-muted-foreground">Available variables</p>
									<div
										class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 rounded border border-border/50 bg-muted/20 p-2 font-mono text-xs text-muted-foreground"
									>
										{#each [['{{.event}}', 'Event name'], ['{{.timestamp}}', 'ISO 8601 timestamp'], ['{{.title}}', 'Event title'], ['{{.color}}', 'Color (int, for Discord)'], ['{{.server_id}}', 'Server ID'], ['{{.server_name}}', 'Server name'], ['{{.server_status}}', 'Server status'], ['{{.server_mc_version}}', 'MC version'], ['{{.server_mod_loader}}', 'Mod loader'], ['{{.server_players}}', 'Player count'], ['{{.server_max_players}}', 'Max players'], ['{{.server_port}}', 'Server port'], ['{{.player}}', 'Player name (player join/leave events)']] as [variable, description]}
											<button
												class="cursor-pointer text-left transition-colors hover:text-foreground"
												title="Copy {variable}"
												onclick={() => {
													navigator.clipboard.writeText(variable);
													toast.success(`Copied ${variable}`);
												}}>{variable}</button
											>
											<span class="font-sans">{description}</span>
										{/each}
									</div>
								</div>
							{:else if activeSection === 'schedule'}
								<div class="space-y-3">
									<Label>Schedule Type</Label>
									<Select.Root
										type="single"
										name="scheduleType"
										value={scheduleType.toString()}
										onValueChange={(v) => {
											if (v) scheduleType = parseInt(v) as ScheduleType;
										}}
									>
										<Select.Trigger class="h-11! w-full">
											{getScheduleTypeLabel(scheduleType)}
										</Select.Trigger>
										<Select.Content>
											<Select.Item value={ScheduleType.EVENT.toString()} label="On Event"
												>On Event</Select.Item
											>
											<Select.Item value={ScheduleType.CRON.toString()} label="Cron Expression"
												>Cron Expression</Select.Item
											>
											<Select.Item value={ScheduleType.INTERVAL.toString()} label="Fixed Interval"
												>Fixed Interval</Select.Item
											>
											<Select.Item value={ScheduleType.ONCE.toString()} label="Run Once"
												>Run Once</Select.Item
											>
										</Select.Content>
									</Select.Root>
								</div>

								{#if scheduleType === ScheduleType.CRON}
									<div class="space-y-3">
										<Label for="cronExpr">Cron Expression *</Label>
										<Input
											id="cronExpr"
											bind:value={cronExpr}
											placeholder="0 0 * * *"
											class="h-11 font-mono"
										/>
										<p class="text-sm text-muted-foreground">
											Format: minute hour day month weekday (e.g., "0 0 * * *" for daily at
											midnight)
										</p>
									</div>
								{:else if scheduleType === ScheduleType.INTERVAL}
									<div class="space-y-3">
										<Label for="intervalSecs">Interval (seconds)</Label>
										<Input
											id="intervalSecs"
											type="number"
											bind:value={intervalSecs}
											min={60}
											class="h-11"
										/>
										<p class="text-sm text-muted-foreground">
											Minimum 60 seconds. Current: every {formatInterval(intervalSecs)}
										</p>
									</div>
								{:else if scheduleType === ScheduleType.ONCE}
									<div class="space-y-3">
										<Label for="runAt">Run At</Label>
										<Input id="runAt" type="datetime-local" bind:value={runAt} class="h-11" />
										<p class="text-sm text-muted-foreground">
											The task runs once at this time, then is disabled
										</p>
									</div>
								{:else if scheduleType === ScheduleType.EVENT}
									<div class="space-y-3">
										<Label>Events *</Label>
										<div
											class="grid grid-cols-1 gap-2 rounded-lg border border-border/50 bg-muted/20 p-3"
										>
											{#each SERVER_EVENT_TYPES as { type, label, description }}
												<label
													class="flex cursor-pointer items-center gap-3 rounded p-2 hover:bg-muted/40"
												>
													<Checkbox
														checked={eventTriggers.includes(type)}
														onCheckedChange={() => toggleEventTrigger(type)}
													/>
													<div>
														<span class="text-sm font-medium">{label}</span>
														<p class="text-xs text-muted-foreground">{description}</p>
													</div>
												</label>
											{/each}
										</div>
										<p class="text-sm text-muted-foreground">
											The task runs whenever any selected event fires.
										</p>
									</div>
								{/if}
							{:else if activeSection === 'advanced'}
								{#if taskType === TaskType.WEBHOOK}
									<div class="space-y-3">
										<Label for="secret">Secret (optional)</Label>
										<Input
											id="secret"
											type="password"
											bind:value={webhookSecret}
											placeholder={originalWebhookHasSecret ? '(unchanged)' : 'HMAC signing secret'}
											class="h-11"
										/>
										<p class="text-sm text-muted-foreground">
											Signs the payload with HMAC-SHA256 so the receiver can verify it.
										</p>
									</div>

									<div class="grid grid-cols-3 gap-6">
										<div class="space-y-3">
											<Label for="maxRetries">Max Retries</Label>
											<Input
												id="maxRetries"
												type="number"
												bind:value={webhookMaxRetries}
												min={0}
												max={10}
												class="h-11"
											/>
											<p class="text-sm text-muted-foreground">
												Delivery attempts before giving up
											</p>
										</div>
										<div class="space-y-3">
											<Label for="retryDelayMs">Retry Delay (ms)</Label>
											<Input
												id="retryDelayMs"
												type="number"
												bind:value={webhookRetryDelayMs}
												min={100}
												max={60000}
												class="h-11"
											/>
											<p class="text-sm text-muted-foreground">Wait between delivery attempts</p>
										</div>
										<div class="space-y-3">
											<Label for="webhookTimeout">Timeout (ms)</Label>
											<Input
												id="webhookTimeout"
												type="number"
												bind:value={webhookTimeoutMs}
												min={1000}
												max={30000}
												class="h-11"
											/>
											<p class="text-sm text-muted-foreground">Per-attempt request timeout</p>
										</div>
									</div>
								{:else}
									<div class="space-y-3">
										<Label for="timeout">Timeout (seconds)</Label>
										<Input
											id="timeout"
											type="number"
											bind:value={timeout}
											min={10}
											max={3600}
											class="h-11"
										/>
										<p class="text-sm text-muted-foreground">
											Maximum execution time before the task is cancelled
										</p>
									</div>

									<div class="grid grid-cols-2 gap-6">
										<div class="space-y-3">
											<Label for="retryCount">Retry Count</Label>
											<Input
												id="retryCount"
												type="number"
												bind:value={retryCount}
												min={0}
												max={10}
												class="h-11"
											/>
											<p class="text-sm text-muted-foreground">
												Times to retry on failure. 0 = no retries
											</p>
										</div>
										<div class="space-y-3">
											<Label for="retryDelay">Retry Delay (seconds)</Label>
											<Input
												id="retryDelay"
												type="number"
												bind:value={retryDelay}
												min={1}
												class="h-11"
											/>
											<p class="text-sm text-muted-foreground">Wait between retry attempts</p>
										</div>
									</div>

									<label
										class="flex cursor-pointer items-start gap-4 rounded-lg border p-4 transition-colors hover:bg-muted/50"
									>
										<Switch bind:checked={requireOnline} class="mt-0.5" />
										<div class="space-y-1">
											<span class="font-medium">Require Server Online</span>
											<p class="text-sm text-muted-foreground">
												Skip this task when the server is offline
											</p>
										</div>
									</label>
								{/if}
							{/if}
						</div>
					</div>

					<!-- Footer -->
					<div class="flex items-center justify-between border-t bg-muted/20 p-4">
						<Button variant="ghost" onclick={closeDialog}>Cancel</Button>
						<Button
							onclick={saveTask}
							disabled={!taskName.trim() || creating}
							class="min-w-[120px]"
						>
							{#if creating}
								<Loader2 class="mr-2 h-4 w-4 animate-spin" />
								{selectedTask ? 'Saving...' : 'Creating...'}
							{:else}
								{selectedTask ? 'Save Changes' : 'Create Task'}
							{/if}
						</Button>
					</div>
				</div>
			</div>
		</Dialog.Content>
	</Dialog.Root>

	<!-- History Dialog -->
	<Dialog.Root bind:open={showHistoryDialog}>
		<Dialog.Content class="max-h-[80vh] max-w-2xl overflow-y-auto">
			<Dialog.Header>
				<Dialog.Title>Task History: {selectedTask?.name}</Dialog.Title>
				<Dialog.Description>Recent execution history for this task</Dialog.Description>
			</Dialog.Header>

			{#if historyLoading}
				<div class="flex items-center justify-center py-8">
					<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
				</div>
			{:else if taskHistory.length === 0}
				<div class="py-8 text-center text-muted-foreground">
					<History class="mx-auto mb-4 h-12 w-12 opacity-50" />
					<p>No execution history yet</p>
				</div>
			{:else}
				<div class="max-h-96 space-y-2 overflow-y-auto">
					{#each taskHistory as execution (execution.id)}
						{@const statusInfo = getExecutionStatusBadge(execution.status)}
						{@const StatusIcon = statusInfo.icon}
						<div class="rounded-lg border bg-card p-3">
							<div class="mb-2 flex items-start justify-between">
								<div class="flex items-center gap-2">
									<Badge variant={statusInfo.variant} class={statusInfo.class}>
										<StatusIcon
											class="mr-1 h-3 w-3 {execution.status === ExecutionStatus.RUNNING
												? 'animate-spin'
												: ''}"
										/>
										{getExecutionStatusLabel(execution.status)}
									</Badge>
									<span class="text-xs text-muted-foreground">
										{execution.trigger === 'manual'
											? 'Manual'
											: execution.trigger === 'event'
												? 'Event'
												: 'Scheduled'}
									</span>
								</div>
								{#if execution.duration}
									<span class="text-xs text-muted-foreground"
										>{formatDuration(execution.duration)}</span
									>
								{/if}
							</div>
							<div class="mb-1 text-xs text-muted-foreground">
								{new Date(Number(execution.startedAt?.seconds || 0) * 1000).toLocaleString()}
							</div>
							{#if execution.output}
								<div
									class="mt-2 max-h-24 overflow-y-auto rounded bg-muted p-2 font-mono text-xs whitespace-pre-wrap"
								>
									{execution.output}
								</div>
							{/if}
							{#if execution.error}
								<div class="mt-2 rounded bg-destructive/10 p-2 text-xs text-destructive">
									{execution.error}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}

			<Dialog.Footer>
				<Button
					variant="outline"
					onclick={() => {
						showHistoryDialog = false;
						selectedTask = null;
						taskHistory = [];
					}}>Close</Button
				>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>
{/if}
