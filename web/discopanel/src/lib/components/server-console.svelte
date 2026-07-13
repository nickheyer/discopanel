<script lang="ts">
	import { onDestroy, untrack } from 'svelte';
	import { page } from '$app/state';
	import { rpcClient } from '$lib/api/rpc-client';
	import { create } from '@bufbuild/protobuf';
	import { timestampDate } from '@bufbuild/protobuf/wkt';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import { ServerStatus } from '$lib/proto/discopanel/v1/common_pb';
	import type { LogEntry, ServerAction } from '$lib/proto/discopanel/v1/server_pb';
	import {
		GetServerLogsRequestSchema,
		ClearServerLogsRequestSchema,
		SendCommandRequestSchema,
		UploadToMCLogsRequestSchema
	} from '$lib/proto/discopanel/v1/server_pb';
	import { Button } from '$lib/components/ui/button';
	import { toast } from 'svelte-sonner';
	import {
		Send,
		Loader2,
		Download,
		Share,
		Trash2,
		RefreshCw,
		ArrowDown,
		ChevronDown,
		Activity,
		Terminal,
		ChevronRight
	} from '@lucide/svelte';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import AnsiToHtml from 'ansi-to-html';
	import { statusMeta, isUp, TONE_BG } from '$lib/server-status';
	import { wsClient } from '$lib/stores/websocket.svelte';

	const ansiConverter = new AnsiToHtml({
		fg: '#e8e8e8',
		bg: '#000000',
		newline: false,
		escapeXML: true,
		stream: true
	});

	let { server, active = false }: { server: Server; active?: boolean } = $props();

	let logEntries = $state<LogEntry[]>([]);
	let command = $state('');
	let loading = $state(false);
	let autoScroll = $state(true);
	let unseenLines = $state(0);
	let scrollAreaRef = $state<HTMLDivElement | null>(null);
	let inputRef = $state<HTMLInputElement | null>(null);
	let tailLines = $state(500);
	const MAX_LOG_ENTRIES = 5000;
	const TAIL_OPTIONS = [100, 500, 1000, 2000];

	// Second channel holds everything DiscoPanel did to the server
	let channel = $state<'server' | 'actions'>('server');
	let actions = $state<ServerAction[]>([]);
	let actionsLoaded = $state(false);
	let highlightMs = $state(0);
	let sourceFilter = $state('all');
	let expandedId = $state<bigint | null>(null);

	// Subsystems get one look, users another
	const AUTOMATION_SOURCES = new Set([
		'panel',
		'runtime',
		'crash doctor',
		'scheduler',
		'provisioner',
		'mod check',
		'autopause',
		'autostop',
		'autostart',
		'wake-on-connect'
	]);

	let actionSources = $derived.by(() => {
		const seen = new Set<string>();
		for (const a of actions) if (a.source) seen.add(a.source);
		return [...seen].sort();
	});
	let visibleActions = $derived(
		sourceFilter === 'all' ? actions : actions.filter((a) => a.source === sourceFilter)
	);

	function toggleExpanded(a: ServerAction) {
		expandedId = expandedId === a.id ? null : a.id;
	}

	function actionDetails(a: ServerAction): [string, string][] {
		const rows: [string, string][] = [];
		if (a.name) rows.push(['action', a.name]);
		for (const key of Object.keys(a.attrs).sort()) rows.push([key, a.attrs[key]]);
		if (a.traceId) rows.push(['trace', a.traceId]);
		return rows;
	}

	// Repins so the pinned-view effect lands on the newest line
	function switchChannel(next: 'server' | 'actions') {
		if (channel === next) return;
		channel = next;
		autoScroll = true;
		unseenLines = 0;
		expandedId = null;
	}

	// Health dialog deep links pick the channel and highlight window
	$effect(() => {
		const ch = page.url.searchParams.get('channel');
		const hl = page.url.searchParams.get('hl');
		untrack(() => {
			if (ch === 'actions') channel = 'actions';
			highlightMs = hl ? Number(hl) : 0;
		});
	});

	$effect(() => {
		if (!active || channel !== 'actions') return;
		untrack(() => loadActions(true));
		const timer = setInterval(() => loadActions(false), 5000);
		return () => clearInterval(timer);
	});

	async function loadActions(initial: boolean) {
		try {
			const last = actions.length > 0 ? actions[actions.length - 1].id : 0n;
			const res = await rpcClient.server.getServerActions({
				id: server.id,
				afterId: initial ? 0n : last
			});
			if (initial) {
				// Deep links inspect history, unpin before rows render
				if (highlightMs > 0) autoScroll = false;
				actions = res.actions;
			} else if (res.actions.length > 0) {
				actions = [...actions, ...res.actions];
			}
			actionsLoaded = true;
			if (initial) queueMicrotask(scrollToActionAnchor);
		} catch {
			// Poll silently, the toggle shows the empty state
		}
	}

	function actionHighlighted(a: ServerAction) {
		if (highlightMs <= 0 || !a.timestamp) return false;
		return timestampDate(a.timestamp).getTime() >= highlightMs;
	}

	// Lands on the highlight line, else pins to newest
	function scrollToActionAnchor() {
		if (!scrollAreaRef) return;
		const target = scrollAreaRef.querySelector('.action-highlight');
		if (target) {
			target.scrollIntoView({ block: 'center' });
		} else {
			autoScroll = true;
			scrollAreaRef.scrollTop = scrollAreaRef.scrollHeight;
		}
	}

	function actionTime(a: ServerAction) {
		if (!a.timestamp) return '';
		return timestampDate(a.timestamp).toLocaleString(undefined, {
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		});
	}

	// Command history navigated with arrow keys
	let history: string[] = [];
	let historyIndex = $state(-1);
	let draftCommand = '';

	let wsConnectionState = $derived(wsClient.state.connectionState);
	let meta = $derived(statusMeta(server.status));
	let canSend = $derived(isUp(server.status));

	let cleanupHandlers: (() => void)[] = [];
	let previousServerId = server.id;

	onDestroy(() => {
		untrack(() => cleanupWebSocket());
	});

	// Follow the active tab to hold the subscription
	$effect(() => {
		if (active) {
			untrack(() => setupWebSocket());
		} else {
			untrack(() => cleanupWebSocket());
		}
	});

	// Swap subscriptions when viewing a different server
	$effect(() => {
		const currentServerId = server.id;
		if (currentServerId !== previousServerId) {
			const oldServerId = previousServerId;
			previousServerId = currentServerId;

			untrack(() => {
				wsClient.unsubscribe(oldServerId);
				logEntries = [];
				actions = [];
				actionsLoaded = false;
				sourceFilter = 'all';
				expandedId = null;
				command = '';
				unseenLines = 0;

				if (active) {
					wsClient.subscribe(currentServerId, tailLines);
					if (channel === 'actions') loadActions(true);
				}
			});
		}
	});

	function setupWebSocket() {
		cleanupWebSocket();
		wsClient.connect();

		const unsubLogs = wsClient.onLogs((serverId, logs) => {
			if (serverId === server.id) {
				logEntries = logs.length > MAX_LOG_ENTRIES ? logs.slice(-MAX_LOG_ENTRIES) : logs;
			}
		});

		const unsubLogEntry = wsClient.onLogEntry((serverId, logs) => {
			if (serverId === server.id && logs.length > 0) {
				const combined = [...logEntries, ...logs];
				logEntries =
					combined.length > MAX_LOG_ENTRIES ? combined.slice(-MAX_LOG_ENTRIES) : combined;
				if (!autoScroll) unseenLines += logs.length;
			}
		});

		const unsubCommandResult = wsClient.onCommandResult((result) => {
			if (result.serverId === server.id) {
				loading = false;
				if (!result.success) {
					toast.error(result.error || 'Failed to execute command');
				}
			}
		});

		cleanupHandlers = [unsubLogs, unsubLogEntry, unsubCommandResult];
		wsClient.subscribe(server.id, tailLines);
	}

	function cleanupWebSocket() {
		wsClient.unsubscribe(server.id);
		cleanupHandlers.forEach((cleanup) => cleanup());
		cleanupHandlers = [];
	}

	// Keeps the view pinned on the active channel
	$effect(() => {
		const count = channel === 'actions' ? visibleActions.length : logEntries.length;
		if (count > 0 && autoScroll && scrollAreaRef) {
			queueMicrotask(() => {
				if (scrollAreaRef) {
					scrollAreaRef.scrollTop = scrollAreaRef.scrollHeight;
				}
			});
		}
	});

	function handleScroll() {
		if (!scrollAreaRef) return;

		const { scrollTop, scrollHeight, clientHeight } = scrollAreaRef;
		const isAtBottom = scrollHeight - scrollTop - clientHeight < 5;

		if (isAtBottom && !autoScroll) {
			autoScroll = true;
			unseenLines = 0;
		} else if (!isAtBottom && autoScroll) {
			autoScroll = false;
		}
	}

	function jumpToBottom() {
		if (!scrollAreaRef) return;
		scrollAreaRef.scrollTop = scrollAreaRef.scrollHeight;
		autoScroll = true;
		unseenLines = 0;
	}

	async function fetchLogs() {
		if (loading) return;
		if (server.status === ServerStatus.STOPPED) return;

		try {
			const request = create(GetServerLogsRequestSchema, {
				id: server.id,
				tail: tailLines
			});
			const response = await rpcClient.server.getServerLogs(request);
			const logs = response.logs || [];
			logEntries = logs.length > MAX_LOG_ENTRIES ? logs.slice(-MAX_LOG_ENTRIES) : logs;
		} catch (error) {
			console.error('Failed to fetch logs:', error);
		}
	}

	function navigateHistory(direction: -1 | 1) {
		if (history.length === 0) return;
		if (historyIndex === -1) {
			if (direction === 1) return;
			draftCommand = command;
			historyIndex = history.length - 1;
		} else {
			const next = historyIndex + direction;
			if (next >= history.length) {
				historyIndex = -1;
				command = draftCommand;
				return;
			}
			historyIndex = Math.max(next, 0);
		}
		command = history[historyIndex] ?? '';
	}

	function handleInputKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			sendCommand();
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			navigateHistory(-1);
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			navigateHistory(1);
		}
	}

	async function sendCommand() {
		if (!command.trim() || !canSend) return;

		loading = true;
		const cmdToSend = command;
		command = '';
		if (history[history.length - 1] !== cmdToSend) {
			history.push(cmdToSend);
			if (history.length > 100) history.shift();
		}
		historyIndex = -1;
		draftCommand = '';

		// Prefer the socket and fall back to rpc
		if (wsClient.isReady) {
			wsClient.sendCommand(server.id, cmdToSend);
		} else {
			try {
				const request = create(SendCommandRequestSchema, {
					id: server.id,
					command: cmdToSend
				});
				const response = await rpcClient.server.sendCommand(request);
				if (!response.success) {
					toast.error(response.error || 'Failed to execute command');
				}
			} catch (error) {
				console.error(
					'Failed to send command: ' + (error instanceof Error ? error.message : 'Unknown error')
				);
			} finally {
				loading = false;
			}
		}
	}

	async function clearLogs() {
		const request = create(ClearServerLogsRequestSchema, {
			id: server.id
		});
		await rpcClient.server.clearServerLogs(request);
		logEntries = [];
		unseenLines = 0;
		toast.success('Console cleared');
	}

	let uploading = $state(false);

	async function uploadToMCLogs() {
		if (uploading) return;
		uploading = true;
		try {
			const request = create(UploadToMCLogsRequestSchema, { id: server.id });
			const response = await rpcClient.server.uploadToMCLogs(request);
			await navigator.clipboard.writeText(response.url);
			toast.success('mclo.gs URL copied to clipboard');
		} catch (error) {
			toast.error(
				'Failed to upload to mclo.gs: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			uploading = false;
		}
	}

	function downloadLogs() {
		const logText = logEntries.map((entry) => entry.message).join('\n');
		const blob = new Blob([logText], { type: 'text/plain' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `${server.name}-logs-${new Date().toISOString()}.txt`;
		document.body.appendChild(a);
		a.click();
		document.body.removeChild(a);
		URL.revokeObjectURL(url);
		toast.success('Logs downloaded');
	}

	function handleTailChange() {
		// Resubscribe so the socket honors the new tail
		if (wsClient.isReady) {
			wsClient.unsubscribe(server.id);
			wsClient.subscribe(server.id, tailLines);
		} else {
			fetchLogs();
		}
	}

	let streamLive = $derived(wsConnectionState === 'authenticated');
	let streamLabel = $derived(
		wsConnectionState === 'authenticated'
			? 'Live stream connected'
			: wsConnectionState === 'connecting'
				? 'Connecting to stream...'
				: wsConnectionState === 'connected'
					? 'Authenticating stream...'
					: 'Stream offline, polling instead'
	);
</script>

<div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-terminal shadow-sm">
	<div class="flex shrink-0 items-center gap-3 border-b border-white/8 bg-white/3 px-3 py-2">
		<div class="flex min-w-0 items-center gap-2">
			<span class="relative flex size-2 shrink-0">
				{#if meta.transitional}
					<span
						class="absolute inline-flex h-full w-full animate-ping rounded-full opacity-60 {TONE_BG[
							meta.tone
						]}"
					></span>
				{/if}
				<span class="relative inline-flex size-2 rounded-full {TONE_BG[meta.tone]}"></span>
			</span>
			<span class="truncate font-mono text-xs font-medium tracking-wide text-white/80">
				{server.name}
			</span>
			<span class="shrink-0 font-mono text-xs text-white/40">{meta.label.toLowerCase()}</span>
		</div>

		<Tooltip.Root>
			<Tooltip.Trigger>
				<span
					class="inline-flex items-center gap-1.5 rounded-full border border-white/10 px-2 py-0.5 font-mono text-[10px] tracking-wide {streamLive
						? 'text-status-ok'
						: 'text-white/40'}"
				>
					<span
						class="size-1.5 rounded-full {streamLive ? 'bg-status-ok' : 'bg-white/30'}"
						class:animate-pulse={streamLive}
					></span>
					{streamLive ? 'live' : 'polling'}
				</span>
			</Tooltip.Trigger>
			<Tooltip.Content>{streamLabel}</Tooltip.Content>
		</Tooltip.Root>

		<div
			class="flex shrink-0 items-center gap-0.5 rounded-md border border-white/10 p-0.5 font-mono text-[10px]"
		>
			<button
				class="flex items-center gap-1 rounded px-2 py-0.5 transition-colors {channel === 'server'
					? 'bg-white/10 text-white'
					: 'text-white/40 hover:text-white/70'}"
				aria-pressed={channel === 'server'}
				onclick={() => switchChannel('server')}
			>
				<Terminal class="size-3" />
				server
			</button>
			<button
				class="flex items-center gap-1 rounded px-2 py-0.5 transition-colors {channel === 'actions'
					? 'bg-white/10 text-white'
					: 'text-white/40 hover:text-white/70'}"
				aria-pressed={channel === 'actions'}
				onclick={() => switchChannel('actions')}
			>
				<Activity class="size-3" />
				activity
			</button>
		</div>

		<div class="ml-auto flex shrink-0 items-center gap-2" class:hidden={channel !== 'actions'}>
			<div
				class="flex h-6 items-center rounded-md border border-white/10 pl-2 font-mono text-[11px]"
				title="Filter by source"
			>
				<span class="text-white/35">Source:</span>
				<span class="relative flex h-full items-center">
					<select
						bind:value={sourceFilter}
						class="h-full appearance-none bg-transparent pr-5 pl-1.5 font-mono text-[11px] text-white/70 focus:outline-none"
					>
						<option value="all" class="bg-terminal">all</option>
						{#each actionSources as source (source)}
							<option value={source} class="bg-terminal">{source}</option>
						{/each}
					</select>
					<ChevronDown class="pointer-events-none absolute right-1.5 size-3 text-white/35" />
				</span>
			</div>
		</div>

		<div class="flex shrink-0 items-center gap-2" class:hidden={channel === 'actions'}>
			<div
				class="flex h-6 items-center rounded-md border border-white/10 pl-2 font-mono text-[11px]"
				title="Lines loaded / lines of history to keep"
			>
				<span class="text-white/35">Lines:</span>
				<span class="px-1.5 text-white/70 tabular-nums">{logEntries.length}</span>
				<span class="text-white/25">/</span>
				<span class="relative flex h-full items-center">
					<select
						bind:value={tailLines}
						onchange={handleTailChange}
						class="h-full appearance-none bg-transparent pr-5 pl-1.5 font-mono text-[11px] text-white/70 tabular-nums focus:outline-none"
					>
						{#each TAIL_OPTIONS as option (option)}
							<option value={option} class="bg-terminal">{option}</option>
						{/each}
					</select>
					<ChevronDown class="pointer-events-none absolute right-1.5 size-3 text-white/35" />
				</span>
			</div>
			<div class="flex items-center gap-0.5 border-l border-white/10 pl-2">
				<Tooltip.Root>
					<Tooltip.Trigger>
						<Button
							size="icon"
							variant="ghost"
							onclick={fetchLogs}
							disabled={loading}
							class="size-6.5 text-white/45 hover:bg-white/10 hover:text-white"
						>
							{#if loading}
								<Loader2 class="size-3.5 animate-spin" />
							{:else}
								<RefreshCw class="size-3.5" />
							{/if}
						</Button>
					</Tooltip.Trigger>
					<Tooltip.Content>Refresh logs</Tooltip.Content>
				</Tooltip.Root>
				<Tooltip.Root>
					<Tooltip.Trigger>
						<Button
							size="icon"
							variant="ghost"
							onclick={uploadToMCLogs}
							disabled={uploading || logEntries.length === 0}
							class="size-6.5 text-white/45 hover:bg-white/10 hover:text-white"
						>
							{#if uploading}
								<Loader2 class="size-3.5 animate-spin" />
							{:else}
								<Share class="size-3.5" />
							{/if}
						</Button>
					</Tooltip.Trigger>
					<Tooltip.Content>Share via mclo.gs</Tooltip.Content>
				</Tooltip.Root>
				<Tooltip.Root>
					<Tooltip.Trigger>
						<Button
							size="icon"
							variant="ghost"
							onclick={downloadLogs}
							disabled={logEntries.length === 0}
							class="size-6.5 text-white/45 hover:bg-white/10 hover:text-white"
						>
							<Download class="size-3.5" />
						</Button>
					</Tooltip.Trigger>
					<Tooltip.Content>Download logs</Tooltip.Content>
				</Tooltip.Root>
				<Tooltip.Root>
					<Tooltip.Trigger>
						<Button
							size="icon"
							variant="ghost"
							onclick={clearLogs}
							disabled={logEntries.length === 0}
							class="size-6.5 text-white/45 hover:bg-white/10 hover:text-white"
						>
							<Trash2 class="size-3.5" />
						</Button>
					</Tooltip.Trigger>
					<Tooltip.Content>Clear console</Tooltip.Content>
				</Tooltip.Root>
			</div>
		</div>
	</div>

	<div class="relative min-h-48 flex-1">
		<div
			class="absolute inset-0 overflow-x-auto overflow-y-auto px-4 py-3"
			bind:this={scrollAreaRef}
			onscroll={handleScroll}
		>
			{#if channel === 'actions'}
				{#if visibleActions.length === 0}
					<div class="flex h-full flex-col items-center justify-center gap-1.5 text-white/30">
						<Activity class="size-6" />
						<p class="font-mono text-sm">No activity yet</p>
						<p class="font-mono text-xs">
							{actionsLoaded ? 'Starts, stops, repairs, and mod changes appear here' : 'Loading...'}
						</p>
					</div>
				{:else}
					<div class="font-mono text-xs leading-relaxed text-zinc-300">
						{#each visibleActions as a (a.id)}
							{@const details = actionDetails(a)}
							<button
								class="action-line flex w-full items-baseline gap-2 text-left break-words whitespace-pre-wrap {actionHighlighted(
									a
								)
									? 'action-highlight'
									: ''}"
								onclick={() => toggleExpanded(a)}
							>
								<ChevronRight
									class="size-3 shrink-0 self-center text-white/30 transition-transform {expandedId ===
									a.id
										? 'rotate-90'
										: ''} {details.length === 0 ? 'invisible' : ''}"
								/>
								<span class="shrink-0 text-white/35">{actionTime(a)}</span>
								<span
									class="shrink-0 rounded border px-1 text-[10px] tracking-wide uppercase {AUTOMATION_SOURCES.has(
										a.source
									)
										? 'border-white/10 text-white/50'
										: 'border-sky-400/30 text-sky-300/80'}"
								>
									{a.source}
								</span>
								<span>{a.message}</span>
							</button>
							{#if expandedId === a.id && details.length > 0}
								<div class="action-detail ml-9 grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5">
									{#each details as [key, value] (key)}
										<span class="text-white/35">{key}</span>
										<span class="break-all text-white/70">{value}</span>
									{/each}
								</div>
							{/if}
						{/each}
					</div>
				{/if}
			{:else if logEntries.length === 0}
				<div class="flex h-full flex-col items-center justify-center gap-1.5 text-white/30">
					<ChevronRight class="size-6" />
					<p class="font-mono text-sm">No output</p>
					<p class="font-mono text-xs">
						{[ServerStatus.STOPPED, ServerStatus.ERROR].includes(server.status)
							? 'Start the server to see logs here'
							: 'Waiting for output...'}
					</p>
				</div>
			{:else}
				<div class="font-mono text-xs leading-relaxed text-zinc-300">
					{#each logEntries as entry, i (i)}
						<div
							class="log-line break-all whitespace-pre-wrap"
							data-type={entry.source === 'command' || entry.source === 'command_output'
								? entry.source
								: entry.level}
						>
							<!-- eslint-disable-next-line svelte/no-at-html-tags -->
							{@html ansiConverter.toHtml(entry.message)}
						</div>
					{/each}
				</div>
			{/if}
		</div>

		{#if !autoScroll}
			<button
				class="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-white/15 bg-terminal/95 px-3 py-1 font-mono text-xs text-white/80 shadow-lg backdrop-blur-sm transition-colors hover:bg-white/10"
				onclick={jumpToBottom}
			>
				<ArrowDown class="size-3" />
				{#if channel === 'server' && unseenLines > 0}
					{unseenLines} new {unseenLines === 1 ? 'line' : 'lines'}
				{:else}
					latest
				{/if}
			</button>
		{/if}
	</div>

	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
	<div
		class="flex shrink-0 cursor-text items-center gap-2 border-t border-white/8 bg-black/25 px-3.5 py-2.5"
		class:hidden={channel === 'actions'}
		onclick={() => inputRef?.focus()}
	>
		<span
			class="shrink-0 font-mono text-sm font-semibold {canSend
				? 'text-status-ok'
				: 'text-white/25'}"
		>
			❯
		</span>
		<input
			type="text"
			bind:this={inputRef}
			placeholder={canSend
				? 'Type a command, ↑ for history'
				: 'Server must be running to send commands'}
			bind:value={command}
			disabled={!canSend}
			onkeydown={handleInputKeydown}
			spellcheck="false"
			autocomplete="off"
			class="min-w-0 flex-1 bg-transparent font-mono text-sm text-white outline-none placeholder:text-white/25 disabled:cursor-not-allowed"
		/>
		<Button
			onclick={sendCommand}
			disabled={!canSend || !command.trim()}
			size="sm"
			variant="ghost"
			class="h-7 shrink-0 gap-1.5 px-2.5 font-mono text-xs text-white/60 hover:bg-white/10 hover:text-white disabled:text-white/20"
		>
			<Send class="size-3" />
			run
		</Button>
	</div>
</div>

<style>
	.log-line {
		padding: 0.125rem 0;
		line-height: 1.45;
	}

	.log-line:hover {
		background-color: rgba(255, 255, 255, 0.04);
	}

	.log-line[data-type='command'] {
		color: #4ade80;
		font-weight: 500;
	}

	.log-line[data-type='command']::before {
		content: '❯ ';
		color: #22c55e;
		font-weight: bold;
	}

	.log-line[data-type='command_output'] {
		opacity: 0.9;
		padding-left: 1rem;
	}

	.log-line[data-type='warn'] {
		color: #fbbf24;
	}

	.log-line[data-type='error'],
	.log-line[data-type='fatal'] {
		color: #f87171;
	}

	.action-line {
		padding: 0.2rem 0.375rem;
		line-height: 1.45;
		border-radius: 0.25rem;
	}

	.action-line:hover {
		background-color: rgba(255, 255, 255, 0.04);
	}

	.action-highlight {
		background-color: rgba(139, 92, 246, 0.12);
		box-shadow: inset 2px 0 0 rgba(139, 92, 246, 0.7);
	}

	.action-detail {
		padding: 0.2rem 0.375rem 0.35rem;
		border-left: 1px solid rgba(255, 255, 255, 0.12);
		font-size: 11px;
		line-height: 1.4;
	}
</style>
