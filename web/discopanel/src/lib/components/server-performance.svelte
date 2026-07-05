<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { Button } from '$lib/components/ui/button';
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { Badge } from '$lib/components/ui/badge';
	import {
		Loader2,
		Gauge,
		Sparkles,
		Info,
		AlertTriangle,
		AlertCircle,
		CheckCircle2,
		ChevronRight,
		CircleDashed,
		Wrench
	} from '@lucide/svelte';
	import { ServerStatus, type Server } from '$lib/proto/discopanel/v1/common_pb';
	import { PerformanceSeverity, type PerformanceFinding } from '$lib/proto/discopanel/v1/server_pb';

	let { server }: { server: Server } = $props();

	let loading = $state(true);
	let detailsOpen = $state(false);
	let applyingFix = $state('');
	let findings = $state<PerformanceFinding[]>([]);
	let agentConnected = $state(false);
	let refreshTimer: ReturnType<typeof setInterval> | undefined;
	let initialized = false;

	const serverUp = $derived(
		server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY
	);
	const sorted = $derived([...findings].sort((a, b) => b.severity - a.severity));
	const problems = $derived(sorted.filter((f) => f.severity >= PerformanceSeverity.WARNING));
	const criticals = $derived(
		problems.filter((f) => f.severity === PerformanceSeverity.CRITICAL).length
	);
	const status = $derived(
		criticals > 0
			? {
					label: criticals === 1 ? '1 critical issue' : `${criticals} critical issues`,
					text: 'text-red-500',
					tint: 'from-red-500/5'
				}
			: problems.length > 0
				? {
						label: problems.length === 1 ? '1 issue found' : `${problems.length} issues found`,
						text: 'text-yellow-500',
						tint: 'from-yellow-500/5'
					}
				: serverUp
					? {
							label: 'No issues found',
							text: 'text-green-500',
							tint: 'from-green-500/5'
						}
					: {
							label: 'Config checks passed',
							text: 'text-muted-foreground/80',
							tint: 'from-muted/20'
						}
	);

	function chipClass(severity: PerformanceSeverity) {
		return severity === PerformanceSeverity.CRITICAL
			? 'border-red-500/30 bg-red-500/10 text-red-500'
			: 'border-yellow-500/30 bg-yellow-500/10 text-yellow-600 dark:text-yellow-500';
	}

	async function loadReport(silent = false) {
		try {
			const res = await rpcClient.server.getServerPerformanceReport(
				{ id: server.id },
				silent ? { headers: { 'X-Silent-Request': '1' } } : undefined
			);
			findings = res.findings;
			agentConnected = res.agentConnected;
		} catch {
			if (!silent) toast.error('Failed to load performance report');
		} finally {
			loading = false;
		}
	}

	async function applyFix(finding: PerformanceFinding) {
		applyingFix = finding.fixId;
		try {
			const res = await rpcClient.server.applyPerformanceFix({
				id: server.id,
				fixId: finding.fixId
			});
			toast.success(res.message, {
				description: res.restartRequired ? 'Restart the server to apply.' : undefined
			});
			await loadReport(true);
		} catch {
			toast.error('Failed to apply fix');
		} finally {
			applyingFix = '';
		}
	}

	// Refetch whenever the server status flips
	$effect(() => {
		void server.status;
		loadReport(initialized);
		initialized = true;
	});

	onMount(() => {
		refreshTimer = setInterval(() => loadReport(true), 30000);
	});

	onDestroy(() => {
		if (refreshTimer) clearInterval(refreshTimer);
	});
</script>

<div
	class="mt-auto border-t border-border/30 bg-linear-to-b {status.tint} to-transparent transition-colors duration-500"
