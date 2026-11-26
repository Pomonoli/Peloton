<script lang="ts">
	import { appearance, appearanceColor } from '#lib/appearance.js';
	import { unwrapLongitude } from '#lib/waas/recorded.js';
	import type { RoutePoint } from '#lib/recorded-routes.js';

	let {
		lines,
		excluded,
		origin,
		point
	}: {
		lines: number[][][];
		excluded: number[][][];
		origin: number;
		point: RoutePoint | null;
	} = $props();

	let width = $state(0);
	let height = $state(0);
	let canvas: HTMLCanvasElement;
	const bounds = $derived.by(() => {
		let west = Infinity,
			east = -Infinity,
			south = Infinity,
			north = -Infinity;
		for (const line of [...lines, ...excluded]) {
			for (const [lon, lat] of line) {
				west = Math.min(west, lon!);
				east = Math.max(east, lon!);
				south = Math.min(south, lat!);
				north = Math.max(north, lat!);
			}
		}
		const cos = Math.max(0.05, Math.cos(((south + north) * Math.PI) / 360));
		return {
			x: (west + east) / 2,
			y: (south + north) / 2,
			cos,
			scale: Math.min(
				(width - 70) / Math.max(0.00001, (east - west) * cos),
				(height - 70) / Math.max(0.00001, north - south)
			)
		};
	});

	function project(lon: number, lat: number) {
		return [
			width / 2 + (lon - bounds.x) * bounds.scale * bounds.cos,
			height / 2 - (lat - bounds.y) * bounds.scale
		] as const;
	}

	const cursor = $derived(point && project(unwrapLongitude(point.lon, origin), point.lat));

	$effect(() => {
		if (!$appearance.mode || width < 72 || height < 72) return;
		const ctx = canvas.getContext('2d');
		if (!ctx) return;
		const ratio = window.devicePixelRatio || 1;
		canvas.width = width * ratio;
		canvas.height = height * ratio;
		ctx.scale(ratio, ratio);
		ctx.lineCap = 'round';
		ctx.lineJoin = 'round';
		function trace(segments: number[][][]) {
			ctx!.beginPath();
			for (const line of segments) {
				for (const [i, [lon, lat]] of line.entries()) {
					const xy = project(lon!, lat!);
					if (i === 0) ctx!.moveTo(...xy);
					else ctx!.lineTo(...xy);
				}
			}
		}
		ctx.setLineDash([4, 7]);
		trace(excluded);
		ctx.lineWidth = 4;
		ctx.strokeStyle = appearanceColor('--plot-axis');
		ctx.stroke();
		ctx.setLineDash([]);
		trace(lines);
		ctx.lineWidth = 8;
		ctx.strokeStyle = appearanceColor('--map-casing');
		ctx.stroke();
		ctx.lineWidth = 4.5;
		ctx.strokeStyle = appearanceColor('--accent-primary');
		ctx.stroke();
		for (const line of lines) {
			let remaining = 48;
			for (let i = 1; i < line.length; i++) {
				const [x, y] = project(line[i - 1]![0]!, line[i - 1]![1]!);
				const [nextX, nextY] = project(line[i]![0]!, line[i]![1]!);
				const dx = nextX - x;
				const dy = nextY - y;
				const leg = Math.hypot(dx, dy);
				while (leg > remaining) {
					ctx.save();
					ctx.translate(x + (dx * remaining) / leg, y + (dy * remaining) / leg);
					ctx.rotate(Math.atan2(dy, dx));
					ctx.beginPath();
					ctx.moveTo(-2, -4);
					ctx.lineTo(2, 0);
					ctx.lineTo(-2, 4);
					ctx.strokeStyle = '#101d35';
					ctx.lineWidth = 3.5;
					ctx.stroke();
					ctx.strokeStyle = '#fff';
					ctx.lineWidth = 1.5;
					ctx.stroke();
					ctx.restore();
					remaining += 110;
				}
				remaining -= leg;
			}
		}
		const endpoints = [lines[0]?.[0], lines.at(-1)?.at(-1)];
		const [a, b] = endpoints.map((p) => p && project(p[0]!, p[1]!));
		if (a && b) {
			const close = Math.hypot(a[0] - b[0], a[1] - b[1]) < 24;
			for (const [i, [x, y]] of [a, b].entries()) {
				ctx.beginPath();
				ctx.arc(x + (close ? (i ? 11 : -11) : 0), y, 8, 0, Math.PI * 2);
				ctx.fillStyle = i ? '#bb2345' : '#137443';
				ctx.fill();
				ctx.lineWidth = 2;
				ctx.strokeStyle = '#fff';
				ctx.stroke();
			}
		}
	});
</script>

<div
	class="pointer-events-none absolute inset-0 bg-zinc-800"
	bind:clientWidth={width}
	bind:clientHeight={height}
	aria-hidden="true"
>
	<canvas class="block h-full w-full" bind:this={canvas}></canvas>
	{#if cursor && width >= 72 && height >= 72}
		<span
			class="absolute size-4 -translate-1/2 rounded-full border-2 border-white bg-accent-500"
			style:left={cursor[0] + 'px'}
			style:top={cursor[1] + 'px'}
		></span>
	{/if}
</div>
