// Runs in the document head so a saved appearance is applied before first paint.
(() => {
	const key = 'peloton.appearance';
	const system = matchMedia('(prefers-color-scheme: dark)');
	const root = document.documentElement;
	const resolvePreference = (value) => (value === 'light' || value === 'dark' ? value : 'system');
	let preference = 'system';
	let storage;
	try {
		storage = window.localStorage;
		preference = resolvePreference(storage.getItem(key));
	} catch {
		// The System setting still works when browser storage is unavailable.
	}

	function apply() {
		const mode = preference === 'system' ? (system.matches ? 'dark' : 'light') : preference;
		root.dataset.theme = mode;
		root.dataset.appearance = preference;
		const icon = document.getElementById('app-icon');
		if (icon) icon.href = `${icon.href.split('#')[0]}#${mode}`;
		document.getElementById('app-color-scheme')?.setAttribute('content', mode);
		document
			.getElementById('app-theme-color')
			?.setAttribute('content', mode === 'dark' ? '#131e31' : '#fbfdff');
		root.dispatchEvent(new Event('peloton:appearancechange'));
	}

	window.addEventListener('peloton:set-appearance', (event) => {
		preference = resolvePreference(event.detail);
		try {
			storage?.setItem(key, preference);
		} catch {
			// Apply the choice for the current page even when it cannot be persisted.
		}
		apply();
	});
	system.addEventListener('change', () => {
		if (preference === 'system') apply();
	});
	window.addEventListener('storage', (event) => {
		if (event.storageArea !== storage || (event.key !== key && event.key !== null)) return;
		preference = resolvePreference(event.newValue);
		apply();
	});
	apply();
})();
