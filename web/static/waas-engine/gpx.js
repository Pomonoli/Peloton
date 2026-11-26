function escapeXML(value) {
	return String(value).replace(
		/[&<>"']/g,
		(c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&apos;' })[c]
	);
}

export function writeGPX(title, description, segments, waypoints = []) {
	const metadata =
		`<name>${escapeXML(title)}</name>` +
		(description ? `<desc>${escapeXML(description)}</desc>` : '');
	const waypointXML = waypoints
		.map(
			({ point, name }) =>
				`<wpt lat="${point[0].toFixed(6)}" lon="${point[1].toFixed(6)}"><name>${escapeXML(name)}</name></wpt>`
		)
		.join('\n');
	const trackXML = segments
		.map(
			(points) =>
				'<trkseg>\n' +
				points
					.map(([lat, lon, elevation]) => {
						const altitude = Number.isFinite(elevation) ? `<ele>${elevation.toFixed(1)}</ele>` : '';
						return `<trkpt lat="${lat}" lon="${lon}">${altitude}</trkpt>`;
					})
					.join('\n') +
				'\n</trkseg>'
		)
		.join('\n');
	return `<?xml version="1.0" encoding="UTF-8"?>\n<gpx version="1.1" creator="Peloton Waasland planner" xmlns="http://www.topografix.com/GPX/1/1">\n<metadata>${metadata}</metadata>\n${waypointXML}\n<trk><name>${escapeXML(title)}</name><type>cycling</type>${trackXML}</trk></gpx>\n`;
}
