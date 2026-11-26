import { runInNewContext } from 'node:vm';
import { afterEach, describe, expect, it, vi } from 'vitest';
import bootstrap from '../../static/appearance.js?raw';
import {
	appearance,
	appearanceColor,
	setAppearance,
	type AppearancePreference
} from './appearance';

const key = 'peloton.appearance';

function start({ dark = false, saved = new Map<string, string>(), blocked = false } = {}) {
	const media = Object.assign(new EventTarget(), { matches: dark });
	const root = Object.assign(new EventTarget(), { dataset: {} as DOMStringMap });
	const icon = { href: 'http://localhost/favicon.svg' };
	const colorScheme = { setAttribute: vi.fn() };
	const themeColor = { setAttribute: vi.fn() };
	const elements = new Map<string, typeof icon | typeof colorScheme>([
		['app-icon', icon],
		['app-color-scheme', colorScheme],
		['app-theme-color', themeColor]
	]);
	const document = {
		documentElement: root,
		getElementById: (id: string) => elements.get(id) ?? null
	};
	const localStorage = {
		getItem: (name: string) => saved.get(name) ?? null,
		setItem: (name: string, value: string) => saved.set(name, value)
	};
	const window = new EventTarget();
	Object.defineProperty(window, 'localStorage', {
		get: () => {
			if (blocked) throw new Error('storage blocked');
			return localStorage;
		}
	});
	runInNewContext(bootstrap, { window, document, matchMedia: () => media, Event });
	vi.stubGlobal('window', window);
	vi.stubGlobal('document', document);
	return {
		root,
		icon,
		colorScheme,
		themeColor,
		saved,
		changeSystem(matches: boolean) {
			media.matches = matches;
			media.dispatchEvent(new Event('change'));
		},
		changeStoredPreference(value: string) {
			window.dispatchEvent(
				Object.assign(new Event('storage'), { storageArea: localStorage, key, newValue: value })
			);
		}
	};
}

afterEach(() => vi.unstubAllGlobals());

describe('appearance', () => {
	it('defaults to System and follows changes to the device color scheme', () => {
		const app = start({ dark: true });
		expect(app.root.dataset).toEqual({ appearance: 'system', theme: 'dark' });
		expect(app.themeColor.setAttribute).toHaveBeenLastCalledWith('content', '#131e31');
		app.changeSystem(false);
		expect(app.root.dataset.theme).toBe('light');
		expect(app.icon.href).toBe('http://localhost/favicon.svg#light');
		expect(app.colorScheme.setAttribute).toHaveBeenLastCalledWith('content', 'light');
		expect(app.themeColor.setAttribute).toHaveBeenLastCalledWith('content', '#fbfdff');
	});

	it.each(['light', 'dark'] satisfies AppearancePreference[])(
		'saves an explicit %s choice and uses it on reload',
		(choice) => {
			const app = start({ dark: choice === 'light' });
			setAppearance(choice);
			expect(app.saved.get(key)).toBe(choice);
			app.changeSystem(choice === 'light');
			expect(app.root.dataset.theme).toBe(choice);
			const reloaded = start({ dark: choice === 'light', saved: app.saved });
			expect(reloaded.root.dataset).toEqual({ appearance: choice, theme: choice });
		}
	);

	it('updates subscribers and resumes following System after a change in another tab', () => {
		const app = start({ dark: true, saved: new Map([[key, 'light']]) });
		const subscriber = vi.fn();
		const unsubscribe = appearance.subscribe(subscriber);
		expect(subscriber).toHaveBeenLastCalledWith({ preference: 'light', mode: 'light' });
		app.changeStoredPreference('system');
		expect(subscriber).toHaveBeenLastCalledWith({ preference: 'system', mode: 'dark' });
		unsubscribe();
	});

	it('accepts a choice for the current page when storage is blocked', () => {
		const app = start({ blocked: true });
		setAppearance('dark');
		expect(app.root.dataset.theme).toBe('dark');
		expect(app.saved.size).toBe(0);
	});

	it('reads literal colors for canvas and WebGL', () => {
		start();
		const getPropertyValue = vi.fn(() => ' #4263eb ');
		vi.stubGlobal('getComputedStyle', () => ({ getPropertyValue }));
		expect(appearanceColor('--accent-primary')).toBe('#4263eb');
		expect(getPropertyValue).toHaveBeenCalledWith('--accent-primary');
	});
});
