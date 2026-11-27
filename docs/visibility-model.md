# Member visibility

Peloton is invite-only. **Social → Riders** opens member profiles and the shared views of their cycling data. **Social → Head-to-Head** compares members' records and segment efforts. Members can read available historical rides and derived statistics without changing sharing settings. Share links also require a signed-in member.

Provider-restricted rides and rides hidden by an administrator are excluded from other members' reads and community rankings. Soft-deleted rides are excluded. A segment definition retains its own scope. Geographic privacy zones mask locations for other members, including in rider history. When a history page has activities, the server loads the profile owner's zones once for that page. Shared photos use re-encoded variants. Activity edits, original source files, and source management require ownership.

## Existing databases

Migration 88 established member visibility for legacy profiles and activities. The database value `public` used by that migration means visible to eligible signed-in members. Existing activities, moderation flags, privacy zones, source identities, and account credentials continue to use the existing migrations.

The former feeds, follow and block controls, clubs, kudos, comments, ActivityPub federation, and profile audience settings have been retired. The notification inbox, delivery preferences, and outbound notification webhooks have also been retired. Their schema and historical rows remain in place. Account email, live activity and admin events, and inbound provider webhooks remain available.

Head-to-Head Shared results apply the same activity visibility rules. A member's GPS privacy zones also exclude affected sections from another member's shared distance, duration, and encounter locations. [Shared activities](ride-analytics.md#shared-activities)
