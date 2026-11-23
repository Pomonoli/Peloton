// Run npm run build:static and npx playwright-core install chromium first.
import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const origin = 'http://peloton-browser.test';
const build = fileURLToPath(new URL('../build/', import.meta.url));
const endpoint = '/api/activities/heatmap/performance';
const originalEndpoint = '/api/activities/heatmap';
const members = ['ada', 'mira', 'sam'].map((username) => ({
	id: username,
	username,
	display_name: username[0].toUpperCase() + username.slice(1),
	avatar_url: '',
	activity_count: 1,
	cycling_distance_m: 5000
}));
const basemap = {
	version: 8,
	sources: {},
	layers: [{ id: 'background', type: 'background', paint: { 'background-color': '#343434' } }]
};
const browser = await chromium.launch({
	executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE,
	args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader']
});

try {
	const page = await browser.newPage({ viewport: { width: 1280, height: 900 }, locale: 'en' });
	page.setDefaultTimeout(10_000);
	const calls = [];
	const errors = [];
	let cold = true;
	let stall = false;
	page.on('pageerror', (error) => errors.push(error.message));
	await page.route('**/*', async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname;
		if (url.hostname === 'tiles.openfreemap.org' || path === '/map-style.json') {
			return route.fulfill({ json: basemap });
		}
		if (url.origin !== origin) return route.abort();
		if (path === endpoint || path === originalEndpoint) {
			const member = url.searchParams.get('member');
			const viewport = url.searchParams.has('level');
			const from = url.searchParams.get('from');
			const count = member === 'mira' ? (from ? 24 : 25) : member === 'sam' ? 2 : 1;
			const status = (cold && member === 'mira') || stall ? 202 : 200;
			calls.push({ member, viewport, from, query: url.search, status });
			if (status === 202) {
				return route.fulfill({ status, json: { matched: count, with_track: 24, pending: 1 } });
			}
			const lon = member === 'mira' ? 8 : 4;
			return route.fulfill({
				json: {
					matched: count,
					with_track: count,
					facets: { types: [], years: [], disciplines: [] },
					geojson: {
						type: 'FeatureCollection',
						features: [
							{
								type: 'Feature',
								geometry: {
									type: 'LineString',
									coordinates: [
										[lon, 47],
										[lon + 0.05, 47]
									]
								},
								properties: { density: count, speed: 32 }
							}
						]
					}
				}
			});
		}
		if (path === '/api/overview') {
			return route.fulfill({
				json: {
					totals: { count: 0, distance_m: 0, moving_s: 0, elevation_gain_m: 0 },
					cycling_weeks: [],
					recent: []
				}
			});
		}
		if (path === '/api/riders') return route.fulfill({ json: { members } });
		if (path.startsWith('/api/members/')) {
			return route.fulfill({ json: members.find((member) => path.endsWith('/' + member.id)) });
		}
		if (path.startsWith('/api/profiles/')) {
			const user = members.find((member) => path.endsWith('/' + member.id));
			return route.fulfill({ json: { user, is_self: user.id === 'ada', activities: [] } });
		}
		if (path.endsWith('/GetCurrentUser')) {
			return route.fulfill({
				json: { user: { id: 'ada', username: 'ada', displayName: 'Ada', emailVerified: true } }
			});
		}
		if (path === '/cairn.v1.GearService/ListGear') return route.fulfill({ json: { gear: [] } });
		if (path.startsWith('/api/') || path.startsWith('/cairn.v1.')) {
			return route.fulfill({ json: {} });
		}
		return route.fulfill({
			path:
				route.request().resourceType() === 'document'
					? resolve(build, 'index.html')
					: resolve(build, '.' + path)
		});
	});

	const heatmap = () => page.getByRole('main').getByRole('link', { name: 'Heatmap', exact: true });
	const fit = page.getByRole('button', { name: 'Fit to tracks' });
	const loading = page.getByText('Loading heatmap…', { exact: true });
	const tracks = (count) => page.getByText(`${count} tracks from ${count}`, { exact: false });
	const nextViewport = () =>
		page.waitForResponse(
			(response) =>
				new URL(response.url()).pathname === endpoint &&
				new URL(response.url()).searchParams.has('level')
		);
	await page.goto(origin + '/u/mira');
	await heatmap().click();
	await page.waitForURL(origin + '/heatmap?member=mira');
	await page.getByText('Preparing ride history (1 remaining)…').waitFor();
	const firstViewport = nextViewport();
	cold = false;
	await firstViewport;
	await tracks(25).waitFor();
	await loading.waitFor({ state: 'hidden' });
	assert.ok(calls.some(({ status }) => status === 202));
	assert.deepEqual(
		calls.filter(({ status }) => status === 200).map(({ member, viewport }) => [member, viewport]),
		[
			['mira', false],
			['mira', true]
		]
	);

	const settled = calls.length;
	await fit.click();
	await fit.click();
	const layer = page.locator('select').filter({ has: page.locator('option[value="density"]') });
	await layer.selectOption('speed');
	await page.getByLabel('Heatmap legend').getByText('32 km/h').waitFor();
	await layer.selectOption('gradient');
	await delay(750);
	assert.equal(calls.length, settled, 'unchanged viewport and layers should reuse geometry');

	const [moved] = await Promise.all([
		nextViewport(),
		page.locator('.maplibregl-canvas').dblclick()
	]);
	assert.notEqual(new URL(moved.url()).search, calls[settled - 1].query);
	await loading.waitFor({ state: 'hidden' });

	await page.getByLabel('From date').fill('2020-03-03');
	await tracks(24).waitFor();
	await loading.waitFor({ state: 'hidden' });
	assert.deepEqual(
		calls.filter(({ from }) => from).map(({ member, viewport }) => [member, viewport]),
		[
			['mira', false],
			['mira', true]
		]
	);

	const fullscreen = page.getByRole('button', { name: 'Fullscreen', exact: true });
	await fullscreen.click();
	await page.getByRole('button', { name: 'Exit fullscreen' }).waitFor();
	await page.getByRole('button', { name: 'Exit fullscreen' }).click();
	await fullscreen.waitFor();
	assert.equal(await layer.inputValue(), 'gradient');

	const scale = page.locator('select').filter({ has: page.locator('option[value="fixed"]') });
	await scale.selectOption('fixed');
	assert.equal(await scale.inputValue(), 'fixed');
	await page.getByRole('button', { name: 'Light', exact: true }).click();
	assert.equal(
		await page.getByRole('button', { name: 'Light', exact: true }).getAttribute('aria-pressed'),
		'true'
	);
	await page.getByRole('button', { name: 'Dark', exact: true }).click();
	await layer.selectOption('density');
	const originalTracks = page.getByLabel('Show original tracks');
	await Promise.all([
		page.waitForResponse((response) => new URL(response.url()).pathname === originalEndpoint),
		originalTracks.check()
	]);
	await originalTracks.uncheck();

	await page.getByRole('link', { name: 'Switch member' }).click();
	await page.getByRole('link', { name: /Sam @sam/ }).click();
	await heatmap().click();
	await page.waitForURL(origin + '/heatmap?member=sam');
	await tracks(2).waitFor();
	assert.ok(calls.some(({ member, from }) => member === 'sam' && from === null));

	await page.setViewportSize({ width: 390, height: 760 });
	await page.addInitScript(() =>
		Object.defineProperty(HTMLElement.prototype, 'requestFullscreen', { value: undefined })
	);
	await page.goto(origin + '/heatmap?member=sam');
	await tracks(2).waitFor();
	const filterButton = page.getByRole('button', { name: 'Map filters', exact: true });
	assert.equal(await filterButton.getAttribute('aria-expanded'), 'false');
	await filterButton.click();
	const filterSheet = page.getByRole('dialog', { name: 'Map filters' });
	await fit.waitFor();
	await filterSheet.getByRole('button', { name: 'Done' }).click();
	await filterSheet.waitFor({ state: 'hidden' });
	await fullscreen.click();
	const exit = page.getByRole('button', { name: 'Exit fullscreen' });
	await exit.waitFor();
	assert.equal(
		await exit.evaluate((button) =>
			Math.round(button.parentElement.getBoundingClientRect().height)
		),
		760
	);
	await page.keyboard.press('Escape');
	await fullscreen.waitFor();
	await page.setViewportSize({ width: 1280, height: 900 });

	stall = true;
	await page.clock.install();
	await page.goto(origin + '/heatmap?member=mira');
	await page.getByText('Preparing ride history (1 remaining)…').waitFor();
	const stalled = page.getByText('Heatmap preparation stopped making progress.', { exact: false });
	for (let i = 0; i < 5 && !(await stalled.isVisible()); i++) {
		await page.clock.fastForward(i === 0 ? 31_000 : 1000);
		await delay(200);
	}
	await stalled.waitFor();
	const stopped = calls.length;
	await page.clock.fastForward(31_000);
	assert.equal(calls.length, stopped, 'stalled preparation should stop polling');
	assert.deepEqual(errors, []);
	console.log('Member heatmap browser regression passed');
} finally {
	await browser.close();
}
