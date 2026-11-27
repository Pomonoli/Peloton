<script lang="ts">
	import { onMount } from 'svelte';
	import { SvelteURLSearchParams } from 'svelte/reactivity';
	import ChartAxes from './ChartAxes.svelte';
	import { formatDateOnly, intlLocale } from '#lib/format.js';
	import { memberURL, type MemberView } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		linearTrend,
		riderColor,
		scatterScale,
		type SpeedDistancePoint
	} from '#lib/speed-distance.js';

	let { rider, query }: { rider: MemberView; query: string } = $props();

	type Series = { member: MemberView; color: string; points: SpeedDistancePoint[] };
	type Selection = { series: Series; point: SpeedDistancePoint };

	let directory = $state<MemberView[]>([]);
	let directoryError = $state(false);
	let directoryLoading = $state(true);
	let selected = $state<string[]>([]);
	let series = $state.raw<Series[]>([]);
	let loading = $state(true);
	let failed = $state(false);
	let retry = $state(0);
	let width = $state(0);
	let active = $state.raw<Selection>();
	let lastTap = $state<string>();
	const choices = $derived([rider, ...directory.filter((item) => item.id !== rider.id)]);
	const colorOrder = $derived(choices.map((item) => item.id).sort());
	const selectedIDs = $derived(selected.length ? selected : [rider.id]);
	const legend = $derived(choices.filter((item) => selectedIDs.includes(item.id)));
	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 2 }));
	const tickNumber = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 6 }));

	onMount(() => {
		const request = new AbortController();
		void (async () => {
			try {
				const res = await fetch('/api/riders', { signal: request.signal });
				if (!res.ok) throw new Error();
				const body: { members: MemberView[] } = await res.json();
				if (!Array.isArray(body.members)) throw new Error();
				if (!request.signal.aborted) directory = body.members;
			} catch {
				if (!request.signal.aborted) directoryError = true;
			} finally {
				if (!request.signal.aborted) directoryLoading = false;
			}
		})();
		return () => request.abort();
	});

	$effect(() => {
		const request = new AbortController();
		const members = legend.map((member) => ({ member, color: color(member.id) }));
		const attempt = retry;
		loading = true;
		failed = false;
		active = undefined;
		lastTap = undefined;
		series = [];
		void (async () => {
			const results = await Promise.allSettled(
				members.map(async (entry) => {
					const params = new SvelteURLSearchParams(query);
					params.set('member', entry.member.username);
					const res = await fetch(`/api/stats/speed-distance?${params}`, {
						signal: request.signal
					});
					if (!res.ok) throw new Error();
					const points: SpeedDistancePoint[] = await res.json();
					if (!Array.isArray(points)) throw new Error();
					return { ...entry, points };
				})
			);
			if (request.signal.aborted || attempt !== retry) return;
			const next = results.flatMap((result) =>
				result.status === 'fulfilled' ? [result.value] : []
			);
			series = next;
			failed = next.length < members.length;
			loading = false;
		})();
		return () => request.abort();
	});

	const height = $derived(width < 520 ? 260 : 320);
	const x0 = 48;
	const top = 12;
	const right = $derived(width - 14);
	const bottom = $derived(height - 40);
	const trends = $derived(series.map((item) => ({ item, trend: linearTrend(item.points) })));
	const bounds = $derived.by(() => {
		let xMin = Infinity;
		let xMax = 0;
		let yMin = Infinity;
		let yMax = 0;
		for (const item of series) {
			for (const p of item.points) {
				xMin = Math.min(xMin, p.distance_km);
				xMax = Math.max(xMax, p.distance_km);
				yMin = Math.min(yMin, p.speed_kmh);
				yMax = Math.max(yMax, p.speed_kmh);
			}
		}
		if (!Number.isFinite(xMin)) return null;
		for (const { trend } of trends) {
			if (!trend) continue;
			yMin = Math.min(yMin, Math.max(0, trend.lowSpeed), Math.max(0, trend.highSpeed));
			yMax = Math.max(yMax, trend.lowSpeed, trend.highSpeed);
		}
		return {
			x: scatterScale(xMin, xMax, Math.max(3, Math.floor(width / 105))),
			y: scatterScale(yMin, yMax, 5)
		};
	});

	function x(value: number) {
		const range = bounds!.x;
		return x0 + ((value - range.min) * (right - x0)) / (range.max - range.min);
	}

	function y(value: number) {
		const range = bounds!.y;
		return bottom - ((value - range.min) * (bottom - top)) / (range.max - range.min);
	}

	function color(id: string) {
		return riderColor(colorOrder.indexOf(id));
	}

	function label(member: MemberView) {
		return member.display_name || member.username;
	}

	function title(point: SpeedDistancePoint) {
		return point.title || m.comparison_unnamed_ride();
	}

	function measurement(point: SpeedDistancePoint) {
		return `${number.format(point.distance_km)} km · ${number.format(point.speed_kmh)} km/h`;
	}

	function toggle(id: string, checked: boolean) {
		selected = checked ? [...selectedIDs, id] : selectedIDs.filter((item) => item !== id);
	}

	function activate(event: MouseEvent, entry: Series, point: SpeedDistancePoint) {
		if (event instanceof PointerEvent && event.pointerType === 'touch' && lastTap !== point.id) {
			event.preventDefault();
			lastTap = point.id;
			active = { series: entry, point };
		}
	}
</script>

