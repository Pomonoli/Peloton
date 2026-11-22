<script lang="ts">
	import { appearance, appearanceColor } from '#lib/appearance.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		metricLegend,
		missingMetricColor,
		type RideMetric,
		type RidePoint
	} from '#lib/ride-track.js';

	let {
		coordinates,
		track,
		point,
		metric,
		disconnected
	}: {
		coordinates: [number, number][];
		track: RidePoint[];
		point: RidePoint | null;
		metric: RideMetric | null;
		disconnected: boolean;
	} = $props();

	let canvas: HTMLCanvasElement;
	let width = $state(0);
	let height = $state(0);
	const bounds = $derived.by(() => {
		let west = Infinity,
			east = -Infinity,
			south = Infinity,
			north = -Infinity;
		const origin = coordinates[0]?.[0] ?? 0;
		for (const [lon, lat] of coordinates) {
			const x = ((lon - origin + 540) % 360) - 180;
			west = Math.min(west, x);
			east = Math.max(east, x);
			south = Math.min(south, lat);
			north = Math.max(north, lat);
		}
		const cos = Math.max(0.05, Math.cos(((south + north) * Math.PI) / 360));
		const scale = Math.min(
			(width - 32) / Math.max(0.00001, (east - west) * cos),
			(height - 32) / Math.max(0.00001, north - south)
		);
		return { origin, x: (west + east) / 2, y: (south + north) / 2, scale, cos };
	});

	function project(lon: number, lat: number) {
		const x = ((lon - bounds.origin + 540) % 360) - 180;
		return [
			width / 2 + (x - bounds.x) * bounds.scale * bounds.cos,
			height / 2 - (lat - bounds.y) * bounds.scale
		] as const;
	}

	const marker = $derived(point && project(point.lon, point.lat));
	$effect(() => {
		if (!$appearance.mode || width < 32 || height < 32 || !coordinates.length) return;
		const ctx = canvas.getContext('2d');
		if (!ctx) return;
		const ratio = window.devicePixelRatio || 1;
		canvas.width = width * ratio;
		canvas.height = height * ratio;
		ctx.scale(ratio, ratio);
		ctx.lineWidth = 3;
		ctx.lineCap = 'round';
		ctx.lineJoin = 'round';
		ctx.strokeStyle = appearanceColor('--accent-primary');
		if (!metric) {
			ctx.beginPath();
			for (const [i, [x, y]] of coordinates.entries()) {
				const to = project(x, y);
				if (i === 0 || (disconnected && !track[i]?.connected)) ctx.moveTo(...to);
				else ctx.lineTo(...to);
			}
			ctx.stroke();
			return;
		}
		const legend = metricLegend(track, metric);
		for (const [i, p] of track.entries()) {
			if (!p.connected) continue;
			const prev = track[i - 1]!;
			const value = p[metric];
			ctx.strokeStyle =
				value == null
					? missingMetricColor
					: legend.reduce((best, step) =>
							Math.abs(step.value - value) < Math.abs(best.value - value) ? step : best
						).color;
			ctx.beginPath();
			ctx.moveTo(...project(prev.lon, prev.lat));
			ctx.lineTo(...project(p.lon, p.lat));
			ctx.stroke();
		}
	});
</script>

<div
	class="absolute inset-0 overflow-hidden rounded-lg border border-zinc-800 bg-zinc-900"
	bind:clientWidth={width}
	bind:clientHeight={height}
>
	<canvas class="h-full w-full" bind:this={canvas} aria-label={m.ride_route_preview()}></canvas>
	{#if marker && width >= 32 && height >= 32}
		<span
			class="pointer-events-none absolute -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-white bg-accent-500 size-4"
			style:left={`${marker[0]}px`}
			style:top={`${marker[1]}px`}
		></span>
	{/if}
</div>
