# v1.2 browser-managed Consumer Web session — design backlog only

Status: **NOT IMPLEMENTED**. POC and authentication work are frozen until
explicitly reopened. R25 remains unchanged. This document does not authorize
browser startup, login, provider requests, or production integration.

## Target

Gemini Pro membership → dedicated persistent browser profile → user interactive
login → browser-owned Google session lifecycle → SUB2 browser-managed authenticated
context → existing API/task architecture.

One interactive login is the desired initial experience, not a promise that
Google will never require reauthentication. No manual cookie copy/export/import,
no migration of Account #2 cookies into the profile, and no daily cookie-refresh
workflow. Profiles are sensitive credential stores even without cookie export.

## Minimal replacement boundary

Replace only Consumer Web session authentication/transport lifecycle behind the
existing service boundary. Reuse Account, Scheduler, PersonalProviderResolver,
Unified Personal API Key, Model Mapping, Proxy, video tasks, compatible R15
request construction, R19 bounded polling, and R20 durable trace.

Do not add a second account pool, scheduler, model mapping system, or API surface.
Never silently change provider or proxy. A browser adapter must enforce the
existing account/proxy binding and refuse incompatible configurations.

Candidate integration files, subject to a passing isolated POC:

- `backend/internal/service/consumer_web_session.go`: preserve public lifecycle
  contract; delegate browser-managed acquisition/closure behind an adapter.
- A new isolated browser-session transport implementation under
  `backend/internal/service/`, not edits to frozen R15 request semantics.
- Existing dependency wiring (`backend/internal/handler/wire.go` and generated
  server wiring) only when production integration is separately authorized.
- Existing video task/recovery/trace consumers should remain unchanged unless a
  demonstrated interface mismatch requires review.

Database changes are not required for the isolated POC. Production may need an
opaque profile binding/lifecycle status attached to the existing Account;
schema migration is TBD, never a parallel credential/account database. Avoid
persisting browser-derived tokens in Account credentials.

Proposed profile root: `%LOCALAPPDATA%\Sub2 Personal\browser-profiles\<opaque-id>`.
Use a dedicated POC root first, outside the repository and ordinary Chrome
profile. No existing profile copies. Restrict local access; never log or upload
profile data. One owner per profile; clean shutdown before reopen. Do not kill
the user's ordinary browser.

A browser process is required while the context is used; indefinite background
residency is not yet required/proven. Profile persistence is disk lifecycle, not
just keeping a tab open. Loopback-only debugging endpoints, if needed, require
access-control review; no public remote-debug port or disabled sandbox/security.

## Explicitly gated POC backlog

1. Verify installed browser and supported automation can create, cleanly close,
   and reopen the same dedicated persistent profile without touching R25/8080.
2. User performs interactive login directly; agent does not receive credentials.
3. Verify authenticated context, clean browser restart, then authentication
   without another user login. A new tab is not a restart test.
4. Validate browser-native Bootstrap/otAQ AVAILABLE with safe metadata only.
   UI login alone is not sufficient evidence of the required RPC capability.
5. Only after auth PASS, one Image → Video operation using compatible frozen
   request contract, bounded polling, then verified final media retrieval.
6. Record direct evidence separately from inference. Classify PASS only with
   restart survival, AVAILABLE and obtained video; auth-only success is PARTIAL.
7. Review browser version drift, proxy parity, process crashes/profile locks,
   account isolation, resource consumption, reauthentication UX, cancellation,
   cleanup, and dependency distribution before proposing integration.

No second generation or unbounded retries. Failure stops at the first concrete
blocker; no automatic return to PSID/PSIDTS patching. Do not infer long-term
durability from a single browser restart. No POC feasibility or adoption decision
has yet been demonstrated.

## Rollback / product safety

Keep production DB, credentials, R25 source/runtime, and existing profiles
untouched. Stop only the POC-owned browser on completion. Profile deletion is a
separate explicit user decision because it discards login state. Retain only
non-secret evidence; no raw cookies, tokens, provider bodies, or profile archives.
No commits/releases or new background services/tasks without separate approval.
