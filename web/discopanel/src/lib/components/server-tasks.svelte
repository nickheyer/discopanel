<script lang="ts">
	import { onMount } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { Input } from '$lib/components/ui/input';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Switch } from '$lib/components/ui/switch';
	import * as Select from '$lib/components/ui/select';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Accordion from '$lib/components/ui/accordion';
	import { Loader2, Plus, Play, Pause, Trash2, Clock, CheckCircle2, XCircle, AlertCircle, RefreshCw, Terminal, RotateCcw, Square, Power, FileText, History } from '@lucide/svelte';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { ScheduledTask, TaskExecution } from '$lib/proto/discopanel/v1/task_pb';
	import { TaskType, TaskStatus, ScheduleType, ExecutionStatus, CreateTaskRequestSchema, UpdateTaskRequestSchema, ToggleTaskRequestSchema, TriggerTaskRequestSchema, DeleteTaskRequestSchema, ListTasksRequestSchema, ListTaskExecutionsRequestSchema } from '$lib/proto/discopanel/v1/task_pb';
	import { create } from '@bufbuild/protobuf';

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
	let requireOnline = $state(true);
	let taskConfig = $state('');

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
		selectedTask = null;
	}

	function openCreateDialog() {
		resetForm();
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
		showCreateDialog = true;
	}

	async function saveTask() {
		if (!taskName.trim()) {
			toast.error('Task name is required');
			return;
		}

		creating = true;
		try {
			let config = taskConfig;
			if (!config && taskType === TaskType.COMMAND) {
				config = JSON.stringify({ command: '' });
			}

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
					timezone: timezone,
					config: config,
					timeout: timeout,
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
					timezone: timezone,
					config: config,
					timeout: timeout,
					requireOnline: requireOnline,
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
		if (!confirm(`Are you sure you want to delete the task "${task.name}"?`)) {
			return;
		}
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
								<div class="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center flex-shrink-0">
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
										<Clock class="h-4 w-4" />
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
		<Dialog.Content class="max-w-lg max-h-[90vh] overflow-y-auto">
			<Dialog.Header>
				<Dialog.Title>{selectedTask ? 'Edit Task' : 'Create New Task'}</Dialog.Title>
				<Dialog.Description>
					{selectedTask ? 'Update the scheduled task configuration' : 'Configure a new scheduled task for your server'}
				</Dialog.Description>
			</Dialog.Header>

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
						<Select.Trigger class="w-full">
							{getTaskTypeLabel(taskType)}
						</Select.Trigger>
						<Select.Content>
							<Select.Item value={TaskType.COMMAND.toString()} label="Command">Command</Select.Item>
							<Select.Item value={TaskType.RESTART.toString()} label="Restart Server">Restart Server</Select.Item>
							<Select.Item value={TaskType.START.toString()} label="Start Server">Start Server</Select.Item>
							<Select.Item value={TaskType.STOP.toString()} label="Stop Server">Stop Server</Select.Item>
							<Select.Item value={TaskType.SCRIPT.toString()} label="Script">Script</Select.Item>
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
						<Select.Trigger class="w-full">
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
