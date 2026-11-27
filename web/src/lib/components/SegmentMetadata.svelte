<script lang="ts">
	import { intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import Icon from './Icon.svelte';

	let {
		holder,
		riders,
		attempts,
		attemptsLabel = m.segment_attempts()
	}: { holder?: string; riders?: number; attempts?: number; attemptsLabel?: string } = $props();
	const number = $derived(new Intl.NumberFormat(intlLocale()));
</script>

<dl class="relative flex min-w-0 items-center gap-1.5 whitespace-nowrap tabular-nums sm:gap-2">
	{#if holder !== undefined}
		{@const name = holder || m.placeholder_dash()}
		<div
			class="flex min-w-3 flex-1 items-center gap-0.5"
			title={`${m.segment_local_kom()}: ${name}`}
		>
			<dt class="sr-only">{m.segment_local_kom()}</dt>
			<Icon name="crown" size={12} />
			<dd class="w-0 flex-1 truncate">{name}</dd>
		</div>
	{/if}
	{#if riders !== undefined}
		{@const count = number.format(riders)}
		<div class="flex shrink-0 items-center gap-0.5" title={`${m.segment_riders()}: ${count}`}>
			<dt class="sr-only">{m.segment_riders()}</dt>
			<Icon name="friends" size={12} />
			<dd>{count}</dd>
		</div>
	{/if}
	{#if attempts !== undefined}
		{@const count = number.format(attempts)}
		<div class="flex shrink-0 items-center gap-0.5" title={`${attemptsLabel}: ${count}`}>
			<dt class="sr-only">{attemptsLabel}</dt>
			<Icon name="repeat" size={12} />
			<dd>{count}</dd>
		</div>
	{/if}
</dl>
