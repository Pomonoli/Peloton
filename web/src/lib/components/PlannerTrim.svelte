<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { intlLocale } from '#lib/format.js';

	let {
		length,
		from,
		to,
		onchange
	}: {
		length: number;
		from: number;
		to: number;
		onchange: (from: number, to: number) => void;
	} = $props();

	const gap = $derived(Math.min(25, length / 2));
	const kilometers = (meters: number) =>
		new Intl.NumberFormat(intlLocale(), {
			style: 'unit',
			unit: 'kilometer',
			minimumFractionDigits: 2,
			maximumFractionDigits: length < 1000 ? 3 : 2
		}).format(meters / 1000);

	function update(meters: number, isStart: boolean) {
		const value = isStart
			? Math.max(0, Math.min(meters, to - gap))
			: Math.min(length, Math.max(from + gap, meters));
		if (isStart) onchange(value, to);
		else onchange(from, value);
		return value;
	}

	function jump(event: PointerEvent) {
		event.preventDefault();
		const rail = event.currentTarget as HTMLButtonElement;
		const rect = rail.getBoundingClientRect();
		const meters = length * ((event.clientX - rect.left) / rect.width);
		const isStart = Math.abs(meters - from) < Math.abs(meters - to);
		update(meters, isStart);
		rail.parentElement?.querySelectorAll('input')[isStart ? 0 : 1]?.focus();
	}
</script>

<fieldset class="min-w-0">
	<legend class="text-sm font-semibold text-zinc-200">{m.planner_trim()}</legend>
	<div class="mt-2 flex justify-between gap-2 text-xs text-zinc-400">
		<label for="trim-start"
			>{m.planner_start()} <strong class="text-zinc-200">{kilometers(from)}</strong></label
		>
		<label for="trim-end"
			>{m.planner_finish()} <strong class="text-zinc-200">{kilometers(to)}</strong></label
		>
	</div>
	<div
		class="trim-control"
		style:--low={(100 * from) / length + '%'}
		style:--high={(100 * to) / length + '%'}
	>
		<button type="button" class="rail" tabindex="-1" aria-hidden="true" onpointerdown={jump}
		></button>
		<input
			id="trim-start"
			type="range"
			min="0"
			max={length}
			step="any"
			value={from}
			aria-label={m.planner_trim_start()}
			aria-valuetext={kilometers(from)}
			oninput={(event) => {
				event.currentTarget.valueAsNumber = update(event.currentTarget.valueAsNumber, true);
			}}
		/>
		<input
			id="trim-end"
			type="range"
			min="0"
			max={length}
			step="any"
			value={to}
			aria-label={m.planner_trim_end()}
			aria-valuetext={kilometers(to)}
			oninput={(event) => {
				event.currentTarget.valueAsNumber = update(event.currentTarget.valueAsNumber, false);
			}}
		/>
	</div>
</fieldset>

<style>
	.trim-control {
		position: relative;
		height: 2.75rem;
	}
	.rail {
		position: absolute;
		inset: 0 0.75rem;
		width: calc(100% - 1.5rem);
		cursor: pointer;
	}
	.rail::before {
		content: '';
		position: absolute;
		inset: calc(50% - 3px) 0 auto;
		height: 6px;
		border-radius: 99px;
		background: linear-gradient(
			to right,
			var(--surface-muted) var(--low),
			var(--accent-primary) var(--low) var(--high),
			var(--surface-muted) var(--high)
		);
	}
	input {
		appearance: none;
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		pointer-events: none;
		background: transparent;
		outline: none;
	}
	input:focus {
		z-index: 1;
	}
	input::-webkit-slider-thumb {
		appearance: none;
		width: 24px;
		height: 38px;
		background: radial-gradient(
			circle,
			var(--surface-card) 0 5px,
			var(--accent-primary) 5.5px 10px,
			transparent 10.5px
		);
		pointer-events: auto;
		cursor: ew-resize;
	}
	input::-moz-range-thumb {
		width: 24px;
		height: 38px;
		border: 0;
		background: radial-gradient(
			circle,
			var(--surface-card) 0 5px,
			var(--accent-primary) 5.5px 10px,
			transparent 10.5px
		);
		pointer-events: auto;
		cursor: ew-resize;
	}
	input:focus-visible::-webkit-slider-thumb {
		outline: 2px solid var(--accent-primary);
		border-radius: 6px;
	}
	input:focus-visible::-moz-range-thumb {
		outline: 2px solid var(--accent-primary);
		border-radius: 6px;
	}
</style>
