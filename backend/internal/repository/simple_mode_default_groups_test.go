package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSimpleModeDefaultGroupsFreshInstallUsesSingleAntigravityGroup(t *testing.T) {
	ctx := context.Background()
	client, closeDB := openDefaultGroupTestDB(t)
	defer closeDB()

	require.NoError(t, ensureSimpleModeDefaultGroups(ctx, client))
	require.NoError(t, ensureSimpleModeDefaultGroups(ctx, client))
	groups, err := client.Group.Query().Where(
		group.PlatformEQ(service.PlatformAntigravity), group.DeletedAtIsNil(),
	).All(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "antigravity-default", groups[0].Name)
}

func TestSimpleModeDefaultGroupsConvergesOnlyLegacyAutoGroups(t *testing.T) {
	ctx := context.Background()
	client, closeDB := openDefaultGroupTestDB(t)
	defer closeDB()

	legacy1 := client.Group.Create().SetName("antigravity-default-1").SetDescription(simpleModeDefaultGroupDescription).SetPlatform(service.PlatformAntigravity).SaveX(ctx)
	legacy2 := client.Group.Create().SetName("antigravity-default-2").SetDescription(simpleModeDefaultGroupDescription).SetPlatform(service.PlatformAntigravity).SaveX(ctx)
	custom := client.Group.Create().SetName("antigravity-custom").SetDescription("Private routing group").SetPlatform(service.PlatformAntigravity).SaveX(ctx)
	customLegacyName := client.Group.Create().SetName("antigravity-default-1-custom").SetDescription("Operator-created").SetPlatform(service.PlatformAntigravity).SaveX(ctx)
	account := client.Account.Create().SetName("ag-account").SetPlatform(service.PlatformAntigravity).SetType(service.AccountTypeOAuth).SaveX(ctx)
	require.NoError(t, client.AccountGroup.Create().SetAccountID(account.ID).SetGroupID(legacy1.ID).SetPriority(7).Exec(ctx))
	user := client.User.Create().SetUsername("owner").SetEmail("owner@example.test").SetPasswordHash("hash").SetRole("admin").SaveX(ctx)
	require.NoError(t, client.UserAllowedGroup.Create().SetUserID(user.ID).SetGroupID(legacy2.ID).Exec(ctx))
	key := client.APIKey.Create().SetUserID(user.ID).SetKey("sk-test-converge").SetName("legacy-pinned").SetGroupID(legacy2.ID).SaveX(ctx)
	usage := client.UsageLog.Create().SetUserID(user.ID).SetAPIKeyID(key.ID).SetAccountID(account.ID).SetRequestID("req-before-convergence").SetModel("gemini-3.1-pro-high").SetGroupID(legacy1.ID).SaveX(ctx)

	require.NoError(t, ensureSimpleModeDefaultGroups(ctx, client))
	canonical := client.Group.Query().Where(group.NameEQ("antigravity-default"), group.DeletedAtIsNil()).OnlyX(ctx)
	bindings, err := client.AccountGroup.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, canonical.ID, bindings[0].GroupID)
	require.Equal(t, 7, bindings[0].Priority)
	reloadedKey := client.APIKey.GetX(ctx, key.ID)
	require.NotNil(t, reloadedKey.GroupID)
	require.Equal(t, canonical.ID, *reloadedKey.GroupID)
	permissions, err := client.UserAllowedGroup.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Equal(t, canonical.ID, permissions[0].GroupID)
	require.True(t, client.Group.Query().Where(group.IDEQ(custom.ID), group.DeletedAtIsNil()).ExistX(ctx))
	require.True(t, client.Group.Query().Where(group.IDEQ(customLegacyName.ID), group.DeletedAtIsNil()).ExistX(ctx))
	require.False(t, client.Group.Query().Where(group.IDIn(legacy1.ID, legacy2.ID), group.DeletedAtIsNil()).ExistX(ctx))
	require.Equal(t, legacy1.ID, *client.UsageLog.GetX(ctx, usage.ID).GroupID, "historical usage must retain its original group id")

	// Repeated startup remains stable and never duplicates membership.
	require.NoError(t, ensureSimpleModeDefaultGroups(ctx, client))
	require.Equal(t, 1, client.AccountGroup.Query().CountX(ctx))
}

func openDefaultGroupTestDB(t *testing.T) (*ent.Client, func()) {
	t.Helper()
	drv, _, err := openPersonalSQLite(filepath.Join(t.TempDir(), "groups.db"))
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	return client, func() { require.NoError(t, client.Close()) }
}
