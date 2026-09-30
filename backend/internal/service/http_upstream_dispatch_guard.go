package service

import "context"

// HTTPUpstreamDispatchGuard is an optional internal, request-scoped observer.
// A nil guard leaves the production HTTP path unchanged.
type HTTPUpstreamDispatchGuard interface {
	BeforeRoundTrip(role string) error
}

type httpUpstreamDispatchGuardKey struct{}
type httpUpstreamDispatchRoleKey struct{}

func WithHTTPUpstreamDispatchGuard(ctx context.Context, guard HTTPUpstreamDispatchGuard) context.Context {
	return context.WithValue(ctx, httpUpstreamDispatchGuardKey{}, guard)
}

func WithHTTPUpstreamDispatchRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, httpUpstreamDispatchRoleKey{}, role)
}

func HTTPUpstreamDispatchGuardFromContext(ctx context.Context) (HTTPUpstreamDispatchGuard, string) {
	guard, _ := ctx.Value(httpUpstreamDispatchGuardKey{}).(HTTPUpstreamDispatchGuard)
	role, _ := ctx.Value(httpUpstreamDispatchRoleKey{}).(string)
	return guard, role
}
