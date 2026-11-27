<script lang="ts">
	import type { IconName } from '#lib/icons.js';
	import Icon from './Icon.svelte';

	type Metric = {
		label: string;
		shortLabel?: string;
		value: string;
		icon: IconName;
		href?: string | undefined;
	};
	let { metrics, label }: { metrics: Metric[]; label: string } = $props();
</script>

<dl class="surface metric-strip" aria-label={label}>
	{#each metrics as metric (metric.label)}
		<div class="metric">
			<dt aria-label={metric.shortLabel ? metric.label : undefined} title={metric.label}>
				<Icon name={metric.icon} size={18} />
				{#if metric.shortLabel}
					<span class="md:hidden" aria-hidden="true">{metric.shortLabel}</span>
					<span class="hidden md:inline" aria-hidden="true">{metric.label}</span>
				{:else}
					{metric.label}
				{/if}
			</dt>
			<dd class="metric-value">
				{#if metric.href}
					<a href={metric.href}>{metric.value}</a>
				{:else}
					{metric.value}
				{/if}
			</dd>
		</div>
	{/each}
</dl>

<style>
	.metric-strip {
		display: flex;
		flex-wrap: wrap;
		row-gap: 0.65rem;
		padding-block: 0.7rem;
	}

	.metric {
		position: relative;
		display: flex;
		min-width: min-content;
		flex: 1 0 0;
		flex-direction: column;
		align-items: center;
		gap: 0.16rem;
		padding: 1.35rem 0.25rem 0;
		text-align: center;
	}

	dt {
		color: var(--text-muted);
		font-size: clamp(0.625rem, 2.6vw, 0.75rem);
		line-height: 1.2;
		text-wrap: balance;
	}

	dt :global(svg) {
		position: absolute;
		inset-block-start: 0;
		inset-inline-start: 50%;
		translate: -50%;
		color: var(--accent-text);
	}

	dd {
		order: -1;
		font-size: clamp(0.75rem, 3.1vw, 1.35rem);
		font-weight: 750;
		letter-spacing: -0.045em;
		line-height: 1.25;
	}

	dd a:hover {
		color: var(--accent-text);
		text-decoration: underline;
	}

	@media (width >= 48rem) {
		.metric-strip {
			padding-block: 1rem;
		}

		.metric + .metric {
			border-inline-start: 1px solid var(--line-subtle);
		}
	}
</style>
