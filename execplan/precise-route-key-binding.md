# Bind one gateway route without changing upstream keys

This is a living ExecPlan. Progress, discoveries, decisions, and outcomes must stay consistent with the implementation and its verified results.

## Purpose / Big Picture

An administrator can bind an existing upstream API key to one saved gateway route without renaming that key, changing its group, quota, expiry, model restrictions, or touching sibling routes. A route is one gateway group's link to a monitored upstream channel and an upstream source group. The gateway stores an encrypted copy of the upstream key for forwarding; it must not silently retain that copy when the route is changed to a different source group.

The existing group-wide Ensure action currently updates existing upstream keys and only searches the first 100 keys. This change keeps its response contract while making it fill missing bindings only. It skips disabled and already bound routes, reuses an exactly matching key without updating it, and creates a dedicated key only after a complete successful search establishes no such key exists.

## Progress

- [x] (2026-10-02, UTC+8) Confirmed existing source-selection and upstream-update hazards in current upstream main, commit 821e613779a72bcf426577e0f37920315f25b77f.
- [x] Created an isolated checkout and fix branch; old local checkouts have broken worktree metadata and remain untouched.
- [x] Agreed on the storage, service, and HTTP interfaces and assigned non-overlapping files to three implementation agents.
- [x] Implement and test source identity plus a conditional single-row binding update.
- [x] Implement read-only explicit binding and safe missing-only Ensure behavior.
- [x] Add the authenticated HTTP endpoint, request validation, and administrator documentation.
- [x] Integrate and run focused, full, race, static, and disposable MySQL tests; complete the review loop.
- [x] Publish the task branch and verify upstream PR #25 is open against main; codex-pr-flow confirmed reuse rather than duplication.

## Surprises & Discoveries

The checked-in main branch differs from the customized LAN deployment. The fix is developed against current repository main; do not replace the deployed service wholesale with this checkout because that could discard unrelated local features. The old workspace's .git file points into a removed temporary directory, so it is used only for historical context.

`backend/gateway/admin_routes.go:ensureSourceAPIKey` calls UpdateAPIKey for existing keys, explicitly changing names, source groups, and NewAPI quota/expiry. Some upstream implementations also reset fields omitted from their PUT request. Avoiding every UpdateAPIKey call during binding removes that class of side effect.

`backend/storage/gateway.go:SaveForGroup` currently compares only source kind, channel, and provider when keeping the old key; it omits the upstream group identity.

Final integration found that MySQL's ordinary initial SELECT in SaveForGroup could read an old key, allow a precise binding to commit, and then save that old key back. The transaction now locks its source snapshot with FOR UPDATE. A real MySQL 8.0.46 regression test passes; temporarily removing the lock makes that test fail because the competing binding succeeds too early. Restoring the lock passes again.

Default case-insensitive MySQL collation is unsuitable for strict encrypted-key/source-name snapshot comparisons; the conditional update now compares strings as binary in MySQL. SQLite and MySQL tests cover legacy NULL ciphertext/update time. Local statement logging is suppressed for this credential-bearing update only, including database failures.

## Decision Log

Decision: add `PUT /api/gateway/groups/:id/routes/:route_id/key` with a single positive `source_api_key_id` input. The explicit endpoint binds an existing key and never creates or edits a remote key. Administrators may use the separate channel API to create a dedicated key before binding. This makes the requested mutation scope unambiguous. Date: 2026-10-02.

Decision: prefer stable upstream group IDs. If either side has a group ID, both must have the same ID. Only when both IDs are absent may trimmed group names identify a source. A rename with the same ID preserves a binding; a different ID with the same name does not. Date: 2026-10-02.

Decision: use a database conditional update, also called compare-and-swap: write only if the route's identity and previous binding still match the snapshot read before network access. Concurrent edits return a conflict instead of overwriting another operation. The update writes only key ID, key name, encrypted secret, and update time. Date: 2026-10-02.

Decision: serialize key-binding work within one Service instance to prevent overlapping Ensure requests from creating duplicate keys. Storage checks still guard against edits outside that lock. This is not a distributed lock; multiple application instances cannot promise exactly-once remote creation without provider support. Date: 2026-10-02.

Decision: remove the unused UpdateAPIKey capability from the gateway's ChannelAPI interface. Channel administration can still update keys separately, but route binding and Ensure no longer have this operation available through their dependency. Date: 2026-10-02.

## Outcomes & Retrospective

Implementation and local validation are complete. The authenticated endpoint binds an existing key by ID, validates complete inventory and source identity, rejects expired/disabled/masked credentials, and changes only the target binding. Repeating a binding is idempotent, while remote name/secret changes can be refreshed explicitly. Ensure skips disabled/already-bound routes and makes no upstream update calls.

Verification on 2026-10-02: affected-package tests pass; go test ./... passes; go vet ./... passes; git diff --check passes. Storage, gateway, and API packages pass -race with the disposable MySQL integration tests enabled. MySQL tests exercised normal binding, legacy NULLs, case-only source/cipher conflicts, stale configuration saving, and the row-lock interleaving. The lock negative control failed as expected before the fix was restored.

The review loop identified concurrency, compatibility, and credential-log risks; the follow-up review after targeted fixes showed no scored regressions. Remaining documented tradeoffs are serialized administration operations and no distributed exactly-once key creation. There is no schema migration or frontend redesign.

