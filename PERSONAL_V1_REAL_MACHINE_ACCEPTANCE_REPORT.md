# Sub2API Personal Edition V1 Real-Machine Acceptance Report

Date: 2026-08-24 (Asia/Shanghai)  
Host: Windows AMD64  
Branch: `personal-v1`

## 1. Release identity

- Local HEAD at the start of this continuation: `c4e84b54ee32d154f60a153197b67f8f37de4b49`
- `origin/personal-v1` at the start of this continuation: `c4e84b54ee32d154f60a153197b67f8f37de4b49`
- Worktree before acceptance: clean
- Required usage legend commits present: `8e42b49bd`, `64cd2ad23`
- PR #1: open, draft, mergeable/clean at the inspected revision
- GitHub checks at the inspected revision: CI PASS; Personal Edition CI PASS; Security Scan PASS

## 2. Windows executable and runtime

- Executable: `C:\Users\L-R\Desktop\sub2api\sub2api-personal.exe`
- SHA256: `979632464D9A6F91F77FE878009A842374E33F38FBEAECB5F2CAE956B981DD11`
- Size: 104,885,760 bytes
- Last write time: 2026-08-24 12:18:38 +08:00
- Runtime PID after the final controlled restart: 42812
- Runtime process path: exact executable path above
- Web runtime: `http://127.0.0.1:8080/` returned HTTP 200
- SQLite: `C:\Users\L-R\AppData\Local\Sub2 Personal\sub2api-personal.db`
- Log: `C:\Users\L-R\AppData\Local\Sub2 Personal\logs\sub2api.log`

Controlled stop/start of the formal executable completed successfully. There was no panic, duplicate process, database rebuild, or recurring auth-cache-outbox error. The executable restarted against the existing SQLite database.

## 3. Persistence baseline and restart result

At the final continuation baseline, the database contained one active Owner, four active/schedulable Provider accounts (one Antigravity and three OpenAI), two API keys (one Unified and one Group-Pinned), five groups, and 27 Usage records. No Trusted Member is currently configured.

After restart:

- Owner: PASS (preserved)
- Trusted Member: NOT TESTED — no live Trusted Member is configured
- Accounts: PASS (4 preserved and active/schedulable)
- API keys: PASS (2 preserved; Unified key preserved)
- Groups: PASS (5 preserved)
- OAuth credentials/status: PASS (preserved; no reauthorization requested)
- Usage: PASS (27 existing rows preserved, then row 28 was written by the final post-restart OpenAI request)
- SQLite rebuild/data loss: PASS (not observed)

## 4. Unified and Group-Pinned API key authorization

The existing Unified key (`group_id = NULL`) was used without printing its secret.

- Unified key to Antigravity namespace: PASS
- Unified key to Admin API: PASS (rejected with HTTP 401)
- Provider registry/routing: PASS for Antigravity; requested `antigravity/gemini-3.1-pro-high` selected account 2 and the Antigravity upstream
- Cross-provider implicit fallback: PASS (not observed)
- Group-Pinned Antigravity key requesting `openai/gpt-5.6-luna`: PASS (HTTP 404 `model_not_found`; message states the model is unsupported by accounts in that group)
- Existing key secrets changed: NO
- Disable/delete behavior: NOT EXERCISED — the acceptance run did not mutate either existing key

A non-blocking legacy warning occurred on the Group-Pinned rejection path: `failed to build channel cache ... no such table: channels`. Authorization still failed closed with HTTP 404. This should be removed in a later focused maintenance change; it did not enable cross-group access.

## 5. OpenAI / GPT live acceptance

Three OpenAI OAuth accounts are present. Account 3 retained its pre-existing upstream cooldown/reset time (`2026-08-29 05:40 +08:00`); accounts 4 and 5 were active candidates. The existing Unified key was used without printing its secret. Model: `openai/gpt-5.6-luna`.

