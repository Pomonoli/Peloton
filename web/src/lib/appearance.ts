import { readable } from 'svelte/store';

export type ColorMode = 'light' | 'dark';
export type AppearancePreference = 'system' | ColorMode;

export const appearance = readable<{ preference: AppearancePreference; mode: ColorMode }>(
	{ preference: 'system', mode: 'light' },
	(set) => {
		if (typeof document === 'undefined') return;
		const root = document.documentElement;
		function sync() {
			const { appearance: preference, theme } = root.dataset;
			set({
				preference: preference === 'light' || preference === 'dark' ? preference : 'system',
				mode: theme === 'dark' ? 'dark' : 'light'
			});
		}
		sync();
		root.addEventListener('peloton:appearancechange', sync);
		return () => root.removeEventListener('peloton:appearancechange', sync);
	}
);

export function setAppearance(preference: AppearancePreference) {
	window.dispatchEvent(new CustomEvent('peloton:set-appearance', { detail: preference }));
}

// Canvas and WebGL need resolved CSS colors rather than custom property names.
export function appearanceColor(property: string): string {
	return getComputedStyle(document.documentElement).getPropertyValue(property).trim();
}
