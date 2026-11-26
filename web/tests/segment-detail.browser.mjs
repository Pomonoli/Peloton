// Run against npm run build:static with fixture responses.
import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const origin = 'http://peloton-segments.test';
const build = fileURLToPath(new URL('../build/', import.meta.url));
const browser = await chromium.launch({
	executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
});
const riders = [
	{ id: 'ada', username: 'ada', display_name: 'Ada' },
	{ id: 'sam', username: 'sam', display_name: 'Sam' },
	{ id: 'mira', username: 'mira', display_name: 'Mira' },
	{ id: 'chris', username: 'chris', display_name: 'Chris' },
	{ id: 'eve', username: 'eve', display_name: 'Eve' }
];
const segments = [
	{ id: 'canal', name: 'Canal Sprint' },
	{ id: 'forest', name: 'Forest Road' }
].map((segment) => ({
	...segment,
	source: 'strava_catalog',
	activity_type: 'ride',
	distance_m: 1300,
	best_elapsed_s: 120,
	average_speed_kmh: 36,
	local_kom_holder: 'Ada',
	member_count: 3,
	my_attempts: 1,
	start: null,
	polyline: '_p~iF~ps|U_ulLnnqC',
	polyline_precision: 5
}));
const startTime = '2026-11-22T13:00:00Z';
const efforts = (id, uid) => [
	{
		id: `${id}-${uid}-effort`,
		activity_id: `${id}-${uid}-ride`,
		start_time: startTime,
		elapsed_s: 120,
		personal_rank: 1,
		is_personal_record: true
	}
];
const entries = riders.map((rider, index) => ({
	rank: index + 1,
	user_id: rider.id,
	display_name: rider.display_name,
	elapsed_s: 120 + index * 10,
	average_speed_kmh: 36,
	all_efforts_speed_kmh: 34,
	effort_count: 1,
	start_time: startTime,
	activity_id: `${rider.id}-best-ride`
}));

