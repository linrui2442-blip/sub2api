# Sub2API Personal Edition — Final Architecture Audit

## Verdict

**NEEDS FIX**

This audit covers `personal-v1` at `206b9db921a83ce677531f836be3b60acc699af5`.
CI is green, but shared persistence and service contracts still expose legacy commercial
semantics and retain PostgreSQL-oriented implementation paths inside the compiled Personal
binary. Those paths must be refactored deliberately; they must not be deleted by keyword.

## Verified delivery baseline

- GitHub `CI` run 420 passed: lint, frontend, backend tests.
- GitHub `Personal Edition CI` run 409 passed: policy, SQLite storage, owner setup,
  Wire generation, application boot smoke, and Windows AMD64 embedded build.
- GitHub `Security Scan` run 420 passed.
- Personal startup opens SQLite through `initPersonalEnt`, with WAL, foreign keys,
  busy timeout and a one-connection policy; it does not open PostgreSQL.
- Embedded/local Redis remains intentional: it backs token refresh, scheduler snapshots,
  concurrency, RPM, sessions and local caches.

## Current Personal architecture

### Retained — required runtime capabilities

- Gateway and provider architecture: OpenAI, Gemini and Anthropic/Claude routing;
  protocol conversion, Responses API, multimodal input, tool calling, account failover,
  health checks and future adapter extension points.
- Identity and local security: owner/trusted-member local authentication, password,
  session security, TOTP, passkeys, API-key security and audit logging.
- Account pool: groups, provider accounts, OAuth/token refresh, quota windows,
  concurrency, load factor, priority, RPM, availability, cooldown and proxy support.
- Storage: SQLite is the Personal database. Local Redis compatibility services are
  reachable from the Personal Wire graph and covered by the boot smoke.

### Personal route boundary — verified absent

The Personal router does not register public registration, payment, subscriptions,
affiliate, redeem, announcements, channel monitoring, or external user-identity routes.
Provider OAuth remains separately registered for OpenAI and Gemini; Claude support is
kept as an adapter/account capability rather than a SaaS identity flow.

## Audit findings and disposition

### A — remove (proved unreachable from the Personal route/UI surface)

1. **Upstream PostgreSQL/Redis first-run UI**
   - Removed in this audit pass: `frontend/src/views/setup/UpstreamSetupWizardView.vue`,
     unused `SetupEntryView.vue`, and the upstream database/Redis setup API contract.
   - Evidence: both first-run and normal Personal `/setup/status` endpoints set
     `personal: true`; the upstream setup endpoints are absent.

2. **Legacy public-SaaS route assets**
   - Payment/redeem/subscription callback tests, non-Personal router documentation, and
     i18n keys for LinuxDo, DingTalk, WeChat, OIDC and payment are unreferenced by the
     Personal router. Remove them together with stale tests and translation keys.

3. **Container/Apple deployment assets**
   - `deploy/apple-container.sh`, container tests and Docker helpers require PostgreSQL
     and Redis containers and have no Personal Windows runtime reference.
   - They are deletion candidates, but are currently modified by unrelated worktree changes;
     they were not changed or staged in this audit.

### B — retain (runtime or future-provider infrastructure)

1. **AWS SDK and Bedrock implementation**
   - AWS references are Bedrock/Anthropic adapter and SigV4 support, not S3 backup.
     The Personal UI does not expose Bedrock; the adapter is retained for extensibility.

2. **Non-default provider adapters**
   - Grok, Antigravity and CN-provider implementations remain in the shared gateway.
     They are not default Personal UI choices. Keep their backend adapter/protocol layer
     until provider capabilities can be isolated without weakening future adapters.

3. **Embedded Redis**
   - Retain. The generated Personal Wire graph creates Redis-backed scheduler,
     refresh-token, API-key, concurrency, RPM, session and cache services.

### C — refactor before removal (shared contract or compiled reachability)

