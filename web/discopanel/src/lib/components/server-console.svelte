<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/client';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
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
			pollingInterval = setInterval(fetchLogs, 2000);
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
			setTimeout(fetchLogs, 500);
		} catch (error) {
			toast.error('Failed to send command: ' + (error instanceof Error ? error.message : 'Unknown error'));
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
				return 'text-red-500';
			case 'WARN':
			case 'WARNING':
				return 'text-yellow-500';
			case 'INFO':
				return 'text-blue-500';
			case 'DEBUG':
				return 'text-gray-500';
			default:
				return 'text-foreground';
		}
	}
</script>

<Card class="h-full flex flex-col">
	<CardHeader class="flex-shrink-0">
		<div class="flex items-center justify-between">
			<div class="flex items-center gap-2">
				<Terminal class="h-5 w-5" />
				<CardTitle>Server Console</CardTitle>
			</div>
			<div class="flex items-center gap-2">
				<Badge variant={server.status === 'running' ? 'default' : 'secondary'}>
					{server.status}
				</Badge>
				<Button
					size="sm"
					variant="ghost"
					onclick={fetchLogs}
					disabled={loading}
				>
					{#if loading}
						<Loader2 class="h-4 w-4 animate-spin" />
					{:else}
						<RefreshCw class="h-4 w-4" />
					{/if}
				</Button>
				<Button
					size="sm"
					variant="ghost"
					onclick={downloadLogs}
					disabled={!logs}
				>
					<Download class="h-4 w-4" />
				</Button>
				<Button
					size="sm"
					variant="ghost"
					onclick={clearLogs}
					disabled={!logs}
				>
					<Trash2 class="h-4 w-4" />
				</Button>
			</div>
		</div>
		<CardDescription>
			View server logs and send commands
		</CardDescription>
	</CardHeader>
	
	<CardContent class="flex-1 flex flex-col p-0 min-h-0">
		<div class="flex-1 overflow-y-auto px-4 pt-4 custom-scrollbar" bind:this={scrollAreaRef}>
				<div class="font-mono text-sm space-y-1 pb-4">
					{#if !logs}
						<div class="text-muted-foreground text-center py-8">
							No logs available. Start the server to see output.
						</div>
					{:else}
						{#each logs.split('\n').filter(line => line.trim()) as line}
							{@const parsed = formatLogLine(line)}
							<div class="flex gap-2 hover:bg-muted/50 px-2 py-0.5 rounded">
								{#if parsed.timestamp}
									<span class="text-muted-foreground text-xs min-w-[80px]">
										{parsed.timestamp}
									</span>
								{/if}
								<span class="text-xs font-bold min-w-[50px] {getLogLevelColor(parsed.level)}">
									{parsed.level}
								</span>
								<span class="text-sm flex-1 break-all">
									{parsed.message}
								</span>
							</div>
						{/each}
					{/if}
					<div bind:this={endOfLogsRef} aria-hidden="true"></div>
				</div>
		</div>
		
		<div class="border-t p-4 flex gap-2 flex-shrink-0">
			<Input
				type="text"
				placeholder={server.status === 'running' ? "Enter command..." : "Server must be running to send commands"}
				bind:value={command}
				disabled={server.status !== 'running'}
				onkeydown={(e) => e.key === 'Enter' && sendCommand()}
			/>
			<Button
				onclick={sendCommand}
				disabled={server.status !== 'running' || !command.trim()}
			>
				<Send class="h-4 w-4" />
			</Button>
		</div>
		
		<div class="px-4 pb-2 flex items-center justify-between text-xs text-muted-foreground flex-shrink-0">
			<div class="flex items-center gap-4">
				<label class="flex items-center gap-2">
					<input
						type="checkbox"
						bind:checked={autoScroll}
						class="rounded"
					/>
					Auto-scroll
				</label>
				<div class="flex items-center gap-2">
					<span>Tail lines:</span>
					<select
						bind:value={tailLines}
						onchange={fetchLogs}
						class="bg-background border rounded px-2 py-1"
					>
						<option value={100}>100</option>
						<option value={500}>500</option>
						<option value={1000}>1000</option>
						<option value={2000}>2000</option>
					</select>
				</div>
			</div>
			<div>
				{logs.split('\n').filter(line => line.trim()).length} lines
			</div>
		</div>
	</CardContent>
</Card>

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