- Chat Completions non-stream: PASS (HTTP 200; exact `OK`; Usage 17; account 5)
- Chat Completions stream: PASS (HTTP 200; completed stream; exact `OK`; Usage 18; account 5)
- Responses non-stream: PASS (HTTP 200; exact `OK`; Usage 19; account 4)
- Responses stream: PASS (HTTP 200; completed stream; exact `OK`; Usage 20; account 4)
- Tool calling: PASS (`get_test_value` function call observed; Usage 21; account 4)
- Compact live call: NOT EXERCISED — all three accounts are in `auto` compact mode with no known positive compact-capability result; the retired/legacy compact endpoint was not forced
- Minimal scheduler sequence: PASS (6/6 HTTP 200; Usage 22-27)
- Scheduler account use: PASS (accounts 4 and 5 both selected naturally)
- Existing cooldown exclusion: PASS (account 3 selected 0 times)
- Failure-induced cross-account failover: NOT EXERCISED — no natural upstream failure occurred; no failure was induced merely to obtain a failover result
- OAuth credential persistence: PASS (all three credentials remained present after restart)
- Natural token refresh: NOT EXERCISED — healthy account access-token expiries were 2026-09-03 19:56/19:57 +08:00 and were not naturally due
- Post-restart inference: PASS (HTTP 200; exact `OK`; Usage 28; account 5)
- Cross-provider fallback: PASS (not observed)

No cooldown was cleared, no OAuth authorization was repeated, and no high-volume traffic was generated.

## 6. Antigravity live acceptance

Account: `LR` (account ID 2; no credentials printed)

- Paid tier: `g1-pro-tier`
- Endpoint resolver: PASS; no `GATEWAY_ANTIGRAVITY_FORWARD_BASE_URL` override was present, so the tier-aware endpoint is `https://daily-cloudcode-pa.googleapis.com`
- Non-stream Gateway request: PASS (HTTP 200)
- Streaming Gateway request: PASS (HTTP 200; SSE events and `[DONE]` observed)
- Requested model: `gemini-3.1-pro-high`
- Actual upstream model: `gemini-pro-agent`
- Account selected: 2 (`LR`)
- Upstream endpoint path: `/v1internal:streamGenerateContent`
- Proxy: account HTTP proxy `127.0.0.1:7897`; request succeeded in the current Windows/Clash environment
- Reauthorization: NO
- Natural refresh: NOT EXERCISED — the current token was not due for refresh
- Incorrect “needs reauthorization” state during this run: not observed in runtime data/errors
- Post-restart request: PASS (HTTP 200 without OAuth)
- Post-restart Usage persistence: PASS (Usage row 16)

The response carried a valid response ID and model. The strict test phrase was not returned exactly as `OK`, which is a content-conformance detail rather than a transport/auth/routing failure.

## 7. Standalone Gemini and Anthropic

- Standalone Gemini Provider: NOT TESTED — no live standalone Gemini credential is currently configured
- Gemini project-ID resolution and inference: NOT TESTED for the same reason
- Anthropic Provider: NOT TESTED — no live Anthropic credential is currently configured

Antigravity Claude model exposure was not counted as Anthropic Provider acceptance.

## 8. Account Pool, Scheduler, cooldown, and failover

- Antigravity pool/scheduler selection: PASS
- OpenAI multi-account pool eligibility: PASS (accounts 4 and 5 eligible; account 3 excluded by cooldown)
- OpenAI multi-account scheduling: PASS (both healthy candidates were selected during 6 minimal successful requests)
- OpenAI cooldown to healthy candidates: PASS (cooldown account 3 was never selected; requests continued through accounts 4/5)
- Provider isolation: PASS; OpenAI requests did not fall back to Antigravity
- Failure-induced cross-account failover: NOT EXERCISED (all requests succeeded; no artificial failure/cooldown was introduced)

## 9. Usage acceptance

- Antigravity historical records preserved: PASS (16 records, IDs 1-16, 4,863 recorded tokens)
- OpenAI matrix records: PASS (12 records, IDs 17-28, 9,013 recorded tokens)
- OpenAI account distribution: account 4 = 5 successful Usage rows; account 5 = 7 successful Usage rows; cooldown account 3 = 0
- Requested model, actual upstream model, account, inbound endpoint, upstream endpoint, stream flag, input tokens, and output tokens were persisted
- Recent-24-hour Summary/Model/Endpoint consistency: PASS based on the current SQLite-backed dashboard implementation and displayed runtime data
- Unified `group_id = NULL` Group Distribution: EXPECTED EMPTY
- Chinese trend legend: PASS (`输入`, `输出`, `缓存创建`, `缓存读取`, `缓存命中率`)
- English trend legend keys: PASS in source/i18n (`Input`, `Output`, `Cache Creation`, `Cache Read`, `Cache Hit Rate`)
- Restart persistence: PASS
- OpenAI Usage persistence: PASS for Chat, Responses, streaming, tool calling, scheduler sequence, and post-restart request

