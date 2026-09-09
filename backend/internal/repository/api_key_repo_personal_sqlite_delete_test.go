package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/stretchr/testify/require"
)

func TestPersonalSQLiteAPIKeyDeleteWithAudit(t *testing.T) {
	ctx := context.Background()
	drv, db, err := openPersonalSQLite(filepath.Join(t.TempDir(), "api-key-delete.db"))
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()
	require.NoError(t, client.Schema.Create(ctx))
	user := client.User.Create().SetUsername("delete-owner").SetEmail("delete-owner@test").SetPasswordHash("hash").SetRole("admin").SaveX(ctx)
	key := client.APIKey.Create().SetUserID(user.ID).SetKey("sk-delete-regression").SetName("delete").SaveX(ctx)
	repo := newAPIKeyRepositoryWithSQL(client, db)
	require.NoError(t, repo.DeleteWithAudit(ctx, key.ID))
	_, err = client.APIKey.Query().Where(apikey.IDEQ(key.ID)).Only(ctx)
	require.Error(t, err)
	tombstone := client.APIKey.Query().Where(apikey.IDEQ(key.ID)).OnlyX(mixins.SkipSoftDelete(ctx))
	require.NotEqual(t, "sk-delete-regression", tombstone.Key)
	require.NoError(t, repo.DeleteWithAudit(ctx, key.ID))
}
