<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { FORM_ZONES, formZone, metricNumber } from './analysis.js';

	let { value }: { value: number | null } = $props();

	const minimum = -40;
	const maximum = 40;
	const boundaries = [minimum, ...FORM_ZONES.slice(1).map((zone) => zone.from), maximum];
	const widths = FORM_ZONES.map(
		(item, i) => `${(FORM_ZONES[i + 1]?.from ?? maximum) - Math.max(item.from, minimum)}fr`
	).join(' ');
	const zone = $derived(value === null ? null : formZone(value));
	const position = $derived(
		value === null ? 0 : percent(Math.min(maximum, Math.max(minimum, value)))
	);

	function percent(value: number) {
		return (100 * (value - minimum)) / (maximum - minimum);
	}
</script>

{#if zone && value !== null}
	<div class="scale" style:--segments={widths} style:--position={`${position}%`}>
		<div class="track" aria-hidden="true">
			{#each FORM_ZONES as item (item.from)}
				<span style:background={item.color}></span>
			{/each}
		</div>
		<div
			class="marker"
			role="img"
			aria-label={m.analysis_form_marker({ value: metricNumber(value, true), zone: zone.label() })}
		>
			<span class="value">{metricNumber(value, true)}</span>
			<span class="pin"></span>
		</div>
		<div class="bounds" aria-hidden="true">
			{#each boundaries as boundary (boundary)}
				<span style:left={`${percent(boundary)}%`}
					>{boundary > 0 ? '+' : '−'}{Math.abs(boundary)}</span
				>
			{/each}
		</div>
		<ul class="zones" aria-label={m.analysis_form_zones()}>
			{#each FORM_ZONES as item, i (item.from)}
				{@const next = FORM_ZONES[i + 1]}
				<li style:--zone-color={item.color} class:current={item === zone}>
					<span aria-hidden="true"></span>
					{item.label()}
					<span class="sr-only"
						>&nbsp;{!next
							? m.analysis_zone_above({ value: item.from })
							: item.from === -Infinity
								? m.analysis_zone_below({ value: next.from })
								: m.analysis_zone_between({ from: item.from, to: next.from })}</span
					>
				</li>
			{/each}
		</ul>
	</div>
	<p class="guidance">{m.analysis_form_guidance()}</p>
{:else}
	<p class="guidance empty">{m.analysis_form_empty()}</p>
{/if}

<style>
	.scale {
		position: relative;
		padding-top: 2.05rem;
		margin: 0.85rem 0 0.55rem;
	}
	.track,
	.zones {
		display: grid;
		grid-template-columns: var(--segments);
	}
	.track {
		height: 1rem;
		overflow: hidden;
		border-radius: 0.45rem;
	}
	.track span + span {
		border-left: 1px solid var(--surface-card);
	}
	.marker {
		position: absolute;
		inset: 0 0 auto;
		height: 2.95rem;
		pointer-events: none;
	}
	.value {
		position: absolute;
		left: clamp(2.45rem, var(--position), calc(100% - 2.45rem));
		top: 0;
		transform: translateX(-50%);
		border: 1px solid var(--plot-form);
		border-radius: 0.45rem;
		background: var(--surface-card);
		padding: 0.16rem 0.45rem;
		color: var(--plot-form);
		font-size: 0.8rem;
		font-weight: 750;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	.pin {
		position: absolute;
		left: var(--position);
		top: 1.58rem;
		width: 2px;
		height: 1rem;
		transform: translateX(-50%);
		background: var(--plot-form);
	}
	.pin::after {
		content: '';
		position: absolute;
		bottom: -0.27rem;
		left: 50%;
		width: 0.65rem;
		height: 0.65rem;
		transform: translateX(-50%);
		border: 2px solid var(--surface-card);
		border-radius: 50%;
		background: var(--plot-form);
	}
	.bounds {
		position: relative;
		height: 1.15rem;
		margin-top: 0.3rem;
		color: var(--text-quiet);
		font-size: 0.72rem;
		font-variant-numeric: tabular-nums;
	}
	.bounds span {
		position: absolute;
		transform: translateX(-50%);
	}
	.bounds span:first-child {
		transform: none;
	}
	.bounds span:last-child {
		transform: translateX(-100%);
	}
	.zones {
		gap: 0.25rem 0;
		margin-top: 0.25rem;
		font-size: 0.75rem;
		color: var(--text-secondary);
	}
	.zones li {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0.35rem;
		padding-inline: 0.2rem;
		text-align: center;
		line-height: 1.25;
	}
	.zones li span:first-child {
		display: none;
		width: 0.5rem;
		height: 0.5rem;
		flex-shrink: 0;
		border-radius: 50%;
		background: var(--zone-color);
	}
	.zones .current {
		color: var(--text-strong);
		font-weight: 700;
	}
	.guidance {
		color: var(--text-quiet);
		font-size: 0.75rem;
	}
	.empty {
		margin-top: 1rem;
	}
	@media (max-width: 580px) {
		.zones {
			display: flex;
			flex-wrap: wrap;
			gap: 0.45rem 0.75rem;
		}
		.zones li {
			padding: 0;
		}
		.zones li span:first-child {
			display: inline-block;
		}
	}
</style>
