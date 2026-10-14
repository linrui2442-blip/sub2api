# Personal v1.1.8 closeout — R25 frozen

Decision date: 2026-10-14. Closeout preparation only; no commit, tag, release,
new binary, runtime restart, or provider acceptance is authorized by this document.

## Scope and readiness

v1.1.8 closes with the existing Personal gateway and multimodal capabilities.
Consumer Web video is **EXPERIMENTAL** and session durability is **not a
v1.1.8 release blocker**. This is a product scope decision, not a claim that
video acceptance passed. Core readiness is assessed against the targeted
regression and repository evidence below; packaging/publication still requires
separate approval. No indefinite provider compatibility guarantee is implied.

R25 artifact: `dist-dev/sub2api-personal-v1.1.8-video-dev-r25.exe`.
SHA256: `ABB5A6E0D17B5B80C047EE7904782B63F52A8031F4C21E1133D66FB1978CA74E`.
Source identity: `2825c8d9e3a0380ab23b6c7d9eecc0e821e04293-dirty-r25-9dae824ac8fa`.
This is a dirty-worktree development artifact, not a published release/checkpoint commit.

## Feature matrix

| Capability | Classification | Evidence and boundary |
| --- | --- | --- |
| Gemini image generation | STABLE | `acceptance-files/m1.1-gemini-3.1-flash-image-evidence.json`: membership request HTTP 200 and inline image; `m1.4-generation-*.json`, `m1.4-edit-*.json`; Gemini image adapter tests. Historical runtime evidence, not rerun during closeout. |
| Image understanding | STABLE | `acceptance-files/m2-b1-image-understanding-evidence.json`: HTTP 200 and text response to image input. |
| Video understanding | KNOWN_LIMITATION | `acceptance-files/p0-multimodal-final-convergence-evidence.json` B2 records PASS_HISTORICAL_HANDOFF, not equivalent to independently revalidated current runtime evidence. No new failure is established. |
| Existing Personal API gateway | STABLE | `PERSONAL_EDITION_V1.md`, `PERSONAL_EDITION_V1_1_DESKTOP.md`; Personal routing, SQLite and gateway tests. |
| Account / Scheduler / OAuth / Proxy / Model Mapping | STABLE | Existing shared architecture retained; Personal repository/router tests. This classification does not extend to fragile Consumer Web cookie authentication. |
| Consumer Web Image → Video | EXPERIMENTAL | Task route and HTTP 202 creation work; latest task failed before generation. |
| Browser-managed Consumer Web authentication | NOT_COMPLETED | v1.2 design only; no persistent-browser restart or video POC PASS. |

## Completed bounded fixes

- R23: encrypted auxiliary Consumer Web session-state persistence and restore
  across restart, with expiry/deletion handling and CAS tests. It does not prove
  that every restored session remains authenticated indefinitely.
- R24: full credential-revision changes no longer falsely invalidate a session
  when core PSID/PSIDTS identity is unchanged. Core changes remain terminal;
  full database CAS protection is retained. Short runtime acceptance passed;
  later long-idle fresh authentication failed.
- R25: recoverable/unknown background refresh errors no longer permanently end
  the maintenance chain. Bounded delay is 10 → 20 → 40 minutes (40-minute cap).
  Explicit terminal auth/core-change cases stop; retry scheduling is not auth
  success. Offline resilience tests passed; runtime recovery is not proven.

The historical refresh error's exact cause and its causality for later auth
failure remain unproven. These fixes do not establish indefinite Google Web
Session durability.

## Video state and latest evidence

- `/v1/videos` Image → Video route exists; real task creation returned HTTP 202.
- R15 browser-success request contract is frozen.
- R19 pending/no-video READ_CHAT handling continues bounded polling; covered by
  `consumer_web_video_poll_lifecycle_test.go`. Latest R25 run did not reach it.
- R20 encrypted durable provider trace exists, with restart/recovery tests in
  `consumer_web_video_trace_test.go`.
- Latest task: `video_9159699FCBB180F301DA3CA4156CE000`.
  `FINAL_TASK_STATUS=FAILED_AUTH_REQUIRED`, `STREAMGENERATE_SENT=NO`,
  `READ_CHAT_POLL_COUNT=0`.
- Task-correlated logs confirm Account #2 selection and AcquireSession failure
  with cause UNKNOWN. They do not establish the exact Bootstrap/otAQ/Rotate
  boundary. Do not reinterpret UNKNOWN as invalid cookies or provider rejection.

## Known limitations and freeze

Session acquisition may become unavailable after idle time; long-term automatic
Google Web Session durability is not proven. Image → Video may fail before
StreamGenerate. Daily manual cookie refresh is not an acceptable product solution.

Freeze R25 auth/session work: no R26, no PSID/PSIDTS lifecycle patching, auxiliary
8→7 investigation, PSIDCC/DBSC work, overnight tests, background Rotate changes,
normal-response core-cookie persistence changes, or browser POC execution unless
explicitly reopened for v1.2. Do not ask for cookies or perform another sync.

Consumer Web video/auth limitations are non-blocking under this release scope.
Any failing core regression remains blocking. Browser-managed auth is planned,
not implemented; see [v1.2 plan](V1_2_BROWSER_SESSION_PLAN.md).

## Closeout verification

Established offline commands (from `backend`, offline Go environment):

```text
go test ./internal/service ./internal/handler ./internal/repository ./cmd/server -run '^(TestConsumerWeb|TestPersonal|Test.*Credential.*CAS)' -count=1 -timeout=180s
go test -tags=embed ./internal/web ./internal/server/routes -count=1 -timeout=180s
git diff --check
```

Both targeted Go commands passed on 2026-10-14. Covered groups: R23 auxiliary
durability, R24 core-scoped staleness, R25 refresh-error resilience, R19 polling,
R20 durable trace, auth-probe/session tests, and embedded static asset/routes.
No unrelated core regression was found within this selected suite.
`V1_1_8_CORE_RELEASE_READY=YES` within the agreed scope; this is not publication
authorization or a fresh end-to-end provider certification.

The seven unrelated `zz_*` tests
are excluded by test-name selection, not moved/deleted. This is not a full
`go test ./...` claim. No provider, auth-probe, or video request is made by closeout.