## 10. Audit acceptance

- Owner access to audit page/API: PASS (existing authenticated admin requests are recorded)
- Gateway security audit: PASS; allow decisions for Unified Antigravity requests were written to the structured runtime log with API key ID, account-independent model routing context, and no credential secret
- SQLite admin audit log remained operational (48 rows after restart/admin polling)
- Seven-day retention: PASS by implementation and SQLite regression coverage; the existing worker uses `DeleteBefore`, a fixed seven-day cutoff, batched cleanup, startup execution, and periodic execution
- Retention affects Usage: NO (covered by SQLite repository test)
- Delete-all without TOTP: PASS by current route/handler tests and implementation
- Delete-all live destructive execution: NOT EXERCISED — preserving existing audit evidence took priority
- Non-Owner delete-all: NOT TESTED — no Trusted Member is configured

Gateway request records are stored in the structured security runtime log; the SQLite `audit_logs` table primarily contains authenticated management actions. This distinction is expected in the current implementation.

## 11. Windows delivery checks

- Formal EXE starts and serves embedded frontend: PASS
- Data directory is explicit and stable: PASS
- Log location is explicit and active: PASS
- Normal stop/restart: PASS
- Existing SQLite/OAuth/API key/group/Owner survives executable restart: PASS
- Continuous log spam/panic after restart: PASS (not observed)
- Port-conflict UX: NOT TESTED — no second instance was intentionally started
- Forced-crash recovery: NOT TESTED — destructive termination was not required
- EXE overwrite-upgrade preservation: PASS based on the currently running rebuilt executable using the existing data directory
- Brand-new Windows profile first launch: NOT TESTED
- Antivirus false-positive behavior: NOT TESTED
- Clean-machine firewall prompt: NOT TESTED

Startup warnings remain for an auto-generated TOTP encryption key, disabled URL allowlist, unconfigured trusted proxies, and absent CORS origins. The runtime is local (`127.0.0.1`) and functional, but these settings must be reviewed before exposing the management plane outside the trusted local/private environment.

## 12. Result summary

### PASS

- Formal Windows EXE identity and startup
- SQLite/Owner/account/key/group/Usage persistence across restart
- Unified key Antigravity routing
- Unified key denial on Admin API
- Group-Pinned key denial across provider/group boundary
- Antigravity non-stream, stream, tier-aware endpoint, Usage, and post-restart call
- Antigravity pool/scheduler selection
- OpenAI Chat, Responses, streaming, tool calling, multi-account scheduling, cooldown exclusion, and post-restart inference
- Provider isolation and existing OpenAI cooldown enforcement
- Usage persistence and localized trend legend
- Audit retention implementation and non-recursive maintenance behavior

### FAIL

- None in the V1 core acceptance scope

### SKIPPED

- Failure-induced cross-account failover (healthy accounts produced no natural failure; no failure was injected)

### NOT TESTED / NOT EXERCISED

- OpenAI Compact live call (no known positive compact capability on the current accounts)
- Standalone Gemini (no credential)
- Anthropic (no credential)
- Trusted Member authorization (no member configured)
- Destructive API key disable/delete and Audit delete-all
- New-profile/clean-machine/antivirus/firewall scenarios

## 13. Release blockers

No V1 core release blocker remains. No data loss, privilege escalation, cross-provider fallback, OAuth revocation, SQLite corruption, or provider-wide outage was found.

## 14. Non-blocking follow-up

1. Exercise failure-induced cross-account failover only when a natural provider failure occurs or a separately approved safe fault-injection test is available.
2. Exercise Compact only after an account reports known positive compact capability; do not revive retired upstream behavior.
3. Remove the legacy `channels` table lookup from the Group-Pinned rejection path in a focused change.
4. Validate standalone Gemini and Anthropic only after real credentials are intentionally configured.
5. Review local security warnings before any remote/private-network exposure; do not weaken local Owner/Admin controls.

## Final conclusion

**PERSONAL V1 REAL-MACHINE CORE ACCEPTANCE PASS**

The Windows/SQLite/Unified-key/Antigravity/OpenAI core is accepted on the real machine. OpenAI Chat, Responses, streaming, tool calling, multi-account scheduling, cooldown exclusion, Usage persistence, and post-restart inference all passed. Standalone Gemini/Anthropic credentials, known-positive Compact support, and failure-induced failover remain explicitly outside this completed core result.