try {
	for (const { member, width } of [
		{ member: null, width: 1440 },
		{ member: 'mira', width: 424 }
	]) {
		const page = await browser.newPage({
			viewport: { width, height: width === 424 ? 933 : 900 },
			locale: 'en-US',
			timezoneId: 'UTC'
		});
		page.setDefaultTimeout(5000);
		const requests = [];
		const documents = [];
		const navigations = [];
		const errors = [];
		let mapStyles = 0;
		let firstDetail = true;
		let failure;
		const detailGate = Promise.withResolvers();
		const memberQuery = member ? `?member=${member}` : '';
		page.on('request', (request) => {
			const url = new URL(request.url());
			if (request.resourceType() === 'document') documents.push(url.pathname);
			if (/^\/api\/segments\/(canal|forest)(\/|$)/.test(url.pathname)) requests.push(url);
		});
		page.on('framenavigated', (frame) => {
			if (frame === page.mainFrame() && frame.url() !== navigations.at(-1))
				navigations.push(frame.url());
		});
		page.on('pageerror', (error) => errors.push(error.message));

		await page.route('**/*', async (route) => {
			const url = new URL(route.request().url());
			if (url.hostname === 'tiles.openfreemap.org') {
				mapStyles++;
				return route.fulfill({ json: { version: 8, sources: {}, layers: [] } });
			}
			if (url.origin !== origin) return route.abort();
			const { pathname, searchParams } = url;
			if (pathname === '/cairn.v1.AuthService/GetCurrentUser') {
				return route.fulfill({
					json: { user: { id: 'ada', username: 'ada', displayName: 'Ada' } }
				});
			}
			if (pathname === '/api/members/mira') return route.fulfill({ json: riders[2] });
			if (pathname === '/api/segments/catalog') {
				return route.fulfill({ json: { segments, has_more: false } });
			}
			const match = pathname.match(/^\/api\/segments\/(canal|forest)(?:\/(leaderboard|efforts))?$/);
			if (match) {
				const [, id, resource] = match;
				if (failure?.id === id && failure.resource === resource) {
					return route.fulfill({ status: failure.status });
				}
				if (resource === 'leaderboard') return route.fulfill({ json: { entries } });
				if (resource === 'efforts') {
					return route.fulfill({ json: { efforts: efforts(id, searchParams.get('user_id')) } });
				}
				if (firstDetail) {
					firstDetail = false;
					await detailGate.promise;
				}
				return route.fulfill({
					json: {
						segment: segments.find((segment) => segment.id === id),
						efforts: efforts(id, searchParams.get('member') ?? 'ada')
					}
				});
			}
			if (pathname.startsWith('/api/')) return route.fulfill({ json: {} });
			return route.fulfill({
				path: resolve(
					build,
					route.request().resourceType() === 'document' ? 'index.html' : '.' + pathname
				)
			});
		});

		const main = page.getByRole('main');
		const indicator = page.getByRole('progressbar', { includeHidden: true });
		const chart = main.locator('.uplot');
		const defaultRiders = riders.map(({ id }) => id).sort();
		const clickSegment = async (id, name) => {
			await main.getByRole('link', { name, exact: true }).click();
			await page.waitForURL(origin + '/segment/' + id + memberQuery);
		};
		const expectSegment = async (id, name) => {
			await main.getByRole('heading', { name, exact: true }).waitFor();
			await main.getByRole('heading', { name: 'Local KOM' }).waitFor();
			await main.getByText(member ? 'Mira’s Best' : 'Your Best', { exact: true }).waitFor();
			await main.locator('.maplibregl-canvas').waitFor();
			await chart.waitFor();
		};
		const assertRequests = async (
			id,
			historyRiders,
			{ detail = false, board = false, cutoff } = {}
		) => {
			await indicator.waitFor({ state: 'hidden' });
			const paths = [...requests];
			const base = `/api/segments/${id}`;
			assert.deepEqual(paths.map((url) => url.pathname).sort(), [
				...(detail ? [base] : []),
				...historyRiders.map(() => base + '/efforts'),
				...(board ? [base + '/leaderboard'] : [])
			]);
			assert.deepEqual(
				paths
					.filter((url) => url.pathname.endsWith('/efforts'))
					.map((url) => url.searchParams.get('user_id'))
					.sort(),
				historyRiders
			);
			for (const url of paths) {
				if (url.pathname.endsWith('/efforts')) {
					assert.equal(url.searchParams.has('member'), false);
					assert.equal(url.searchParams.get('valid_only'), 'true');
				} else {
					assert.equal(url.searchParams.get('member'), member);
				}
				if (url.pathname !== base) assert.equal(url.searchParams.get('from'), cutoff ?? null);
			}
			requests.length = 0;
		};

		try {
			await page.goto(origin + '/segments' + memberQuery);
			await clickSegment('canal', 'Canal Sprint');
			// Force the shared indicator through both visibility transitions during a fetch.
			await indicator.waitFor({ state: 'visible' });
			detailGate.resolve();
			await expectSegment('canal', 'Canal Sprint');
			await assertRequests('canal', defaultRiders, { detail: true, board: true });
			assert.deepEqual(navigations, [
				origin + '/segments' + memberQuery,
				origin + '/segment/canal' + memberQuery
			]);
			assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));

			const legend = main.getByRole('group', { name: 'Riders in effort history' });
			assert.equal(await legend.getByRole('button', { pressed: true }).count(), 5);
			const rider = legend.getByRole('button', { name: 'Eve' });
			await rider.click();
			assert.equal(await rider.getAttribute('aria-pressed'), 'false');
			await rider.click();
			assert.equal(await rider.getAttribute('aria-pressed'), 'true');
			await assertRequests('canal', []);
			assert.deepEqual(
				await main.locator('table').first().getByRole('columnheader').allTextContents(),
				['Rank', 'Rider', 'Time', 'Speed', 'Date']
			);
			const overviewTab = main.getByRole('tab', { name: 'Overview' });
			const leaderboardTab = main.getByRole('tab', { name: 'Leaderboard' });
			const attemptsTab = main.getByRole('tab', { name: 'Attempts' });
			await overviewTab.focus();
			await page.keyboard.press('ArrowRight');
			assert.equal(await leaderboardTab.getAttribute('aria-selected'), 'true');
			const fullBoard = main.getByRole('tabpanel', { name: 'Leaderboard' });
			assert.equal(await fullBoard.locator('tbody tr').count(), 5);
			await fullBoard.getByRole('columnheader', { name: 'Attempts' }).waitFor();
			await page.keyboard.press('End');
			assert.equal(await attemptsTab.getAttribute('aria-selected'), 'true');
			const attempts = main.getByRole('tabpanel', { name: 'Attempts' });
			await attempts.getByRole('heading', { name: 'Performance trend' }).waitFor();
			await attempts
				.getByRole('heading', { name: member ? 'Member’s efforts' : 'Your efforts' })
				.waitFor();
			await attempts
				.locator(`a[href="/activities/${'canal'}-${member ?? 'ada'}-ride${memberQuery}"]`)
				.waitFor();
			await attempts.getByText('PR', { exact: true }).waitFor();
			await overviewTab.click();
			await chart.waitFor();
			await assertRequests('canal', []);

			const period = main.getByLabel('Period');
			const cutoff = await page.evaluate(() =>
				new Date(new Date().getFullYear(), 0, 1).toISOString()
			);
			await period.selectOption('year');
			await chart.waitFor();
			await assertRequests('canal', defaultRiders, { board: true, cutoff });
			assert.equal(mapStyles, 1);

			await main.getByRole('link', { name: 'Segments', exact: true }).click();
			await clickSegment('forest', 'Forest Road');
			await expectSegment('forest', 'Forest Road');
			await assertRequests('forest', defaultRiders, { detail: true, board: true });
			assert.equal(await main.getByRole('heading', { name: 'Canal Sprint' }).count(), 0);
			assert.equal(mapStyles, 2);

			failure = { id: 'forest', resource: 'leaderboard', status: 500 };
			await leaderboardTab.click();
			await period.selectOption('year');
			await main
				.getByRole('status')
				.getByText('Could not load the Local KOM leaderboard.')
				.waitFor();
			await assertRequests('forest', [], { board: true, cutoff });
			failure = { id: 'forest', resource: 'efforts', status: 500 };
			await main.getByRole('button', { name: 'Try again' }).click();
			await overviewTab.click();
			await main.getByRole('status').getByText('Unable to load effort history.').waitFor();
			await main.getByText('Loading…', { exact: true }).waitFor({ state: 'hidden' });
			await assertRequests('forest', defaultRiders, { board: true, cutoff });

			await main.getByRole('link', { name: 'Segments', exact: true }).click();
			failure = { id: 'canal', status: 401 };
			await clickSegment('canal', 'Canal Sprint');
			await main.getByRole('status').getByText('Could not load the segment.').waitFor();
			await assertRequests('canal', [], { detail: true });
			assert.deepEqual(navigations.slice(-4), [
				origin + '/segments' + memberQuery,
				origin + '/segment/forest' + memberQuery,
				origin + '/segments' + memberQuery,
				origin + '/segment/canal' + memberQuery
			]);
			assert.deepEqual(documents, ['/segments']);
			assert.equal(mapStyles, 2);
			assert.deepEqual(errors, []);
		} catch (error) {
			console.error({
				member,
				documents,
				navigations,
				mapStyles,
				errors,
				requests: requests.slice(0, 12).map(String),
				requestCount: requests.length
			});
			throw error;
		} finally {
			detailGate.resolve();
			await page.close();
		}
	}
	console.log('Segment details settled on desktop and in a mobile member view.');
} finally {
	await browser.close();
}
