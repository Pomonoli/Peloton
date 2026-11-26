<script lang="ts">
	import { memberURL } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import AnalysisChart from './AnalysisChart.svelte';
	import FormScale from './FormScale.svelte';
	import {
		METRICS,
		MIN_RECENT_SCORES,
		aggregateStress,
		changeFrom,
		describeTraining,
		metricNumber,
		type Analysis,
		type Grouping
	} from './analysis.js';

	let { data } = $props();
	const id = $props.id();
	let days = $state(180);
	let group = $state<Grouping>('weekly');
	let hiddenMetrics = $state<string[]>([]);
	let analysis = $state<Analysis | null>(null);
	let loading = $state(true);
	const ranges = [
		{ value: 90, label: '3M' },
		{ value: 180, label: '6M' },
		{ value: 365, label: '1Y' },
		{ value: 3650, label: m.analysis_range_all() }
	];
	const groupings = [
		{ value: 'daily', label: m.analysis_daily },
		{ value: 'weekly', label: m.analysis_weekly },
		{ value: 'monthly', label: m.analysis_monthly }
	] as const;

	const status = $derived(analysis && describeTraining(analysis));
	const hasLoad = $derived(!!analysis?.dates.length);
	const periods = $derived(analysis?.stress_days.length ? aggregateStress(analysis, group) : []);
	const coverage = $derived(
		analysis && [
			{ label: m.analysis_stress_imported(), count: analysis.coverage.imported },
			{ label: m.analysis_stress_sensor(), count: analysis.coverage.sensor_estimate },
			{ label: m.analysis_stress_duration(), count: analysis.coverage.duration_estimate },
			{ label: m.analysis_stress_missing(), count: analysis.coverage.missing }
		]
	);
	const total = $derived(coverage?.reduce((sum, { count }) => sum + count, 0) ?? 0);
	const coverageNote = $derived.by(() => {
		if (!analysis) return '';
		if (analysis.coverage.missing > 0) {
			const scored = total - analysis.coverage.missing;
			return scored
				? m.analysis_coverage_warning({ scored, total })
				: m.analysis_coverage_no_scores();
		}
		if (analysis.recent_scored > 0 && analysis.recent_scored < MIN_RECENT_SCORES)
			return m.analysis_recent_few();
		return '';
	});

	$effect(() => {
		const url = memberURL(`/api/analysis?days=${days}`, data.member);
		const request = new AbortController();
		loading = true;
		analysis = null;
		void fetch(url, { signal: request.signal })
			.then(async (response) => {
				if (!response.ok) throw new Error();
				const result: Analysis = await response.json();
				if (!request.signal.aborted) analysis = result;
			})
			.catch(() => {})
			.finally(() => {
				if (!request.signal.aborted) loading = false;
			});
		return () => request.abort();
	});
</script>

