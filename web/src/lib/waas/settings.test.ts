import { describe, expect, it } from 'vitest';
import { defaults, optionsQuery, readOptions } from './settings.js';
import { parseDataset } from './service.js';

describe('Waasland shared route settings', () => {
	it('round-trips priorities, direction, zones and places', () => {
		const options = {
			...defaults,
			profile: 'climb' as const,
			length: 115,
			direction: 'ccw' as const,
			places: ['kerk-temse', 'kerk-elversele'],
			zones: { cuesta: 'include' as const, oostberg: 'avoid' as const }
		};
		expect(readOptions(new URLSearchParams(optionsQuery(options)))).toEqual(options);
	});
	it('bounds untrusted shared settings and rejects unknown places', () => {
		const result = readOptions(
			new URLSearchParams(
				'km=999&laps=6&margin=-5&profile=unknown&via=unknown&via=kerk-temse&via=kerk-temse'
			)
		);
		expect(result.length).toBe(200);
		expect(result.laps).toBe(3);
		expect(result.tolerance).toBe(0.5);
		expect(result.profile).toBe('climb');
		expect(result.places).toEqual(['kerk-temse']);
	});
});
describe('public dataset parser', () => {
	it('reads the JSON assignment without executing the surrounding script', () => {
		expect(
			parseDataset(
				'window.WAAS_CIRCUITS ||= {}; window.WAAS_CIRCUITS["climb-sn"] = [["a"]];',
				'WAAS_CIRCUITS["climb-sn"]'
			)
		).toEqual([['a']]);
	});
	it.each(['window.DATA = (() => 1)();', 'window.DATA = {}; alert(1);', 'window.OTHER = {};'])(
		'rejects non-JSON datasets: %s',
		(source) => {
			expect(() => parseDataset(source, 'DATA')).toThrow();
		}
	);
});
