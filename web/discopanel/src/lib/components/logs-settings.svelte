<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { ScrollText, RefreshCw, Download, Loader2, AlertCircle, ArrowDown } from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import { rpcClient } from '$lib/api/rpc-client';

	let loading = $state(true);
	let refreshing = $state(false);
	let logs = $state('');
	let filename = $state('');
	let fileSize = $state(0);
	let autoScroll = $state(true);
	let logContainer: HTMLPreElement | null = $state(null);
	let refreshInterval: ReturnType<typeof setInterval> | null = null;

	async function loadLogs(showToast = false) {
		if (refreshing) return;

		refreshing = true;
		try {
			const response = await rpcClient.support.getApplicationLogs({
				tail: 500 // Get last 500 lines
			});
			logs = response.content;
			filename = response.filename;
			fileSize = Number(response.size);

			if (showToast) {
				toast.success('Logs refreshed');
			}

			// Auto-scroll to bottom
			if (autoScroll && logContainer) {
				setTimeout(() => {
					if (logContainer) {
						logContainer.scrollTop = logContainer.scrollHeight;
					}
				}, 50);
			}
		} catch (error) {
			const message = error instanceof Error ? error.message : 'Unknown error occurred';
			if (showToast) {
				toast.error('Failed to load logs', { description: message });
			}
			console.error('Failed to load logs:', error);
		} finally {
			loading = false;
			refreshing = false;
		}
	}

	function downloadLogs() {
		if (!logs) return;

		const blob = new Blob([logs], { type: 'text/plain' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = filename || 'discopanel.log';
		a.click();
		URL.revokeObjectURL(url);
		toast.success('Logs downloaded');
	}

	function formatFileSize(bytes: number): string {
		if (bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
	}

	function scrollToBottom() {
		if (logContainer) {
			logContainer.scrollTop = logContainer.scrollHeight;
		}
	}

	onMount(() => {
		loadLogs();
		// Auto-refresh every 5 seconds
		refreshInterval = setInterval(() => loadLogs(), 5000);
	});

	onDestroy(() => {
		if (refreshInterval) {
			clearInterval(refreshInterval);
		}
	});
</script>

<Card
	class="relative overflow-hidden border-2 bg-linear-to-br from-card to-card/80 transition-all duration-300 hover:border-primary/50 hover:shadow-2xl"
>
	<div
		class="pointer-events-none absolute inset-0 bg-linear-to-br from-primary/10 via-transparent to-transparent opacity-0 transition-opacity duration-300 hover:opacity-100"
	></div>
	<CardHeader class="relative pb-4">
		<div class="flex items-center justify-between">
			<div>
				<CardTitle class="text-2xl font-semibold">Application Logs</CardTitle>
				<CardDescription class="mt-2 text-base">
					View real-time DiscoPanel application logs for debugging and monitoring.
				</CardDescription>
			</div>
			<div class="flex items-center gap-2">
				{#if filename}
					<Badge variant="outline" class="text-xs">
						{filename}
					</Badge>
					<Badge variant="secondary" class="text-xs">
						{formatFileSize(fileSize)}
					</Badge>
				{/if}
			</div>
		</div>
	</CardHeader>
	<CardContent class="space-y-4">
		<!-- Controls -->
		<div class="flex items-center justify-between gap-4">
			<div class="flex items-center gap-2">
				<Button onclick={() => loadLogs(true)} disabled={refreshing} variant="outline" size="sm">
					{#if refreshing}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					{:else}
						<RefreshCw class="mr-2 h-4 w-4" />
					{/if}
					Refresh
				</Button>
				<Button onclick={downloadLogs} disabled={!logs} variant="outline" size="sm">
					<Download class="mr-2 h-4 w-4" />
					Download
				</Button>
				<Button onclick={scrollToBottom} variant="outline" size="sm">
					<ArrowDown class="mr-2 h-4 w-4" />
					Scroll to Bottom
				</Button>
			</div>
			<div class="flex items-center gap-2">
				<label class="flex cursor-pointer items-center gap-2 text-sm text-muted-foreground">
					<input type="checkbox" bind:checked={autoScroll} class="rounded border-border" />
					Auto-scroll
				</label>
			</div>
		</div>

		<!-- Log Display -->
		<div class="relative overflow-hidden rounded-lg border border-border bg-black/90">
			{#if loading}
				<div class="flex h-96 items-center justify-center">
					<div class="space-y-3 text-center">
						<Loader2 class="mx-auto h-8 w-8 animate-spin text-primary" />
						<div class="text-sm text-muted-foreground">Loading logs...</div>
					</div>
				</div>
			{:else if !logs}
				<div class="flex h-96 items-center justify-center">
					<div class="space-y-3 text-center">
						<AlertCircle class="mx-auto h-8 w-8 text-muted-foreground" />
						<div class="text-sm text-muted-foreground">No logs available</div>
						<p class="text-xs text-muted-foreground">
							File logging may not be enabled in your configuration.
						</p>
					</div>
				</div>
			{:else}
				<pre
					bind:this={logContainer}
					class="h-96 overflow-auto p-4 font-mono text-xs break-all whitespace-pre-wrap text-green-400">{logs}</pre>
			{/if}
		</div>

		<!-- Info Notice -->
		<div class="rounded-lg border border-border/50 bg-muted/30 p-4">
			<div class="flex gap-3">
				<ScrollText class="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
				<div class="space-y-1 text-sm text-muted-foreground">
					<p class="font-medium">Log Information</p>
					<p class="text-xs leading-relaxed">
						Showing the last 500 lines of application logs. Logs auto-refresh every 5 seconds. For
						complete logs, use the Support tab to generate a support bundle or click Download.
					</p>
				</div>
			</div>
		</div>
	</CardContent>
</Card>
