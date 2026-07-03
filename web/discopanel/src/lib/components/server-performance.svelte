<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import { Button } from '$lib/components/ui/button';
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import {
		Loader2,
		Gauge,
		Sparkles,
		Info,
		AlertTriangle,
		AlertCircle,
		CheckCircle2,
		Wrench
	} from '@lucide/svelte';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import {
		PerformanceSeverity,
		type PerformanceFinding
	} from '$lib/proto/discopanel/v1/server_pb';

	let { server }: { server: Server } = $props();

	let loading = $state(true);
	let applyingFix = $state('');
	let grade = $state('');
	let findings = $state<PerformanceFinding[]>([]);
	let agentConnected = $state(false);
	let refreshTimer: ReturnType<typeof setInterval> | undefined;

	const gradeStyles: Record<string, string> = {
		A: 'bg-green-500/15 text-green-600 dark:text-green-400 border-green-500/30',
		B: 'bg-lime-500/15 text-lime-600 dark:text-lime-400 border-lime-500/30',
		C: 'bg-yellow-500/15 text-yellow-600 dark:text-yellow-400 border-yellow-500/30',
		D: 'bg-orange-500/15 text-orange-600 dark:text-orange-400 border-orange-500/30',
		F: 'bg-red-500/15 text-red-600 dark:text-red-400 border-red-500/30'
	};

	async function loadReport(silent = false) {
		try {
			const res = await rpcClient.server.getServerPerformanceReport(
				{ id: server.id },
				silent ? { headers: { 'X-Silent-Request': '1' } } : undefined
			);
			grade = res.grade;
			findings = res.findings;
			agentConnected = res.agentConnected;
		} catch (e) {
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
		} catch (e) {
			toast.error('Failed to apply fix');
		} finally {
			applyingFix = '';
		}
	}

	onMount(() => {
		loadReport();
		refreshTimer = setInterval(() => loadReport(true), 30000);
	});

	onDestroy(() => {
		if (refreshTimer) clearInterval(refreshTimer);
	});
</script>

<Card class="border-border/50 shadow-sm">
	<CardHeader class="pb-4">
		<div class="flex items-center justify-between">
			<div>
				<CardTitle class="flex items-center gap-2 text-xl">
					<Gauge class="h-5 w-5" />
					Performance Report Card
				</CardTitle>
				<CardDescription>
					{#if agentConnected}
						Live checks from the DiscoPanel agent
					{:else}
						Configuration checks (start the server for live telemetry)
					{/if}
				</CardDescription>
			</div>
			{#if !loading && grade}
				<div
					class={`flex h-14 w-14 items-center justify-center rounded-xl border text-3xl font-bold ${gradeStyles[grade] ?? ''}`}
				>
					{grade}
				</div>
			{/if}
		</div>
	</CardHeader>
	<CardContent>
		{#if loading}
			<div class="flex items-center justify-center py-8">
				<Loader2 class="h-6 w-6 animate-spin text-muted-foreground" />
			</div>
		{:else}
			<div class="space-y-3">
				{#each findings as finding (finding.id)}
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
		{/if}
	</CardContent>
</Card>
