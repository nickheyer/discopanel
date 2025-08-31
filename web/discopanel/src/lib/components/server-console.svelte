<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/client';
	import { ResizablePaneGroup, ResizablePane, ResizableHandle } from '$lib/components/ui/resizable';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { Terminal, Send, Loader2, Download, Trash2, RefreshCw } from '@lucide/svelte';
	import type { Server, CommandEntry } from '$lib/api/types';

	let { server, active = false }: { server: Server; active?: boolean } = $props();

	let logs = $state('');
	let commandHistory = $state<CommandEntry[]>([]);
	let command = $state('');
	let loading = $state(false);
	let autoScroll = $state(true);
	let scrollAreaRef = $state<HTMLDivElement | null>(null);
	let endOfLogsRef = $state<HTMLDivElement | null>(null);
	let pollingInterval: ReturnType<typeof setInterval> | null = null;
	let tailLines = $state(500);

	onMount(() => {
		if (active) {
			// Load command history first to ensure persistence across refreshes
			fetchCommandHistory();
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
			// Ensure command history is loaded first for proper persistence
			fetchCommandHistory();
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
			commandHistory = [];
			command = '';
			if (active) {
				// Load command history first to ensure persistence
				fetchCommandHistory();
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
		if ((logs || commandHistory.length > 0) && autoScroll && endOfLogsRef) {
			// Use a microtask to ensure DOM has updated
			queueMicrotask(() => {
				endOfLogsRef?.scrollIntoView({ behavior: 'instant', block: 'end' });
			});
		}
	});

	async function fetchLogs() {
		if (loading) return;

		// Don't try to fetch logs if server is not running
		if (server.status === 'stopped') {
			return;
		}

		try {
			const response = await api.getServerLogs(server.id, tailLines);
			logs = response.logs;
		} catch (error) {
			console.error('Failed to fetch logs:', error);
		}
	}

	async function fetchCommandHistory() {
		try {
			const response = await api.getServerCommandHistory(server.id, 50);
			commandHistory = response.commands || [];
		} catch (error) {
			console.error('Failed to fetch command history:', error);
			// Don't clear existing history on error to maintain persistence
			if (commandHistory.length === 0) {
				commandHistory = [];
			}
		}
	}

	async function sendCommand() {
		if (!command.trim()) return;

		loading = true;
		const commandToExecute = command;
		command = ''; // Clear input immediately for better UX
		
		try {
			const response = await api.sendServerCommand(server.id, commandToExecute);
			if (response.success) {
				toast.success('Command sent successfully');
			} else {
				toast.error(response.error || 'Failed to execute command');
			}

			// Refresh command history immediately to show the new command with output
			await fetchCommandHistory();
			
			// Refresh logs after a short delay to capture any server-side effects
			setTimeout(fetchLogs, 1000);
			
			// Ensure auto-scroll is triggered to show the new command output
			if (autoScroll && endOfLogsRef) {
				setTimeout(() => {
					endOfLogsRef?.scrollIntoView({ behavior: 'smooth', block: 'end' });
				}, 100);
			}
		} catch (error) {
			// Restore command in input if there was an error
			command = commandToExecute;
			toast.error(
				'Failed to send command: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			loading = false;
		}
	}

	function clearLogs() {
		logs = '';
		commandHistory = [];
		toast.success('Console cleared');
	}

	function downloadLogs() {
		const combinedDisplay = getCombinedDisplay();
		const content = combinedDisplay.map(item => {
			if (item.type === 'command') {
				// Strip HTML tags for plain text download and format nicely
				const cleanContent = item.content.replace(/<[^>]*>/g, '');
				return `--- COMMAND ---\n${cleanContent}\n--- END COMMAND ---`;
			}
			return item.content;
		}).join('\n');
		
		const blob = new Blob([content], { type: 'text/plain' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `${server.name}-console-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.txt`;
		document.body.appendChild(a);
		a.click();
		document.body.removeChild(a);
		URL.revokeObjectURL(url);
		toast.success('Console downloaded with command history');
	}

	function formatLogLine(line: string): { timestamp: string; level: string; message: string; rawTimestamp?: string } {
		// Parse Minecraft log format: [HH:MM:SS] [Thread/LEVEL]: Message
		const mcMatch = line.match(/\[(\d{2}:\d{2}:\d{2})\]\s*\[([^\/]+)\/([A-Z]+)\]:\s*(.+)/);
		if (mcMatch) {
			return {
				timestamp: mcMatch[1],
				level: mcMatch[3],
				message: mcMatch[4],
				rawTimestamp: mcMatch[1]
			};
		}

		// Parse ISO timestamp format from mc-server-runner (2025-08-31T09:05:53.937084718Z)
		const isoMatch = line.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z)\s+(.+)/);
		if (isoMatch) {
			return {
				timestamp: new Date(isoMatch[1]).toLocaleTimeString('en-US', { 
					timeZone: 'UTC',
					hour12: false 
				}),
				level: 'INFO',
				message: isoMatch[2],
				rawTimestamp: isoMatch[1]
			};
		}

		// Parse simple format with level: [LEVEL] message
		const levelMatch = line.match(/^\[([A-Z]+)\]\s+(.+)/);
		if (levelMatch) {
			return {
				timestamp: '',
				level: levelMatch[1],
				message: levelMatch[2]
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

	function formatCommandEntry(entry: CommandEntry): string {
		// Convert to UTC to match server log timestamps
		const timestamp = new Date(entry.timestamp).toLocaleTimeString('en-US', { 
			timeZone: 'UTC',
			hour12: false 
		});
		const status = entry.success ? '✓' : '✗';
		const statusColor = entry.success ? 'text-green-400' : 'text-red-400';
		
		let result = `<span class="text-zinc-600">[${timestamp}]</span> `;
		result += `<span class="${statusColor}">[CMD ${status}]</span> `;
		result += `<span class="text-cyan-400">$ ${entry.command}</span>`;
		
		if (entry.output) {
			// Highlight common error patterns in output
			let output = entry.output;
			// Highlight error messages
			output = output.replace(/(error|failed|exception|invalid|unknown)/gi, '<span class="text-red-400">$1</span>');
			// Highlight success messages
			output = output.replace(/(success|completed|done|ok)/gi, '<span class="text-green-400">$1</span>');
			// Highlight warnings
			output = output.replace(/(warning|warn)/gi, '<span class="text-yellow-400">$1</span>');
			
			result += `\n<span class="text-zinc-300">${output}</span>`;
		}
		
		if (entry.error) {
			result += `\n<span class="text-red-400 font-semibold">Error: ${entry.error}</span>`;
		}
		
		return result;
	}

	// Create a combined display of logs and command history
	function getCombinedDisplay(): Array<{type: 'log' | 'command', content: string, timestamp?: Date}> {
		const items: Array<{type: 'log' | 'command', content: string, timestamp?: Date}> = [];
		
		// Add server logs with extracted timestamps
		if (logs) {
			logs.split('\n').forEach((line, index) => {
				if (line.trim()) {
					const parsed = formatLogLine(line);
					let logTimestamp: Date | undefined;
					
					// Try to extract a proper timestamp for chronological ordering
					if (parsed.rawTimestamp) {
						// For ISO format timestamps (2025-08-31T09:05:53.937084718Z)
						if (parsed.rawTimestamp.includes('T') && parsed.rawTimestamp.includes('Z')) {
							logTimestamp = new Date(parsed.rawTimestamp);
						} else if (parsed.rawTimestamp.match(/^\d{2}:\d{2}:\d{2}$/)) {
							// For time-only format [HH:MM:SS], use today's date
							const today = new Date();
							const [hours, minutes, seconds] = parsed.rawTimestamp.split(':').map(Number);
							logTimestamp = new Date(today.getFullYear(), today.getMonth(), today.getDate(), hours, minutes, seconds);
						}
					}
					
					// If no timestamp could be parsed, use a very old date to ensure logs appear first
					if (!logTimestamp) {
						logTimestamp = new Date(2000, 0, 1, 0, 0, index); // Use index to maintain order
					}
					
					items.push({ 
						type: 'log', 
						content: line,
						timestamp: logTimestamp
					});
				}
			});
		}
		
		// Add command history with proper timestamps
		commandHistory.forEach(entry => {
			items.push({ 
				type: 'command', 
				content: formatCommandEntry(entry),
				timestamp: new Date(entry.timestamp)
			});
		});
		
		// Sort by timestamp chronologically
		items.sort((a, b) => {
			const timeA = a.timestamp?.getTime() || 0;
			const timeB = b.timestamp?.getTime() || 0;
			return timeA - timeB;
		});
		
		return items;
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
					<Badge variant={(server.status === 'running' || server.status === 'unhealthy') ? 'default' : 'secondary'} class="text-xs">
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
						disabled={!logs && commandHistory.length === 0}
						class="h-7 w-7 p-0 text-zinc-400 hover:text-white"
					>
						<Download class="h-3 w-3" />
					</Button>
					<Button
						size="sm"
						variant="ghost"
						onclick={clearLogs}
						disabled={!logs && commandHistory.length === 0}
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
					{#if getCombinedDisplay().length === 0}
						<div class="py-8 text-center text-zinc-500">
							No logs or commands available. {['running', 'starting', 'unhealthy'].includes(server.status) ? 'Try refreshing the page.' : 'Start the server to see output.'}
						</div>
					{:else}
						{#each getCombinedDisplay() as item}
							{#if item.type === 'command'}
								<div class="py-0.5 hover:bg-zinc-900/50 whitespace-pre-wrap break-all border-l-2 border-cyan-500 pl-2 my-1 bg-zinc-900/20">
									{@html item.content}
								</div>
							{:else}
								{@const parsed = formatLogLine(item.content)}
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
							{/if}
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
					placeholder={(server.status === 'running' || server.status === 'unhealthy')? 'Enter command...' : 'Server must be running'}
					bind:value={command}
					disabled={server.status !== 'running'}
					onkeydown={(e) => e.key === 'Enter' && sendCommand()}
					class="flex-1 bg-transparent font-mono text-sm text-white outline-none placeholder:text-zinc-600"
				/>
			</div>
			<Button
				onclick={sendCommand}
				disabled={server.status === 'stopped' || !command.trim()}
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
				{getCombinedDisplay().length} lines ({commandHistory.length} commands)
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
