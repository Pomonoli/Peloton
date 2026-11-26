<script lang="ts">
	import { onDestroy } from 'svelte';
	import { connectClients } from '#lib/connect.js';
	import { intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import { StreamChannel } from '#lib/proto/cairn/v1/activity_pb.js';
	import { streamToView } from '#lib/ride-stream.js';
	import { analyzeTrack, type RidePoint } from '#lib/ride-track.js';
	import { defaultResistance, estimatePower } from '#lib/estimated-power.js';
	import StreamChart from './StreamChart.svelte';

	let {
		sourceId,
		otherSourceIds,
		activityId,
		userId,
		hoverT,
		onHover
	}: {
		sourceId: string;
		otherSourceIds: string[];
		activityId: string;
		userId: string | null;
		hoverT: number | null;
		onHover: (t: number | null) => void;
	} = $props();

	type PowerInputs = { weight_kg: number | null; bicycle_weight_kg: number | null };

	let riderKg = $state<number | undefined>();
	let equipmentKg = $state<number | undefined>();
	let bicycleKg = $state<number>();
	let crr = $state(defaultResistance.crr);
	let cdaM2 = $state(defaultResistance.cdaM2);
	let wind = $state<number | undefined>();
	let track = $state.raw<RidePoint[] | null>(null);
	let loading = $state(false);
	let measured = $state(false);
	let failed = $state(false);
	let controller: AbortController | undefined;
	let result = $state<ReturnType<typeof estimatePower>>();
	const storageKey = $derived(userId ? `cairn.estimated-power.${userId}` : null);
	const overrideKey = $derived(storageKey ? `${storageKey}.${activityId}` : null);
	const offsets = $derived(track?.map((p) => p.t) ?? []);

	function rememberEquipment() {
		try {
			if (storageKey) {
				const saved = JSON.parse(localStorage.getItem(storageKey) || '{}');
				localStorage.setItem(
					storageKey,
					JSON.stringify({
						equipmentKg: bicycleKg == null ? equipmentKg : saved.equipmentKg,
						crr,
						cdaM2
					})
				);
			}
			if (overrideKey && bicycleKg != null) {
				if (equipmentKg === bicycleKg) localStorage.removeItem(overrideKey);
				else localStorage.setItem(overrideKey, String(equipmentKg));
			}
		} catch {
			// Calculations work without storage.
		}
	}

	async function open() {
		if (loading || track || measured) return;
		loading = true;
		failed = false;
		const request = new AbortController();
		controller = request;
		try {
			const settings = JSON.parse((storageKey && localStorage.getItem(storageKey)) || '{}');
			if (typeof settings.equipmentKg === 'number') equipmentKg = settings.equipmentKg;
			if (typeof settings.crr === 'number') crr = settings.crr;
			if (typeof settings.cdaM2 === 'number') cdaM2 = settings.cdaM2;
		} catch {
			// Malformed or disabled storage can be ignored.
		}
		const client = connectClients(fetch, location.origin).activity;
		const options = { signal: request.signal };
		const read = (id: string, channels?: StreamChannel[]) =>
			client.getActivityStream(
				{ activitySourceId: id, maxResolutionHz: 1, channels: channels ?? [] },
				options
			);
		const weight = fetch(`/api/activities/${activityId}/power-inputs`, options)
			.then(async (response) => (response.ok ? ((await response.json()) as PowerInputs) : null))
			.catch(() => null);
		try {
			const [primary, ...others] = await Promise.all([
				read(sourceId),
				...otherSourceIds.map((id) => read(id, [StreamChannel.POWER]))
			]);
			if (request.signal.aborted) return;
			measured = [primary, ...others].some(({ stream }) =>
				stream?.channels.includes(StreamChannel.POWER)
			);
			if (!measured) {
				const savedWeight = await weight;
				if (request.signal.aborted) return;
				if (typeof savedWeight?.weight_kg === 'number') riderKg = savedWeight.weight_kg;
				if (typeof savedWeight?.bicycle_weight_kg === 'number') {
					bicycleKg = savedWeight.bicycle_weight_kg;
					equipmentKg = bicycleKg;
					try {
						const override = Number(overrideKey && localStorage.getItem(overrideKey));
						if (override >= 2 && override <= 100) equipmentKg = override;
					} catch {
						// Storage is optional.
					}
				}
				track = analyzeTrack(streamToView(primary.stream)?.track ?? []);
			}
		} catch {
			if (!request.signal.aborted) failed = true;
		} finally {
			if (!request.signal.aborted) loading = false;
		}
	}

	function calculate(event: SubmitEvent) {
		event.preventDefault();
		if (!track || riderKg == null || equipmentKg == null) return;
		rememberEquipment();
		result = estimatePower(track, { riderKg, equipmentKg, crr, cdaM2, headwindMps: wind ?? 0 });
	}

	onDestroy(() => controller?.abort());
</script>

<details
	class="surface p-4"
	ontoggle={(event) => {
		if (event.currentTarget.open) void open();
	}}
>
	<summary class="cursor-pointer text-sm font-medium">{m.ride_estimated_power()}</summary>
	<div class="mt-3 space-y-3 text-sm">
		{#if measured}
			<p class="text-quiet">{m.ride_measured_power_available()}</p>
		{:else if failed}
			<p role="status" class="text-quiet">{m.ride_estimate_load_failed()}</p>
			<button class="min-h-10 text-accent-300 hover:underline" type="button" onclick={open}
				>{m.community_retry()}</button
			>
		{:else if loading || !track}
			<p role="status" class="text-quiet">{m.ride_estimate_loading()}</p>
		{:else}
			<p class="max-w-prose text-quiet">{m.ride_estimate_accuracy()}</p>
			<p class="max-w-prose text-quiet">{m.ride_estimate_defaults()}</p>
			{#if bicycleKg != null}
				<div class="flex flex-wrap items-center gap-x-3 text-xs text-quiet">
					{#if equipmentKg === bicycleKg}
						<span>{m.ride_assigned_bicycle({ weight: bicycleKg })}</span>
					{:else}
						<span>{m.ride_equipment_override()}</span>
						<button
							type="button"
							class="min-h-9 text-accent-300"
							onclick={() => {
								equipmentKg = bicycleKg;
								result = undefined;
								rememberEquipment();
							}}>{m.ride_reset_bicycle()}</button
						>
					{/if}
				</div>
			{/if}
			<form class="grid grid-cols-2 gap-3 sm:grid-cols-3" onsubmit={calculate}>
				<label class="flex flex-col gap-1">
					{m.ride_rider_mass()}
					<input type="number" min="20" max="300" step="any" required bind:value={riderKg} />
				</label>
				<label class="flex flex-col gap-1">
					{m.ride_equipment_mass()}
					<input type="number" min="2" max="100" step="any" required bind:value={equipmentKg} />
				</label>
				<label class="flex flex-col gap-1">
					{m.ride_rolling_resistance()}
					<input type="number" min="0.001" max="0.03" step="any" required bind:value={crr} />
				</label>
				<label class="flex flex-col gap-1">
					{m.ride_drag_area()}
					<input type="number" min="0.1" max="1.5" step="any" required bind:value={cdaM2} />
				</label>
				<label class="col-span-2 flex flex-col gap-1">
					{m.ride_headwind()}
					<input type="number" min="-25" max="25" step="any" bind:value={wind} />
				</label>
				<button
					type="submit"
					class="col-span-2 min-h-10 justify-self-start rounded-md bg-accent-500 px-4 font-medium text-on-accent sm:col-span-3"
				>
					{m.ride_calculate_estimate()}
				</button>
			</form>
			{#if result}
				<div class="flex flex-wrap items-baseline gap-3">
					<p class="font-medium">{m.ride_estimated_average({ watts: result.averageW })}</p>
					<p class="text-xs text-quiet">
						{m.ride_estimate_coverage({
							share: new Intl.NumberFormat(intlLocale(), { style: 'percent' }).format(
								result.coverage
							)
						})}
					</p>
				</div>
				<StreamChart
					{offsets}
					series={[
						{
							label: m.ride_estimated_power(),
							values: result.values,
							color: 'var(--plot-power)',
							unit: 'W'
						}
					]}
					{hoverT}
					{onHover}
				/>
			{:else if result === null}
				<p role="status" class="text-quiet">{m.ride_estimate_insufficient()}</p>
			{/if}
		{/if}
	</div>
</details>

<style>
	input {
		min-height: 2.5rem;
		width: 100%;
		border: 1px solid var(--line-strong);
		border-radius: 0.375rem;
		padding: 0 0.5rem;
		background: var(--surface-panel);
	}
</style>
