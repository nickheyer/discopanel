<script lang="ts">
	import { LineChart } from 'layerchart';
	import { scaleTime } from 'd3-scale';

	interface Point {
		ts: Date;
		value: number;
	}

	let {
		title,
		color,
		points,
		yDomain = null,
		format = (v: number) => v.toFixed(1),
		unit = ''
	}: {
		title: string;
		color: string;
		points: Point[];
		yDomain?: [number, number] | null;
		format?: (v: number) => string;
		unit?: string;
	} = $props();

	let current = $derived(points.length ? points[points.length - 1].value : null);

	// Tick labels drop the date for ranges within one day
	let spanHours = $derived(
		points.length > 1
			? (points[points.length - 1].ts.getTime() - points[0].ts.getTime()) / 3600000
			: 0
	);

	function xTick(d: Date): string {
		if (spanHours > 24) {
			return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
		}
		return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
	}
</script>

<div
	class="relative overflow-hidden rounded-xl border border-border/30 bg-linear-to-br from-muted/30 to-muted/10 p-3"
>
	<div
		class="absolute top-0 right-0 left-0 h-0.5 opacity-60"
		style="background: linear-gradient(to right, transparent, {color}, transparent)"
	></div>
	<div class="mb-2 flex items-baseline justify-between">
		<span class="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</span>
		<span class="text-sm font-semibold tabular-nums">
			{current === null ? '--' : format(current)}{unit ? ` ${unit}` : ''}
		</span>
	</div>
	<div class="h-28">
		{#if points.length > 1}
			<LineChart
				data={points}
				x="ts"
				xScale={scaleTime()}
				series={[{ key: title, value: (d: Point) => d.value, color }]}
				yDomain={yDomain ?? undefined}
				yNice={!yDomain}
				grid={{ x: false, y: true }}
				rule={false}
				props={{
					spline: { strokeWidth: 2 },
					xAxis: { format: xTick, ticks: 4, tickLength: 0 },
					yAxis: { ticks: 3, format, tickLength: 0 },
					highlight: { points: { r: 4 } }
				}}
			/>
		{:else}
			<div class="flex h-full items-center justify-center text-xs text-muted-foreground">
				No data yet
			</div>
		{/if}
	</div>
</div>
