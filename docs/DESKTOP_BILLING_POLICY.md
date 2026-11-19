# Managed credentials using personal balance

Managed desktop credentials accept an optional `billing_policy` of `balance_only`.
The empty value retains existing account funding behavior. The policy belongs to
the API key and does not change the account's funding preferences.

Apply migration `263_managed_key_billing_policy.sql` before starting this version.
Every inference replica must support this policy before restricted credentials
are issued. Older binaries ignore the new field; a mixed-version deployment
cannot enforce the restriction. Revoke restricted credentials before rolling back
to a binary without policy support.

## Discover and select models

With the user's JWT, request:

```text
GET /api/v1/desktop/bootstrap?device_id=UUID&billing_policy=balance_only
GET /api/v1/desktop/models?billing_policy=balance_only
```

Bootstrap adds `capabilities.managed_billing_policy_balance_only: 1`. Its existing
`managed_device_key` capability still reflects the installation's rollout policy.
The filtered model list contains only available non-subscription groups that the
user can access and pay for using their own balance. Empty balance returns an
empty list. Protocol, capability, pricing, quota and permission checks remain the
existing desktop checks. Unknown policy values return `INVALID_BILLING_POLICY`.

Model fields retain their existing names: `id` is the catalog option, `model` is
the request body model, `api_mode` selects the protocol, and `state` and
`reason_code` describe availability. There is no public `agent_verified` field.
The filtered catalog reports `billing_source: "balance"`.

## Issue a permanent device key

```text
POST /api/v1/desktop/device-keys
Authorization: Bearer USER_JWT
Idempotency-Key: UNIQUE_OPERATION_ID
Content-Type: application/json
```

```json
{"device_id":"00000000-0000-4000-8000-000000000001","billing_policy":"balance_only"}
```

The response data is:

```json
{
  "key_id": "42",
  "device_id": "00000000-0000-4000-8000-000000000001",
  "version": 1,
  "status": "active",
  "expires_at": null,
  "billing_policy": "balance_only",
  "api_key": "sk-aino-device-example"
}
```

Require the response policy to equal `balance_only` before enabling inference.
An older server can ignore unknown request fields, so HTTP success alone does not
acknowledge the policy. Creation retains existing recent login and rollout gates.
GET, reuse and idempotency replay return metadata without the secret.

Rotation and reauthorization retain the persisted policy, including when older
clients omit the field. A nonempty policy different from the existing credential
returns `DESKTOP_BILLING_POLICY_CONFLICT`; use a new installation identity. A
restricted key cannot be widened by rotation or reauthorization.

Legacy lease issuance at `POST /api/v1/desktop/credentials` accepts the same policy
alongside its existing device, connection grant, purpose and model fields, and
echoes the persisted policy. A grant cannot reuse a key with a conflicting policy;
create a new connection grant instead.

## Inference and settlement

Send the device key as Bearer authentication, the selected catalog `id` in
`X-Aino-Model-ID`, and catalog `model` in the request body. The persisted policy
restricts every new funding decision to `personal_balance` with the caller as
payer. Subscription groups and team funding are rejected. Insufficient balance
does not fall back to a subscription or a team.

Reservations read the persisted key policy again inside the existing funding
transaction. Existing personal reservations, wallet debits, settlement,
deduplication and usage records remain the accounting path. Simple run mode does
not enable restricted inference because it cannot guarantee paid settlement.

The wallet summary remains unchanged. `balance`, `frozen_balance` and
`available_balance` are decimal strings. Holds are already removed from `balance`;
clients must not subtract `frozen_balance` from `available_balance` again.
