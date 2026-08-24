# Sub2API Personal Edition V1 Real-Machine Acceptance Report

Date: 2026-08-24 (Asia/Shanghai)  
Host: Windows AMD64  
Branch: `personal-v1`

## 1. Release identity

- Local HEAD: `64cd2ad23f0adeadaf38efaa9ee91127efd62d39`
- `origin/personal-v1`: `64cd2ad23f0adeadaf38efaa9ee91127efd62d39`
- Worktree before acceptance: clean
- Required usage legend commits present: `8e42b49bd`, `64cd2ad23`
- PR #1: open, draft, mergeable/clean at the inspected revision
- GitHub checks at the inspected revision: CI PASS; Personal Edition CI PASS; Security Scan PASS

## 2. Windows executable and runtime

- Executable: `C:\Users\L-R\Desktop\sub2api\sub2api-personal.exe`
- SHA256: `979632464D9A6F91F77FE878009A842374E33F38FBEAECB5F2CAE956B981DD11`
- Size: 104,885,760 bytes
- Last write time: 2026-08-24 12:18:38 +08:00
- Runtime PID after controlled restart: 46956
- Runtime process path: exact executable path above
- Web runtime: `http://127.0.0.1:8080/` returned HTTP 200
- SQLite: `C:\Users\L-R\AppData\Local\Sub2 Personal\sub2api-personal.db`
- Log: `C:\Users\L-R\AppData\Local\Sub2 Personal\logs\sub2api.log`

Controlled stop/start of the formal executable completed successfully. There was no panic, duplicate process, database rebuild, or recurring auth-cache-outbox error. The executable restarted against the existing SQLite database.

## 3. Persistence baseline and restart result

Before restart, the database contained one active Owner, two active/schedulable Provider accounts, two API keys (one Unified and one Group-Pinned), five groups, and 15 Usage records. No Trusted Member is currently configured.

After restart:

- Owner: PASS (preserved)
- Trusted Member: NOT TESTED — no live Trusted Member is configured
- Accounts: PASS (2 preserved and active/schedulable)
- API keys: PASS (2 preserved; Unified key preserved)
- Groups: PASS (5 preserved)
- OAuth credentials/status: PASS (preserved; no reauthorization requested)
- Usage: PASS (15 existing rows preserved, then row 16 was written by the post-restart request)
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

The existing OpenAI OAuth account is active and schedulable, but already had a persisted upstream cooldown/reset time (`2026-08-29 05:40 +08:00`) before this acceptance run. One minimal Unified Gateway attempt returned HTTP 503 because no OpenAI account was currently selectable. The runtime did not fall back to Antigravity.

- Chat Completions non-stream: FAIL (HTTP 503; existing provider cooldown)
- Chat Completions stream: NOT TESTED — redundant while the only account is unavailable
- Responses non-stream: NOT TESTED — only account is unavailable
- Responses stream: NOT TESTED — only account is unavailable
- Tool calling: NOT TESTED — only account is unavailable
- Compact live call: NOT TESTED — only account is unavailable
- OAuth credential persistence: PASS (credential remains present after restart)
- Natural token refresh: NOT EXERCISED — access token was not naturally due for refresh
- Post-restart inference: NOT TESTED — provider remained in the pre-existing cooldown
- 429/cooldown structure: PASS (existing cooldown excluded the account without unsafe retry traffic)

This is an incomplete OpenAI live acceptance result, not evidence of cross-provider routing failure. No high-volume requests were made to force or clear the upstream limit.

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

- Single-account pool membership: PASS for the configured OpenAI and Antigravity accounts
- Single-account scheduler selection: PASS for Antigravity
- Provider isolation: PASS; unavailable OpenAI did not fall back to Antigravity
- Existing OpenAI cooldown exclusion: PASS
- Account Pool multi-account rotation: SKIPPED
- Scheduler multi-account balancing: SKIPPED
- Cross-account failover: SKIPPED
- Cooldown to secondary account: SKIPPED

Reason: only one live account is currently configured for each provider; real failover validation requires at least two accounts.

## 9. Usage acceptance

- Baseline: 13 records / 4,048 input+output tokens before this acceptance sequence
- After non-stream, stream, and post-restart Antigravity calls: 16 records / 4,863 input+output tokens
- Requested model, actual upstream model, account, inbound endpoint, upstream endpoint, stream flag, input tokens, and output tokens were persisted
- Recent-24-hour Summary/Model/Endpoint consistency: PASS based on the current SQLite-backed dashboard implementation and displayed runtime data
- Unified `group_id = NULL` Group Distribution: EXPECTED EMPTY
- Chinese trend legend: PASS (`输入`, `输出`, `缓存创建`, `缓存读取`, `缓存命中率`)
- English trend legend keys: PASS in source/i18n (`Input`, `Output`, `Cache Creation`, `Cache Read`, `Cache Hit Rate`)
- Restart persistence: PASS
- OpenAI Usage row: NOT PRESENT because the OpenAI request did not reach a successful upstream completion

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
- Single-account Antigravity pool/scheduler selection
- Provider isolation and existing OpenAI cooldown enforcement
- Usage persistence and localized trend legend
- Audit retention implementation and non-recursive maintenance behavior

### FAIL

- OpenAI Chat Completions live request: HTTP 503 because the only configured OpenAI account is in an existing upstream cooldown

### SKIPPED

- Multi-account rotation, balancing, secondary failover, and cooldown failover (only one live account per provider)

### NOT TESTED / NOT EXERCISED

- OpenAI stream, Responses API, tool calling, Compact, and post-restart inference while account is unavailable
- Standalone Gemini (no credential)
- Anthropic (no credential)
- Trusted Member authorization (no member configured)
- Destructive API key disable/delete and Audit delete-all
- New-profile/clean-machine/antivirus/firewall scenarios

## 13. Release blockers

`RELEASE BLOCKER: OpenAI/GPT real-machine acceptance is incomplete because the sole live OpenAI account is currently in a genuine upstream cooldown. Chat Completions returned HTTP 503 before upstream selection; Responses, streaming, tool calling, Compact, and post-restart OpenAI inference could not be truthfully accepted.`

No data loss, privilege escalation, cross-provider fallback, OAuth revocation, or Antigravity blocker was found.

## 14. Non-blocking follow-up

1. After the existing OpenAI cooldown expires naturally, run one minimal matrix covering Chat Completions, Responses, streaming, tool calling, Compact, and a restart. Do not force-clear the cooldown.
2. Remove the legacy `channels` table lookup from the Group-Pinned rejection path in a focused change.
3. Add a second live account only when real multi-account rotation/failover acceptance is desired.
4. Validate standalone Gemini and Anthropic only after real credentials are intentionally configured.
5. Review local security warnings before any remote/private-network exposure; do not weaken local Owner/Admin controls.

## Final conclusion

**PERSONAL V1 NOT READY**

The Windows/SQLite/Unified-key/Antigravity core is accepted on the real machine, but the explicitly highest-priority OpenAI/GPT live matrix is incomplete due to the sole account's existing upstream cooldown. A complete V1 release acceptance cannot be claimed until that provider can be retested successfully.
