// Run against the built static app with fixture responses. Go integration tests
// independently verify the corresponding REST endpoints against PostgreSQL.
import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const origin = 'http://peloton-views.test';
const build = fileURLToPath(new URL('../build/', import.meta.url));
const browser = await chromium.launch({
	executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
});
const totals = { count: 1, distance_m: 45000, moving_s: 4500, elevation_gain_m: 50 };
const mira = {
	id: 'mira',
	username: 'mira',
	display_name: 'Mira',
	avatar_url: '',
	activity_count: 1,
	cycling_distance_m: 45000
};
const recent = {
	id: 'mira-ride',
	title: 'Canal ride',
	type: 'ride',
	start_time: '2026-11-22T13:00:00Z',
	distance_m: 45000,
	moving_duration_s: 4500,
	elevation_gain_m: 50
};
const rides = [
	{
		id: 'one',
		segment_id: 'canal',
		segment_name: 'Canal Sprint',
		segment_distance_m: 1000,
		elapsed_s: 90,
		average_speed_kmh: 40,
		local_kom_position: 2,
		is_personal_record: true,
		personal_rank: 1
	},
	{
		id: 'two',
		segment_id: 'forest',
		segment_name: 'Forest Road',
		segment_distance_m: 500,
		elapsed_s: 60,
		average_speed_kmh: 30,
		local_kom_position: 1,
		is_personal_record: false,
		personal_rank: 3
	},
	{
		id: 'three',
		segment_id: 'hill',
		segment_name: 'Hill Climb',
		segment_distance_m: 500,
		elapsed_s: 90,
		average_speed_kmh: 20,
		local_kom_position: 1,
		is_personal_record: true,
		personal_rank: 1
	}
];
const date = new Date('2026-09-07T00:00:00Z');
const weeks = Array.from({ length: 12 }, (_, i) => ({
	start: new Date(date.getTime() + i * 7 * 86400000).toISOString().slice(0, 10),
	distance_m: i === 10 ? 45000 : 0
}));
const asCatalog = (row) => ({
	id: row.segment_id,
	name: row.segment_name,
	distance_m: row.segment_distance_m,
	average_speed_kmh: row.average_speed_kmh,
	best_elapsed_s: row.elapsed_s,
	local_kom_holder: row.local_kom_position === 1 ? 'Mira' : 'Sam',
	member_count: 2,
	my_attempts: 1,
	start: null
});

