.PHONY: build build-backend build-frontend test test-backend test-frontend test-frontend-critical

# Personal Edition critical frontend coverage intentionally focuses on the
# private gateway surfaces that remain in the product. Upstream SaaS-only
# payment, public social-login and channel-monitor tests are not part of the
# Personal branch and must not keep deleted modules in the dependency graph.
FRONTEND_CRITICAL_VITEST := \
	src/api/__tests__/client.spec.ts \
	src/api/__tests__/tokenRefresh.spec.ts \
	src/components/user/profile/__tests__/ProfilePasswordForm.spec.ts \
	src/views/admin/__tests__/UsersView.spec.ts

# 一键编译前后端
build: build-backend build-frontend

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译前端（需要已安装依赖）
build-frontend:
	@pnpm --dir frontend run build

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

test-frontend:
	@pnpm --dir frontend run lint:check
	@pnpm --dir frontend run typecheck
	@$(MAKE) test-frontend-critical

test-frontend-critical:
	@pnpm --dir frontend exec vitest run $(FRONTEND_CRITICAL_VITEST)
