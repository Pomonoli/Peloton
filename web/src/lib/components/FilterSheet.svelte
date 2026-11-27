<script lang="ts">
	import { afterNavigate } from '$app/navigation';
	import { MediaQuery } from 'svelte/reactivity';
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';
	import { m } from '#lib/paraglide/messages.js';

	let {
		children,
		toolbar,
		belowToolbar,
		label = m.mobile_filters(),
		active = false,
		collapsible = false,
		reset
	}: {
		children?: Snippet | undefined;
		toolbar?: Snippet;
		belowToolbar?: Snippet | undefined;
		label?: string;
		active?: boolean;
		collapsible?: boolean;
		reset?: () => void;
	} = $props();

	const mobile = new MediaQuery('width < 48rem');
	const id = $props.id();
	let dialog = $state<HTMLDialogElement>();
	let open = $state(false);

	$effect(() => {
		if (!mobile.current) open = false;
	});

	afterNavigate(({ from, to }) => {
		if (from?.url.pathname !== to?.url.pathname) dialog?.close();
	});
</script>

{#if toolbar || (children && mobile.current)}
	<div class="flex min-w-0 items-start justify-between gap-2">
		{#if toolbar}
			<div class="min-w-0 flex-1">{@render toolbar()}</div>
		{/if}
		{#if children && mobile.current}
			<button
				type="button"
				class="action ml-auto shrink-0 data-[active=true]:border-accent-500/40 data-[active=true]:text-accent-300"
				data-active={active}
				aria-label={active ? label + ' (' + m.mobile_filters_active() + ')' : label}
				aria-haspopup="dialog"
				aria-expanded={open}
				aria-controls={id}
				onclick={() => {
					dialog?.showModal();
					open = !!dialog?.open;
				}}
			>
				<Icon name="filter" size={16} />
				{label}
				{#if active}<span class="size-1.5 rounded-full bg-accent-500" aria-hidden="true"
					></span>{/if}
			</button>
		{/if}
	</div>
{/if}

{@render belowToolbar?.()}

{#if children && mobile.current}
	<dialog
		class="filter-sheet"
		{id}
		bind:this={dialog}
		aria-labelledby={id + '-title'}
		onclose={() => (open = false)}
	>
		<div class="sheet-layout grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto]">
			<header class="flex items-center justify-between gap-3 border-b border-zinc-800 px-4 py-1.5">
				<h2 id={id + '-title'} class="font-semibold">{label}</h2>
				<button
					type="button"
					aria-label={m.mobile_close()}
					class="grid size-11 place-items-center rounded-lg text-zinc-400 hover:text-zinc-100"
					onclick={() => dialog?.close()}
				>
					<svg
						class="size-5"
						aria-hidden="true"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.8"
						stroke-linecap="round"><path d="m6 6 12 12M18 6 6 18" /></svg
					>
				</button>
			</header>
			<div class="min-h-0 overflow-y-auto overscroll-contain p-3.5">
				{@render children()}
			</div>
			<footer class="sheet-footer flex gap-2 border-t border-zinc-800 px-4 pt-2.5">
				{#if reset}
					<button type="button" class="action flex-1" onclick={reset}
						>{m.mobile_filters_reset()}</button
					>
				{/if}
				<button type="button" class="action action-primary flex-1" onclick={() => dialog?.close()}
					>{m.mobile_filters_done()}</button
				>
			</footer>
		</div>
	</dialog>
{:else if children && collapsible}
	<details open class="mt-2">
		<summary class="mb-2 w-fit cursor-pointer text-sm font-medium text-accent-300">{label}</summary>
		{@render children()}
	</details>
{:else if children}
	<div class:mt-2={!!toolbar}>
		{@render children()}
	</div>
{/if}

<style>
	dialog {
		position: fixed;
		inset: auto 0 0;
		margin: 0;
		width: 100%;
		max-width: none;
		max-height: none;
		overflow: hidden;
		border: 1px solid var(--surface-muted);
		border-bottom: 0;
		border-radius: 1.25rem 1.25rem 0 0;
		background: var(--surface-panel);
		color: var(--text-strong);
	}
	dialog::backdrop {
		background: rgb(8 16 36 / 0.56);
	}
	.sheet-layout {
		max-height: min(80dvh, 44rem);
	}
	.sheet-footer {
		padding-bottom: calc(0.625rem + env(safe-area-inset-bottom, 0px));
	}
	:global(html:has(.filter-sheet[open])) {
		overflow: hidden;
	}

	@media (prefers-reduced-motion: no-preference) {
		dialog[open] {
			animation: reveal 140ms ease-out;
		}
		@keyframes reveal {
			from {
				opacity: 0.65;
			}
			to {
				opacity: 1;
			}
		}
	}
</style>
