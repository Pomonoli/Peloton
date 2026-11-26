// Run against npm run build:static with fixture responses.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const originalGPX =
	'<gpx version="1.1"><trk><trkseg><trkpt lat="51" lon="4"/></trkseg></trk></gpx>';
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
	executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
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
	riders: riders.slice(0, 1)
};

try {
	for (const width of [1440, 390, 320]) {
		const mobile = width < 768;
		const page = await browser.newPage({
			viewport: { width, height: 900 },
			locale: 'en-US',
			timezoneId: 'UTC',
			colorScheme: mobile ? 'dark' : 'light'
		});
		const errors = [];
		page.on('pageerror', (error) => errors.push(error.message));
		await page.route('**/*', async (request) => {
			const url = new URL(request.request().url());
			if (url.hostname === 'tiles.openfreemap.org') {
				return request.fulfill({ json: { version: 8, sources: {}, layers: [] } });
			}
			if (url.origin !== origin) return request.abort();
			const { pathname, searchParams } = url;
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
		await main.getByRole('checkbox', { name: 'Reverse direction' }).check();
		const edited = page.waitForEvent('download');
		await main.getByRole('button', { name: 'Export edited GPX' }).click();
		const exportedText = await readFile(await (await edited).path(), 'utf8');
		assert.match(exportedText, /<name>Waasland Loop<\/name>/);
		assert.ok(
			exportedText.indexOf('lat="51.03"') < exportedText.indexOf('lat="51.01"'),
			'GPX must contain the reversed GPS sequence'
		);
		await main.getByRole('link', { name: 'Back to Routes' }).click();
		await main.getByRole('searchbox', { name: 'Search routes' }).fill('canal');
		await main.getByText('1 route found').waitFor();
		await main.getByRole('button', { name: /Canal Ride/ }).waitFor();
		assert.equal(await main.getByRole('button', { name: /Waasland Loop/ }).count(), 0);
		assert.deepEqual(errors, []);
		await page.close();
	}
	console.log('Recorded routes, desktop and mobile, including GPX and planner editing passed.');
} finally {
	await browser.close();
	await new Promise((done) => server.close(done));
}
