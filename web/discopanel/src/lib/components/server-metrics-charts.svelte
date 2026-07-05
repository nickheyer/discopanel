<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { timestampFromDate, timestampDate } from '@bufbuild/protobuf/wkt';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { wsClient } from '$lib/stores/websocket.svelte';
	import { Button } from '$lib/components/ui/button';
	import MetricsChart from '$lib/components/metrics-chart.svelte';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import type { MetricsSample } from '$lib/proto/discopanel/v1/server_pb';

	let { server }: { server: Server } = $props();

	interface SamplePoint {
		ts: Date;
		tps: number;
		mspt: number;
		players: number;
		cpuPercent: number;
		memoryMb: number;
	}

	const ranges = [
		{ key: '1h', label: '1H', hours: 1 },
		{ key: '6h', label: '6H', hours: 6 },
		{ key: '24h', label: '24H', hours: 24 },
		{ key: '7d', label: '7D', hours: 168 }
	] as const;

	let range = $state<(typeof ranges)[number]>(ranges[0]);
	let samples = $state<SamplePoint[]>([]);
	let loading = $state(true);
	let unsubscribeMetrics: (() => void) | null = null;

	// Tick charts only render once tick data has actually been observed,
	// live or in history. Servers that can never report it (vanilla, agent
	// disabled, no TPS command) simply never show the panels.
	let tickCapable = $derived(
		server.tps > 0 || server.mspt > 0 || samples.some((s) => s.tps > 0 || s.mspt > 0)
	);

	function toPoint(s: MetricsSample): SamplePoint | null {
		if (!s.timestamp) return null;
		return {
			ts: timestampDate(s.timestamp),
			tps: s.tps,
			mspt: s.mspt,
			players: s.players,
			cpuPercent: s.cpuPercent,
			memoryMb: s.memoryMb
		};
	}

	async function loadHistory() {
		loading = true;
		try {
			const to = new Date();
			const from = new Date(to.getTime() - range.hours * 60 * 60 * 1000);
			const response = await rpcClient.server.getServerMetricsHistory(
				{
					id: server.id,
					from: timestampFromDate(from),
					to: timestampFromDate(to),
					resolution: 0
				},
				silentCallOptions
			);
			samples = response.samples.map(toPoint).filter((p): p is SamplePoint => p !== null);
		} catch (error) {
			console.error('Failed to load metrics history:', error);
		} finally {
			loading = false;
		}
	}

	function selectRange(r: (typeof ranges)[number]) {
		if (range.key === r.key) return;
		range = r;
		loadHistory();
	}

	function appendLive(serverId: string, sample: MetricsSample) {
		if (serverId !== server.id) return;
		const point = toPoint(sample);
		if (!point) return;
		const cutoff = Date.now() - range.hours * 60 * 60 * 1000;
		samples = [...samples.filter((p) => p.ts.getTime() >= cutoff), point];
	}

	onMount(() => {
		loadHistory();
		wsClient.subscribeMetrics(server.id);
		unsubscribeMetrics = wsClient.onMetrics(appendLive);
	});

	onDestroy(() => {
		unsubscribeMetrics?.();
		wsClient.unsubscribeMetrics(server.id);
	});

	function formatMemory(v: number): string {
		return v >= 1024 ? `${(v / 1024).toFixed(1)}G` : `${Math.round(v)}M`;
	}

	let tpsPoints = $derived(samples.map((s) => ({ ts: s.ts, value: s.tps })));
	let msptPoints = $derived(samples.map((s) => ({ ts: s.ts, value: s.mspt })));
	let playerPoints = $derived(samples.map((s) => ({ ts: s.ts, value: s.players })));
	let cpuPoints = $derived(samples.map((s) => ({ ts: s.ts, value: s.cpuPercent })));
	let memoryPoints = $derived(samples.map((s) => ({ ts: s.ts, value: s.memoryMb })));
</script>

<div
	class="group relative overflow-hidden rounded-xl bg-linear-to-br from-background via-background/95 to-background/90 shadow-xl transition-all duration-500 hover:shadow-2xl"
>
	<div
		class="pointer-events-none absolute inset-0 bg-linear-to-br from-sky-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
	></div>
	<div
		class="absolute top-0 right-0 left-0 h-1 bg-linear-to-r from-transparent via-sky-500/50 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
	></div>

	<div class="relative p-4 sm:p-5">
		<div class="mb-4 flex items-center justify-between">
			<div class="space-y-1">
				<h3 class="text-xs font-bold tracking-widest text-muted-foreground/70 uppercase">
					Performance
				</h3>
				<p class="text-xs text-muted-foreground/50">
					Sampled every 30 seconds while the server runs
				</p>
			</div>
			<div class="flex gap-1">
				{#each ranges as r (r.key)}
					<Button
						variant={range.key === r.key ? 'secondary' : 'ghost'}
						size="sm"
						class="h-7 px-2 text-xs"
						onclick={() => selectRange(r)}
					>
						{r.label}
					</Button>
				{/each}
			</div>
		</div>

		{#if !loading && samples.length === 0}
			<div class="py-10 text-center text-sm text-muted-foreground">
				No metrics recorded for this range yet. Charts fill in while the server runs.
			</div>
		{:else}
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
				{#if tickCapable}
					<MetricsChart
						title="TPS"
						color="var(--metric-tps)"
						points={tpsPoints}
						yDomain={[0, 20]}
						format={(v) => v.toFixed(1)}
					/>
					<MetricsChart
						title="Tick Time"
						color="var(--metric-mspt)"
						points={msptPoints}
						format={(v) => v.toFixed(1)}
						unit="ms"
					/>
				{/if}
				<MetricsChart
					title="Players"
					color="var(--metric-players)"
					points={playerPoints}
					format={(v) => Math.round(v).toString()}
				/>
				<MetricsChart
					title="CPU"
					color="var(--metric-cpu)"
					points={cpuPoints}
					format={(v) => v.toFixed(0)}
					unit="%"
				/>
				<MetricsChart
					title="Memory"
					color="var(--metric-memory)"
					points={memoryPoints}
					format={formatMemory}
				/>
			</div>
		{/if}
	</div>
</div>