try {
	for (const width of [390, 1440]) {
		const page = await browser.newPage({
			viewport: { width, height: 900 },
			locale: 'en-US',
			hasTouch: width < 500
		});
		page.setDefaultTimeout(6000);
		const errors = [];
		page.on('pageerror', (error) => errors.push(error.message));
		let catalogSort = [];
		await page.route('**/*', async (route) => {
			const url = new URL(route.request().url());
			if (url.origin !== origin) return route.abort();
			const path = url.pathname;
			if (path === '/api/segments/catalog') {
				catalogSort = url.searchParams.getAll('sort');
				return route.fulfill({ json: { segments: rides.map(asCatalog), has_more: false } });
			}
			const fixtures = {
				'/cairn.v1.AuthService/GetCurrentUser': {
					user: { id: 'ada', username: 'ada', displayName: 'Ada' }
				},
				'/cairn.v1.ActivityService/GetActivity': {
					activity: {
						id: recent.id,
						title: recent.title,
						userId: 'mira',
						type: 'ACTIVITY_TYPE_RIDE',
						startTime: recent.start_time,
						primaryStreamSourceId: 'mira-source',
						movingDuration: '4500s',
						elapsedDuration: '5000s',
						summary: { distanceM: 45000 }
					}
				},
				'/cairn.v1.ActivityService/GetActivityStream': {
					stream: {
						sampleCount: 4,
						channels: ['STREAM_CHANNEL_SPEED'],
						timeS: [0, 5, 10, 15],
						speedMps: [8, 10, 10, 11]
					}
				},
				'/api/members/mira': mira,
				'/api/riders': { members: [mira] },
				'/api/overview': {
					totals,
					recent: url.searchParams.has('member') ? [recent] : [],
					cycling_weeks: weeks
				},
				'/api/activities/mira-ride/segment-efforts': { efforts: rides, can_rescan: false },
				'/api/stats': {
					totals,
					by_year: [],
					by_discipline: [],
					records: {},
					sport_totals: [
						{ value: 'ride', count: 1, distance_m: 45000, moving_s: 4500, speed_kmh: 36 }
					]
				},
				'/api/analysis': {
					dates: ['2026-11-22'],
					ctl: [1],
					atl: [3],
					tsb: [-2],
					tss: [12],
					current: { ctl: 1, atl: 3, tsb: -2 },
					coverage: { imported: 0, sensor_estimate: 0, duration_estimate: 1, missing: 0 }
				},
				'/api/records': {
					records: [
						{
							activity_type: 'ride',
							metric: 'speed',
							window_kind: 'duration',
							window_value: 60,
							achieved_value: url.searchParams.has('exclude_downhills') ? 8 : 15,
							activity_id: recent.id,
							timestamp: recent.start_time
						}
					]
				},
				'/api/athlete/metrics': { specs: [], entries: [] }
			};
			if (path.startsWith('/api/') || path.startsWith('/cairn.v1.')) {
				return route.fulfill({ json: fixtures[path] ?? {} });
			}
			return route.fulfill({
				path: resolve(
					build,
					route.request().resourceType() === 'document' ? 'index.html' : '.' + path
				)
			});
		});
		const open = (path) => page.goto(origin + path);
		await open('/u/mira');
		await page.waitForURL(origin + '/?member=mira');
		await page.getByRole('heading', { name: 'Mira' }).waitFor();
		const chart = page.getByRole('region', { name: 'Weekly cycling distance' });
		const bar = chart.getByRole('button', { name: /45 km/ });
		if (width < 500) await bar.tap();
		else await bar.hover();
		assert.equal(await chart.getByRole('button').count(), 12);
		assert.match(await chart.locator('[aria-live]').textContent(), /45 km/);
		assert.equal(await chart.getByRole('button', { name: / · 0 km/ }).count(), 11);
		await page.getByText('36.0 km/h', { exact: true }).waitFor();
		assert.equal(
			await page.getByRole('main').getByRole('link', { name: 'Health', exact: true }).count(),
			0
		);

		await page.getByRole('link', { name: 'Canal ride' }).click();
		const pr = page.getByRole('button', { name: /2 segment PRs/ });
		await pr.click();
		const achievements = page.locator('#segment-prs');
		await achievements.waitFor();
		await achievements.getByRole('link', { name: 'Hill Climb' }).waitFor();
		assert.equal(await achievements.getByRole('link').count(), 2);
		assert.equal(await achievements.getByRole('link', { name: 'Forest Road' }).count(), 0);
		const segmentTable = page
			.locator('table')
			.filter({ has: page.getByRole('columnheader', { name: /Local KOM/ }) });
		await segmentTable.getByRole('button', { name: 'Avg speed', exact: false }).click();
		await segmentTable.getByRole('button', { name: 'Local KOM', exact: false }).click();
		assert.match(await segmentTable.locator('tbody tr').first().innerText(), /Hill Climb/);
		assert.equal(
			await segmentTable.getByRole('columnheader', { name: /Local KOM/ }).getAttribute('aria-sort'),
			'ascending'
		);
		assert.equal(await page.getByText('Kudos', { exact: true }).count(), 0);
		assert.equal(await page.getByText('Comments', { exact: true }).count(), 0);
		await open('/activities/mira-ride/streams');
		await page.locator('.uplot').waitFor();

		await open('/segments');
		await page.getByRole('button', { name: /Avg speed/ }).click();
		await page.getByRole('button', { name: /Local KOM/ }).click();
		await page.waitForFunction(() =>
			document.querySelector('th[aria-sort="ascending"]')?.textContent.includes('Local KOM')
		);
		assert.deepEqual(catalogSort.slice(0, 2), ['local_kom_holder:asc', 'average_speed_kmh:asc']);
		await open('/stats?member=mira');
		await page.getByRole('heading', { name: 'By sport' }).waitFor();
		await page.getByRole('cell', { name: '36.0 km/h' }).waitFor();
		await open('/analysis?member=mira');
		await page.getByText('Your long-term training load', { exact: false }).waitFor();
		await page.getByText('Duration-only estimates', { exact: true }).waitFor();
		await open('/records');
		await page.getByText('54 km/h', { exact: true }).waitFor();
		await page.getByLabel('Exclude downhills').check();
		await page.getByText('28.8 km/h', { exact: true }).waitFor();
		await page.reload();
		await page.getByText('28.8 km/h', { exact: true }).waitFor();
		await open('/athlete');
		await page.waitForURL(origin + '/health');
		await page.getByRole('heading', { name: 'Athlete settings' }).waitFor();
		assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
		assert.deepEqual(errors, []);
		await page.close();
	}
	console.log(
		'Overview, activity segments and streams, catalog sorting, Stats, Analysis, Records, and Health passed at 390px and 1440px.'
	);
} finally {
	await browser.close();
}
