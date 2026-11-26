<script lang="ts" generics="Key extends string">
	let {
		tabs,
		active,
		id,
		label,
		onchange
	}: {
		tabs: readonly { key: Key; label: string }[];
		active: Key;
		id: string;
		label: string;
		onchange: (key: Key) => void;
	} = $props();

	function move(event: KeyboardEvent, index: number) {
		let target: number;
		switch (event.key) {
			case 'ArrowLeft':
				target = (index + tabs.length - 1) % tabs.length;
				break;
			case 'ArrowRight':
				target = (index + 1) % tabs.length;
				break;
			case 'Home':
				target = 0;
				break;
			case 'End':
				target = tabs.length - 1;
				break;
			default:
				return;
		}
		event.preventDefault();
		const key = tabs[target]?.key;
		if (key === undefined) return;
		onchange(key);
		document.getElementById(`${id}-tab-${key}`)?.focus();
	}
</script>

<div role="tablist" aria-label={label} class="section-tabs">
	{#each tabs as item, index (item.key)}
		<button
			type="button"
			role="tab"
			id={`${id}-tab-${item.key}`}
			class="tab-item"
			aria-selected={active === item.key}
			aria-controls={`${id}-panel-${item.key}`}
			tabindex={active === item.key ? 0 : -1}
			onclick={() => onchange(item.key)}
			onkeydown={(event) => move(event, index)}>{item.label}</button
		>
	{/each}
</div>
