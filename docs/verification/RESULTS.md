# Sequential verification record

Base: haiwei816/miniverse-studio-backend, existing S0.1 retained.
Branch: codex/s0-s12-mvp. Date: 2026-11-06 Asia/Taipei.

Tests are recorded only after execution. Sandbox results do not certify production readiness.

## S0 — PASS (sandbox)
- Existing configuration and router tests retained; `go test -race ./...` and `go vet ./...` exited 0 in golang:1.27.3.
- Compose build initially failed (missing go.sum); fixed with go mod tidy, rebuilt successfully.
- `node tests/s0.mjs`: health/ready/request IDs and 100 concurrent requests PASS.
- Migration second execution exited 0. MySQL stop: /health=200, /ready=503; restart: /ready=200.
- No production deployment performed. Migration files are explicitly idempotent SQL, serialized by a MySQL advisory lock; later non-idempotent DDL requires a versioned migration upgrade.

## S1 — PASS (sandbox)
- Go race tests and vet exited 0.
- tests/s1.mjs: credential verification, ownership, game scope and logout PASS.
- tests/s1-isolation.mjs: same account has independent game profiles PASS.
- tests/s1-expiry.mjs: expired token rejected with 401 PASS.
- Sandbox game registration requires an authenticated player; production mode disables guest login and development registration.

## S2 — PASS (Godot 4.5.1 sandbox)
- Official Godot runtime imported the SDK project and executed test_sdk.gd.
- Login/profile, atomic local cache, offline failure preserving progress, reconnect and logout PASS (exit 0).
- Host editor had a pre-existing invalid editor settings file; project imported and tests still passed. Subsequent editor runs use a portable settings directory in work/tools.
- Platform secure credential storage remains a game integration responsibility; SDK keeps access tokens in memory.

## S3 — PASS (sandbox)
- Initial 8-way concurrent write test exposed MySQL deadlocks from concurrent INSERT IGNORE. Added bounded retry for MySQL deadlock/lock-timeout only; no retry on other failures.
- Rebuilt and reran tests/s3.mjs: exactly one write succeeded, seven returned 409; cross-player isolation, gzip/checksum, deletion tombstone, history and restore PASS.
- Godot test_saves.gd: conflict preserves both pending local data and remote snapshot PASS.
- History is retained in MySQL; API returns latest 50 metadata entries. Restore is a new revision and requires current revision.

## S4 — PASS (sandbox)
- Go checks and tests/s4.mjs passed: event dedupe, client trust cannot be forged, capped cumulative progress, daily/signin/milestone rules.
- Event dedupe and progress updates share a MySQL transaction. Activity window uses server UTC time. Client events can only advance client_allowed activities.
- Sandbox activity setup is disabled outside sandbox; production publishing will use GM approval.

## S5 — PASS (sandbox)
- Go race/vet checks passed. First build encountered Docker registry EOF; retry succeeded.
- tests/s5.mjs: event completion automatically produces one mail; eight repeated reward requests and eight concurrent claims produce one grant and exactly +10 wood; cross-player claim rejected.
- Inventory is server-authoritative and scoped per game. Clients display the returned inventory instead of independently crediting local currency.

## S6 — PASS (sandbox)
- Go checks and tests/s6.mjs passed: max/sum/min modes, submission dedupe, nearby rank, cutoff and repeated settlement.
- MySQL remains authoritative; ties use first reached time then player ID. Top 3 receive a fixed configured reward. Ordinary client scores are explicitly client_unverified.
- Ranking queries currently materialize one season; intended for small MVP seasons. Paginated/window queries are a documented scaling follow-up.

## S7 — PASS (sandbox)
- Go checks and tests/s7.mjs: GM credentials, role identity, authorized game read, cross-game rejection and separation from player tokens PASS.
- Vue3 + Element Plus front end built successfully. Initial npm audit found 3 high advisories; upgraded Vue to 3.5.45 and Vite to 7.3.7, rebuilt; audit reports zero vulnerabilities.
- Browser interaction check will follow the S8 operations UI. Bundle size warning remains; acceptable for local GM MVP.

## S8 — PASS (sandbox)
- Go checks and tests/s8.mjs passed: support cannot write, self approval forbidden, repeated approval idempotent, cancellation state checked, activity publish/new-version rollback/stop and audit chain verified.
- GM operations UI built; browser verified actual login and displayed populated request records (executed/cancelled), approval controls, rollback and announcement form.
- Rollback is a newly reviewed version copied from history. Batch grants are atomic for at most 100 players; no asynchronous mass-mail job is introduced.