>
	<button
		type="button"
		class="w-full px-6 pt-3 pb-4 text-left transition-colors duration-300 hover:bg-muted/20"
		onclick={() => (detailsOpen = true)}
	>
		<div class="mb-2 flex items-center justify-between">
			<span class="text-[10px] font-bold tracking-widest text-muted-foreground/70 uppercase"
				>Health</span
			>
			{#if loading}
				<Loader2 class="h-3.5 w-3.5 animate-spin text-muted-foreground/40" />
			{:else if criticals > 0}
				<AlertCircle class="h-3.5 w-3.5 text-red-500" />
			{:else if problems.length > 0}
				<AlertTriangle class="h-3.5 w-3.5 text-yellow-500" />
			{:else if serverUp}
				<CheckCircle2 class="h-3.5 w-3.5 text-green-500" />
			{:else}
				<CircleDashed class="h-3.5 w-3.5 text-muted-foreground/50" />
			{/if}
		</div>
		{#if loading}
			<span class="font-mono text-sm text-muted-foreground/50">--</span>
		{:else}
			<div class="mb-2 flex items-center justify-between">
				<span class="text-sm font-bold {status.text}">{status.label}</span>
				<span class="flex items-center text-[10px] text-muted-foreground/50">
					Details <ChevronRight class="h-3 w-3" />
				</span>
			</div>
			{#if problems.length > 0}
				<div class="flex flex-wrap gap-1.5">
					{#each problems.slice(0, 2) as finding (finding.id)}
						<div
							class="max-w-full truncate rounded border px-1.5 py-0.5 text-[10px] font-medium {chipClass(
								finding.severity
							)}"
						>
							{finding.title}
						</div>
					{/each}
					{#if problems.length > 2}
						<div
							class="rounded border border-border/50 bg-muted/30 px-1.5 py-0.5 text-[10px] text-muted-foreground/70"
						>
							+{problems.length - 2} more
						</div>
					{/if}
				</div>
			{:else}
				<p class="text-[10px] text-muted-foreground/50">
					{#if !serverUp}
						Start the server for the full checkup
					{:else if agentConnected}
						Live checks from the DiscoPanel agent
					{:else}
						Agent offline, basic checks only
					{/if}
				</p>
			{/if}
		{/if}
	</button>
</div>

<Dialog bind:open={detailsOpen}>
	<DialogContent class="max-h-[80vh] overflow-y-auto sm:max-w-lg">
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2">
				<Gauge class="h-5 w-5" />
				Health Check
			</DialogTitle>
			<DialogDescription>
				{#if agentConnected}
					Live checks from the DiscoPanel agent
				{:else if serverUp}
					Agent offline, basic checks only
				{:else}
					Configuration checks (start the server for live telemetry)
				{/if}
			</DialogDescription>
		</DialogHeader>
		<div class="space-y-3">
			{#each sorted as finding (finding.id)}
				<div class="flex items-start gap-3 rounded-lg border border-border/50 p-3">
					{#if finding.severity === PerformanceSeverity.CRITICAL}
						<AlertCircle class="mt-0.5 h-5 w-5 shrink-0 text-red-500" />
					{:else if finding.severity === PerformanceSeverity.WARNING}
						<AlertTriangle class="mt-0.5 h-5 w-5 shrink-0 text-yellow-500" />
					{:else if finding.severity === PerformanceSeverity.INFO}
						<Info class="mt-0.5 h-5 w-5 shrink-0 text-blue-500" />
					{:else}
						<CheckCircle2 class="mt-0.5 h-5 w-5 shrink-0 text-green-500" />
					{/if}
					<div class="min-w-0 flex-1">
						<div class="flex items-center gap-2">
							<span class="font-medium">{finding.title}</span>
							{#if finding.severity === PerformanceSeverity.CRITICAL}
								<Badge variant="destructive" class="text-xs">critical</Badge>
							{/if}
						</div>
						<p class="mt-1 text-sm text-muted-foreground">{finding.detail}</p>
					</div>
					{#if finding.fixId}
						<Button
							size="sm"
							variant="outline"
							class="shrink-0"
							disabled={applyingFix !== ''}
							onclick={() => applyFix(finding)}
						>
							{#if applyingFix === finding.fixId}
								<Loader2 class="mr-1 h-3.5 w-3.5 animate-spin" />
							{:else if finding.fixId === 'enable_zgc'}
								<Sparkles class="mr-1 h-3.5 w-3.5" />
							{:else}
								<Wrench class="mr-1 h-3.5 w-3.5" />
							{/if}
							{finding.fixLabel}
						</Button>
					{/if}
				</div>
			{/each}
		</div>
	</DialogContent>
</Dialog>
