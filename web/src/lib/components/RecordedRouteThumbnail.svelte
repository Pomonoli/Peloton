<script lang="ts">
	import { onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { appearance } from '#lib/appearance.js';
	import { m } from '#lib/paraglide/messages.js';
	import { mapLines, type RecordedTrack } from '#lib/recorded-routes.js';

	let { track }: { track: RecordedTrack } = $props();
	let container: HTMLDivElement;
	let visible = $state(false);
	const failedTiles = new SvelteSet<string>();
	const width = 256;
	const height = 164;
	const tileSize = 256;
	const map = $derived.by(() => {
		const lines = mapLines(track).map((line) =>
			line.map(([lon, lat]) => {
				const radians = (Math.max(-85, Math.min(85, lat)) * Math.PI) / 180;
				return [(lon + 180) / 360, (1 - Math.asinh(Math.tan(radians)) / Math.PI) / 2] as const;
			})
		);
		let minX = Infinity,
			minY = Infinity,
			maxX = -Infinity,
			maxY = -Infinity;
		for (const [x, y] of lines.flat()) {
			minX = Math.min(minX, x);
			maxX = Math.max(maxX, x);
			minY = Math.min(minY, y);
			maxY = Math.max(maxY, y);
		}
		if (!Number.isFinite(minX)) return null;
		const zoom = Math.max(
			0,
			Math.min(
				15,
				Math.floor(
					Math.log2(
						Math.min(
							(width - 40) / (tileSize * (maxX - minX)),
							(height - 38) / (tileSize * (maxY - minY))
						)
					)
				)
			)
		);
		const size = 2 ** zoom;
		const world = size * tileSize;
		const left = ((minX + maxX) / 2) * world - width / 2;
		const top = ((minY + maxY) / 2) * world - height / 2;
		const tiles = [];
		for (let x = Math.floor(left / tileSize); x * tileSize < left + width; x++) {
			for (
				let y = Math.max(0, Math.floor(top / tileSize));
				y < size && y * tileSize < top + height;
				y++
			) {
				const wrapped = ((x % size) + size) % size;
				tiles.push({
					key: `${zoom}/${x}/${y}`,
					url: `https://tile.openstreetmap.org/${zoom}/${wrapped}/${y}.png`,
					x: x * tileSize - left,
					y: y * tileSize - top
				});
			}
		}
		const paths = lines.map((line) =>
			line.map(([x, y]) => `${x * world - left},${y * world - top}`).join(' ')
		);
		return { paths, tiles };
	});

	onMount(() => {
		const observer = new IntersectionObserver((entries) => {
			if (entries.some((entry) => entry.isIntersecting)) {
				visible = true;
				observer.disconnect();
			}
		});
		observer.observe(container);
		return () => observer.disconnect();
	});
</script>

<div
	bind:this={container}
	class="relative h-full min-h-[5.5rem] w-full overflow-hidden rounded-lg border border-zinc-800 bg-zinc-900"
>
	{#if map}
		<svg
			viewBox="0 0 {width} {height}"
			preserveAspectRatio="xMidYMid meet"
			class="absolute inset-0 size-full"
			role="img"
			aria-label={m.ride_route_preview()}
		>
			{#if visible}
				<g class:dark={$appearance.mode === 'dark'}>
					{#each map.tiles as tile (tile.key)}
						{#if !failedTiles.has(tile.url)}
							<image
								href={tile.url}
								x={tile.x}
								y={tile.y}
								width={tileSize + 0.2}
								height={tileSize + 0.2}
								onerror={() => failedTiles.add(tile.url)}
							/>
						{/if}
					{/each}
				</g>
			{/if}
			<g class="track" fill="none" stroke-linecap="round" stroke-linejoin="round">
				{#each map.paths as path, index (index)}
					<polyline points={path} stroke="var(--map-casing)" stroke-width="7" />
					<polyline points={path} stroke="var(--accent-primary)" stroke-width="4" />
				{/each}
			</g>
		</svg>
		{#if visible && map.tiles.some((tile) => !failedTiles.has(tile.url))}
			<span
				class="absolute right-0 bottom-0 rounded-tl bg-panel px-1 text-[10px] leading-tight text-zinc-300"
			>
				© OpenStreetMap
			</span>
		{/if}
	{:else}
		<div class="grid h-full place-items-center px-2 text-center text-xs text-quiet">
			{m.routes_map_unavailable()}
		</div>
	{/if}
</div>

<style>
	.dark {
		filter: invert(89%) hue-rotate(180deg) saturate(65%) brightness(83%);
	}
</style>