Parallel storage/service/API lanes produced the implementation; centralized integration caught and repaired the cross-layer MySQL race. The reusable acceptance gate is target/sibling equality, zero upstream updates, complete inventory, adversarial concurrency, and a real-database negative control. No live service configuration, upstream keys, or deployment was changed by this coding task. Publishing is tracked separately below.

Publication: bejix/upstream-ops PR #25 is open from the task branch on the authorized personal fork (the upstream account has read-only permission). The first code commit is c0ac22b; the final documentation-only commit records this outcome. GitHub reported no status checks at verification time, so remote CI is not claimed. The disposable MySQL instance has been shut down after tests. No merge or deployment was performed.

## Context and Orientation

The Go module is `github.com/bejix/upstream-ops`. `backend/api/gateway_admin.go` registers authenticated administrator routes and maps errors to HTTP responses. `backend/gateway/admin_routes.go` implements route saving and group-wide Ensure. `backend/gateway/service.go` owns shared dependencies; `service_admin_delegates.go` exposes administrator methods through Service. `backend/gateway/admin_types.go` defines JSON request and response types. `backend/storage/gateway.go` persists route records using GORM and supports SQLite/MySQL. `backend/connector/connector.go` normalizes NewAPI and Sub2API key/group records; `ChannelAPI` provides list, create, update, and reveal operations.

The new binding service must not use the remote Update operation. Reveal is a read of a selected key's secret; its plaintext remains in memory until encryption and must not appear in responses, errors, or tests' logs. Existing encrypted secrets use `json:"-"` on the storage model.

## Plan of Work

The storage agent owns `backend/storage/gateway.go` and new binding tests. It repairs source comparison and implements `BindSourceKeyIfUnchanged` with an exported conflict error. The service agent owns gateway service/type/delegate files and its tests. It validates route/group ownership, source identity, key status and expiry, complete paginated inventory, and idempotent binding. The HTTP agent owns the API handler, API tests, and `docs/gateway-route-key-binding.md`. The primary agent owns integration, this plan, validation, and the PR.

Milestone one is a tested storage primitive: changing source group clears stale credentials, whereas saving a same-ID rename retains them. Milestone two is a service that binds just one route and makes zero upstream update calls. Milestone three exposes the authenticated endpoint and exercises it through the real router with fake upstreams and a temporary database. Each milestone is locally testable without production credentials or network access to vendors.

## Concrete Steps

From the repository root, run focused tests as implementation lands:

    go test ./backend/storage -run 'GatewayRoute|BindSourceKey'
    go test ./backend/gateway -run 'BindRouteKey|EnsureRouteKeys'
    go test ./backend/api -run 'GatewayRouteKey'

Then run the affected packages with the race detector, the complete Go suite, and static checks:

    go test -race ./backend/storage ./backend/gateway ./backend/api
    go test ./...
    go vet ./...
    git diff --check

Inspect every result and record any unrelated baseline failures rather than changing unrelated behavior. Commit only the scoped fix and documentation on the task branch, push, and ensure one PR exists using codex-pr-flow. If direct upstream push is unavailable, use an authorized personal fork; do not pretend a local commit is a published PR.

## Validation and Acceptance

A request binding one route succeeds and leaves every sibling route unchanged. The upstream fake's update counter remains zero and its key name, group, quota, expiry, IP/model restrictions are unchanged. Keys beyond page one can be selected; incomplete, inconsistent, or duplicate pagination fails without creating or binding a key. A key from another source group, unknown ID, wrong group ownership, invalid body, or unsupported source is rejected without writes. A changed/deleted route between read and save yields a conflict. Repeating a successful binding does not create a key or alter unrelated fields. No plaintext secret is serialized in success or error responses. Concurrent Ensure calls in one service do not create duplicate keys. Source-ID changes invalidate old bindings, including same-name/different-ID changes.

## Idempotence and Recovery

Tests use disposable local databases and fake providers. No schema migration is planned. Explicit binding is repeatable; partial remote failures leave the local route untouched. Ensure may create a key before a later reveal or local update fails; the key must not be automatically deleted because other routes may already use it. Its stable identity must allow a later verified retry to find it. Ambiguous remote create failures must not lead to blind duplicate creation. Revert the scoped commit to remove this API; live binding changes require their own configuration backup and must not be conflated with a code rollback.

## Artifacts and Notes

The reviewable deliverables are the Go fix, behavioral regression tests, the administrator API guide, and a pull request. This plan contains no tokens, passwords, vendor credentials, or live snapshots.

## Interfaces and Dependencies

The storage method is `func (r *GatewayRoutes) BindSourceKeyIfUnchanged(expected *GatewayRoute, keyID int64, keyName, keyCipher string) error`, with `ErrGatewayRouteChanged` for a stale expected row. The shared source comparison is `SameGatewayRouteSource(left, right GatewayRoute) bool`.

The service method is `BindRouteKey(ctx context.Context, groupID, routeID uint, in BindRouteKeyInput) (*storage.GatewayRoute, error)`. The input field is `SourceAPIKeyID int64` with JSON name `source_api_key_id`. The handler returns 200 for a bound route, 400 for invalid or incompatible input, 404 for an absent route/group or route outside the requested group, and 409 for concurrent modification. The existing group-wide Ensure response shape remains unchanged.

Revision 2026-10-02: initial plan based on current source inspection; implementation assigned in separate file ownership lanes.

Revision 2026-10-02: integrated all lanes, corrected fixture snapshots to compare database-read timestamps, repaired MySQL lost-update/logging/collation boundaries, and completed local verification before PR publication.
