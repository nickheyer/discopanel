<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { create } from '@bufbuild/protobuf';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import { ServerStatus } from '$lib/proto/discopanel/v1/common_pb';
	import type { LogEntry } from '$lib/proto/discopanel/v1/server_pb';
	import { GetServerLogsRequestSchema, ClearServerLogsRequestSchema, SendCommandRequestSchema } from '$lib/proto/discopanel/v1/server_pb';
	import { ResizablePaneGroup, ResizablePane, ResizableHandle } from '$lib/components/ui/resizable';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { Terminal, Send, Loader2, Download, Trash2, RefreshCw } from '@lucide/svelte';
	import AnsiToHtml from 'ansi-to-html';
	import { getStringForEnum } from '$lib/utils';

	// Create ansi-to-html converter with proper options
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
	let scrollAreaRef = $state<HTMLDivElement | null>(null);
	let endOfLogsRef = $state<HTMLDivElement | null>(null);
	let pollingInterval: ReturnType<typeof setInterval> | null = null;
	let tailLines = $state(500);

	onMount(() => {
		if (active) {
			fetchLogs();
			startPolling();
		}
	});

	onDestroy(() => {
		stopPolling();
	});

	// Start/stop polling based on active prop
	$effect(() => {
		if (active) {
			fetchLogs();
		} else {
			stopPolling();
		}
	});

	// Clear logs and fetch new ones when server changes
	let previousServerId = $state(server.id);
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			logEntries = [];
			command = '';
			if (active) {
				fetchLogs();
			}
		}
	});

	function startPolling() {
		if (!pollingInterval) {
			pollingInterval = setInterval(fetchLogs, 3000);
		}
	}

	function stopPolling() {
		if (pollingInterval) {
			clearInterval(pollingInterval);
			pollingInterval = null;
		}
	}

	// Handle auto-scrolling in a separate effect to avoid scroll-linked positioning issues
	$effect(() => {
		if (logEntries.length > 0 && autoScroll && endOfLogsRef) {
			// Use a microtask to ensure DOM has updated
			queueMicrotask(() => {
				endOfLogsRef?.scrollIntoView({ behavior: 'instant', block: 'end' });
			});
		}
	});

	async function fetchLogs() {
		if (loading) return;

		// Don't try to fetch logs if server is not running
		if (server.status === ServerStatus.STOPPED) {
			return;
		}

		try {
			const request = create(GetServerLogsRequestSchema, {
				id: server.id,
				tail: tailLines
			});
			const response = await rpcClient.server.getServerLogs(request);
			logEntries = response.logs || [];
		} catch (error) {
			console.error('Failed to fetch logs:', error);
		}
	}

	async function sendCommand() {
		if (!command.trim()) return;

		loading = true;
		try {
			const request = create(SendCommandRequestSchema, {
				id: server.id,
				command: command
			});
			const response = await rpcClient.server.sendCommand(request);
			if (response.success) {
				toast.success('Command sent successfully');
			} else {
				toast.error(response.error || 'Failed to execute command');
			}
			command = '';

			// Refresh logs after delay to see cmd output
			setTimeout(fetchLogs, 1000);
		} catch (error) {
			toast.error(
				'Failed to send command: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			loading = false;
		}
	}

	async function clearLogs() {
		const request = create(ClearServerLogsRequestSchema, {
			id: server.id
		});
		await rpcClient.server.clearServerLogs(request);
		logEntries = [];
		toast.success('Console cleared');
	}

	function downloadLogs() {
		const logText = logEntries.map(entry => entry.message).join('\n');
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
</script>

<ResizablePaneGroup
	direction="vertical"
	class="h-full max-h-[800px] min-h-[400px] w-full rounded-lg border bg-black overflow-hidden"
>
	<ResizablePane defaultSize={75} minSize={30}>
		<div class="flex h-full flex-col">
			<div class="flex items-center justify-between border-b border-zinc-800 bg-zinc-900 px-4 py-2">
				<div class="flex items-center gap-2">
					<Terminal class="h-4 w-4 text-green-500" />
					<span class="font-mono text-sm text-green-500">Server Console</span>
					<Badge variant={(server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY) ? 'default' : 'secondary'} class="text-xs">
						{getStringForEnum(ServerStatus, server.status)?.toLowerCase()}
					</Badge>
				</div>
				<div class="flex items-center gap-1">
					<Button
						size="sm"
						variant="ghost"
						onclick={fetchLogs}
						disabled={loading}
						class="h-7 w-7 p-0 text-zinc-400 hover:text-white"
					>
						{#if loading}
							<Loader2 class="h-3 w-3 animate-spin" />
						{:else}
							<RefreshCw class="h-3 w-3" />
						{/if}
					</Button>
					<Button
						size="sm"
						variant="ghost"
						onclick={downloadLogs}
						disabled={logEntries.length === 0}
						class="h-7 w-7 p-0 text-zinc-400 hover:text-white"
					>
						<Download class="h-3 w-3" />
					</Button>
					<Button
						size="sm"
						variant="ghost"
						onclick={clearLogs}
						disabled={logEntries.length === 0}
						class="h-7 w-7 p-0 text-zinc-400 hover:text-white"
					>
						<Trash2 class="h-3 w-3" />
					</Button>
				</div>
			</div>
			<div
				class="custom-scrollbar min-h-0 flex-1 overflow-y-auto overflow-x-auto bg-black px-4 py-2"
				bind:this={scrollAreaRef}
			>
				<div class="font-mono text-xs text-zinc-300">
					{#if logEntries.length === 0}
						<div class="py-8 text-center text-zinc-500">
							No logs available. {[ServerStatus.RUNNING, ServerStatus.STARTING, ServerStatus.UNHEALTHY].includes(server.status) ? 'Try refreshing the page.' : 'Start the server to see output.'}
						</div>
					{:else}
						{#each logEntries as entry}
							<div class="log-line whitespace-pre-wrap break-all" data-type={entry.level}>
								{@html ansiConverter.toHtml(entry.message)}
							</div>
						{/each}
					{/if}
					<div bind:this={endOfLogsRef} aria-hidden="true"></div>
				</div>
			</div>
		</div>
	</ResizablePane>

	<ResizableHandle class="bg-zinc-800 hover:bg-zinc-700" />

	<div class="flex flex-col bg-zinc-950">
		<div class="flex flex-shrink-0 gap-2 border-t border-zinc-800 p-3">
			<div class="flex flex-1 items-center gap-2">
				<span class="font-mono text-sm text-green-500">$</span>
				<input
					type="text"
					placeholder={(server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY)? 'Enter command...' : 'Server must be running'}
					bind:value={command}
					disabled={server.status !== ServerStatus.RUNNING && server.status !== ServerStatus.UNHEALTHY}
					onkeydown={(e) => e.key === 'Enter' && sendCommand()}
					class="flex-1 bg-transparent font-mono text-sm text-white outline-none placeholder:text-zinc-600"
				/>
			</div>
			<Button
				onclick={sendCommand}
				disabled={server.status === ServerStatus.STOPPED || !command.trim()}
				size="sm"
				class="h-7 bg-zinc-800 px-3 text-white hover:bg-zinc-700"
			>
				<Send class="h-3 w-3" />
			</Button>
		</div>

		<div class="flex flex-shrink-0 items-center justify-between px-3 pb-2 text-xs text-zinc-500">
			<div class="flex items-center gap-4">
				<label class="flex items-center gap-2">
					<input type="checkbox" bind:checked={autoScroll} class="h-3 w-3 rounded" />
					Auto-scroll
				</label>
				<div class="flex items-center gap-2">
					<span>Tail:</span>
					<select
						bind:value={tailLines}
						onchange={fetchLogs}
						class="rounded border border-zinc-800 bg-zinc-900 px-2 py-0.5 text-xs"
					>
						<option value={100}>100</option>
						<option value={500}>500</option>
						<option value={1000}>1000</option>
						<option value={2000}>2000</option>
					</select>
				</div>
			</div>
			<div class="font-mono">
				{logEntries.length} lines
			</div>
		</div>
	</div>
</ResizablePaneGroup>

<style>
	.custom-scrollbar {
		scrollbar-width: thin;
		scrollbar-color: hsl(var(--muted-foreground) / 0.3) transparent;
	}

	.custom-scrollbar::-webkit-scrollbar {
		width: 12px;
	}

	.custom-scrollbar::-webkit-scrollbar-track {
		background: transparent;
	}

	.custom-scrollbar::-webkit-scrollbar-thumb {
		background-color: hsl(var(--muted-foreground) / 0.3);
		border-radius: 6px;
		border: 3px solid transparent;
		background-clip: content-box;
	}

	.custom-scrollbar::-webkit-scrollbar-thumb:hover {
		background-color: hsl(var(--muted-foreground) / 0.5);
	}

	.log-line {
		padding: 0.125rem 0;
		line-height: 1.4;
	}

	.log-line:hover {
		background-color: rgba(39, 39, 42, 0.5);
	}

	/* Visually distinguish command inputs */
	.log-line[data-type="command"] {
		color: #4ade80;
		font-weight: 500;
	}

	.log-line[data-type="command"]::before {
		content: '$ ';
		color: #22c55e;
		font-weight: bold;
	}

	/* Style command output differently */
	.log-line[data-type="command_output"] {
		opacity: 0.9;
		padding-left: 1rem;
	}
</style>