<section aria-labelledby="speed-distance-title" class="surface scatter space-y-3 p-3 sm:p-4">
	<div class="flex flex-wrap items-start justify-between gap-2">
		<h2 id="speed-distance-title" class="section-title">{m.stats_speed_distance()}</h2>
		<details class="relative">
			<summary class="action flex cursor-pointer items-center gap-2">
				{m.stats_compare_riders()} <span class="text-quiet">({selectedIDs.length})</span>
			</summary>
			<fieldset
				class="surface picker absolute right-0 z-10 max-h-64 min-w-48 overflow-auto p-2 shadow-lg"
			>
				<legend class="sr-only">{m.stats_compare_riders()}</legend>
				{#each choices as item (item.id)}
					<label class="flex min-h-10 items-center gap-2 px-1 text-sm">
						<input
							type="checkbox"
							checked={selectedIDs.includes(item.id)}
							disabled={selectedIDs.length === 1 && selectedIDs.includes(item.id)}
							onchange={(event) => toggle(item.id, event.currentTarget.checked)}
						/>
						{label(item)}
					</label>
				{/each}
				{#if directoryLoading}
					<p class="px-1 text-sm text-quiet">{m.ride_loading()}</p>
				{:else if directoryError}
					<p role="status" class="px-1 text-sm text-quiet">{m.friends_failed()}</p>
				{/if}
			</fieldset>
		</details>
	</div>

	<ul
		class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-quiet"
		aria-label={m.stats_compare_riders()}
	>
		{#each legend as member (member.id)}
			<li class="flex items-center gap-1.5">
				<span class="size-2.5 rounded-full" style:background={color(member.id)}></span>
				{label(member)}
			</li>
		{/each}
	</ul>

	{#if loading}
		<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
	{:else if failed}
		<div class="flex items-center gap-3 text-sm">
			<p role="alert" class="text-quiet">{m.stats_speed_distance_failed()}</p>
			<button
				type="button"
				class="min-h-10 text-accent-300 hover:underline"
				onclick={() => retry++}
			>
				{m.community_retry()}
			</button>
		</div>
	{/if}

	<div class="min-w-0" bind:clientWidth={width}>
		{#if bounds && width > 130}
			<svg {width} {height} role="group" aria-label={m.stats_speed_distance()} class="max-w-full">
				<ChartAxes
					left={x0}
					{right}
					{top}
					{bottom}
					{width}
					xTicks={bounds.x.ticks.map((value) => ({
						position: x(value),
						label: tickNumber.format(value)
					}))}
					yTicks={bounds.y.ticks.map((value) => ({
						position: y(value),
						label: tickNumber.format(value)
					}))}
				/>
				<text class="axis-label" x={(x0 + right) / 2} y={height - 5} text-anchor="middle">
					{m.stats_distance_km()}
				</text>
				<text
					class="axis-label"
					transform={`translate(13 ${(top + bottom) / 2}) rotate(-90)`}
					text-anchor="middle"
				>
					{m.stats_speed_kmh()}
				</text>
				{#each trends as { item, trend } (item.member.id)}
					{#if trend}
						<line
							x1={x(trend.low)}
							x2={x(trend.high)}
							y1={y(Math.max(0, trend.lowSpeed))}
							y2={y(Math.max(0, trend.highSpeed))}
							stroke={item.color}
							stroke-width="1.5"
							stroke-dasharray="5 4"
							aria-hidden="true"
						/>
					{/if}
				{/each}
				{#each series as entry (entry.member.id)}
					{#each entry.points as point (point.id)}
						<a
							class="point"
							class:active={active?.point.id === point.id}
							href={memberURL(`/activities/${point.id}`, entry.member)}
							aria-label={`${label(entry.member)} · ${title(point)} · ${formatDateOnly(point.start)} · ${measurement(point)}`}
							onpointerenter={() => (active = { series: entry, point })}
							onfocus={() => (active = { series: entry, point })}
							onclick={(event) => activate(event, entry, point)}
						>
							<circle
								class="hit"
								cx={x(point.distance_km)}
								cy={y(point.speed_kmh)}
								r="10"
								fill="transparent"
							/>
							<circle
								class="dot"
								cx={x(point.distance_km)}
								cy={y(point.speed_kmh)}
								r="3.5"
								fill={entry.color}
							/>
						</a>
					{/each}
				{/each}
			</svg>
			<div class="point-details flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
				{#if active}
					<div class="min-w-0 break-words">
						<strong>{title(active.point)}</strong>
						<span class="text-quiet">
							· {label(active.series.member)} · {formatDateOnly(active.point.start)} · {measurement(
								active.point
							)}
						</span>
					</div>
					<a
						class="text-accent-300 hover:underline"
						href={memberURL(`/activities/${active.point.id}`, active.series.member)}
					>
						{m.comparison_open_activity()}
					</a>
				{:else}
					<p class="text-quiet">{m.stats_inspect_point()}</p>
				{/if}
			</div>
		{:else if !loading && !failed}
			<p class="py-4 text-sm text-quiet">{m.stats_speed_distance_empty()}</p>
		{/if}
	</div>
</section>

<style>
	.scatter {
		--scatter-lightness: 38%;
	}

	:global([data-theme='dark']) .scatter {
		--scatter-lightness: 67%;
	}

	.picker {
		top: calc(100% + 0.35rem);
	}

	.axis-label {
		fill: var(--plot-axis);
		font: var(--plot-font);
	}

	.point .dot {
		pointer-events: none;
	}

	.point.active .dot,
	.point:focus-visible .dot {
		r: 5;
		stroke: var(--plot-axis);
		stroke-width: 1.5;
	}

	.point-details {
		min-height: 3.25rem;
	}
</style>
