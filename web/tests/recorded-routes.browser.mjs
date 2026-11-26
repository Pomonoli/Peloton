// Run against npm run build:static with fixture responses.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';
import { WaasRouteMetrics } from '../static/waas-engine/route-metrics.js';
import { chromium } from 'playwright-core';

const originalGPX =
	'<gpx version="1.1"><trk><trkseg><trkpt lat="51" lon="4"/></trkseg></trk></gpx>';
const basemap = {
	version: 8,
	sources: {
		openmaptiles: {
			type: 'geojson',
			data: { type: 'FeatureCollection', features: [] },
			attribution:
				'<a href="https://openfreemap.org">OpenFreeMap</a> <a href="https://www.openmaptiles.org/">&copy; OpenMapTiles</a> Data from <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>'
		}
	},
	layers: [{ id: 'basemap', type: 'fill', source: 'openmaptiles' }]
};
const tile = Buffer.from(
	'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4//8/AAX+Av4N70a4AAAAAElFTkSuQmCC',
	'base64'
);
const server = createServer((req, res) => {
	if (req.url !== '/api/routes/waasland/gpx') {
		res.writeHead(404).end();
		return;
	}
	res
		.writeHead(200, {
			'Content-Type': 'application/gpx+xml',
			'Content-Disposition': 'attachment; filename="waasland.gpx"'
		})
		.end(originalGPX);
});
await new Promise((done) => server.listen(0, '127.0.0.1', done));
const origin = 'http://127.0.0.1:' + server.address().port;
const build = fileURLToPath(new URL('../build/', import.meta.url));
const browser = await chromium.launch({
	executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE,
	args: ['--enable-unsafe-swiftshader']
});
const riders = [
	{ id: 'a', username: 'ada', display_name: 'Ada', avatar_url: '', rides: 2, best_speed_kmh: 34.2 },
	{
		id: 'b',
		username: 'mira',
		display_name: 'Mira',
		avatar_url: '',
		rides: 1,
		best_speed_kmh: 39.1
	}
];
const track = [
	[
		[51, 4, 20, 0],
		[51.01, 4.01, 33, 800],
		[51.03, 4.07, 17, 3800],
		[51, 4, 20, 7200]
	].map(([lat, lon, elevation_m, distance_m]) => ({ lat, lon, elevation_m, distance_m }))
];
const loop = {
	id: 'waasland',
	name: 'Waasland Loop',
	type: 'loop',
	distance_m: 7200,
	elevation_gain_m: 92,
	rides: 3,
	rider_count: 2,
	best_speed_kmh: 39.1,
	riders,
	track,
	recent: [
		{
			id: 'latest',
			title: 'Morning ride',
			rider_id: 'b',
			start_time: '2025-01-03T17:30:00Z',
			distance_m: 7210,
			speed_kmh: 39.1
		}
	]
};
const canal = {
	...loop,
	id: 'canal',
	name: 'Canal Ride',
	type: 'point_to_point',
	riders: riders.slice(0, 1),
	track: [
		[
			[51, 4, 20, 0],
			[51.004, 4.005, 30, 800],
			[51.008, 4.015, 40, 1800]
		],
		[
			[51.02, 4.025, 80, 3500],
			[51.026, 4.03, 70, 4900],
			[51.03, 4.045, null, 7200]
		]
	].map((line) =>
		line.map(([lat, lon, elevation_m, distance_m]) => ({ lat, lon, elevation_m, distance_m }))
	)
};

