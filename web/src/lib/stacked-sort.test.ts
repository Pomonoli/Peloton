import { expect, it } from 'vitest';
import { compareSortedValues, promoteSort } from './stacked-sort.js';

it('promotes a column while preserving prior criteria and toggles the primary', () => {
	const speed = promoteSort([], 'speed' as 'speed' | 'kom');
	const kom = promoteSort(speed, 'kom');
	expect(kom).toEqual([{ column: 'kom', direction: 'asc' }, ...speed]);
	expect(promoteSort(kom, 'kom')).toEqual([{ column: 'kom', direction: 'desc' }, ...speed]);
	expect(promoteSort(kom, 'speed')).toEqual([...speed, { column: 'kom', direction: 'asc' }]);
	expect(compareSortedValues(100, 9, 'asc')).toBeGreaterThan(0);
	expect(compareSortedValues(null, 9, 'desc')).toBeGreaterThan(0);
});
