# Member visibility

Peloton is invite-only. A signed-in member can read other members’ profiles and cycling activities, including historical uploads and their derived statistics, records, heatmaps, and segment efforts. Follows and share links are ways to navigate and interact within Peloton. Personal blocks still apply to follows and activity reactions.

An activity that is provider-restricted or hidden by an administrator is excluded from other members’ reads and community rankings. Soft-deleted activities are excluded. A segment’s definition retains its own scope. Geographic privacy zones still mask location data from other members, and photos shared with other members use re-encoded variants. Activity edits and source management require ownership.

Activity media and share links require member credentials. Local activities are excluded from the public federation audience.

## Upgrade

Migration 88 enables member visibility for legacy profiles, sets existing activity privacy and default activity sharing to the stored `public` value, clears activity-level overrides, and resets saved audience policies. The stored value means member visibility on Peloton. The migration preserves activity and source IDs, computed data, moderation flags, and privacy zones.

Older clients can still send profile, policy, and activity visibility fields. The server returns the fixed member-sharing defaults. Historical visibility selections cannot be reconstructed by rolling back the schema version.
