<script lang="ts">
	import { icons, type IconName } from '#lib/icons.js';

	let {
		name,
		size = 20,
		class: className = ''
	}: { name: IconName; size?: number; class?: string } = $props();
</script>

<svg
	aria-hidden="true"
	class="shrink-0 {className}"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="1.8"
	stroke-linecap="round"
	stroke-linejoin="round"><path d={icons[name]} /></svg
>
