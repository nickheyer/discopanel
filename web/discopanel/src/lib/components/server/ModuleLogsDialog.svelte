<script lang="ts">
	import { onDestroy } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Dialog, DialogContent, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { Module } from '$lib/proto/discopanel/v1/module_pb';
	import { ModuleStatus } from '$lib/proto/discopanel/v1/module_pb';
	import type { LogEntry } from '$lib/proto/discopanel/v1/server_pb';
	import { Terminal, Download, Trash2, RefreshCw, Loader2 } from '@lucide/svelte';
	import AnsiToHtml from 'ansi-to-html';
	import { toast } from 'svelte-sonner';

	// Create ansi-to-html converter with proper options
	const ansiConverter = new AnsiToHtml({
		fg: '#e8e8e8',
		bg: '#000000',
		newline: false,
		escapeXML: true,
		stream: true
	});

	interface Props {
		open: boolean;
		module: Module;
	}

	let { open = $bindable(), module }: Props = $props();

	let logEntries = $state<LogEntry[]>([]);
	let loading = $state(false);
	let autoScroll = $state(true);
	let scrollAreaRef = $state<HTMLDivElement | null>(null);
	let pollingInterval: ReturnType<typeof setInterval> | null = null;
	let tailLines = $state(500);

	// Fetch logs when dialog opens
	$effect(() => {
		if (open) {
			fetchLogs();
			startPolling();
		} else {
			stopPolling();
			logEntries = [];
		}
	});

	onDestroy(() => {
		stopPolling();
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

	// Handle auto-scrolling
	$effect(() => {
		if (logEntries.length > 0 && autoScroll && scrollAreaRef) {
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
		} else if (!isAtBottom && autoScroll) {
			autoScroll = false;
		}
	}

	async function fetchLogs() {
		if (loading) return;

		// Only fetch logs if the module has a container that could have logs
		// Skip only CREATING status (no container yet) - all other states may have logs
		if (module.status === ModuleStatus.CREATING) {
			return;
		}

		try {
			const response = await rpcClient.module.getModuleLogs({
				id: module.id,
				tail: tailLines
			});
			logEntries = response.logs || [];
		} catch (error) {
			console.error('Failed to fetch module logs:', error);
		}
	}

	function clearLogs() {
		logEntries = [];
		toast.success('Logs cleared (local only)');
	}

	function downloadLogs() {
		const logText = logEntries.map((entry) => entry.message).join('\n');
		const blob = new Blob([logText], { type: 'text/plain' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `${module.name}-logs-${new Date().toISOString()}.txt`;
		document.body.appendChild(a);
		a.click();
		document.body.removeChild(a);
		URL.revokeObjectURL(url);
		toast.success('Logs downloaded');
	}

	function getStatusLabel(status: ModuleStatus): string {
		switch (status) {
			case ModuleStatus.RUNNING:
				return 'Running';
			case ModuleStatus.STOPPED:
				return 'Stopped';
			case ModuleStatus.STARTING:
				return 'Starting';
			case ModuleStatus.STOPPING:
				return 'Stopping';
			case ModuleStatus.ERROR:
				return 'Error';
			case ModuleStatus.CREATING:
				return 'Creating';
			default:
				return 'Unknown';
		}
	}

	function getStatusVariant(status: ModuleStatus): 'default' | 'secondary' | 'destructive' {
		switch (status) {
			case ModuleStatus.RUNNING:
				return 'default';
			case ModuleStatus.ERROR:
				return 'destructive';
			default:
				return 'secondary';
		}
	}
</script>

<Dialog bind:open>
	<DialogContent class="max-w-4xl h-[80vh] flex flex-col p-0 gap-0">
		<DialogHeader class="px-4 py-3 border-b border-zinc-800 bg-zinc-900 flex-shrink-0">
			<div class="flex items-center justify-between">
				<div class="flex items-center gap-2">
					<Terminal class="h-4 w-4 text-green-500" />
					<DialogTitle class="font-mono text-sm text-green-500">
						{module.name} Logs
					</DialogTitle>
					<Badge variant={getStatusVariant(module.status)} class="text-xs">
						{getStatusLabel(module.status)}
					</Badge>
				</div>
				<div class="flex items-center gap-1 mr-8">
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
		</DialogHeader>

		<div
			class="custom-scrollbar flex-1 overflow-y-auto overflow-x-auto bg-black px-4 py-2"
			bind:this={scrollAreaRef}
			onscroll={handleScroll}
		>
			<div class="font-mono text-xs text-zinc-300">
				{#if logEntries.length === 0}
					<div class="py-8 text-center text-zinc-500">
						{#if module.status === ModuleStatus.STOPPED}
							No logs available. Start the module to see output.
						{:else if module.status === ModuleStatus.STARTING || module.status === ModuleStatus.CREATING}
							Waiting for module to start...
						{:else}
							No logs available. Try refreshing.
						{/if}
					</div>
				{:else}
					{#each logEntries as entry}
						<div class="log-line whitespace-pre-wrap break-all" data-type={entry.level}>
							{@html ansiConverter.toHtml(entry.message)}
						</div>
					{/each}
				{/if}
			</div>
		</div>

		<div class="flex items-center justify-between px-4 py-2 border-t border-zinc-800 bg-zinc-950 text-xs text-zinc-500 flex-shrink-0">
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
	</DialogContent>
</Dialog>

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
</style>
