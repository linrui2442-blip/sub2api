package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

func TestPersonalSQLiteGatewayControlsCRUD(t *testing.T) {
	drv, _, err := openPersonalSQLite(filepath.Join(t.TempDir(), "gateway-controls.db"))
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	ctx := context.Background()
	require.NoError(t, client.Schema.Create(ctx))

	tlsRepo := NewTLSFingerprintProfileRepository(client)
	profiles, err := tlsRepo.List(ctx)
	require.NoError(t, err)
	require.Empty(t, profiles)
	profile, err := tlsRepo.Create(ctx, &model.TLSFingerprintProfile{Name: "acceptance-tls"})
	require.NoError(t, err)
	profile.Name = "acceptance-tls-updated"
	_, err = tlsRepo.Update(ctx, profile)
	require.NoError(t, err)
	require.NoError(t, tlsRepo.Delete(ctx, profile.ID))
	profiles, err = tlsRepo.List(ctx)
	require.NoError(t, err)
	require.Empty(t, profiles)

	ruleRepo := NewErrorPassthroughRepository(client)
	rules, err := ruleRepo.List(ctx)
	require.NoError(t, err)
	require.Empty(t, rules)
	rule, err := ruleRepo.Create(ctx, &model.ErrorPassthroughRule{
		Name: "acceptance-rule", Enabled: true, MatchMode: "any", PassthroughCode: true, PassthroughBody: true,
	})
	require.NoError(t, err)
	rule.Priority = 10
	_, err = ruleRepo.Update(ctx, rule)
	require.NoError(t, err)
	require.NoError(t, ruleRepo.Delete(ctx, rule.ID))
	rules, err = ruleRepo.List(ctx)
	require.NoError(t, err)
	require.Empty(t, rules)
}