## S9 — PASS (sandbox)
- tests/s9.mjs passed for sky-island and card-game using one account: profile, revisioned save, event, reward mail, authoritative inventory and leaderboard.
- Support role cannot access card-game; authorized admin can.
- Godot 4.5.1 imported both sample scenes with exit 0 and no script errors.
- Samples demonstrate different event/config integration through the same SDK and backend; they are small integration demos, not finished games.

## S10 — PASS within authorized sandbox scope; real platform acceptance NOT MET
- Adapter unit tests passed for signature, application scope, expired/forged proof and production rejection.
- tests/s10.mjs passed: Steam/TapTap mapping, nonce replay, binding conflict, cross-application rejection and safe unlink.
- No Steam/TapTap real key or real client ticket was used. Both real integrations remain UNVERIFIED; production login explicitly returns 503 rather than accepting mocks.

## S11 — PASS within authorized mock scope; real AI UNVERIFIED
- Go tests include JSON Schema rejection and daily simulation rewards. tests/s11.mjs passed for four generated briefs, deterministic fault injection/repair, unknown item/schema rejection and independent publication approval.
- Generated plans remain drafts. Ordinary generation cannot publish; a separate GM request and another reviewer are required.
- Evaluation stored in planner-evaluation.json. Rates and zero cost refer only to deterministic mock cases, not model quality or real AI spend.
- GM UI includes generation/validation/simulation and loads the draft for review.

## S12 — PASS (local Docker sandbox)
- C++20 CMake/CTest passed: fixed golden vector score=36/hash=8660768014306689972; 1000 repeated results, illegal actions/version/seed and terminal-state validation.
- Go race/vet checks passed; Protobuf clients generated by protoc.
- tests/s12.mjs passed through actual Go → gRPC → C++ service and MySQL leaderboard; forged/unfinished/illegal scores rejected and raw score endpoint cannot write verified board.
- No real competitive match authenticity is claimed: client-chosen seed/replay does not prove an actual played match. See rulecore.md.

## Final integration and hardening
- Complete S0–S12 HTTP regression passed on the persistent sandbox after correcting the S10 test fixture to use a unique external subject each run.
- Migration repeat and tamper checks passed for all 12 files; line endings are canonicalized before checksumming.
- Production boundary test passed: guest login, sandbox GM credentials, pre-existing sandbox player/GM sessions and mock provider login are rejected outside sandbox.
- Recovery test passed: API restart retained sessions and saves; MySQL outage/recovery preserved data; RuleCore outage rejected validation while ordinary saves remained available.
- Local load sample: 100 authenticated profile requests, concurrency 10, zero errors, p95 about 29.5 ms. This is a short local sanity sample, not a capacity or production benchmark.
- Godot SDK and save/conflict tests rerun successfully. Vue production build passed; npm audit reported zero vulnerabilities. GM container served HTTP 200.
- Added and passed an in-flight HTTP shutdown test after code review found that returning immediately from Serve could otherwise terminate requests before Shutdown finished.
- First independent Compose startup lacked project-specific image names; explicit shared image tags fixed reproducibility. Fresh MySQL initialization also exposed an early socket-only health result; health now requires a real TCP query by the business database user.
- Failed GM transactions and abnormal score/replay requests have authorized-game diagnostics in the audit store; they are recorded after transaction rollback and contain no request body or credentials.

## Remaining limits
- Steam/TapTap real tickets and AI-model generation: UNVERIFIED; sandbox/mock paths only.
- GitHub Actions configured, not executed remotely. No public deployment performed.
- Rule replay proves deterministic rule compliance, not actual-match authenticity; client-selected seeds are unsuitable for high-value competitive rewards.
- MVP list limits, full-season rank materialization, synchronous batches (100 players), internal plaintext gRPC, and compiler-bearing C++ image are documented follow-up work.
- Production backup/restore, real platform secure credential storage, capacity planning and production admin provisioning are not certified by this local run.

## Clean-environment acceptance — PASS
- A separate Compose project (miniverse-clean-final) initialized a new MySQL data volume using the corrected TCP business-user healthcheck.
- All migrations applied on first startup; full tests/e2e.mjs S0–S12 suite passed, including failed-operation diagnostics.
- Migration repeat no-op and modified-file rejection also passed against that fresh database.
- Source logs retained: final-go.log, final-e2e.log, clean-e2e.log and clean-migrations.log. The clean test containers were stopped without deleting data volumes.
