<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/client';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Button } from '$lib/components/ui/button';
	import { ScrollArea } from '$lib/components/ui/scroll-area';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { Terminal, Send, Loader2, Download, Trash2, RefreshCw } from '@lucide/svelte';
	import type { Server } from '$lib/api/types';

	let { server }: { server: Server } = $props();

	let logs = $state('');
	let command = $state('');
	let loading = $state(false);
	let autoScroll = $state(true);
	let scrollAreaRef = $state<HTMLDivElement | null>(null);
	let pollingInterval: ReturnType<typeof setInterval>;
	let tailLines = $state(500);

	onMount(() => {
		fetchLogs();
		// Poll for new logs every 2 seconds
		pollingInterval = setInterval(fetchLogs, 2000);
	});

	onDestroy(() => {
		if (pollingInterval) {
			clearInterval(pollingInterval);
		}
	});

	async function fetchLogs() {
		if (loading) return;
		
		try {
			const response = await api.getServerLogs(server.id, tailLines);
			logs = response.logs;
			
			if (autoScroll && scrollAreaRef) {
				// Scroll to bottom after logs update
				setTimeout(() => {
					if (scrollAreaRef) {
						const scrollContainer = scrollAreaRef.querySelector('[data-scroll-container]');
						if (scrollContainer) {
							scrollContainer.scrollTop = scrollContainer.scrollHeight;
						}
					}
				}, 100);
			}
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
	<CardHeader class="flex-none">
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
	
	<CardContent class="flex-1 flex flex-col p-0">
		<ScrollArea class="flex-1 p-4" bind:ref={scrollAreaRef}>
			<div class="font-mono text-sm space-y-1" data-scroll-container>
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
			</div>
		</ScrollArea>
		
		<div class="border-t p-4 flex gap-2">
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
		
		<div class="px-4 pb-2 flex items-center justify-between text-xs text-muted-foreground">
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