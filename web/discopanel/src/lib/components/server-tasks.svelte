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
	import * as Select from '$lib/components/ui/select';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Loader2, Plus, Play, Pause, Trash2, Clock, CheckCircle2, XCircle, AlertCircle, RefreshCw, Terminal, RotateCcw, Square, Power, FileText, History, Archive, Wrench, X, Pencil } from '@lucide/svelte';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { ScheduledTask, TaskExecution } from '$lib/proto/discopanel/v1/task_pb';
	import { TaskType, TaskStatus, ScheduleType, ExecutionStatus, CreateTaskRequestSchema, UpdateTaskRequestSchema, ToggleTaskRequestSchema, TriggerTaskRequestSchema, DeleteTaskRequestSchema, ListTasksRequestSchema, ListTaskExecutionsRequestSchema } from '$lib/proto/discopanel/v1/task_pb';
	import { create } from '@bufbuild/protobuf';
	import { timestampFromDate } from '@bufbuild/protobuf/wkt';

	let { server, active }: { server: Server, active?: boolean } = $props();

	let loading = $state(true);
	let tasks = $state<ScheduledTask[]>([]);
	let initialized = $state(false);
	// svelte-ignore state_referenced_locally
	let previousServerId = $state(server.id);

	// Dialog state
	type DialogSection = 'general' | 'schedule' | 'advanced';
	let showCreateDialog = $state(false);
	let showHistoryDialog = $state(false);
	let selectedTask = $state<ScheduledTask | null>(null);
	let taskHistory = $state<TaskExecution[]>([]);
	let historyLoading = $state(false);
	let creating = $state(false);
	let activeSection = $state<DialogSection>('general');

	// Form state
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

	const dialogSections: { id: DialogSection; label: string; icon: typeof FileText; title: string; description: string }[] = [
		{ id: 'general', label: 'General', icon: FileText, title: 'General', description: 'Task name, type, and configuration' },
		{ id: 'schedule', label: 'Schedule', icon: Clock, title: 'Schedule', description: 'When and how often the task runs' },
		{ id: 'advanced', label: 'Advanced', icon: Wrench, title: 'Advanced', description: 'Timeouts, retries, and execution conditions' }
	];

	const currentSection = $derived(dialogSections.find((s) => s.id === activeSection) ?? dialogSections[0]);
	const DialogTaskIcon = $derived(getTaskTypeIcon(taskType));

	onMount(() => {
		if (server && !initialized) {
			initialized = true;
			loadTasks();
		}
	});

	// Reset state when server changes
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
		taskType = TaskType.COMMAND;
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
		selectedTask = null;
	}

	function openCreateDialog() {
		resetForm();
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
		try { parsed = JSON.parse(task.config || '{}'); } catch { parsed = {}; }
		command = typeof parsed.command === 'string' ? parsed.command : '';
		scriptPath = typeof parsed.script_path === 'string' ? parsed.script_path : '';
		scriptArgs = Array.isArray(parsed.args) ? parsed.args.join(' ') : '';
		backupName = typeof parsed.backup_name === 'string' ? parsed.backup_name : '';
		backupPaths = Array.isArray(parsed.paths) ? parsed.paths.join(', ') : '';
		backupCompress = typeof parsed.compress === 'boolean' ? parsed.compress : true;
		backupRetentionDays = typeof parsed.retention_days === 'number' ? parsed.retention_days : 0;
		backupMinBackups = typeof parsed.min_backups === 'number' ? parsed.min_backups : 0;
		backupMaxBackups = typeof parsed.max_backups === 'number' ? parsed.max_backups : 0;

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
					args: scriptArgs.split(' ').map((a) => a.trim()).filter(Boolean)
				});
			case TaskType.BACKUP:
				return JSON.stringify({
					backup_name: backupName.trim(),
					paths: backupPaths.split(',').map((p) => p.trim()).filter(Boolean),
					compress: backupCompress,
					retention_days: backupRetentionDays,
					min_backups: backupMinBackups,
					max_backups: backupMaxBackups
				});
			default:
				return '';
		}
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

		creating = true;
		try {
			const config = buildTaskConfig();

			const runAtTimestamp = scheduleType === ScheduleType.ONCE && runAt ? timestampFromDate(new Date(runAt)) : undefined;

			if (selectedTask) {
				// Update existing task
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
				});
				await rpcClient.task.updateTask(request);
				toast.success('Task updated successfully');
			} else {
				// Create new task
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
			const newStatus = task.status === TaskStatus.ENABLED ? TaskStatus.DISABLED : TaskStatus.ENABLED;
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
		if (!confirm(`Are you sure you want to delete the task "${task.name}"?`)) {
			return;
		}
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
			case TaskType.COMMAND: return 'Command';
			case TaskType.BACKUP: return 'Backup';
			case TaskType.RESTART: return 'Restart';
			case TaskType.START: return 'Start';
			case TaskType.STOP: return 'Stop';
			case TaskType.SCRIPT: return 'Script';
			default: return 'Unknown';
		}
	}

	function getTaskTypeIcon(type: TaskType) {
		switch (type) {
			case TaskType.COMMAND: return Terminal;
			case TaskType.BACKUP: return Archive;
			case TaskType.RESTART: return RotateCcw;
			case TaskType.START: return Power;
			case TaskType.STOP: return Square;
			case TaskType.SCRIPT: return FileText;
			default: return Clock;
		}
	}

	function getScheduleLabel(task: ScheduledTask): string {
		switch (task.schedule) {
			case ScheduleType.CRON: return `Cron: ${task.cronExpr}`;
			case ScheduleType.INTERVAL: return `Every ${formatInterval(task.intervalSecs)}`;
			case ScheduleType.ONCE: return task.runAt ? `Once at ${new Date(Number(task.runAt.seconds) * 1000).toLocaleString()}` : 'Once';
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
				<h3 class="text-lg font-semibold">Scheduled Tasks</h3>
				<p class="text-sm text-muted-foreground">Automate server operations with scheduled tasks</p>
			</div>
			<div class="flex gap-2">
				<Button variant="outline" size="sm" onclick={loadTasks}>
					<RefreshCw class="h-4 w-4 mr-2" />
					Refresh
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
							<p class="text-lg font-medium">No scheduled tasks</p>
							<p class="text-sm text-muted-foreground">Create a task to automate server operations</p>
						</div>
						<Button onclick={openCreateDialog}>
							<Plus class="h-4 w-4 mr-2" />
							Create Task
						</Button>
					</div>
				</CardContent>
			</Card>
		{:else}
			<div class="space-y-3">
				{#each tasks as task (task.id)}
					{@const TaskIcon = getTaskTypeIcon(task.taskType)}
					<Card class="hover:shadow-md transition-shadow">
						<CardContent class="p-4">
							<div class="flex items-start gap-4">
								<div class="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center shrink-0">
									<TaskIcon class="h-5 w-5 text-primary" />
								</div>
								<div class="flex-1 min-w-0">
									<div class="flex items-center gap-2 mb-1">
										<h4 class="font-medium truncate">{task.name}</h4>
										<Badge variant={task.status === TaskStatus.ENABLED ? 'default' : 'secondary'} class="text-xs">
											{task.status === TaskStatus.ENABLED ? 'Enabled' : 'Disabled'}
										</Badge>
									</div>
									{#if task.description}
										<p class="text-sm text-muted-foreground mb-2 truncate">{task.description}</p>
									{/if}
									<div class="flex flex-wrap gap-2 text-xs text-muted-foreground">
										<span class="flex items-center gap-1">
											<Clock class="h-3 w-3" />
											{getScheduleLabel(task)}
										</span>
										{#if task.nextRun && task.status === TaskStatus.ENABLED}
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
								<div class="flex items-center gap-1 shrink-0">
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
		<Dialog.Content class="max-w-4xl! w-[95vw]! h-[80vh]! p-0! gap-0! overflow-hidden flex flex-col" showCloseButton={false}>
			<div class="flex h-full">
				<!-- Sidebar -->
				<div class="w-56 border-r bg-muted/30 flex flex-col shrink-0">
					<div class="p-6 border-b">
						<div class="flex items-center gap-3">
							<div class="h-12 w-12 rounded-xl bg-primary/10 flex items-center justify-center shrink-0">
								<DialogTaskIcon class="h-6 w-6 text-primary" />
							</div>
							<div class="flex-1 min-w-0">
								<h3 class="font-semibold truncate">{taskName || (selectedTask ? 'Edit Task' : 'New Task')}</h3>
								<p class="text-sm text-muted-foreground truncate">{getTaskTypeLabel(taskType)}</p>
							</div>
						</div>
					</div>

					<nav class="flex-1 p-4 space-y-1">
						{#each dialogSections as section (section.id)}
							{@const SectionIcon = section.icon}
							<button
								onclick={() => (activeSection = section.id)}
								class="w-full flex items-center gap-3 px-4 py-3 rounded-lg text-left transition-colors {activeSection === section.id
									? 'bg-primary text-primary-foreground'
									: 'hover:bg-muted text-muted-foreground hover:text-foreground'}"
							>
								<SectionIcon class="h-5 w-5" />
								<span class="font-medium">{section.label}</span>
							</button>
						{/each}
					</nav>
				</div>

				<!-- Main Content -->
				<div class="flex-1 flex flex-col min-w-0">
					<!-- Content Header -->
					<div class="flex items-center justify-between px-8 py-6 border-b bg-muted/30">
						<div>
							<h2 class="text-2xl font-semibold tracking-tight">{currentSection.title}</h2>
							<p class="text-muted-foreground mt-1">{currentSection.description}</p>
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
									<Input id="taskName" bind:value={taskName} placeholder="Daily Backup" class="h-11" />
								</div>

								<div class="space-y-3">
									<Label for="taskDescription">Description</Label>
									<Input id="taskDescription" bind:value={taskDescription} placeholder="Runs every day at midnight" class="h-11" />
								</div>

								<div class="space-y-3">
									<Label>Task Type</Label>
									<Select.Root type="single" name="taskType" value={taskType.toString()} onValueChange={(v) => { if (v) taskType = parseInt(v) as TaskType; }}>
										<Select.Trigger class="w-full h-11!">
											{getTaskTypeLabel(taskType)}
										</Select.Trigger>
										<Select.Content>
											<Select.Item value={TaskType.COMMAND.toString()} label="Command">Command</Select.Item>
											<Select.Item value={TaskType.BACKUP.toString()} label="Backup">Backup</Select.Item>
											<Select.Item value={TaskType.RESTART.toString()} label="Restart Server">Restart Server</Select.Item>
											<Select.Item value={TaskType.START.toString()} label="Start Server">Start Server</Select.Item>
											<Select.Item value={TaskType.STOP.toString()} label="Stop Server">Stop Server</Select.Item>
											<Select.Item value={TaskType.SCRIPT.toString()} label="Script">Script</Select.Item>
										</Select.Content>
									</Select.Root>
								</div>

								{#if taskType === TaskType.COMMAND}
									<div class="space-y-3">
										<Label for="command">RCON Command *</Label>
										<Input id="command" bind:value={command} placeholder="say Hello World!" class="h-11 font-mono" />
										<p class="text-sm text-muted-foreground">The command to execute via RCON</p>
									</div>
								{:else if taskType === TaskType.SCRIPT}
									<div class="space-y-3">
										<Label for="scriptPath">Script Path or Executable *</Label>
										<Input id="scriptPath" bind:value={scriptPath} placeholder="/data/scripts/cleanup.sh" class="h-11 font-mono" />
										<p class="text-sm text-muted-foreground">Path to the script/executable inside the container</p>
									</div>
									<div class="space-y-3">
										<Label for="scriptArgs">Arguments</Label>
										<Input id="scriptArgs" bind:value={scriptArgs} placeholder="--verbose --level 2" class="h-11 font-mono" />
										<p class="text-sm text-muted-foreground">Space-separated arguments to pass to the script/executable</p>
									</div>
								{:else if taskType === TaskType.BACKUP}
									<div class="space-y-3">
										<Label for="backupName">Backup Name</Label>
										<Input id="backupName" bind:value={backupName} placeholder={taskName || 'Daily Backup'} class="h-11" />
										<p class="text-sm text-muted-foreground">Used as the archive filename prefix. Defaults to the task name.</p>
									</div>
									<div class="space-y-3">
										<Label for="backupPaths">Paths to Include</Label>
										<Input id="backupPaths" bind:value={backupPaths} placeholder="world, world_nether, world_the_end" class="h-11 font-mono" />
										<p class="text-sm text-muted-foreground">
											Comma-separated paths relative to the server directory. Leave empty to back up the world directory.
										</p>
									</div>
									<label class="flex items-start gap-4 p-4 border rounded-lg cursor-pointer hover:bg-muted/50 transition-colors">
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
											<Input id="retentionDays" type="number" bind:value={backupRetentionDays} min={0} class="h-11" />
											<p class="text-sm text-muted-foreground">Delete backups older than this. 0 = keep forever</p>
										</div>
										<div class="space-y-3">
											<Label for="minBackups">Min Backups</Label>
											<Input id="minBackups" type="number" bind:value={backupMinBackups} min={0} disabled={backupRetentionDays <= 0} class="h-11" />
											<p class="text-sm text-muted-foreground">Never expire by age below this many, even past retention</p>
										</div>
										<div class="space-y-3">
											<Label for="maxBackups">Max Backups</Label>
											<Input id="maxBackups" type="number" bind:value={backupMaxBackups} min={0} class="h-11" />
											<p class="text-sm text-muted-foreground">Hard cap, oldest deleted first. 0 = unlimited</p>
										</div>
									</div>
									<p class="text-sm text-muted-foreground">
										World saving is automatically paused and flushed while the backup runs, then re-enabled.
									</p>
								{:else}
									<div class="p-4 border rounded-lg border-dashed text-sm text-muted-foreground">
										No additional configuration required for this task type.
									</div>
								{/if}
							{:else if activeSection === 'schedule'}
								<div class="space-y-3">
									<Label>Schedule Type</Label>
									<Select.Root type="single" name="scheduleType" value={scheduleType.toString()} onValueChange={(v) => { if (v) scheduleType = parseInt(v) as ScheduleType; }}>
										<Select.Trigger class="w-full h-11!">
											{scheduleType === ScheduleType.CRON ? 'Cron Expression' : scheduleType === ScheduleType.INTERVAL ? 'Fixed Interval' : 'Run Once'}
										</Select.Trigger>
										<Select.Content>
											<Select.Item value={ScheduleType.CRON.toString()} label="Cron Expression">Cron Expression</Select.Item>
											<Select.Item value={ScheduleType.INTERVAL.toString()} label="Fixed Interval">Fixed Interval</Select.Item>
											<Select.Item value={ScheduleType.ONCE.toString()} label="Run Once">Run Once</Select.Item>
										</Select.Content>
									</Select.Root>
								</div>

								{#if scheduleType === ScheduleType.CRON}
									<div class="space-y-3">
										<Label for="cronExpr">Cron Expression *</Label>
										<Input id="cronExpr" bind:value={cronExpr} placeholder="0 0 * * *" class="h-11 font-mono" />
										<p class="text-sm text-muted-foreground">
											Format: minute hour day month weekday (e.g., "0 0 * * *" for daily at midnight)
										</p>
									</div>
								{:else if scheduleType === ScheduleType.INTERVAL}
									<div class="space-y-3">
										<Label for="intervalSecs">Interval (seconds)</Label>
										<Input id="intervalSecs" type="number" bind:value={intervalSecs} min={60} class="h-11" />
										<p class="text-sm text-muted-foreground">
											Minimum 60 seconds. Current: every {formatInterval(intervalSecs)}
										</p>
									</div>
								{:else if scheduleType === ScheduleType.ONCE}
									<div class="space-y-3">
										<Label for="runAt">Run At</Label>
										<Input id="runAt" type="datetime-local" bind:value={runAt} class="h-11" />
										<p class="text-sm text-muted-foreground">The task runs once at this time, then is disabled</p>
									</div>
								{/if}
							{:else if activeSection === 'advanced'}
								<div class="space-y-3">
									<Label for="timeout">Timeout (seconds)</Label>
									<Input id="timeout" type="number" bind:value={timeout} min={10} max={3600} class="h-11" />
									<p class="text-sm text-muted-foreground">Maximum execution time before the task is cancelled</p>
								</div>

								<div class="grid grid-cols-2 gap-6">
									<div class="space-y-3">
										<Label for="retryCount">Retry Count</Label>
										<Input id="retryCount" type="number" bind:value={retryCount} min={0} max={10} class="h-11" />
										<p class="text-sm text-muted-foreground">Times to retry on failure. 0 = no retries</p>
									</div>
									<div class="space-y-3">
										<Label for="retryDelay">Retry Delay (seconds)</Label>
										<Input id="retryDelay" type="number" bind:value={retryDelay} min={1} class="h-11" />
										<p class="text-sm text-muted-foreground">Wait between retry attempts</p>
									</div>
								</div>

								<label class="flex items-start gap-4 p-4 border rounded-lg cursor-pointer hover:bg-muted/50 transition-colors">
									<Switch bind:checked={requireOnline} class="mt-0.5" />
									<div class="space-y-1">
										<span class="font-medium">Require Server Online</span>
										<p class="text-sm text-muted-foreground">Skip this task when the server is offline</p>
									</div>
								</label>
							{/if}
						</div>
					</div>

					<!-- Footer -->
					<div class="p-4 border-t bg-muted/20 flex justify-between items-center">
						<Button variant="ghost" onclick={closeDialog}>Cancel</Button>
						<Button onclick={saveTask} disabled={!taskName.trim() || creating} class="min-w-[120px]">
							{#if creating}
								<Loader2 class="h-4 w-4 animate-spin mr-2" />
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
										{execution.trigger === 'manual' ? 'Manual trigger' : 'Scheduled'}
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
