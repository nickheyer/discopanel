<script lang="ts" module>
	let instance = 0;
</script>

<script lang="ts">
	import { cn } from '$lib/utils';

	let { class: className = '', spotlight = false }: { class?: string; spotlight?: boolean } =
		$props();

	// Unique ids keep multiple logos from colliding
	const uid = ++instance;
	const patternId = `disco-grass-${uid}`;
	const clipId = `disco-chip-${uid}`;
</script>

<svg viewBox="0 0 24 24" fill="none" class={cn('shrink-0', className)} aria-hidden="true">
	<defs>
		<pattern id={patternId} x="8" y="8" width="4" height="4" patternUnits="userSpaceOnUse">
			<path fill="#5a5" d="M0 0h4v4H0z" />
			<path fill="#3c7f3c" d="M0 0h2v2H0z" />
			<path fill="#8b5e3c" d="M2 2h2v2H2z" />
		</pattern>
		<clipPath id={clipId}>
			<rect x="4" y="4" width="16" height="16" rx="3" />
		</clipPath>
	</defs>
	<!-- Pins follow text color as quiet detail -->
	<path
		class="pins"
		stroke="currentColor"
		stroke-width="2"
		stroke-linecap="round"
		d="m 12,20 v 2 M 12,2 v 2 m 5,16 v 2 M 17,2 V 4 M 2,12 H 4 M 2,17 H 4 M 2,7 h 2 m 16,5 h 2 m -2,5 h 2 M 20,7 h 2 M 7,20 v 2 M 7,2 v 2"
	/>
	<!-- Chip body stays dark in both themes -->
	<rect class="chip" x="4" y="4" width="16" height="16" rx="3" />
	<rect x="8" y="8" width="8" height="8" rx="1" fill="url(#{patternId})" />
	{#if spotlight}
		<!-- Soft colored lights drift over the chip face -->
		<g clip-path="url(#{clipId})">
			<circle class="light light-a" r="6" />
			<circle class="light light-b" r="5" />
		</g>
	{/if}
</svg>

<style>
	.pins {
		opacity: 0.45;
	}

	.chip {
		fill: oklch(0.22 0.03 291);
	}

	:global(.dark) .chip {
		stroke: oklch(1 0 0 / 0.22);
		stroke-width: 0.75;
	}

	.light {
		mix-blend-mode: screen;
		opacity: 0.45;
		filter: blur(2.5px);
	}

	.light-a {
		fill: var(--primary);
		animation: disco-sweep-a 7s ease-in-out infinite;
	}

	.light-b {
		fill: var(--chart-2);
		animation: disco-sweep-b 9s ease-in-out infinite;
	}

	@keyframes disco-sweep-a {
		0%,
		100% {
			transform: translate(6px, 5px);
		}
		50% {
			transform: translate(18px, 17px);
		}
	}

	@keyframes disco-sweep-b {
		0%,
		100% {
			transform: translate(18px, 7px);
		}
		50% {
			transform: translate(5px, 18px);
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.light {
			animation: none;
			opacity: 0.25;
		}
		.light-a {
			transform: translate(9px, 8px);
		}
		.light-b {
			transform: translate(16px, 15px);
		}
	}
</style>
