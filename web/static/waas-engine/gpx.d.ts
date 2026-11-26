export function writeGPX(
 title: string,
 description: string,
 segments: readonly (readonly (readonly [latitude: string | number, longitude: string | number, elevation: number | null])[])[],
 waypoints?: readonly { point: readonly number[]; name: string }[]
): string;
