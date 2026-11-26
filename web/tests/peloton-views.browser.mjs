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
	elapsed_duration_s: 5000,
	avg_speed_mps: 10,
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

const analysisEnd = '2026-11-26';
const analysisDay = (index) =>
	new Date(Date.parse(analysisEnd) - index * 86_400_000).toISOString().slice(0, 10);
function trainingFixture(days, rider) {
	const start = analysisDay(days === 3650 ? 450 : days - 1);
	const extreme = rider === 'mira';
	if (rider === 'empty' || rider === 'unscored') {
		const missing = rider === 'unscored' ? 1 : 0;
		return {
			start,
			end: analysisEnd,
			dates: [],
			ctl: [],
			atl: [],
			tsb: [],
			current: { ctl: 0, atl: 0, tsb: 0 },
			stress_days: missing ? [{ date: analysisEnd, tss: null, scored: 0, total: 1 }] : [],
			coverage: { imported: 0, sensor_estimate: 0, duration_estimate: 0, missing },
			recent_total: missing,
			recent_scored: 0
		};
	}
	const current = extreme
		? { ctl: 34, atl: 159, tsb: -125 }
		: rider === 'very-fresh'
			? { ctl: 130, atl: 5, tsb: 125 }
			: { ctl: 7.9, atl: 0.3, tsb: 7.6 };
	const dates = Array.from({ length: 31 }, (_, i) => analysisDay(30 - i));
	const stress_days = [
		{ date: '2026-11-22', tss: 60, scored: 1, total: 2 },
		{ date: '2026-11-23', tss: 10, scored: 1, total: 1 },
		{ date: '2026-11-25', tss: null, scored: 0, total: 1 },
		{ date: '2026-11-26', tss: 0, scored: 1, total: 1 }
	];
	return {
		start,
		end: analysisEnd,
		dates,
		ctl: [...Array(30).fill(extreme ? 25 : 14), current.ctl],
		atl: [...Array(30).fill(extreme ? 20 : 1), current.atl],
		tsb: [...Array(30).fill(extreme ? 5 : 13), current.tsb],
		current,
		stress_days,
		coverage: { imported: 0, sensor_estimate: 2, duration_estimate: 1, missing: 2 },
		recent_total: 5,
		recent_scored: 3
	};
}

