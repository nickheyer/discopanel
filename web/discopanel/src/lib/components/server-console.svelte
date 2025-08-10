<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/client';
	import { ResizablePaneGroup, ResizablePane, ResizableHandle } from '$lib/components/ui/resizable';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { Terminal, Send, Loader2, Download, Trash2, RefreshCw } from '@lucide/svelte';
	import type { Server } from '$lib/api/types';

	let { server, active = false }: { server: Server; active?: boolean } = $props();

	let logs = $state('');
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
			startPolling();
		} else {
			stopPolling();
		}
	});

	// Clear logs and fetch new ones when server changes
	let previousServerId = $state(server.id);
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			logs = '';
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
		if (logs && autoScroll && endOfLogsRef) {
			// Use a microtask to ensure DOM has updated
			queueMicrotask(() => {
				endOfLogsRef?.scrollIntoView({ behavior: 'instant', block: 'end' });
			});
		}
	});

	async function fetchLogs() {
		if (loading) return;

		// Don't try to fetch logs if server is not running
		if (server.status !== 'running' && server.status !== 'starting') {
			return;
		}

		try {
			const response = await api.getServerLogs(server.id, tailLines);
			logs = response.logs;
		} catch (error) {
			console.error('Failed to fetch logs:', error);
		}
	}

	async function sendCommand() {
		if (!command.trim()) return;

		loading = true;
		try {
			const response = await api.sendServerCommand(server.id, command);
			if (response.success) {
				// Command executed successfully
				if (response.output) {
					// Add command output to logs temporarily
					logs += `\n> ${command}\n${response.output}`;
				}
				toast.success('Command sent successfully');
			} else {
				toast.error(response.error || 'Failed to execute command');
			}
			command = '';

			// Refresh logs after command
			setTimeout(fetchLogs, 3000);
		} catch (error) {
			toast.error(
				'Failed to send command: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			loading = false;
		}
	}

	function clearLogs() {
		logs = '';
		toast.success('Console cleared');
	}

	function downloadLogs() {
		const blob = new Blob([logs], { type: 'text/plain' });
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

	function formatLogLine(line: string): { timestamp: string; level: string; message: string } {
		// Parse Minecraft log format: [HH:MM:SS] [Thread/LEVEL]: Message
		const mcMatch = line.match(/\[(\d{2}:\d{2}:\d{2})\]\s*\[([^\/]+)\/([A-Z]+)\]:\s*(.+)/);
		if (mcMatch) {
			return {
				timestamp: mcMatch[1],
				level: mcMatch[3],
				message: mcMatch[4]
			};
		}

		// Parse ISO timestamp format from mc-server-runner
		const isoMatch = line.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z)\s+([A-Z]+)\s+(.+)/);
		if (isoMatch) {
			return {
				timestamp: new Date(isoMatch[1]).toLocaleTimeString(),
				level: isoMatch[2],
				message: isoMatch[3]
			};
		}

		// Default format
		return {
			timestamp: '',
			level: 'INFO',
			message: line
		};
	}

	function getLogLevelColor(level: string): string {
		switch (level.toUpperCase()) {
			case 'ERROR':
			case 'FATAL':
				return 'text-red-400';
			case 'WARN':
			case 'WARNING':
				return 'text-yellow-400';
			case 'INFO':
				return 'text-blue-400';
			case 'DEBUG':
				return 'text-gray-400';
			default:
				return 'text-zinc-300';
		}
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
					<Badge variant={server.status === 'running' ? 'default' : 'secondary'} class="text-xs">
						{server.status}
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
						disabled={!logs}
						class="h-7 w-7 p-0 text-zinc-400 hover:text-white"
					>
						<Download class="h-3 w-3" />
					</Button>
					<Button
						size="sm"
						variant="ghost"
						onclick={clearLogs}
						disabled={!logs}
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
				<div class="font-mono text-xs">
					{#if !logs}
						<div class="py-8 text-center text-zinc-500">
							No logs available. {['running', 'starting', 'unhealthy'].includes(server.status) ? 'Try refreshing the page.' : 'Start the server to see output.'}
						</div>
					{:else}
						{#each logs.split('\n') as line}
							{@const parsed = formatLogLine(line)}
							<div class="py-0.5 hover:bg-zinc-900/50 whitespace-pre-wrap break-all">
								{#if parsed.timestamp}
									<span class="text-zinc-600">[{parsed.timestamp}]</span>
								{/if}
								{#if parsed.level !== 'INFO'}
									<span class={getLogLevelColor(parsed.level)}>[{parsed.level}]</span>
								{/if}
								<span class="text-zinc-300">
									{parsed.message}
								</span>
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
					placeholder={server.status === 'running' ? 'Enter command...' : 'Server must be running'}
					bind:value={command}
					disabled={server.status !== 'running'}
					onkeydown={(e) => e.key === 'Enter' && sendCommand()}
					class="flex-1 bg-transparent font-mono text-sm text-white outline-none placeholder:text-zinc-600"
				/>
			</div>
			<Button
				onclick={sendCommand}
				disabled={server.status !== 'running' || !command.trim()}
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
				{logs.split('\n').length} lines
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
</style>
