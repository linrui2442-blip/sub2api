# Sub2API Personal Private Edition V1

## Purpose

Sub2API is a local/private LLM gateway for the Binance Square AI Operator
ecosystem. This repository contains only the gateway infrastructure; it does
not implement Operator business logic.

The product supports one owner plus a small, trusted private team. It is not a
public SaaS product and it does not sell or distribute quota.

## Runtime boundary

The Windows Personal executable runs with:

```text
sub2api-personal.exe
  ├─ local Web control plane + API Gateway
  ├─ SQLite durable store
  ├─ in-process cache/lock compatibility service
  └─ provider account pools
```

No Docker, WSL, external PostgreSQL or external Redis service is required.
The default listener is loopback-only. Local data lives in
`%LOCALAPPDATA%\Sub2 Personal` unless overridden by
`SUB2_PERSONAL_DATA_DIR` or `SUB2_PERSONAL_SQLITE_PATH`.

## Kept capabilities

- Provider extension boundary, registry and common gateway routing.
- GPT/OpenAI and Gemini Provider OAuth, token persistence and refresh.
- Claude/Anthropic-compatible extension path without gateway, scheduler or
  account-pool redesign.
- Account pool, priority, quota/health state, cooldown, scheduling and
  same-model failover.
- API Gateway and protocol conversion.
- Owner-managed members, groups, permissions, API keys, usage and audit logs.
- Owner initialization, TOTP/passkey support and Windows local startup.

Provider account sharing defaults to `owner_only`. `private_members` may only
be selected when the upstream provider terms and account plan permit it.

## Excluded capabilities

- Public registration, invitations and social sign-in.
- Tenant/organization and enterprise RBAC layers.
- Payment, billing, subscriptions, balance, top-up, price or referral systems.
- Marketplace, marketing, channel-monitor and commercial analytics features.
- Cloud backup, S3, Docker, Kubernetes, Linux service and Apple-container
  deployment stacks.

## Verification gates

Each change must preserve:

```powershell
cd backend
go generate ./cmd/server
go test ./...

cd ..\frontend
corepack pnpm@9.15.9 run typecheck
```

The Personal Edition workflow additionally verifies the SQLite boot smoke test
and a Windows `amd64` embedded build. Real-provider acceptance remains a
separate, credential-bearing test: GPT OAuth/login, Gemini OAuth/login, token
refresh, multiple-account rotation, gateway forwarding and failover.
