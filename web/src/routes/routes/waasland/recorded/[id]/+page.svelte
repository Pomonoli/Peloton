<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import RecordedPlannerRoute from '#lib/components/RecordedPlannerRoute.svelte';

	let { data } = $props();
</script>

<svelte:head><title>{m.nav_route_planner()}</title></svelte:head>

{#key data.routeId}
	<RecordedPlannerRoute id={data.routeId} />
{/key}
