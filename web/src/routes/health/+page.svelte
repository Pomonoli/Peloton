<script lang="ts">
	import { APP_NAME } from '$app/env/public';
	import AthleteSettings from '#lib/components/AthleteSettings.svelte';
	import { m } from '#lib/paraglide/messages.js';
</script>

<svelte:head><title>{m.health_athlete_settings()} · {APP_NAME}</title></svelte:head>

<AthleteSettings />
