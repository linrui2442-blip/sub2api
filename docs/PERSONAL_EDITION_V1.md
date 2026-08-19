# Sub2 Personal Edition V1

This branch turns upstream Sub2API into a private, Windows-first gateway for one owner and a small set of manually managed private members.

## Product boundary

Personal Edition is not a public SaaS product. It must not expose public registration, invitation growth, payments, top-up, referral, marketplace, public pricing, or multi-tenant commercial billing flows.

The intended deployment is:

- one owner/admin;
- a small number of manually created private members;
- one local/private gateway instance;
- GPT/OpenAI and Gemini account pools first;
- per-member API keys and usage visibility;
- no public signup.

## Upstream account sharing boundary

Every upstream account must have an explicit sharing scope:

- `owner_only`: only the owner may route requests through the account;
- `private_members`: the owner plus manually approved private members may route requests through the account.

The safe default is always `owner_only`.

A provider/account may only be marked `private_members` when the applicable upstream plan, API terms, account policy, or other authorization actually permits that use. Consumer subscription credentials must never be assumed shareable merely because the gateway can technically route them.

## V1 member policy

- Public registration: disabled.
- Self-service invitation: disabled.
- Payment/top-up/referral: disabled.
- Member creation: admin only.
- Default private member cap: 10 (owner excluded).
- Each member receives a separate gateway API key.
- A member can be disabled without deleting upstream accounts.
- Usage must remain attributable to the member API key.

## Core capabilities to preserve

The Personal Edition must preserve the upstream work that provides the actual gateway value:

- OpenAI/GPT account authentication and token refresh;
- Gemini account authentication and token refresh;
- account health and schedulability;
- quota/limit state where available;
- account priority;
- 429/temporary failure handling;
- same-model account failover;
- text and vision request paths;
- structured-output compatibility;
- request/usage logs needed for operation and troubleshooting.

## Infrastructure migration

The upstream production path currently assumes PostgreSQL and Redis. Personal Edition will remove those external runtime dependencies incrementally rather than deleting them blindly.

Target end state:

```text
Windows
  -> sub2api-personal.exe
  -> local/private Web UI + API
  -> SQLite persistent store
  -> in-process cache / locks / scheduler
  -> GPT + Gemini account pools
```

Target runtime dependencies after migration:

- no WSL requirement;
- no Docker requirement;
- no external PostgreSQL requirement;
- no external Redis requirement.

## Migration sequence

1. Freeze Personal Edition policy and Windows release target.
2. Add a first-class `personal` run mode without changing upstream `standard` and `simple` behavior.
3. Disable public/commercial routes and UI in personal mode.
4. Introduce storage/cache contracts where upstream code is directly bound to PostgreSQL/Redis.
5. Replace durable personal data with SQLite-compatible implementations.
6. Replace distributed Redis coordination with single-process equivalents where safe.
7. Preserve account scheduling/failover semantics and add regression tests.
8. Produce a Windows x64 release artifact.
9. Validate one GPT account and one Gemini account with real OAuth/token refresh/calls.
10. Validate multiple accounts and same-model failover.

## Non-goals for V1

- Public internet service.
- Public registration.
- Selling AI quota.
- Payment collection.
- Referral/affiliate systems.
- Multi-organization tenancy.
- Rewriting provider/OAuth implementations from scratch when upstream code can be retained.

## Upstream maintenance rule

Keep the fork structurally close enough to upstream that provider/OAuth fixes can still be cherry-picked or merged. Prefer personal-mode gates and replaceable interfaces over destructive deletion of unrelated upstream code until the Personal Edition is stable.
