# Group priority routing

Set **Group priority** in the group's scheduling settings (modern or classic UI).
Priority accepts integers from `0` to `100`; larger numbers run first.
Existing groups default to `0`, preserving their existing routing behavior.

For the same client model, eligible groups are considered in descending priority
order. Route preferences and group/credential weights apply within a priority
tier. Credential affinity cannot select a lower tier while a higher tier is
available. After retryable failures exhaust a tier's eligible credentials, the
next tier is used. Disabled, cooling, blacklisted, filtered, or zero-weight
candidates are not eligible. The configured retry budget still applies; client
errors and responses already committed to the client keep their existing retry
rules. Requests tied to a specific upstream resource still respect that binding.

Example: groups A and B have priority `100`, and C has priority `0`. A and B
share traffic using their configured weights. C is used only when A and B have
no remaining eligible candidates for that request. Each new request starts at
the highest available priority.

The create API and `PUT /api/groups/{id}/settings` accept `priority`. Setting
`{"priority":0}` resets a group to the default tier; omitting it preserves the
current value. Settings and group collection responses include `priority`.

## Container upgrade

The **Fork container image** GitHub Actions workflow runs on every push to `main`
(and on manual dispatch). It tests the backend, builds both web interfaces,
verifies PostgreSQL/MySQL migration compatibility, and then publishes AMD64 and
ARM64 images to `ghcr.io/<repository>:latest`. A separate `sha-<full-commit>` tag
identifies the exact source revision, and `:priority` is kept as a legacy alias.

For this fork, the Compose image reference never has to change again:

```yaml
image: ghcr.io/wangxingfan/gpt-load:latest
```

Because upstream synchronization also lands on `main`, every sync rebuilds
`:latest` with your fork's features included. Update a running deployment with
`docker compose pull` followed by `docker compose up -d`; pin `:sha-<full-commit>`
when you need the exact revision that was tested.

Keep the existing Compose project name, data volume, `.env`, database connection,
and encryption key. Before switching, stop the service and back up the complete
data volume and any external database. The database, `auth.key`, and
`encryption.key` belong together. Do not run `docker compose down -v`.

Migration `0029_group_priority` adds a non-null integer column with default zero
and preserves existing groups and credentials. This applies to the compatible
2.x schema through migration 0028; the floating upstream `:2` tag alone does not
identify a specific schema version. An older image may reject the new migration
ledger. To roll back, restore the matching pre-upgrade database backup as well
as the old image.

Migration `0030_group_priority_range` clamps priorities stored before the range
was narrowed, so an existing `999` becomes `100` and a stored `-5` becomes `0`
instead of failing startup validation. The clamp preserves relative ordering
between tiers. Groups already inside `0`-`100` are untouched.