{#snippet infoIcon()}
	<svg
		width="17"
		height="17"
		viewBox="0 0 24 24"
		fill="none"
		stroke="currentColor"
		stroke-width="1.7"
		aria-hidden="true"
	>
		<circle cx="12" cy="12" r="9" />
		<path stroke-linecap="round" d="M12 11v5m0-8h.01" />
	</svg>
{/snippet}

<div class="analysis-page">
	<header>
		<h1 class="page-title">{m.nav_analysis()}</h1>
		<div class="segmented ranges" role="group" aria-label={m.analysis_period()}>
			{#each ranges as range (range.value)}
				<button
					type="button"
					aria-pressed={days === range.value}
					onclick={() => (days = range.value)}
				>
					{range.label}
				</button>
			{/each}
		</div>
	</header>

	{#if loading}
		<p class="card loading" role="status">{m.analysis_loading()}</p>
	{:else if analysis && status && coverage}
		<section class="card status" data-tone={status.tone} aria-label={m.analysis_status()}>
			<div class="status-body">
				<div class="status-summary">
					<div class="status-icon">
						<svg
							width="31"
							height="31"
							viewBox="0 0 24 24"
							fill="none"
							stroke="currentColor"
							stroke-width="1.6"
							stroke-linecap="round"
							stroke-linejoin="round"
							aria-hidden="true"
						>
							{#if status.tone === 'fresh'}
								<path
									d="M5 19c2.5-3.9 5.8-6.7 9-8.6M6.2 17C1.8 10.2 9 5 20 5c0 8.5-4.7 16-13.8 12Z"
								/>
							{:else if status.tone === 'load'}
								<path d="M2.5 12h4l2.8-6.5 4 13 2.9-6.5h5.3" />
							{:else}
								<path d="M12 4v15m-5.5 0h11M4 8h16M6 8l-3 6h6L6 8Zm12 0-3 6h6l-3-6Z" />
							{/if}
						</svg>
					</div>
					<div>
						<h2>{status.title}</h2>
						<p>{status.detail}</p>
					</div>
				</div>
				{#if status.insight}
					<div class="insight">
						<svg
							width="20"
							height="20"
							viewBox="0 0 24 24"
							fill="none"
							stroke="currentColor"
							stroke-width="1.6"
							stroke-linecap="round"
							stroke-linejoin="round"
							aria-hidden="true"
						>
							<path d="M9 18h6m-5 3h4M8.1 14.4a6 6 0 1 1 7.8 0L15 16H9l-.9-1.6Z" />
						</svg>
						<div>
							<h3>{m.analysis_insight()}</h3>
							<p>{status.insight}</p>
						</div>
					</div>
				{/if}
			</div>
			{#if coverageNote}
				<p class="coverage-note">{@render infoIcon()}{coverageNote}</p>
			{/if}
		</section>

		<div class="training-grid">
			<section class="card metrics" aria-labelledby={id + '-metrics'}>
				<h2 id={id + '-metrics'}>{m.analysis_metrics()}</h2>
				<div class="metric-list">
					{#each METRICS as { key, label, color, info } (key)}
						<div class="metric" style:--metric-color={color}>
							<div class="metric-reading">
								<div class="metric-name">
									<h3>{label()}</h3>
									<button
										type="button"
										popovertarget={id + key}
										aria-label={m.analysis_metric_info({ metric: label() })}
										aria-describedby={id + key}
									>
										{@render infoIcon()}
									</button>
									<p id={id + key} class="metric-help" popover="auto">{info()}</p>
								</div>
								<p class="metric-value">
									{hasLoad ? metricNumber(analysis.current[key], key === 'tsb') : '—'}
								</p>
								{#if hasLoad}
									<div class="deltas">
										{#each [{ days: 7, label: m.analysis_days_7() }, { days: 30, label: m.analysis_days_30() }] as period (period.days)}
											{@const change = changeFrom(analysis, key, period.days)}
											{#if change !== null}
												<span
													class="delta"
													aria-label={m.analysis_change({
														value: metricNumber(change, true),
														period: period.label
													})}
												>
													<strong>{metricNumber(change, true)}</strong>
													<span>{period.label}</span>
												</span>
											{/if}
										{/each}
									</div>
								{/if}
							</div>
							{#if hasLoad}
								<div class="metric-explanation">
									<span class="chip">{status.metrics[key].label}</span>
									<p>{status.metrics[key].detail}</p>
								</div>
							{/if}
						</div>
					{/each}
				</div>
			</section>

			<section class="card trends" aria-labelledby={id + '-trends'}>
				<h2 id={id + '-trends'}>{m.analysis_trends()}</h2>
				<p class="description">{m.analysis_trends_description()}</p>
				<AnalysisChart {analysis} bind:hidden={hiddenMetrics} />
			</section>
		</div>

		<section class="card" aria-labelledby={id + '-form'}>
			<div class="form-heading">
				<div>
					<h2 id={id + '-form'}>{m.analysis_form()}</h2>
					<p class="description">{m.analysis_form_intro()}</p>
				</div>
				{#if hasLoad}
					<p class="form-interpretation">
						<strong class="chip" style:--metric-color="var(--plot-form)"
							>{status.metrics.tsb.label}</strong
						>
						{status.formDetail}
					</p>
				{/if}
			</div>
			<FormScale value={hasLoad ? analysis.current.tsb : null} />
		</section>

		<section class="card stress" aria-labelledby={id + '-stress'}>
			<div class="stress-heading">
				<div>
					<h2 id={id + '-stress'}>{m.analysis_tss()}</h2>
					<p class="description">{m.analysis_tss_description()}</p>
				</div>
				<div class="segmented grouping" role="group" aria-label={m.analysis_tss_grouping()}>
					{#each groupings as item (item.value)}
						<button
							type="button"
							aria-pressed={group === item.value}
							onclick={() => (group = item.value)}
						>
							{item.label()}
						</button>
					{/each}
				</div>
			</div>

			<dl class="coverage">
				{#each coverage as item (item.label)}
					<div>
						<dt>{item.label}</dt>
						<dd>{item.count}</dd>
					</div>
				{/each}
			</dl>

			{#if periods.length}
				{#key group}
					<AnalysisChart {analysis} {periods} />
				{/key}
			{:else}
				<p class="empty-stress">{m.analysis_tss_empty_range()}</p>
			{/if}
		</section>
	{:else}
		<p class="card" role="alert">{m.analysis_failed()}</p>
	{/if}
</div>

<style>
	.analysis-page {
		--card-border: var(--line-subtle);
		--zone-very-fatigued: light-dark(#df8799, #be526f);
		--zone-accumulated: light-dark(#edb480, #b47746);
		--zone-balanced: light-dark(#f1d885, #b19b50);
		--zone-fresh: light-dark(#bbd989, #8bb65b);
		--zone-very-fresh: light-dark(#83c49a, #489d75);
		container-type: inline-size;
		display: grid;
		gap: 0.9rem;
	}
	header {
		display: grid;
		gap: 0.75rem;
	}
	h2 {
		font-size: 1.1rem;
		font-weight: 730;
		letter-spacing: -0.025em;
		line-height: 1.25;
	}
	.card {
		min-width: 0;
		border: 1px solid var(--card-border);
		border-radius: var(--radius-panel);
		background: var(--surface-card);
		box-shadow: var(--panel-shadow);
		padding: clamp(0.8rem, 2vw, 1.2rem);
	}
	.segmented {
		display: grid;
		grid-auto-columns: 1fr;
		grid-auto-flow: column;
		gap: 0.15rem;
		border: 1px solid var(--card-border);
		border-radius: 0.65rem;
		background: color-mix(in srgb, var(--surface-muted) 67%, var(--surface-page));
		padding: 0.17rem;
	}
	.ranges {
		width: min(100%, 24rem);
	}
	.segmented button {
		min-height: var(--target-size);
		border-radius: 0.48rem;
		padding: 0.1rem 0.75rem;
		color: var(--text-secondary);
		font-size: 0.8rem;
		font-weight: 600;
	}
	.segmented button:hover {
		color: var(--accent-text);
	}
	.segmented button[aria-pressed='true'] {
		box-shadow: 0 1px 2px light-dark(#172d5229, #090d1880);
		background: var(--surface-card);
		color: var(--accent-text-strong);
	}
	.status {
		--tone: var(--accent-text);
		--tone-wash: color-mix(in srgb, var(--accent-primary) 12%, var(--surface-card));
		border-color: color-mix(in srgb, var(--tone) 26%, var(--card-border));
		background: color-mix(in srgb, var(--tone-wash) 44%, var(--surface-card));
	}
	.status[data-tone='fresh'] {
		--tone: var(--plot-form);
		--tone-wash: color-mix(in srgb, var(--plot-form) 13%, var(--surface-card));
	}
	.status[data-tone='load'] {
		--tone: var(--warning-strong);
		--tone-wash: color-mix(in srgb, var(--warning-strong) 13%, var(--surface-card));
	}
	.status h2 {
		font-size: clamp(1.22rem, 2.6vw, 1.65rem);
		letter-spacing: -0.035em;
	}
	.status-body,
	.status-summary,
	.insight,
	.coverage-note {
		display: flex;
	}
	.status-body {
		gap: 1.1rem;
	}
	.status-summary {
		flex: 1;
		align-items: center;
		gap: clamp(0.65rem, 2vw, 1.1rem);
	}
	.status-summary p {
		margin-top: 0.35rem;
		color: var(--text-primary);
		font-size: 0.9rem;
		line-height: 1.5;
	}
	.status-icon {
		display: grid;
		width: clamp(2.8rem, 6vw, 4rem);
		aspect-ratio: 1;
		flex: 0 0 auto;
		place-items: center;
		border-radius: 0.9rem;
		background: var(--tone-wash);
		color: var(--tone);
	}
	.insight {
		width: min(30%, 17rem);
		align-self: center;
		gap: 0.5rem;
		border-left: 1px solid var(--card-border);
		padding-left: 1rem;
		font-size: 0.82rem;
	}
	.insight svg {
		flex: 0 0 auto;
		color: var(--warning-strong);
	}
	.insight h3 {
		margin-bottom: 0.15rem;
		font-size: 0.85rem;
		font-weight: 700;
	}
	.insight p {
		color: var(--text-secondary);
	}
	.coverage-note {
		align-items: flex-start;
		gap: 0.45rem;
		margin-top: 0.8rem;
		border: 1px solid var(--card-border);
		border-radius: 0.5rem;
		background: color-mix(in srgb, var(--surface-muted) 28%, var(--surface-card));
		padding: 0.42rem 0.65rem;
		color: var(--text-secondary);
		font-size: 0.8rem;
	}
	.coverage-note svg {
		flex: 0 0 auto;
		margin-top: 0.09rem;
	}
	.training-grid {
		display: grid;
		gap: 0.9rem;
	}
	.metric-list {
		margin-top: 0.55rem;
	}
	.metric {
		position: relative;
		display: grid;
		grid-template-columns: minmax(7.5rem, 0.95fr) minmax(0, 1.05fr);
		gap: 0.65rem;
		border-top: 1px solid var(--plot-grid);
		padding: 0.55rem 0 0.55rem 0.7rem;
	}
	.metric:last-child {
		padding-bottom: 0;
	}
	.metric::before {
		position: absolute;
		inset: 0.7rem auto 0.7rem 0;
		width: 0.22rem;
		border-radius: 0.4rem;
		background: var(--metric-color);
		content: '';
	}
	.metric:last-child::before {
		bottom: 0;
	}
	.metric-name {
		display: flex;
		align-items: center;
	}
	.metric-name h3 {
		color: var(--text-secondary);
		font-size: 0.79rem;
		font-weight: 660;
	}
	.metric-name button {
		display: grid;
		width: 1.6rem;
		min-height: 1.7rem;
		place-items: center;
		border-radius: 0.3rem;
		color: var(--text-quiet);
	}
	.metric-name button:hover {
		color: var(--accent-text);
	}
	.metric-help {
		position: fixed;
		inset: auto 1rem max(5rem, env(safe-area-inset-bottom));
		width: max-content;
		max-width: min(17rem, calc(100vw - 2rem));
		margin-inline: auto;
		border: 1px solid var(--line-strong);
		border-radius: 0.5rem;
		background: var(--surface-panel);
		padding: 0.7rem;
		box-shadow: 0 3px 15px rgb(0 0 0 / 13%);
		color: var(--text-primary);
		font-size: 0.82rem;
	}
	@supports (position-area: block-end span-inline-end) {
		.metric-help {
			inset: auto;
			margin: 0.25rem 0;
			position-area: block-end span-inline-end;
			position-try-fallbacks:
				flip-inline,
				flip-block,
				flip-block flip-inline;
		}
	}
	.metric-value {
		font-size: 1.75rem;
		font-weight: 750;
		letter-spacing: -0.05em;
		line-height: 1;
		font-variant-numeric: tabular-nums;
	}
	.deltas {
		display: flex;
		flex-wrap: wrap;
		gap: 0.1rem 0.6rem;
		margin-top: 0.35rem;
	}
	.delta {
		display: flex;
		align-items: baseline;
		gap: 0.2rem;
		color: var(--text-quiet);
		font-size: 0.68rem;
		white-space: nowrap;
	}
	.delta strong {
		color: var(--text-secondary);
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}
	.metric-explanation {
		align-self: center;
		color: var(--text-secondary);
		font-size: 0.76rem;
		line-height: 1.35;
	}
	.metric-explanation p {
		margin-top: 0.3rem;
	}
	.chip {
		display: inline-block;
		width: fit-content;
		border-radius: 0.4rem;
		background: color-mix(in srgb, var(--metric-color) 13%, var(--surface-card));
		padding: 0.18rem 0.45rem;
		color: color-mix(in srgb, var(--metric-color) 83%, var(--text-strong));
		font-size: 0.72rem;
		font-weight: 750;
		line-height: 1.2;
	}
	.form-heading,
	.stress-heading {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		justify-content: space-between;
		gap: 0.6rem 1rem;
	}
	.description {
		margin-top: 0.2rem;
		color: var(--text-muted);
		font-size: 0.84rem;
		line-height: 1.4;
	}
	.form-interpretation {
		display: flex;
		align-items: flex-start;
		gap: 0.45rem;
		color: var(--text-secondary);
		font-size: 0.83rem;
	}
	.form-interpretation .chip {
		flex: 0 0 auto;
	}
	.coverage {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		margin: 0.75rem 0 0.35rem;
		border: 1px solid var(--card-border);
		border-radius: 0.55rem;
		background: color-mix(in srgb, var(--surface-muted) 20%, var(--surface-card));
	}
	.coverage > div {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 0.25rem 0.45rem;
		padding: 0.45rem 0.65rem;
	}
	.coverage > div + div {
		border-left: 1px solid var(--card-border);
	}
	.coverage dt {
		color: var(--text-secondary);
		font-size: 0.75rem;
	}
	.coverage dd {
		font-size: 0.9rem;
		font-weight: 750;
		font-variant-numeric: tabular-nums;
	}
	.loading,
	.empty-stress {
		display: grid;
		min-height: 10rem;
		place-items: center;
		color: var(--text-quiet);
		font-size: 0.9rem;
	}
	@container (min-width: 750px) {
		.training-grid {
			grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
		}
	}
	@container (610px <= width < 750px) {
		.metric-list {
			display: grid;
			grid-template-columns: repeat(3, minmax(0, 1fr));
		}
		.metric {
			grid-template-columns: minmax(0, 1fr);
			align-content: start;
			gap: 0.35rem;
			padding-inline: 0.8rem 0.5rem;
		}
		.metric + .metric {
			border-left: 1px solid var(--plot-grid);
		}
		.metric::before {
			display: none;
		}
	}
	@container (max-width: 620px) {
		.status-body {
			flex-direction: column;
			gap: 0.7rem;
		}
		.status-summary {
			align-items: flex-start;
		}
		.insight {
			align-self: stretch;
			width: auto;
			border-left: 0;
			border-top: 1px solid var(--card-border);
			padding: 0.6rem 0 0;
		}
		.grouping {
			width: 100%;
		}
		.coverage {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
		.coverage > div:nth-child(3) {
			border-left: 0;
		}
		.coverage > div:nth-child(n + 3) {
			border-top: 1px solid var(--card-border);
		}
	}
</style>
