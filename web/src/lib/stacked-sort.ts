export type SortDirection = 'asc' | 'desc';
export type SortCriterion<Column extends string> = { column: Column; direction: SortDirection };

export function promoteSort<Column extends string>(
	order: SortCriterion<Column>[],
	column: Column
): SortCriterion<Column>[] {
	const active = order.find((criterion) => criterion.column === column);
	const direction =
		order[0]?.column === column && active?.direction === 'asc'
			? 'desc'
			: order[0]?.column === column
				? 'asc'
				: (active?.direction ?? 'asc');
	return [{ column, direction }, ...order.filter((criterion) => criterion.column !== column)];
}

export function compareSortedValues(
	left: string | number | null | undefined,
	right: string | number | null | undefined,
	direction: SortDirection
) {
	if (left == null) return right == null ? 0 : 1;
	if (right == null) return -1;
	const result =
		typeof left === 'number' && typeof right === 'number'
			? left - right
			: String(left).localeCompare(String(right));
	return direction === 'asc' ? result : -result;
}