function planningLoop(km, bearings) {
	const points = Array.from({ length: 361 }, (_, i) => {
		const angle = (i * Math.PI) / 180 + bearings;
		const radius = km / (Math.PI * 2);
		return [
			51.164 + (radius * Math.sin(angle)) / 111.2,
			4.139 + (radius * Math.cos(angle)) / 69.7,
			35 + 17 * Math.sin(angle * 4) + 10 * Math.sin(angle)
		];
	});
	const measured = WaasRouteMetrics.measure(points);
	return {
		...measured,
		from: 'sn',
		to: 'sn',
		climbs: [],
		blocked: [],
		rough: 0,
		catalogRoute: true
	};
}
const catalogs = new Map([
	['route-spines.js', 'window.WAAS_SPINES = {};'],
	[
		'terrain-data.js',
		'window.WAAS_DATA = {"routes":{},"circuits":[],"bounds":[[51,4],[51.3,4.3]]};'
	],
	[
		'circuits-climb-sn.js',
		'window.WAAS_CIRCUITS["climb-sn"] = ' +
			JSON.stringify({
				seventy: planningLoop(70, 0),
				seventyFive: planningLoop(75, Math.PI / 5)
			}) +
			';'
	],
	['focus-climb-sn.bin', gzipSync(JSON.stringify({ routes: [] }))]
]);

async function metrics(main) {
	const values = await main
		.getByRole('region', { name: 'Route summary' })
		.locator('dd')
		.allTextContents();
	return values.map((value) => Number.parseFloat(value));
}

async function pressRange(slider, key, count = 1) {
	for (let i = 0; i < count; i++) await slider.press(key);
}

async function checkMapLegend(main) {
	const legend = main.locator('.map-legend');
	await legend.waitFor();
	const gap = await legend.evaluate(
		(element) =>
			element.parentElement.getBoundingClientRect().bottom - element.getBoundingClientRect().bottom
	);
	assert.ok(Math.abs(gap - 12) < 1, `Expected a 12px gap beneath the legend, got ${gap}px`);
	assert.equal(await main.locator('.maplibregl-ctrl-attrib').count(), 0);
}

async function drag(page, start, end, touch) {
	if (touch) {
		const cdp = await page.context().newCDPSession(page);
		try {
			await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [start] });
			await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [end] });
			await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
		} finally {
			await cdp.detach();
		}
	} else {
		await page.mouse.move(start.x, start.y);
		await page.mouse.down();
		await page.mouse.move(end.x, end.y, { steps: 6 });
		await page.mouse.up();
	}
}

