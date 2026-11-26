export function downloadGPX(gpx: string, name: string) {
	const blob = new Blob([gpx], { type: 'application/gpx+xml;charset=utf-8' });
	const url = URL.createObjectURL(blob);
	const anchor = document.createElement('a');
	anchor.href = url;
	anchor.download = name;
	document.body.append(anchor);
	anchor.click();
	anchor.remove();
	window.setTimeout(() => URL.revokeObjectURL(url), 2000);
}
