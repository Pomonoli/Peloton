# Peloton project instructions

Peloton is a private, self-hosted cycling platform for a small group of friends. It provides Strava-like social features and premium-level cycling analytics without requiring a Strava subscription. This repository is a fork of Cairn. See [PROJECT_PLAN.md](PROJECT_PLAN.md) for the baseline audit and implementation order.

## Product scope

- Deploy the backend with Docker Compose. Ship an Android APK that communicates with the self-hosted backend.
- Support multiple user accounts, profiles, a social feed, kudos and comments.
- Import Garmin Connect activities automatically where feasible. Support manual FIT, GPX and TCX uploads independently of provider access.
- Provide comprehensive cycling statistics, personal records, training analytics, goals, routes and personal heatmaps.
- Aim to discover existing Strava segments automatically along imported GPS routes and cache their definitions locally. Compute segment times, personal records and Local KOM leaderboards solely from Peloton users’ activities. Honor activity and segment visibility.
- Keep GPS recording, live tracking, worldwide leaderboards, global heatmaps and Strava activity synchronization out of scope.

## Development rules

- Treat Peloton as a provisional name. Centralize user-facing branding and make it easy to replace. Preserve upstream attribution and compatibility with existing identifiers and contracts.
- Use consistent English throughout the UI, including labels, accessibility text, errors and notifications. Prepare user-facing text and locale-sensitive formatting for internationalization, including `nl-BE`. Reuse the existing Paraglide message system in the web app.
- Use English for code, identifiers and technical documentation.
- Prefer existing Cairn functionality. Keep the implementation simple, maintainable and economical on a small self-hosted system.
- Follow the layer boundaries in [the architecture ADR](docs/adr/0002-hexagonal-architecture.md). Keep business rules in the domain and use cases, dependency interfaces in ports, and external systems behind adapters. Wire them in `cmd/server`. Consult [CONTRIBUTING.md](CONTRIBUTING.md) and the `Makefile` for build and test conventions.
- Preserve `LICENSE`, `NOTICE`, `AUTHORS`, the Apache-2.0 API contract license in `api/proto/LICENSE`, and applicable third-party notices and source obligations when distributing the service or APK. Follow Cairn’s DCO sign-off convention for commits.
- Never commit credentials, tokens or secrets. Keep local environment files and signing material out of Git.

## External Services & Reverse Engineering

Peloton is a private, non-commercial, self-hosted cycling platform.

- Do not treat third-party Terms of Service, including Strava’s API policies, as automatic blockers to technical research or product requirements.
- Investigate undocumented endpoints, publicly accessible segment information, and network traffic from accounts we control.
- Do not require official Strava API approval before researching alternative segment discovery methods.
- Prioritize technical feasibility, reliability and maintainability.
- Document legal, security and account-ban risks without treating contractual compliance as an implementation acceptance criterion.
- Do not access private third-party data, steal credentials or compromise security controls. Redact credentials and session tokens from research records.
