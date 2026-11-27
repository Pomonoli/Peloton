#!/usr/bin/env python3
"""Populate the isolated preview through Peloton's normal HTTP APIs."""

import datetime as dt
import http.cookiejar
import importlib
import json
import math
import os
from pathlib import Path
import random
import sys
import urllib.error
import urllib.parse
import urllib.request

sample = importlib.import_module("dev-sample-data")


def request(client, path, data=None, *, form=False):
    headers = {}
    if data is not None:
        headers["Content-Type"] = (
            "application/x-www-form-urlencoded" if form else "application/json"
        )
        data = (urllib.parse.urlencode(data) if form else json.dumps(data)).encode()
    try:
        with client.open(urllib.request.Request(BASE + path, data, headers), timeout=30) as response:
            if form:
                return urllib.parse.parse_qs(urllib.parse.urlsplit(response.geturl()).query)
            return json.load(response)
    except urllib.error.HTTPError as error:
        raise RuntimeError(f"{path}: HTTP {error.code}") from error


def sign_in(email, password, temporary=None):
    # The seed client only talks to loopback, including when the server sets
    # Secure cookies for the authenticated HTTPS preview.
    jar = http.cookiejar.CookieJar(http.cookiejar.DefaultCookiePolicy(secure_protocols=("http",)))
    client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    result = request(client, "/auth/password", {"identifier": email, "password": password}, form=True)
    if result.get("error") == ["invalid_credentials"] and temporary:
        result = request(client, "/auth/password", {"identifier": email, "password": temporary}, form=True)
    if "set_password" in result:
        request(client, "/auth/password/required", {
            "challenge_token": result["set_password"][0], "password": password,
        })
    request(client, "/cairn.v1.AuthService/GetCurrentUser", {})
    return client


def encoded_polyline(track):
    output = []
    previous = [0, 0]
    for point in track:
        for axis, coordinate in enumerate(point):
            value = round(coordinate * 100_000)
            delta = value - previous[axis]
            previous[axis] = value
            encoded = ~(delta << 1) if delta < 0 else delta << 1
            while encoded >= 32:
                output.append(chr((encoded & 31) + 95))
                encoded >>= 5
            output.append(chr(encoded + 63))
    return "".join(output)


def seed(state):
    anchor = state / "fixture-date"
    if not anchor.exists():
        anchor.write_text(dt.datetime.now(dt.timezone.utc).date().isoformat())
    day = dt.datetime.fromisoformat(anchor.read_text()).replace(tzinfo=dt.timezone.utc, hour=9)
    admin = sign_in("admin@peloton.local", os.environ["PREVIEW_ADMIN_PASSWORD"],
                    os.environ["PREVIEW_BOOTSTRAP_PASSWORD"])
    clients = [admin]
    users = request(admin, "/api/admin/users").get("users", [])
    for username, display, key in (
        ("maya", "Maya Reed", "PREVIEW_RIDER_ONE_PASSWORD"),
        ("noah", "Noah Vale", "PREVIEW_RIDER_TWO_PASSWORD"),
    ):
        email = f"{username}@peloton.local"
        password = os.environ[key]
        if not any(user["username"] == username for user in users):
            request(admin, "/api/admin/users", {
                "username": username, "email": email, "display_name": display,
                "role": "user", "initial_password": os.environ["PREVIEW_BOOTSTRAP_PASSWORD"],
            })
        clients.append(sign_in(email, password, os.environ["PREVIEW_BOOTSTRAP_PASSWORD"]))

    for course, title in enumerate(("Polder Loop", "Heathland Circuit", "Riverside Loop")):
        track = [
            (51.20 + 0.07 * math.sin(i * math.tau / 720),
             3.75 + 0.35 * course + 0.12 * math.cos(i * math.tau / 720))
            for i in range(721)
        ]
        segments = request(admin, "/api/segments/browse").get("segments", [])
        segment_name = f"{title} — North Section"
        if not any(segment["name"] == segment_name for segment in segments):
            request(admin, "/api/segments", {
                "name": segment_name, "activity_type": "ride", "scope": "instance",
                "polyline": encoded_polyline(track[70:105]), "precision": 5, "backfill": True,
            })
        for rider, client in enumerate(clients):
            for repeat in range(2):
                start = day - dt.timedelta(days=2 + course * 9 + repeat * 4 + rider)
                name = f"{title} · {'Tempo' if repeat else 'Endurance'}"
                body = sample.gpx(name, "Ride", start, track, 6.2 + rider + repeat * 0.3,
                                  14 + course * 10, random.Random(course * 10 + rider * 2 + repeat))
                status, result = sample.upload(
                    client, BASE, f"preview-{course}-{rider}-{repeat}.gpx", body,
                )
                if status != 200 or not result.get("activity_id"):
                    raise RuntimeError(f"Fixture upload failed: HTTP {status}, {result}")
                request(client, "/cairn.v1.ActivityService/UpdateActivity", {
                    "id": result["activity_id"], "field_mask": ["title", "privacy"],
                    "title": name, "privacy": "PRIVACY_PUBLIC",
                })
    print("Seeded 3 riders, 18 cycling activities and 3 shared segments.")


if __name__ == "__main__":
    BASE = f"http://127.0.0.1:{int(sys.argv[1])}"
    seed(Path(sys.argv[2]))
