# OpenAI Quota Query Diagnosis

## Scope

This diagnosis covers the Personal v1.1 Desktop OpenAI OAuth quota display and its existing cooldown/scheduler path. It does not change OAuth, Gateway routing, account pools, or SQLite persistence.

## Root cause

The defect was inherited from the v1.0.1 baseline. After reading the upstream quota snapshot, `AccountUsageService` queried local usage statistics for both the 5-hour and 7-day periods. A successful local statistics query created an empty `UsageProgress` object even when the upstream response had not supplied that quota window.

The frontend correctly renders only non-null windows. The synthetic object therefore appeared as `0% now`, making an absent upstream window look like a real exhausted/reset window.

The fix only attaches local request/token statistics to quota windows that already exist in the upstream-derived snapshot. A missing 5-hour or 7-day window remains absent.

## Query call chain

- Account table `Query` action
- frontend account usage API with forced refresh/cache bypass
- admin account-usage handler
- `AccountUsageService.getOpenAIUsage`
- normal OpenAI OAuth: a minimal ChatGPT Responses probe and `x-codex-*` rate-limit headers
- Spark shadow account: OpenAI `/backend-api/wham/usage`
- normalized 5-hour/7-day snapshot plus local window statistics

The Query action reads current quota information. It does not reset quota.

## Reset-credit controls

- `次数 / Credits` queries the available upstream reset-credit count and expiration metadata.
- `重置 / Reset` consumes one upstream reset credit to restore the eligible current window.
- The existing frontend labels remain unchanged as requested; this quota-window fix contains no copy or i18n changes.

## 429, cooldown, and scheduler

The existing production path remains unchanged:

1. Gateway receives a real upstream 429.
2. Rate-limit headers are persisted and the reset time is calculated.
3. The account is marked rate-limited/cooling down.
4. `Account.IsSchedulable` excludes it until the reset time.
5. The account becomes eligible again after cooldown recovery.

No synthetic 429 was sent during this diagnosis. Failure-induced live failover is therefore `NOT EXERCISED`; the code path remains covered by existing tests.

## Live query status

The existing Gateway at `127.0.0.1:8080` remained untouched. The Codex in-app browser opened a separate unauthenticated session and was redirected to the login page, so the one-click authenticated live quota query was `NOT EXERCISED`. No credentials were requested, no reauthorization occurred, and no second Gateway was started.

## Regression coverage

- only 5-hour window: enrich 5-hour statistics and keep 7-day absent
- 5-hour plus 7-day windows: enrich both
- existing quota normalization tests cover missing secondary/reset metadata
- existing rate-limit tests cover 429 cooldown and scheduler exclusion/recovery