1. **Usage cost/billing fields — high priority**
   - `usage_logs` still stores costs, `rate_multiplier`, `account_rate_multiplier`,
     billing type/mode/tier and `subscription_id`.
   - Generic Usage DTOs and handlers still serialize/filter these fields; repository
     statistics still aggregate them.
   - Personal Gateway `recordOperationalUsage` records request/token/latency/model,
     image metadata, group/account and audit context, but does **not** populate commercial
     cost fields. The fields are legacy Personal semantics, yet raw SQL, DTOs and Ent
     schema must be migrated together.
   - Required change: add a Personal operational usage projection; remove commercial
     fields from Personal API filters/responses; perform a SQLite-safe schema migration
     and regenerate Ent/Wire. Preserve token/image/video metadata.

2. **PostgreSQL-oriented shared repository and Ent code — high priority**
   - `repository.InitEnt` is SQLite-only, but the compiled repository package still
     imports `lib/pq`, retains PostgreSQL JSON/ANY/lock paths, generated Ent PostgreSQL
     annotations, PostgreSQL defaults, and a `securityaudit.PostgreSQLRepository`
     selected by the Personal Wire graph.
   - The green boot smoke proves SQLite startup, not every deferred audit/usage branch.
   - Required change: split Personal repository providers from upstream-only PostgreSQL
     implementations, replace the security-audit repository with a SQLite-compatible
     implementation, then remove `lib/pq` only after a Wire/dependency proof.

3. **External Sub2API identity persistence — medium priority**
   - LinuxDo, DingTalk, WeChat and OIDC routes are absent, but `AuthIdentity`,
     `AuthIdentityChannel`, adoption tables and generic binding helpers remain.
   - Preserve email/local password, TOTP and passkeys. Introduce a Personal local-identity
     projection, then remove non-email providers and unused schema tables. Do not touch
     provider OAuth.

4. **Frontend Personal API barrel — medium priority**
   - The unified `adminAPI` barrel statically imports unused SaaS modules (channels,
     compliance, risk control, operations, system and historical provider UI modules).
   - Make it Personal-only and prune latent Grok/Antigravity/CN-provider UI code while
     retaining backend adapters.

## Database assessment

- No active `backend/migrations` tree is used by Personal boot.
- SQLite Ent schema creation plus `ensurePersonalSQLiteInfrastructure` is active.
- Personal-core entities (accounts, groups, API keys, users, allowed groups, proxies,
  settings, secrets, passkeys, audit data and operational usage) are retained.
- `AuthIdentity*`, adoption/pending-auth tables and UsageLog commercial fields are
  the remaining schema candidates.

## Dependency assessment

- No Testcontainers module is present.
- No S3 backup implementation was found; AWS is Bedrock/SigV4 adapter support.
- `modernc.org/sqlite`, `go-redis`, Wire, WebAuthn, OAuth, cron and protocol
  dependencies are runtime requirements.
- `github.com/lib/pq` cannot yet be removed because shared repository/security-audit
  code imports it. This is a refactor outcome, not a dependency-only deletion.
- Frontend `marked` has no identified source import; verify it with the lock file in
  the dependency-cleanup change rather than hand-editing `pnpm-lock.yaml`.

## Required completion sequence

1. Split Personal usage projection and migrate SQLite away from commercial
   cost/price/subscription fields.
2. Split the Personal repository/Wire graph from PostgreSQL-only and `lib/pq` paths,
   preserving local Redis-backed operational services and audit.
3. Remove external Sub2API identity schemas/helpers and stale UI/i18n/tests while keeping
   local password, TOTP and passkeys.
4. Prune the Personal frontend API barrel and unreferenced SaaS/provider UI modules.
5. Remove dirty legacy container deployment assets after unrelated worktree changes are
   resolved.
6. Re-run full backend tests, Personal SQLite smoke, Wire generation, frontend typecheck,
   production build, Windows AMD64 build, CI, Personal Edition CI, Security Scan and
   `git diff --check`.

## Production readiness

**NEEDS FIX** — the verified Windows/SQLite build is functional, but remaining shared
commercial Usage and PostgreSQL-oriented implementation paths prevent calling this a fully
physically-clean Personal Edition.
