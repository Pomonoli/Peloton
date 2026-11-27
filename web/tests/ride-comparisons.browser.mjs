// Run npm run build:static first. API behavior is covered separately in Go.
import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const origin = 'http://peloton-comparisons.test';
const build = fileURLToPath(new URL('../build/', import.meta.url));
const browser = await chromium.launch({
	executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE,
	args: ['--enable-unsafe-swiftshader']
});
const ada = { id: 'ada', username: 'ada', display_name: 'Ada' };
const mira = { id: 'mira', username: 'mira', display_name: 'Mira' };
const date = '2026-05-09T09:00:30Z';
const activity = (id, title, rider) => ({ id, title, rider });
const a = activity('ada-ride', 'Canal ride', 'ada');
const b = activity('mira-ride', 'Morning ride', 'mira');
const score = { wins: 0, losses: 0, ties: 0, comparable: 0 };
const totals = { count: 2, distance_m: 80000, moving_s: 8000, elevation_gain_m: 200 };
const point = (id, title, distance_km, speed_kmh) => ({
	id,
	title,
	start: date,
	distance_km,
	speed_kmh
});

try {
	for (const mobile of [false, true]) {
		const page = await browser.newPage({
			viewport: { width: mobile ? 320 : 1200, height: 850 },
			hasTouch: mobile,
			colorScheme: mobile ? 'dark' : 'light',
			locale: 'en-US',
			timezoneId: 'UTC'
		});
		const errors = [];
		const scatterRequests = [];
		page.on('pageerror', (err) => errors.push(err.message));
		await page.route('**/*', async (route) => {
			const url = new URL(route.request().url());
			const path = url.pathname;
			const query = url.searchParams;
			if (url.hostname === 'tiles.openfreemap.org') {
				return route.fulfill({ json: { version: 8, sources: {}, layers: [] } });
			}
			if (url.origin !== origin) return route.abort();
			const fixtures = {
				'/cairn.v1.AuthService/GetCurrentUser': {
					user: { id: ada.id, username: ada.username, displayName: 'Ada' }
				},
				'/api/riders': { members: [ada, mira] },
				'/api/members/mira': mira,
				'/api/stats': {
					totals,
					sport_totals: [],
					by_discipline: [],
					by_year: [2026, 2025].map((year) => ({ year, ...totals })),
					records: {}
				},
				'/api/head-to-head': {
					first: ada,
					opponent: mira,
					segments: [],
					records: [],
					segment_score: score,
					record_score: score
				},
				'/api/head-to-head/shared':
					query.get('from') === '2026-07-01'
						? { rides: [], encounters: [], shared_distance_m: 0 }
						: {
								rides: [{ activities: [a, b], date, distance_m: 9000, duration_s: 1200 }],
								encounters: [{ activities: [a, b], time: date, lat: 51.12345, lon: 4.12345 }],
								shared_distance_m: 9000
							}
			};
			if (path === '/api/stats/speed-distance') {
				scatterRequests.push(query);
				if (query.get('year') === '2025') return route.fulfill({ json: [] });
				return route.fulfill({
					json:
						query.get('member') === 'ada'
							? [point(a.id, a.title, 30, 32), point('ada-long', 'Long ride', 50, 28)]
							: [point(b.id, b.title, 21, 26), point('mira-long', 'Weekend ride', 77, 27)]
				});
			}
			if (path.startsWith('/api/') || path.startsWith('/cairn.v1.')) {
				return route.fulfill({ json: fixtures[path] ?? {} });
			}
			return route.fulfill({
				path:
					route.request().resourceType() === 'document'
						? resolve(build, 'index.html')
						: resolve(build, '.' + path)
			});
		});

		await page.goto(origin + '/stats?commute=false');
		const chart = page.getByRole('region', { name: 'Speed vs Distance' });
		const aPoint = chart.getByRole('link', { name: /Ada.*Canal ride.*30 km.*32 km\/h/ });
		await aPoint.waitFor();
		assert.equal(scatterRequests[0]?.get('member'), 'ada');
		assert.equal(scatterRequests[0]?.get('commute'), 'false');
		assert.equal(await chart.getByRole('link', { name: /Mira/ }).count(), 0);
		const initialColor = await aPoint.locator('.dot').evaluate((dot) => getComputedStyle(dot).fill);
		assert.equal(await chart.locator('line[stroke-dasharray]').count(), 1);
		if (mobile) await aPoint.tap();
		else await aPoint.hover();
		await chart.getByRole('link', { name: 'Open activity' }).waitFor();
		assert.equal(
			await chart.getByRole('link', { name: 'Open activity' }).getAttribute('href'),
			'/activities/ada-ride?member=ada'
		);

		await chart.getByText('Compare riders', { exact: false }).filter({ hasText: /\(1\)/ }).click();
		await chart.getByRole('checkbox', { name: 'Mira' }).check();
		const bPoint = chart.getByRole('link', { name: /Mira.*Morning ride/ });
		await bPoint.waitFor();
		assert.equal(await chart.locator('line[stroke-dasharray]').count(), 2);
		assert.equal(
			await aPoint.locator('.dot').evaluate((dot) => getComputedStyle(dot).fill),
			initialColor
		);
		assert.notEqual(
			await bPoint.locator('.dot').evaluate((dot) => getComputedStyle(dot).fill),
			initialColor
		);
		assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
		await page.getByRole('combobox', { name: 'Year' }).selectOption('2025');
		await chart.getByText(/No cycling activities/).waitFor();
		assert(scatterRequests.some((request) => request.get('year') === '2025'));

		await page.goto(origin + '/head-to-head?second=mira');
		await page.getByRole('tab', { name: 'Shared', exact: true }).click();
		const panel = page.getByRole('tabpanel', { name: 'Shared' });
		await panel.getByRole('button', { name: 'Shared Rides 1' }).click();
		await panel.getByRole('link', { name: 'Canal ride · @ada' }).waitFor();
		await panel.getByRole('button', { name: 'Shared Distance 9 km' }).click();
		await panel.getByText('20:00', { exact: true }).waitFor();
		const encounters = panel.getByRole('button', { name: 'Crossed Paths 1' });
		assert.equal(
			await encounters.getAttribute('title'),
			'Brief GPS-detected encounters during separate rides, excluding shared rides.'
		);
		await encounters.click();
		await panel.getByText(/Brief GPS-detected encounters/).waitFor();
		await panel.getByRole('button', { name: 'Show on map' }).click();
		await panel.locator('.maplibregl-marker').waitFor();
		await panel.getByText('51.12345, 4.12345').waitFor();
		assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));

		await panel.getByLabel('From date').fill('2026-07-01');
		await panel.getByText('No encounters were detected in this date range.').waitFor();
		assert.deepEqual(errors, []);
		await page.close();
	}
	console.log('Ride comparison browser checks passed on desktop and mobile.');
} finally {
	await browser.close();
}
