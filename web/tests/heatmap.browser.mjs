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
const territoryEndpoint = '/api/activities/heatmap/territory';
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
	args: ['--enable-unsafe-swiftshader']
});

try {
	const page = await browser.newPage({ viewport: { width: 1280, height: 900 }, locale: 'en' });
	page.setDefaultTimeout(15_000);
	const calls = [];
	const discoveries = [];
	const territoryCalls = [];
	let territoryPending = true;
	let territoryOwner = members[1];
	const errors = [];
	let cold = true;
	let stall = false;
	let regionalFixture = false;
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
			const all = url.searchParams.get('riders') === 'all';
			const viewport = url.searchParams.has('level');
			const from = url.searchParams.get('from');
			const count = all ? 3 : member === 'mira' ? (from ? 24 : 25) : member === 'sam' ? 2 : 1;
			const status = (cold && member === 'mira') || stall ? 202 : 200;
			calls.push({ member, viewport, from, query: url.search, status });
			if (status === 202) {
				return route.fulfill({ status, json: { matched: count, with_track: 24, pending: 1 } });
			}
			if (regionalFixture) {
				const regions =
					member !== 'mira'
						? [
								[-4, 0],
								[16, 1000]
							]
						: [[-4, 100]];
				const features = regions.flatMap(([longitude, altitude]) =>
					Array.from({ length: 31 }, (_, i) => ({
						type: 'Feature',
						geometry: {
							type: 'LineString',
							coordinates: [
								[longitude, 47 + i * 0.00015],
								[longitude + 0.01, 47 + i * 0.00015]
							]
						},
						properties: {
							cell: '2:0:10:10',
							density: count,
							speed: 20 + i,
							elevation: altitude + (from ? 100 : 0) + i * (altitude === 1000 ? 25 : 1),
							cadence: 60 + i
						}
					}))
				);
				return route.fulfill({
					json: {
						matched: count,
						with_track: count,
						facets: { types: [], years: [], disciplines: [] },
						geojson: { type: 'FeatureCollection', features }
					}
				});
			}
			const lon = all || member === 'mira' ? 8 : 4;
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
								properties: { cell: '2:0:10:10', density: all ? 6 : count, speed: 32 }
							}
						]
					}
				}
			});
		}
		if (path === territoryEndpoint) {
			const version = territoryOwner.id;
			const other = members.find((member) => member.id !== version);
			const tag = route.request().headers()['if-none-match'];
			territoryCalls.push({ query: url.searchParams, tag });
			if (url.searchParams.has('lon')) {
				return route.fulfill({
					json: {
						owner_id: version,
						distances: [
							{ rider_id: version, distance_m: 1250 },
							{ rider_id: other.id, distance_m: 850 }
						]
					}
				});
			}
			if (territoryPending) {
				territoryPending = false;
				return route.fulfill({ status: 202, json: { pending: 2 } });
			}
			if (tag === '"' + version + '"') return route.fulfill({ status: 304 });
			const colors = { ada: '#128753', mira: '#b13e3e', sam: '#2473b0' };
			return route.fulfill({
				json: {
					version,
					legend: members.map((rider) => ({
						id: rider.id,
						name: rider.display_name,
						color: colors[rider.id],
						cells: rider.id === version ? 2 : 0,
						area_km2: rider.id === version ? 1.48 : 0,
						percentage: rider.id === version ? 100 : 0
					})),
					geojson: {
						type: 'FeatureCollection',
						features: [
							{
								type: 'Feature',
								properties: { rider_id: version, color: colors[version] },
								geometry: {
									type: 'MultiPolygon',
									coordinates: [
										[
											[
												[8, 46.99],
												[8.04, 46.99],
												[8.04, 47.01],
												[8, 47.01],
												[8, 46.99]
											]
										]
									]
								}
							}
						]
					}
				}
			});
		}
		if (path === '/api/activities/heatmap/discover') {
			const q = url.searchParams;
			discoveries.push(q);
			const riders = [
				{ ...members[1], passages: 5, activities: 2 },
				{ ...members[2], passages: 1, activities: 1 }
			];
			const rows = [
				['repeat', 'Repeated crossing', 'mira'],
				['morning', 'Morning ride', 'mira'],
				['evening', 'Evening ride', 'sam']
			].filter(([, , rider]) => !q.get('rider') || rider === q.get('rider'));
			return route.fulfill({
				json: {
					riders,
					passages: 6,
					matched: rows.length,
					has_more: false,
					activities: rows.map(([id, title, rider_id]) => ({
						id,
						title,
						rider_id,
						type: 'ride',
						discipline: 'road',
						start_time: '2020-04-01T12:00:00Z',
						timezone: 'Europe/Brussels',
						distance_m: 35000,
						moving_duration_s: 4400,
						elapsed_duration_s: 5000,
						elevation_gain_m: 150,
						avg_speed_mps: 7
					}))
				}
			});
		}
		if (path === '/api/activities/repeat/heatmap-track') {
			return route.fulfill({
				json: {
					lines: [
						[
							[8, 47],
							[8.025, 47],
							[8.04, 47.01]
						]
					]
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

	const heatmap = () => page.getByRole('link', { name: 'Heatmap', exact: true });
	const fit = page.getByRole('button', { name: 'Fit to tracks' });
	const loading = page.getByText('Loading heatmap…', { exact: true });
	const tracks = (count) => page.getByText(`${count} tracks from ${count}`, { exact: false });
	const nextViewport = () =>
		page.waitForResponse(
			(response) =>
				new URL(response.url()).pathname === endpoint &&
				new URL(response.url()).searchParams.has('level')
		);
	await page.goto(origin + '/u/mira', { waitUntil: 'commit' });
	await page.waitForURL(origin + '/?member=mira');
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
	await page
		.getByText('Hover over a line to inspect it, or select it to discover activities.')
		.waitFor();
	await page.getByRole('button', { name: 'Exit fullscreen' }).click();
	await fullscreen.waitFor();
	assert.equal(await layer.inputValue(), 'gradient');

	const scale = page.locator('select').filter({ has: page.locator('option[value="fixed"]') });
	assert.equal(await scale.count(), 0);
	await layer.selectOption('density');
	await scale.selectOption('fixed');
	assert.equal(await scale.inputValue(), 'fixed');
	await scale.selectOption('auto');
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

	await page.getByRole('button', { name: 'Light', exact: true }).click();
	const riderFilter = page.getByRole('combobox', { name: 'Rider', exact: true });
	await riderFilter.selectOption('*');
	await tracks(3).waitFor();
	await loading.waitFor({ state: 'hidden' });
	await fit.click();
	for (
		let attempt = 0;
		attempt < 10 && !(await page.getByText('3 unique activities').isVisible());
		attempt++
	) {
		await page.locator('.maplibregl-canvas').click();
		await delay(180);
	}
	await page.getByRole('heading', { name: 'Passages: 6' }).waitFor();
	await page.getByText('3 unique activities').waitFor();
	assert.equal(discoveries.at(-1).get('riders'), 'all');
	assert.equal(discoveries.at(-1).get('cell'), '2:0:10:10');
	assert.equal(discoveries.at(-1).get('from'), '2020-03-03');
	assert.equal(
		await page.getByRole('link', { name: 'Repeated crossing', exact: false }).count(),
		1
	);
	const breakdown = page.getByRole('group', { name: 'Passages by rider' });
	await breakdown.getByRole('button', { name: 'Mira 5' }).click();
	await page.getByText('2 unique activities', { exact: true }).waitFor();
	assert.equal(discoveries.at(-1).get('rider'), 'mira');
	assert.equal(await page.getByRole('link', { name: /Evening ride/ }).count(), 0);
	const selected = page.getByRole('button', { name: 'Highlight track for Repeated crossing' });
	await Promise.all([
		page.waitForResponse(
			(response) => new URL(response.url()).pathname === '/api/activities/repeat/heatmap-track'
		),
		selected.click()
	]);
	assert.equal(await selected.getAttribute('aria-pressed'), 'true');
	assert.equal(
		await page.getByRole('link', { name: /Repeated crossing/ }).getAttribute('href'),
		'/activities/repeat'
	);
	await page.getByRole('button', { name: 'Clear location' }).click();
	await breakdown.waitFor({ state: 'hidden' });

	await page.getByRole('link', { name: 'Riders', exact: true }).click();
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
	await layer.waitFor();
	await filterButton.click();
	const filterSheet = page.getByRole('dialog', { name: 'Map filters' });
	await fit.waitFor();
	await filterSheet.getByRole('button', { name: 'Done' }).click();
	await filterSheet.waitFor({ state: 'hidden' });
	await riderFilter.selectOption('*');
	await tracks(3).waitFor();
	await filterButton.click();
	await page.getByRole('button', { name: 'Dark', exact: true }).click();
	await fit.click();
	await filterSheet.getByRole('button', { name: 'Done' }).click();
	await filterSheet.waitFor({ state: 'hidden' });
	const mobileMap = page.locator('.maplibregl-canvas');
	const mobileCounts = page.getByRole('heading', { name: 'Passages: 6' });
	for (let attempt = 0; attempt < 10 && !(await mobileCounts.isVisible()); attempt++) {
		await mobileMap.click();
		await delay(180);
	}
	await mobileCounts.waitFor();
	const mapBox = await mobileMap.boundingBox();
	const panelBox = await mobileCounts.boundingBox();
	assert.ok(mapBox.y + mapBox.height <= panelBox.y, 'Activities must follow the map on mobile');
	assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 390);
	await page.getByRole('button', { name: 'Clear location' }).click();
	await fullscreen.click();
	const exit = page.getByRole('button', { name: 'Exit fullscreen' });
	await exit.waitFor();
	await page
		.getByText('Hover over a line to inspect it, or select it to discover activities.')
		.waitFor();
	assert.equal(
		await exit.evaluate((button) =>
			Math.round(button.parentElement.getBoundingClientRect().height)
		),
		760
	);
	await page.keyboard.press('Escape');
	await fullscreen.waitFor();
	await page.setViewportSize({ width: 1280, height: 900 });

	await layer.focus();
	await layer.selectOption('territory');
	await page.getByText('All riders · Rolling 90 days').waitFor();
	await page.getByText('Preparing ride history (2 remaining)…').waitFor();
	const legend = page.getByRole('table', { name: 'Heatmap legend' });
	await legend.getByRole('row', { name: 'Mira 2 1.48 100%' }).waitFor();
	assert.equal(await layer.evaluate((select) => select === document.activeElement), true);
	assert.equal(await riderFilter.count(), 0);
	assert.ok(territoryCalls.every(({ query }) => !query.size));
	const canvas = page.locator('.maplibregl-canvas');
	await canvas.hover();
	await page.waitForFunction(
		() => document.querySelector('.maplibregl-canvas').style.cursor === 'pointer'
	);
	await canvas.click();
	const owner = page.getByTestId('territory-owner');
	await owner.getByText('Owned by Mira').waitFor();
	await page.getByText('1.25 km', { exact: true }).waitFor();
	await page.getByText('850 m', { exact: true }).waitFor();
	assert.ok(Math.abs(Number(territoryCalls.at(-1).query.get('lon')) - 8.02) < 0.001);

	await page.getByRole('button', { name: 'Light', exact: true }).click();
	await page.getByRole('button', { name: 'Fit territories' }).click();
	await page.setViewportSize({ width: 390, height: 760 });
	await legend.waitFor();
	assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 390);
	assert.equal(await filterButton.count(), 0);
	// Returning to the tab revalidates both the map and the selected hexagon.
	territoryOwner = members[2];
	await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
	await legend.getByRole('row', { name: 'Sam 2 1.48 100%' }).waitFor();
	await owner.getByText('Owned by Sam').waitFor();
	assert.ok(territoryCalls.some(({ tag }) => tag === '"mira"'));
	await layer.selectOption('heart_rate');
	await tracks(3).waitFor();
	assert.equal(await legend.count(), 0);
	await filterButton.waitFor();
	await page.setViewportSize({ width: 1280, height: 900 });

	regionalFixture = true;
	await page.goto(origin + '/heatmap');
	await riderFilter.selectOption('*');
	await tracks(3).waitFor();
	await layer.selectOption('elevation');
	const numericalLegend = page.getByRole('group', { name: 'Heatmap legend' });
	const elevation = (value) => numericalLegend.getByText(value + ' m', { exact: true });
	await elevation('1,700').waitFor();
	const wideCalls = calls.length;
	const box = await canvas.boundingBox();
	assert.ok(box);
	const mapY = box.y + box.height / 2;
	await page.mouse.dblclick(box.x + 41, mapY);
	await elevation('29').waitFor();
	assert.equal(await elevation('1,700').count(), 0);
	assert.equal(calls.length, wideCalls, 'zoom within the overview should reuse geometry');

	await page.mouse.move(box.x + box.width / 2, mapY);
	await page.mouse.down();
	await page.mouse.move(box.x + box.width / 4, mapY, { steps: 12 });
	await page.mouse.up();
	await page.getByText('No tracks in this view', { exact: false }).waitFor();
	await numericalLegend.getByText('Insufficient data').waitFor();
	assert.equal(await elevation('29').count(), 0);

	await fit.click();
	await elevation('1,700').waitFor();
	await page.mouse.dblclick(box.x + box.width - 43, mapY);
	await elevation('1,025').waitFor();
	assert.equal(await elevation('29').count(), 0);
	const detailed = nextViewport();
	for (let zoom = 0; zoom < 4 && calls.length === wideCalls; zoom++) {
		await page.mouse.dblclick(box.x + box.width - 43, mapY);
		await Promise.race([detailed, delay(600)]);
	}
	await detailed;
	await elevation('1,725').waitFor();

	await page.getByLabel('From date').fill('2020-03-03');
	await elevation('1,125').waitFor();
	await riderFilter.selectOption('mira');
	await tracks(24).waitFor();
	await fit.click();
	await elevation('229').waitFor();
	await layer.selectOption('cadence');
	await numericalLegend.getByText('89 rpm', { exact: true }).waitFor();

	await page.setViewportSize({ width: 390, height: 760 });
	await fullscreen.click();
	await page.getByRole('button', { name: 'Exit fullscreen' }).waitFor();
	const cadence = numericalLegend.getByText('89 rpm', { exact: true });
	await cadence.waitFor();
	assert.ok(await cadence.evaluate((tick) => tick.closest('.fixed.inset-0')));
	await page.keyboard.press('Escape');
	await fullscreen.waitFor();
	await cadence.waitFor();
	await page.setViewportSize({ width: 1280, height: 900 });
	regionalFixture = false;

	stall = true;
	await page.addInitScript(() => {
		const now = performance.now.bind(performance);
		window.heatmapTestTimeOffset = 0;
		Object.defineProperty(performance, 'now', {
			value: () => now() + window.heatmapTestTimeOffset
		});
	});
	await page.goto(origin + '/heatmap?member=mira');
	await page.getByText('Preparing ride history (1 remaining)…').waitFor();
	const stalled = page.getByText('Heatmap preparation stopped making progress.', { exact: false });
	await page.evaluate(() => (window.heatmapTestTimeOffset = 31_000));
	await stalled.waitFor();
	const stopped = calls.length;
	await delay(1000);
	assert.equal(calls.length, stopped, 'stalled preparation should stop polling');
	assert.deepEqual(errors, []);
	console.log('Member heatmap and Territory browser regression passed');
} finally {
	await browser.close();
}
