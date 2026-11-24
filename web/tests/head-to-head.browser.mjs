// Run npm run build:static and npx playwright-core install chromium first.
import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const origin = 'http://peloton-browser.test';
const build = fileURLToPath(new URL('../build/', import.meta.url));
const browser = await chromium.launch({
	executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
});
const you = { id: 'ada', username: 'ada', display_name: 'Ada', avatar_url: '' };
const rider = { id: 'mira', username: 'mira', display_name: 'Mira', avatar_url: '' };
const third = { id: 'sam', username: 'sam', display_name: 'Sam', avatar_url: '' };
const effort = (value, activity_id) => ({ value, activity_id });
const segment = (id, name, myTime, theirTime, winner) => ({
	id,
	name,
	distance_m: 1200,
	you: effort(myTime, 'ada-ride'),
	opponent: effort(theirTime, 'mira-ride'),
	you_speed_kmh: 4320 / myTime,
	opponent_speed_kmh: 4320 / theirTime,
	time_difference_s: Math.abs(theirTime - myTime),
	speed_difference_kmh: Math.abs(4320 / myTime - 4320 / theirTime),
	winner
});
const segments = [
	segment('canal', 'Canal Sprint', 100, 120, 'you'),
	segment('forest', 'Forest Climb', 150, 110, 'opponent'),
	segment('village', 'Village Road', 120, 120, 'tie')
];
const distance = {
	activity_type: 'ride',
	metric: 'pace',
	window_kind: 'distance',
	window_value: 5000
};
const records = [
	{
		...distance,
		you: effort(100, 'ada-ride'),
		opponent: effort(125, 'mira-ride'),
		winner: 'you',
		difference: 125,
		difference_percent: 25
	},
	{
		...distance,
		window_value: 20000,
		you: null,
		opponent: null,
		winner: '',
		difference: null,
		difference_percent: null
	}
];

try {
	for (const width of [320, 360, 390, 430, 1440]) {
		const mobile = width < 768;
		const page = await browser.newPage({ viewport: { width, height: 940 } });
		page.setDefaultTimeout(7500);
		const errors = [];
		let improved = false;
		page.on('pageerror', (error) => errors.push(error.message));

		await page.route('**/*', async (route) => {
			const url = new URL(route.request().url());
			if (url.origin !== origin) return route.abort();
			const path = url.pathname;
			const fixtures = {
				'/cairn.v1.AuthService/GetCurrentUser': {
					user: { id: you.id, username: you.username, displayName: you.display_name }
				},
				'/api/overview': {
					totals: { count: 0, distance_m: 0, moving_s: 0, elevation_gain_m: 0 },
					cycling_weeks: [],
					recent: []
				},
				'/api/riders': { members: [you, rider, third] },
				'/api/members/mira': rider,
				'/api/head-to-head': {
					first: url.searchParams.get('first') === 'sam' ? third : you,
					opponent: rider,
					segments: improved
						? [segment('canal', 'Canal Sprint', 100, 90, 'opponent'), ...segments.slice(1)]
						: segments,
					records,
					segment_score: improved
						? { wins: 0, losses: 2, ties: 1, comparable: 3 }
						: { wins: 1, losses: 1, ties: 1, comparable: 3 },
					record_score: { wins: 1, losses: 0, ties: 0, comparable: 1 }
				}
			};
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

		await page.goto(origin + '/head-to-head');
		const picker = page.getByLabel('Second rider', { exact: true });
		await picker.selectOption('mira');
		await page.waitForURL(origin + '/head-to-head?member=mira');
		await page.getByRole('tabpanel', { name: 'Segments' }).getByText('Canal Sprint').waitFor();
		assert.equal(await picker.locator('option[value="ada"]').count(), 0);

		await page.goto(origin + '/u/mira');
		await page.waitForURL(origin + '/?member=mira');
		if (mobile) {
			await page
				.getByRole('navigation', { name: 'Main navigation' })
				.getByRole('link', { name: 'Social' })
				.click();
		}
		await page.getByRole('link', { name: 'Head-to-Head', exact: true }).click();
		await page.waitForURL(origin + '/head-to-head?viewing=mira&second=mira');
		const panel = page.getByRole('tabpanel', { name: 'Segments' });
		const canal = panel.getByRole('row').filter({ hasText: 'Canal Sprint' });
		for (const label of ['1:40', '43.2 km/h', '2:00', '36.0 km/h']) {
			await canal.getByText(label, { exact: true }).waitFor();
		}
		assert.equal(
			await canal.getByRole('link', { name: 'Canal Sprint' }).getAttribute('href'),
			'/segment/canal'
		);
		assert.equal(
			await canal.getByRole('link', { name: '36.0 km/h' }).getAttribute('href'),
			'/activities/mira-ride'
		);
		assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
		assert.equal(await canal.evaluate((row) => getComputedStyle(row).display), 'table-row');
		assert(
			await canal.evaluate((row) => {
				const visibleCells = [...row.cells].filter((cell) => cell.offsetWidth > 0);
				return visibleCells.every((cell) => cell.getBoundingClientRect().right <= innerWidth);
			})
		);

		assert.equal(await panel.getByRole('columnheader', { name: /Gap|Difference/ }).count(), 0);
		assert.deepEqual(await page.getByRole('main').locator('dl dt').allTextContents(), [
			'Ada wins',
			'Ties',
			'Mira wins',
			'Total'
		]);
		await panel.getByRole('button', { name: 'Mira', exact: true }).click();
		await panel.locator('tbody tr').first().getByRole('link', { name: 'Forest Climb' }).waitFor();
		const showFilters = () => panel.getByRole('button', { name: /^Filters/ }).click();
		const done = () => page.getByRole('dialog').getByRole('button', { name: 'Done' }).click();
		if (mobile) await showFilters();
		await panel.getByLabel('Filter by winner').selectOption('tie');
		if (mobile) await done();
		await panel.getByRole('link', { name: 'Village Road' }).waitFor();
		assert.equal(await panel.locator('tbody tr').count(), 1);

		await page.getByRole('tab', { name: 'Segments' }).focus();
		await page.keyboard.press('ArrowRight');
		const pr = page.getByRole('tabpanel', { name: 'Personal Records' });
		await pr.getByLabel('Search record categories').fill('5 km');
		await pr.getByText('8:20', { exact: true }).waitFor();
		await pr.getByText('36 km/h', { exact: true }).waitFor();
		assert.equal(await pr.getByRole('columnheader', { name: /Gap|Difference/ }).count(), 0);
		assert.equal(await pr.locator('tbody tr').count(), 1);
		assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));

		await page.getByRole('tab', { name: 'Segments' }).click();
		if (mobile) await showFilters();
		assert.equal(await panel.getByLabel('Filter by winner').inputValue(), 'tie');
		await panel.getByLabel('Filter by winner').selectOption('');
		if (mobile) await done();
		improved = true;
		await page.getByLabel('First rider', { exact: true }).selectOption('sam');
		await page.waitForURL(
			(url) => url.searchParams.get('first') === 'sam' && url.searchParams.get('member') === 'mira'
		);
		assert.equal(await picker.locator('option[value="sam"]').count(), 0);
		await canal.getByText('1:30', { exact: true }).waitFor();
		assert.equal(await page.getByRole('main').locator('dl dd').first().textContent(), '0');
		assert.deepEqual(errors, []);
		await page.close();
	}
	console.log('Head-to-Head passed at 320px, 360px, 390px, 430px, and 1440px.');
} finally {
	await browser.close();
}