try {
	for (const width of [360, 390, 424, 430, 900, 1440]) {
		const mobile = width < 768;
		const page = await browser.newPage({
			viewport: { width, height: width === 424 ? 933 : 900 },
			locale: mobile ? 'en-US' : 'de-DE',
			timezoneId: 'Europe/Brussels',
			hasTouch: mobile,
			colorScheme: width === 360 || width === 430 ? 'dark' : 'light'
		});
		page.setDefaultTimeout(6000);
		const errors = [];
		let activityQuery;
		let analysisQuery;
		let selfHistory = 'typical';
		let manualActivity;
		let displayName = 'Ada';
		const profileUpdates = [];
		page.on('pageerror', (error) => errors.push(error.message));
		let catalogSort = [];
		let weatherRequests = 0;
		let weatherLocation;
		await page.addInitScript((allow) => {
			Object.defineProperty(navigator, 'geolocation', {
				value: {
					getCurrentPosition: (success, failure) =>
						allow
							? success({ coords: { latitude: 50.8466, longitude: 4.3528 } })
							: failure({ code: 1 })
				}
			});
		}, width === 390);
		await page.route('**/*', async (route) => {
			const url = new URL(route.request().url());
			if (url.hostname === 'api.open-meteo.com') {
				weatherRequests++;
				weatherLocation = url.searchParams.get('latitude');
				return width === 430
					? route.fulfill({ status: 503 })
					: route.fulfill({
							json: {
								current: {
									temperature_2m: 9,
									wind_speed_10m: 14,
									wind_direction_10m: 225,
									weather_code: 2,
									is_day: 1
								}
							}
						});
			}
			if (url.origin !== origin) return route.abort();
			const path = url.pathname;
			if (path === '/api/activities/feed') {
				activityQuery = url.searchParams;
				return route.fulfill({
					json: {
						total: 1,
						matched: 1,
						facets: { types: [], disciplines: [], years: [] },
						activities: [{ ...recent, discipline: 'road', timezone: 'UTC' }],
						has_more: false
					}
				});
			}
			if (path === '/api/segments/catalog') {
				catalogSort = url.searchParams.getAll('sort');
				return route.fulfill({ json: { segments: rides.map(asCatalog), has_more: false } });
			}
			if (path === '/api/analysis') {
				analysisQuery = url.searchParams;
				return route.fulfill({
					json: trainingFixture(
						Number(analysisQuery.get('days')),
						analysisQuery.get('member') ?? selfHistory
					)
				});
			}
			if (path === '/api/activities/manual' && route.request().method() === 'POST') {
				manualActivity = route.request().postDataJSON();
				return route.fulfill({ json: { activity_id: recent.id } });
			}
			if (path === '/cairn.v1.AuthService/UpdateProfile') {
				const patch = route.request().postDataJSON();
				profileUpdates.push(patch);
				if (profileUpdates.length === 1) {
					return route.fulfill({ status: 500, json: { code: 'internal' } });
				}
				displayName = patch.displayName;
				return route.fulfill({ json: { user: { id: 'ada', username: 'ada', displayName } } });
			}
			const fixtures = {
				'/cairn.v1.AuthService/GetCurrentUser': {
					user: {
						id: 'ada',
						username: 'ada',
						displayName,
						emailVerified: true,
						timeFormat: 'TIME_FORMAT_12H'
					},
					permissions: width === 390 ? ['admin'] : []
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
						summary: { distanceM: 45000, avgSpeedMps: recent.avg_speed_mps, maxSpeedMps: 11 }
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
				'/api/riders': {
					members: [
						mira,
						{ ...mira, id: 'admin', username: 'admin', display_name: 'Dedicated Admin' }
					]
				},
				'/api/overview': {
					totals,
					recent: [recent],
					this_week: totals,
					last_week: totals,
					cycling_weeks: weeks
				},
				'/api/activities/mira-ride/segment-efforts': { efforts: rides, can_rescan: false },
				'/api/activities/mira-ride/similar': {
					target_segments: 1,
					activities: [
						{
							id: recent.id,
							is_current: true,
							start_time: recent.start_time,
							distance_m: recent.distance_m,
							moving_s: recent.moving_duration_s,
							elapsed_s: recent.elapsed_duration_s,
							average_speed_kmh: 36
						},
						{
							id: 'mira-previous-ride',
							is_current: false,
							start_time: '2026-11-14T13:00:00Z',
							distance_m: recent.distance_m,
							moving_s: 4800,
							elapsed_s: 5400,
							average_speed_kmh: 33.75,
							overlap_pct: 0.99
						}
					],
					summary: {
						count: 2,
						best_speed_kmh: 36,
						current_speed_kmh: 36,
						current_rank: 1,
						is_best: true
					}
				},
				'/api/stats': {
					totals,
					by_year: [
						{ year: 2026, ...totals },
						{ year: 2025, ...totals }
					],
					by_discipline: [],
					records: { longest_distance: recent },
					sport_totals: [
						{ value: 'ride', count: 1, distance_m: 45000, moving_s: 4500, speed_kmh: 36 }
					]
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
		const noPageOverflow = (path) =>
			page.evaluate((url) => {
				if (document.documentElement.scrollWidth > innerWidth)
					throw new Error('Horizontal overflow at ' + url);
				return true;
			}, path);
		const main = page.getByRole('main');
		const sheet = page.getByRole('dialog');
		const done = () => sheet.getByRole('button', { name: 'Done' }).click();
		await open('/');
		await main.getByRole('heading', { name: /Good (morning|afternoon|evening), Ada/ }).waitFor();
		await main.getByRole('term').filter({ hasText: 'Activities' }).first().waitFor();
		await main.getByRole('link', { name: 'Add activity' }).waitFor();
		await noPageOverflow('/');
		await main.getByText('36.00 km/h', { exact: true }).waitFor();
		assert.equal(
			await page.locator('html').getAttribute('data-theme'),
			width === 360 || width === 430 ? 'dark' : 'light'
		);
		assert.equal(await main.getByRole('button', { name: 'Last week' }).count(), 0);
		assert.equal(await page.getByRole('contentinfo').count(), 0);
		if (width === 430) {
			await main.getByText('Weather unavailable').waitFor();
			assert.equal(await main.getByRole('link', { name: 'Open-Meteo' }).count(), 0);
		} else {
			await main.getByRole('img', { name: 'Partly cloudy' }).waitFor();
			const wind = main.getByRole('img', { name: 'Wind blowing toward northeast at 14 km/h' });
			assert.equal((await wind.textContent()).trim(), '14 km/h');
			assert.equal(await wind.locator('svg').evaluate((node) => node.style.rotate), '45deg');
			const source = main.getByRole('link', { name: 'Open-Meteo' });
			assert.equal(await source.getAttribute('href'), 'https://open-meteo.com/en/licence');
			assert(await source.evaluate((link) => !link.closest('[role="status"]')));
		}
		assert.equal(weatherRequests, 1);
		assert.equal(weatherLocation, width === 390 ? '50.85' : '51.165');
		assert.equal(
			await main.getByText('Sint-Niklaas', { exact: true }).count(),
			width === 390 || width === 430 ? 0 : 1
		);
		assert.deepEqual(await main.locator('h2').allTextContents(), [
			'Weekly cycling distance',
			'Recent activities'
		]);
		if (mobile) {
			const tabbar = page.getByRole('navigation', { name: 'Main navigation' });
			assert.deepEqual(
				(await tabbar.getByRole('link').allTextContents()).map((label) => label.trim()),
				['Home', 'Activities', 'Explore', 'Insights', 'Social']
			);
			const profile = page.getByRole('button', { name: 'Profile menu' });
			await profile.click();
			const menu = page.getByRole('navigation', { name: 'Profile menu' });
			assert.deepEqual(
				(await menu.getByRole('link').allTextContents()).map((label) => label.trim()),
				width === 390
					? ['My body', 'Gear', 'Connections', 'Settings', 'Admin']
					: ['My body', 'Gear', 'Connections', 'Settings']
			);
			await page.keyboard.press('Escape');
			await menu.waitFor({ state: 'hidden' });
			await tabbar.getByRole('link', { name: 'Insights' }).click();
			await page
				.getByRole('navigation', { name: 'Insights' })
				.getByRole('link', { name: 'Analysis' })
				.click();
			await page.waitForURL(origin + '/analysis');
			await tabbar.getByRole('link', { name: 'Social' }).click();
			await main.getByRole('button', { name: 'Cycling data for Mira' }).click();
			await main.getByRole('link', { name: 'Activity history' }).waitFor();
			assert.equal(await main.getByText('@admin', { exact: true }).count(), 0);
			await noPageOverflow('/riders');
			await tabbar.getByRole('link', { name: 'Insights' }).click();
			await page.waitForURL(origin + '/analysis');
			await page.goBack();
			await page.waitForURL(origin + '/riders');
			await page.goForward();
			await page.waitForURL(origin + '/analysis');
			await tabbar.getByRole('link', { name: 'Activities' }).click();
			await main.getByRole('link', { name: /Canal ride/ }).waitFor();
			await main.getByText('36.00 km/h', { exact: true }).waitFor();
			await main.getByText('1:15:00', { exact: true }).waitFor();
			await main.getByText('1:23:20', { exact: true }).waitFor();
			const filters = main.getByRole('button', { name: /^Filters/ });
			await filters.click();
			const filteredRequest = page.waitForRequest((request) => {
				const url = new URL(request.url());
				return (
					url.pathname === '/api/activities/feed' && url.searchParams.get('from') === '2026-01-01'
				);
			});
			await sheet.getByLabel('From date').fill('2026-01-01');
			await done();
			await filteredRequest;
			await sheet.waitFor({ state: 'hidden' });
			assert.equal(await filters.evaluate((button) => document.activeElement === button), true);
			await filters.click();
			assert.equal(await sheet.getByLabel('From date').inputValue(), '2026-01-01');
			await sheet.getByRole('button', { name: 'Reset' }).click();
			assert.equal(await sheet.getByLabel('From date').inputValue(), '');
			await page.keyboard.press('Escape');
			await sheet.waitFor({ state: 'hidden' });
			const [fileChooser] = await Promise.all([
				page.waitForEvent('filechooser'),
				main.getByRole('button', { name: 'Upload files' }).click()
			]);
			assert.equal(fileChooser.isMultiple(), true);
			assert.equal(activityQuery?.get('sort'), 'date_desc');
			await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
			assert(await tabbar.evaluate((bar) => bar.getBoundingClientRect().bottom <= innerHeight));

			await noPageOverflow('/activities');
		} else {
			assert.equal(await page.getByRole('navigation', { name: 'Main navigation' }).count(), 1);
			await open('/riders');
			await main.getByRole('button', { name: 'Cycling distance' }).click();
			assert.equal(
				await main
					.getByRole('columnheader', { name: /Cycling distance/ })
					.getAttribute('aria-sort'),
				'descending'
			);
			assert.equal(await main.getByText('@admin', { exact: true }).count(), 0);
			await noPageOverflow('/riders');
		}
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
		await page.getByText('36.00 km/h', { exact: true }).waitFor();
		assert.equal(
			await page.getByRole('main').getByRole('link', { name: 'Health', exact: true }).count(),
			0
		);

		await page.getByRole('link', { name: 'Canal ride' }).click();
		await page.waitForURL(origin + '/activities/mira-ride?member=mira');
		await page.getByText('Viewing Mira', { exact: true }).filter({ visible: true }).waitFor();
		const headline = main.locator('dl').first();
		await headline.getByText('36.00 km/h', { exact: true }).waitFor();
		await headline.getByText('1:15:00', { exact: true }).waitFor();
		const secondary = main
			.locator('dl')
			.filter({ has: page.getByText('Max speed', { exact: true }) });
		await secondary.getByText('1:23:20', { exact: true }).waitFor();
		await secondary.getByText('39.60 km/h', { exact: true }).waitFor();
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
		await open('/activities/mira-ride/similar');
		const comparison = main.getByRole('table');
		await comparison.getByRole('cell', { name: '1:23:20', exact: true }).waitFor();
		await comparison.getByRole('cell', { name: /36\.00 km\/h/ }).waitFor();
		await open('/activities/mira-ride/streams');
		await page.locator('.uplot').waitFor();

		await open('/segments');
		if (mobile) {
			await main.getByRole('button', { name: /^Filters/ }).click();
			await sheet.getByLabel('Sort priority').selectOption('average_speed_kmh');
			await sheet.getByLabel('Sort priority').selectOption('local_kom_holder');
			await done();
			await main.getByRole('link', { name: 'Hill Climb' }).waitFor();
		} else {
			await main.getByRole('button', { name: /Top speed/ }).click();
			await main.getByRole('button', { name: /Local KOM/ }).click();
			await page.waitForFunction(() =>
				document.querySelector('th[aria-sort="ascending"]')?.textContent.includes('Local KOM')
			);
		}
		await noPageOverflow('/segments');
		assert.deepEqual(catalogSort.slice(0, 2), ['local_kom_holder:asc', 'average_speed_kmh:desc']);
		await open('/stats?member=mira');
		await page.getByRole('heading', { name: 'By sport' }).waitFor();
		await page.getByRole('cell', { name: '36.00 km/h' }).waitFor();
		await main.getByText('Moving time 1:15:00 · 36.00 km/h', { exact: true }).waitFor();
		await main.getByLabel('Year', { exact: true }).selectOption('2025');
		assert.equal(await main.getByRole('rowheader', { name: '2026' }).count(), 0);
		await main.getByRole('rowheader', { name: '2025' }).waitFor();
		assert.equal(
			await main.getByRole('link', { name: /Longest distance/ }).getAttribute('href'),
			'/activities/mira-ride?member=mira'
		);
		await noPageOverflow('/stats');
		await open('/analysis?member=mira');
		await page.getByRole('button', { name: 'About Fitness (CTL)' }).click();
		await page.getByText('42-day time constant', { exact: false }).waitFor();
		await page.keyboard.press('Escape');
		await page.getByRole('heading', { name: 'Training Stress Score (TSS)' }).waitFor();
		await page.getByText('Duration-only estimates', { exact: true }).waitFor();
		await page.getByRole('img', { name: 'Current form -125.0, Very fatigued' }).waitFor();
		await page.getByRole('heading', { name: 'High recent training load' }).waitFor();
		assert.equal(analysisQuery.get('member'), 'mira');
		const metrics = main.getByRole('region', { name: 'Key Training Metrics' });
		const trends = main.getByRole('region', { name: 'Training Load Trends' });
		const metricsBox = await metrics.boundingBox();
		const trendsBox = await trends.boundingBox();
		if (width <= 900) assert.ok(trendsBox.y > metricsBox.y + metricsBox.height);
		else
			assert.ok(
				trendsBox.x > metricsBox.x + metricsBox.width && trendsBox.width > metricsBox.width
			);
		const stress = main.getByRole('slider', { name: 'Training Stress Score (TSS)' });
		assert.equal(
			await main.getByRole('button', { name: 'Weekly' }).getAttribute('aria-pressed'),
			'true'
		);
		await stress.press('End');
		assert.match(
			await stress.getAttribute('aria-valuetext'),
			/Nov 23, 2026.*Nov 26, 2026.*Total recorded TSS 10.*Activities 3.*with a score 2.*without a score 1/
		);
		await main.getByRole('button', { name: 'Daily' }).click();
		await stress.press('End');
		assert.match(
			await stress.getAttribute('aria-valuetext'),
			/Total recorded TSS 0, Activities 1, Activities with a score 1/
		);
		await stress.press('ArrowLeft');
		assert.match(
			await stress.getAttribute('aria-valuetext'),
			/Total recorded TSS Unavailable, Activities 1/
		);
		await stress.press('ArrowLeft');
		assert.match(await stress.getAttribute('aria-valuetext'), /Total recorded TSS 0, Activities 0/);
		await main.getByRole('button', { name: 'Monthly' }).click();
		await stress.press('End');
		assert.match(
			await stress.getAttribute('aria-valuetext'),
			/Total recorded TSS 70, Activities 5, Activities with a score 3/
		);
		const toggleFitness = trends.getByRole('button', { name: 'Fitness (CTL)' });
		await toggleFitness.click();
		await trends.getByRole('button', { name: 'Fatigue (ATL)' }).click();
		assert.equal(await toggleFitness.getAttribute('aria-pressed'), 'false');
		const load = trends.getByRole('slider');
		if (mobile) await load.tap();
		else await load.hover();
		assert.match(
			await load.getAttribute('aria-valuetext'),
			/Fitness \(CTL\).*Fatigue \(ATL\).*Form \(TSB\)/
		);
		for (const [range, days] of [
			['3M', '90'],
			['1Y', '365'],
			['All', '3650'],
			['6M', '180']
		]) {
			await main.getByRole('button', { name: range, exact: true }).click();
			await page.getByRole('img', { name: 'Current form -125.0, Very fatigued' }).waitFor();
			assert.equal(analysisQuery.get('days'), days);
			assert.equal(await toggleFitness.getAttribute('aria-pressed'), 'false');
			assert.equal(
				await main.getByRole('button', { name: 'Monthly' }).getAttribute('aria-pressed'),
				'true'
			);
			for (const chart of [load, stress]) {
				const tickBoxes = await chart.locator('.date-tick').evaluateAll((ticks) =>
					ticks.map((tick) => {
						const { left, right } = tick.getBoundingClientRect();
						return { label: tick.textContent, left, right };
					})
				);
				assert.ok(
					tickBoxes.every((tick, i) => i === 0 || tick.left > tickBoxes[i - 1].right),
					`Overlapping labels at ${width}px in ${range}: ${JSON.stringify(tickBoxes)}`
				);
			}
			await noPageOverflow('/analysis ' + range);
		}
		await open('/analysis');
		await page.getByRole('heading', { name: 'Fresh, but training load is low' }).waitFor();
		assert.equal(analysisQuery.has('member'), false);
		await noPageOverflow('/analysis');
		if (!mobile) {
			await page.emulateMedia({ colorScheme: 'dark' });
			await page.locator('html[data-theme="dark"]').waitFor();
			selfHistory = 'very-fresh';
			await page.reload();
			const marker = main.getByRole('img', { name: 'Current form +125.0, Very fresh' });
			await marker.waitFor();
			assert.equal(
				await marker.locator('.pin').evaluate((pin) => getComputedStyle(pin).left),
				await marker.evaluate((item) => getComputedStyle(item).width)
			);
			await noPageOverflow('/analysis extreme positive form');
			selfHistory = 'empty';
			await page.reload();
			await main.getByRole('heading', { name: 'Insufficient training data' }).waitFor();
			await main.getByText('No activities in this period.', { exact: true }).waitFor();
			assert.equal(await main.getByRole('slider').count(), 0);
			selfHistory = 'unscored';
			await page.reload();
			await main.getByRole('heading', { name: 'Insufficient training data' }).waitFor();
			await stress.press('End');
			assert.match(
				await stress.getAttribute('aria-valuetext'),
				/Total recorded TSS Unavailable, Activities 1/
			);
		}
		await open('/records?member=mira');
		await page.getByText('Viewing Mira', { exact: true }).filter({ visible: true }).waitFor();
		await page.getByText('54 km/h', { exact: true }).waitFor();
		await page.getByLabel('Exclude downhills').check();
		await page.getByRole('button', { name: 'Return to my data' }).click();
		await page.waitForURL(origin + '/records?exclude_downhills=true');
		await page.getByText('28.8 km/h', { exact: true }).waitFor();
		await page.reload();
		await page.getByText('28.8 km/h', { exact: true }).waitFor();
		await noPageOverflow('/records');
		await open('/athlete');
		await page.waitForURL(origin + '/health');
		await page.getByRole('heading', { name: 'My body' }).waitFor();
		await noPageOverflow('/health');
		for (const [path, heading] of [
			['/gear', 'Gear'],
			['/connections', 'Connections'],
			['/settings', 'Settings'],
			['/routes/waasland', 'Waasland route planner']
		]) {
			await open(path);
			await main.getByRole('heading', { name: heading, exact: true }).waitFor();
			await noPageOverflow(path);
			if (path === '/settings') {
				await main.getByText('Jun 2, 2026 16:30', { exact: true }).waitFor();
				assert.equal(await main.getByLabel('Time format', { exact: true }).count(), 0);
				const name = main.getByRole('textbox', { name: 'Display name' });
				assert.equal(await name.inputValue(), 'Ada');
				if (width === 360 || width === 1440) {
					const save = main.getByRole('button', { name: 'Save display name' });
					for (const blank of ['', '   ']) {
						await name.fill(blank);
						await save.click();
						await main.getByRole('status').getByText('Enter a display name.').waitFor();
						assert.equal(await name.getAttribute('aria-invalid'), 'true');
					}
					assert.deepEqual(profileUpdates, []);
					await name.fill(' Mira ');
					await name.press('Enter');
					await main
						.getByRole('status')
						.getByText('Could not save your display name. Try again.')
						.waitFor();
					assert.equal(await name.inputValue(), ' Mira ');
					await save.click();
					await main.getByRole('status').getByText('Display name saved.').waitFor();
					assert.equal(await name.inputValue(), 'Mira');
					assert.deepEqual(
						profileUpdates,
						Array(2).fill({ fieldMask: ['display_name'], displayName: 'Mira' })
					);
					if (mobile) await page.getByRole('button', { name: 'Profile menu' }).click();
					await page.getByText('Mira', { exact: true }).filter({ visible: true }).waitFor();
					await noPageOverflow(path);
					await page.reload();
					assert.equal(await name.inputValue(), 'Mira');
					await open('/');
					await main
						.getByRole('heading', { name: /Good (morning|afternoon|evening), Mira/ })
						.waitFor();
				}
			}
			if (path === '/settings' && width === 390) {
				await main.getByText('Dark', { exact: true }).click();
				assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark');
				await page.reload();
				assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark');
			}
		}
		await open('/activities/new');
		await main.getByLabel('Start date', { exact: true }).fill('2026-06-02');
		const startClock = main.getByLabel('Start time in 24-hour format');
		await startClock.fill('24:00');
		assert.equal(await startClock.evaluate((input) => input.checkValidity()), false);
		await startClock.fill('08:05');
		await main.getByLabel('Duration (min)', { exact: true }).fill('60');
		await noPageOverflow('/activities/new');
		const submitted = page.waitForRequest((request) =>
			request.url().endsWith('/api/activities/manual')
		);
		await main.locator('button[type="submit"]').click();
		await submitted;
		await page.waitForURL(origin + '/activities/' + recent.id);
		assert.equal(manualActivity.start_time, '2026-06-02T06:05:00.000Z');
		assert.equal(manualActivity.timezone, 'Europe/Brussels');
		assert.deepEqual(errors, []);
		await page.close();
	}
	console.log(
		'Peloton views, navigation, filters, and responsive layouts passed at 360px, 390px, 424px, 430px, 900px, and 1440px.'
	);
} finally {
	await browser.close();
}
