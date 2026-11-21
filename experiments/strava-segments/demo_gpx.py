#!/usr/bin/env python3
"""Add simulated timestamps to an independent planned-route GPX for a smoke test."""
import argparse
from datetime import datetime, timedelta, timezone
from math import asin, ceil, cos, radians, sin, sqrt
import xml.etree.ElementTree as ET


def distance(a, b):
    lat1, lon1 = map(radians, a)
    lat2, lon2 = map(radians, b)
    haversine = sin((lat2 - lat1) / 2) ** 2
    haversine += cos(lat1) * cos(lat2) * sin((lon2 - lon1) / 2) ** 2
    return 12_742_000 * asin(sqrt(haversine))


def write_fixture(source, target, reverse):
    points = [
        (float(p.attrib["lat"]), float(p.attrib["lon"]))
        for p in ET.parse(source).findall(".//{*}trkpt")
    ]
    if len(points) < 2:
        raise ValueError("The source needs at least two track points")
    if reverse:
        points.reverse()

    root = ET.Element("gpx", {"xmlns": "http://www.topografix.com/GPX/1/1",
                              "version": "1.1", "creator": "Peloton proof of concept"})
    track = ET.SubElement(root, "trk")
    direction = "reverse" if reverse else "forward"
    ET.SubElement(track, "name").text = f"Blue Loop fixture ({direction}, simulated time)"
    ET.SubElement(track, "type").text = "cycling"
    segment = ET.SubElement(track, "trkseg")
    start = datetime(2026, 1, 1, tzinfo=timezone.utc)

    def add_point(lat, lon, elapsed):
        point = ET.SubElement(segment, "trkpt", {"lat": f"{lat:.8f}", "lon": f"{lon:.8f}"})
        time = start + timedelta(seconds=elapsed)
        ET.SubElement(point, "time").text = time.isoformat(timespec="milliseconds")

    elapsed = 0
    add_point(*points[0], elapsed)
    for a, b in zip(points, points[1:]):
        length = distance(a, b)
        if length == 0:
            continue
        steps = ceil(length / 8)
        for step in range(1, steps + 1):
            fraction = step / steps
            lat, lon = (x + (y - x) * fraction for x, y in zip(a, b))
            add_point(lat, lon, elapsed + (length * fraction) / 5)
        elapsed += length / 5
    ET.ElementTree(root).write(target, encoding="utf-8", xml_declaration=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source")
    parser.add_argument("target")
    parser.add_argument("--reverse", action="store_true")
    args = parser.parse_args()
    write_fixture(args.source, args.target, args.reverse)
