# Desktop device display information

Apply migration `255_desktop_device_info.sql` before enabling this API version.
The migration adds only `desktop_device_info`; it does not modify credentials,
model groups, subscriptions, usage or balances. The normal migration runner embeds
and applies this migration during backend startup.

Authenticated account endpoint:

```http
PUT /api/v1/desktop/devices/:device_id/info
Authorization: Bearer <account JWT>
Content-Type: application/json

{"installation_name":"Work Mac","os":"macos","client_type":"desktop_dev","client_version":"0.23.0"}
```

All four fields must be present strings. Unknown and duplicate fields, nulls,
trailing JSON and invalid UTF-8 are rejected. The body is capped at 4096 bytes.
`installation_name` accepts at most 80 Unicode characters; `client_version` accepts
at most 64. Both reject Unicode control/format characters and are trimmed before
storage. Empty or whitespace-only names and versions are rejected. Installations
without reported metadata omit `device_info`; clients may display “Unknown”.
`os` is `macos`, `windows` or `linux`. `client_type` is `desktop`, `desktop_dev` or
`browser`. These values are untrusted display labels and never grant capabilities.

The response uses the existing success envelope with the saved four-field object
in `data`. Invalid information returns 400; an absent or foreign device association
returns 404. Account authentication remains required. An installation may update
metadata only after an existing permanent device identity or historical desktop
lease belongs to that account. Expired, revoked and soft-deleted authorizations
remain historical associations; metadata updates never restore them.

`GET /api/v1/desktop/devices` adds `credential_type` (`device` or `lease`) and an
optional `device_info` with the same four fields. A permanent identity takes
precedence when the installation also has older leases. Installations whose
clients never reported metadata omit `device_info`. Metadata is scoped by
`(user_id, device_id)`; reusing an installation UUID across accounts shares no
labels. Concurrent updates replace one complete metadata row atomically.