try {
	for (const width of [1440, 390, 320]) {
		const mobile = width < 768;
		const page = await browser.newPage({
			viewport: { width, height: 900 },
			locale: 'en-US',
			timezoneId: 'UTC',
			colorScheme: mobile ? 'dark' : 'light',
			hasTouch: mobile,
			ignoreHTTPSErrors: true
		});
		const errors = [];
		page.on('pageerror', (error) => errors.push(error.message));
		await page.route('**/*', async (request) => {
			const url = new URL(request.request().url());
			if (url.hostname === 'tiles.openfreemap.org') {
				return request.fulfill({ json: basemap });
			}
			if (url.hostname === 'tile.openstreetmap.org') {
				return request.fulfill({ body: tile, contentType: 'image/png' });
			}
			if (url.hostname === 'api.open-meteo.com') {
				return request.fulfill({
					json: {
						utc_offset_seconds: 0,
						current: {
							time: new Date().toISOString().slice(0, 16),
							wind_speed_10m: 12,
							wind_direction_10m: 225,
							wind_gusts_10m: 20
						}
					}
				});
			}
			if (url.origin !== origin) return request.abort();
			const { pathname, searchParams } = url;
			// Simulate stale public runtime assets after an application upgrade.
			// Planning, recorded-route heights and GPX must use the fingerprinted build.
			if (pathname.startsWith('/waas-engine/')) {
				return request.fulfill({ status: 410, body: 'Stale unversioned planner runtime' });
			}
			if (pathname === '/cairn.v1.AuthService/GetCurrentUser') {
				return request.fulfill({
					json: { user: { id: 'a', username: 'ada', displayName: 'Ada', emailVerified: true } }
				});
			}
			if (pathname === '/api/riders') return request.fulfill({ json: { members: riders } });
			if (pathname === '/api/routes') {
				const found =
					searchParams.get('unridden') === 'true'
						? []
						: [loop, canal].filter(
								(route) =>
									(!searchParams.get('rider') ||
										route.riders.some((rider) => rider.username === searchParams.get('rider'))) &&
									route.name.toLowerCase().includes(searchParams.get('q').toLowerCase())
							);
				return request.fulfill({ json: { routes: found, total: found.length } });
			}
			if (pathname === '/api/routes/waasland') return request.fulfill({ json: loop });
			if (pathname === '/api/routes/canal') return request.fulfill({ json: canal });
			if (pathname === '/api/routes/waasland/gpx') return request.continue();
			if (pathname.startsWith('/api/route-planner/waasland/data/')) {
				const body = catalogs.get(pathname.split('/').at(-1));
				return body ? request.fulfill({ body }) : request.fulfill({ status: 404 });
			}
			if (pathname.startsWith('/api/')) return request.fulfill({ json: {} });
			return request.fulfill({
				path: resolve(
					build,
					request.request().resourceType() === 'document' ? 'index.html' : '.' + pathname
				)
			});
		});

		await page.goto(origin + '/explore/routes');
		const main = page.getByRole('main');
		await main.getByRole('heading', { name: 'Routes', exact: true }).waitFor();
		await main.getByText('2 routes found').waitFor();
		await main.locator('svg[role="img"] image').first().waitFor({ state: 'attached' });
		assert.equal(await main.getByText(/© OpenStreetMap/).count(), 0);
		if (mobile) {
			const subnav = page.getByRole('navigation', { name: 'Explore', exact: true });
			assert.deepEqual(await subnav.getByRole('link').allTextContents(), [
				'Heatmap',
				'Segments',
				'Routes',
				'Route Planner'
			]);
			assert.equal(await page.locator('.mobile-tabbar a').count(), 5);
		}
		assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), width);

		const filter = main.getByRole('combobox', { name: 'Rider', exact: true });
		await filter.selectOption('mira');
		await main.getByText('1 route found').waitFor();
		await main.getByRole('combobox', { name: 'Sort routes' }).selectOption('rider_rides');
		await main.getByRole('button', { name: /Waasland Loop/ }).click();
		const panel = mobile
			? page.getByRole('dialog', { name: 'Route details' })
			: main.locator('aside');
		await panel.getByRole('heading', { name: 'Waasland Loop' }).waitFor();
		await panel.getByRole('heading', { name: 'Rider leaderboard' }).waitFor();
		await panel.getByRole('link', { name: 'Morning ride' }).waitFor();
		assert.equal(
			await panel.getByRole('link', { name: 'Morning ride' }).getAttribute('href'),
			'/activities/latest'
		);
		assert.equal(
			await panel.getByRole('link', { name: 'Mira', exact: true }).getAttribute('href'),
			'/u/mira'
		);
		await panel.getByText(/17:30/).waitFor();
		await panel.getByRole('button', { name: 'Best avg. speed' }).click();
		assert.match(await panel.locator('tbody tr').first().innerText(), /Mira/);

		const exported = page.waitForEvent('download');
		await panel.getByRole('link', { name: 'Export GPX' }).click();
		assert.equal(await readFile(await (await exported).path(), 'utf8'), originalGPX);
		if (mobile) {
			await panel.getByRole('button', { name: 'Close' }).click();
			await panel.waitFor({ state: 'hidden' });
			await main.getByRole('button', { name: /Waasland Loop/ }).click();
		}
		await panel.getByRole('link', { name: 'Open in Route Planner' }).click();
		await page.waitForURL(origin + '/routes/waasland/recorded/waasland');
		await main.getByRole('heading', { name: 'Waasland Loop' }).waitFor();
		assert.equal(await main.getByRole('link', { name: 'Back to Routes' }).count(), 0);
		await main.getByRole('link', { name: 'New route' }).waitFor();
		await main.getByRole('button', { name: 'Reverse direction' }).click();
		assert.equal(
			await main.getByRole('button', { name: 'Reverse direction' }).getAttribute('aria-pressed'),
			'true'
		);
		const edited = page.waitForEvent('download');
		await main.getByRole('button', { name: 'Export edited GPX' }).click();
		const exportedText = await readFile(await (await edited).path(), 'utf8');
		assert.match(exportedText, /<name>Waasland Loop<\/name>/);
		assert.ok(
			exportedText.indexOf('lat="51.03"') < exportedText.indexOf('lat="51.01"'),
			'GPX must contain the reversed GPS sequence'
		);
		await page.getByRole('navigation').getByRole('link', { name: 'Routes', exact: true }).click();
		await main.getByRole('searchbox', { name: 'Search routes' }).fill('canal');
		await main.getByText('1 route found').waitFor();
		await main.getByRole('button', { name: /Canal Ride/ }).waitFor();
		assert.equal(await main.getByRole('button', { name: /Waasland Loop/ }).count(), 0);
		await page.goto(origin + '/routes/waasland/recorded/canal');
		await main.getByRole('heading', { name: 'Canal Ride' }).waitFor();
		const fit = main.getByRole('button', { name: 'Fit route' });
		await page.waitForFunction(
			() =>
				!document.querySelector('button[aria-label="Fit route"]')?.disabled &&
				!!document.querySelector('.waas-marker-start')
		);
		await checkMapLegend(main);
		assert.deepEqual(await metrics(main), [5.5, 20, 10, 80]);
		const marker = main.locator('.waas-marker-start');
		const initialMarker = await marker.boundingBox();
		await main.getByRole('button', { name: 'Zoom in' }).click();
		await page.waitForFunction(
			(x) =>
				Math.abs(document.querySelector('.waas-marker-start').getBoundingClientRect().x - x) > 8,
			initialMarker.x
		);
		await fit.click();

		const canvas = main.locator('.maplibregl-canvas');
		const mapBox = await canvas.boundingBox();
		const center = { x: mapBox.x + mapBox.width / 2, y: mapBox.y + mapBox.height / 2 };
		const scroll = await page.evaluate(() => scrollY);
		if (mobile) {
			const cdp = await page.context().newCDPSession(page);
			const touch = (type, touchPoints) =>
				cdp.send('Input.dispatchTouchEvent', { type, touchPoints });
			for (const [offset, spread] of [
				[24, 68],
				[57, 0]
			]) {
				await touch(
					'touchStart',
					spread
						? [
								{ x: center.x - offset, y: center.y },
								{ x: center.x + offset, y: center.y }
							]
						: [center]
				);
				await touch(
					'touchMove',
					spread
						? [
								{ x: center.x - spread, y: center.y },
								{ x: center.x + spread, y: center.y }
							]
						: [{ x: center.x + offset, y: center.y + 26 }]
				);
				await touch('touchEnd', []);
			}
			await cdp.detach();
		} else {
			await page.mouse.move(center.x, center.y);
			await page.mouse.down();
			await page.mouse.move(center.x + 110, center.y + 30, { steps: 7 });
			await page.mouse.up();
			await page.mouse.wheel(0, -320);
		}
		await page.waitForFunction(
			(x) =>
				Math.abs(document.querySelector('.waas-marker-start').getBoundingClientRect().x - x) > 8,
			initialMarker.x
		);
		assert.equal(await page.evaluate(() => scrollY), scroll);
		await fit.click();

		if (width === 320) {
			// Exercise the dialog used when element fullscreen is unavailable.
			await page.evaluate(() => {
				HTMLElement.prototype.requestFullscreen = undefined;
			});
		}
		await main.getByRole('button', { name: 'Enter fullscreen' }).click();
		const exit = main.getByRole('button', { name: 'Exit fullscreen' });
		await exit.waitFor();
		assert.ok((await canvas.boundingBox()).height > 800);
		await checkMapLegend(main);
		await exit.click();
		await main.getByRole('button', { name: 'Enter fullscreen' }).waitFor();

		const chart = main.getByRole('slider', { name: 'Position on route' });
		if (mobile) {
			await chart.tap();
		} else {
			await chart.hover();
		}
		await main.locator('[data-profile-tooltip]').waitFor();
		await main.locator('.waas-marker-profile').waitFor();
		const plot = await chart.boundingBox();
		const plotMiddle = plot.y + plot.height / 2;
		await drag(
			page,
			{ x: plot.x + 12, y: plotMiddle },
			{ x: plot.x + plot.width * 0.7, y: plotMiddle },
			mobile
		);
		assert.ok(Number(await chart.inputValue()) > 3000);
		await chart.press('Home');
		assert.match(await chart.getAttribute('aria-valuetext'), /0 km, 20 m/);

		await main.getByRole('button', { name: 'Reverse direction' }).click();
		assert.deepEqual(await metrics(main), [5.5, 10, 20, 80]);
		await chart.press('Home');
		assert.match(await chart.getAttribute('aria-valuetext'), /0 km, —/);
		const trim = main.getByRole('slider', { name: 'Trim start' });
		await trim.scrollIntoViewIfNeeded();
		const range = await trim.boundingBox();
		const thumbMiddle = range.y + range.height / 2;
		await drag(
			page,
			{ x: range.x + 12, y: thumbMiddle },
			{ x: range.x + range.width * 0.3, y: thumbMiddle },
			mobile
		);
		assert.ok(Number(await trim.inputValue()) > 700);
		await trim.press('Home');
		await pressRange(trim, 'ArrowRight', 10);
		await pressRange(main.getByRole('slider', { name: 'Trim end' }), 'ArrowLeft', 15);
		assert.ok((await metrics(main))[0] < 5.5);
		await main.getByText('Trimmed', { exact: true }).waitFor();
		const cut = page.waitForEvent('download');
		await main.getByRole('button', { name: 'Export edited GPX' }).click();
		const cutText = await readFile(await (await cut).path(), 'utf8');
		const segments = await page.evaluate((xml) => {
			const doc = new DOMParser().parseFromString(xml, 'application/xml');
			if (doc.querySelector('parsererror')) return [];
			return Array.from(doc.querySelectorAll('trkseg'), (segment) =>
				Array.from(segment.querySelectorAll('trkpt'), (pt) => ({
					lat: Number(pt.getAttribute('lat')),
					elevation: pt.querySelector('ele')?.textContent ?? null
				}))
			);
		}, cutText);
		assert.equal(segments.length, 2);
		assert.ok(segments[0][0].lat > segments[0].at(-1).lat);
		assert.equal(segments[0][0].elevation, null);
		assert.equal(segments[0].at(-1).elevation, '80.0');
		assert.ok(segments[1].at(-1).lat > 51);
		await main
			.getByRole('button', { name: 'Reset edits' })
			.filter({ hasText: 'Reset edits' })
			.click();
		assert.deepEqual(await metrics(main), [5.5, 20, 10, 80]);

		if (width !== 320) {
			await page.goto(origin + '/routes/waasland');
			await main.getByRole('heading', { name: 'Waasland route planner' }).waitFor();
			assert.equal(await main.getByRole('link', { name: 'Back to Routes' }).count(), 0);
			const gpX = main.getByRole('button', { name: 'Download GPX' });
			await gpX.waitFor();
			await checkMapLegend(main);
			const first = await metrics(main);
			assert.ok(first[0] >= 69.5 && first[0] <= 70.5);
			assert.ok(first.every(Number.isFinite), 'Every route summary metric must be available');
			assert.ok(first[1] > 0 && first[2] > 0 && first[3] > 0);
			const target = main.getByRole('spinbutton', { name: 'Distance (km)', exact: true });
			await target.fill('75');
			await target.blur();
			await main.getByRole('button', { name: 'Plan ride' }).click();
			await gpX.waitFor();
			const next = await metrics(main);
			assert.ok(next[0] >= 74.5 && next[0] <= 75.5);
			assert.ok(next[1] && next[2] && next[3]);
			const planned = page.waitForEvent('download');
			await gpX.click();
			const xml = await readFile(await (await planned).path(), 'utf8');
			assert.match(xml, /<gpx version="1.1"/);
			assert.match(xml, /<trkpt .+<ele>/);
		}

		assert.deepEqual(errors, []);
		await page.close();
	}
	console.log(
		'Route catalog and planner passed on desktop and mobile, including map, profile, edits and GPX.'
	);
} finally {
	await browser.close();
	await new Promise((done) => server.close(done));
}
