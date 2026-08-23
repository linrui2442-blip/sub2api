package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/group"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPersonalUnifiedAndPinnedAPIKeysPersistAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "unified-keys.db")
	drv1, _, err := openPersonalSQLite(dbPath)
	require.NoError(t, err)
	client1 := ent.NewClient(ent.Driver(drv1))
	require.NoError(t, client1.Schema.Create(ctx))
	require.NoError(t, ensureSimpleModeDefaultGroups(ctx, client1))
	user := client1.User.Create().SetUsername("owner").SetEmail("owner@personal.test").SetPasswordHash("hash").SetRole("admin").SaveX(ctx)
	antigravityGroup := client1.Group.Query().Where(group.NameEQ("antigravity-default"), group.DeletedAtIsNil()).OnlyX(ctx)
	unified := client1.APIKey.Create().SetUserID(user.ID).SetKey("sk-unified-restart-test").SetName("unified").SaveX(ctx)
	pinned := client1.APIKey.Create().SetUserID(user.ID).SetKey("sk-pinned-restart-test").SetName("pinned").SetGroupID(antigravityGroup.ID).SaveX(ctx)
	require.NoError(t, client1.Close())

	drv2, _, err := openPersonalSQLite(dbPath)
	require.NoError(t, err)
	client2 := ent.NewClient(ent.Driver(drv2))
	defer func() { require.NoError(t, client2.Close()) }()
	reloadedUnified := client2.APIKey.Query().Where(apikey.IDEQ(unified.ID)).OnlyX(ctx)
	reloadedPinned := client2.APIKey.Query().Where(apikey.IDEQ(pinned.ID)).OnlyX(ctx)
	require.Nil(t, reloadedUnified.GroupID, "ungrouped remains the durable Unified Personal API Key state")
	require.NotNil(t, reloadedPinned.GroupID)
	require.Equal(t, antigravityGroup.ID, *reloadedPinned.GroupID, "existing group-pinned semantics must survive restart")
	require.Equal(t, service.StatusActive, reloadedUnified.Status)
}
