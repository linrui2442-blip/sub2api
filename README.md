# Sub2API Personal Private Edition

Windows-first private LLM gateway for one owner and a small, trusted team.
It is the infrastructure layer for long-running AI agents, not a public SaaS
service and not a consumer chat application.

## What it provides

- OpenAI/GPT and Gemini account providers, with a provider extension boundary
  for Claude/Anthropic and later providers.
- Provider OAuth, durable token storage and automatic token refresh.
- Account pools, health checks, quota state, scheduler, cooldown and failover.
- OpenAI-compatible API gateway and protocol conversion.
- Owner-managed members, groups, permissions, API keys, usage and audit logs.
- SQLite persistence and a local in-process cache: no Docker, PostgreSQL,
  Redis or WSL is required by the Personal executable.

## Product boundary

Public registration, social login, tenant/organization management, payments,
subscriptions, top-up, affiliate/referral, marketplace, cloud backup and
server deployment stacks are intentionally absent.

The default listener is `127.0.0.1`. A trusted private LAN or VPN listener can
be configured explicitly by the owner.

## Windows quick start

Download `sub2api-personal-windows-x64.zip` from the Personal Edition release,
extract it, and run `sub2api-personal.exe`. The browser opens to the local
setup screen. Create the owner account, then add provider accounts and API
keys in the local control plane.

Persistent data is stored under `%LOCALAPPDATA%\Sub2 Personal` by default:

- `sub2api-personal.db` — SQLite data
- local logs and runtime configuration

Advanced overrides: `SUB2_PERSONAL_DATA_DIR`, `SUB2_PERSONAL_SQLITE_PATH`,
`SERVER_HOST`, and `SERVER_PORT`.

Keep provider accounts `owner_only` unless the relevant provider terms permit
trusted-member sharing. Usage and audit records remain attributable to each
member API key. Back up the SQLite file only while the application is stopped.

## Development checks

```powershell
cd backend
go generate ./cmd/server
go test ./...

cd ..\frontend
corepack pnpm@9.15.9 run typecheck
```

See [Personal Edition V1](docs/PERSONAL_EDITION_V1.md) for the exact product
boundary and verification checklist.
