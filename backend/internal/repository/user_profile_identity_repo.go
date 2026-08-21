package repository

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var ErrAuthIdentityOwnershipConflict = infraerrors.Conflict("AUTH_IDENTITY_OWNERSHIP_CONFLICT", "email identity already belongs to another user")

type sqlQueryExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

var repositoryScopedKeyLocks = struct {
	sync.Mutex
	locks map[string]*sync.Mutex
}{locks: make(map[string]*sync.Mutex)}

func lockRepositoryScopedKeys(_ context.Context, _ *dbent.Client, _ sqlQueryExecutor, keys ...string) (func(), error) {
	keys = append([]string(nil), keys...)
	sort.Strings(keys)
	locks := make([]*sync.Mutex, 0, len(keys))
	repositoryScopedKeyLocks.Lock()
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		lock := repositoryScopedKeyLocks.locks[key]
		if lock == nil {
			lock = &sync.Mutex{}
			repositoryScopedKeyLocks.locks[key] = lock
		}
		locks = append(locks, lock)
	}
	repositoryScopedKeyLocks.Unlock()
	for _, lock := range locks {
		lock.Lock()
	}
	return func() {
		for i := len(locks) - 1; i >= 0; i-- {
			locks[i].Unlock()
		}
	}, nil
}

// WithUserProfileIdentityTx keeps profile and its local email identity in one
// SQLite transaction. External social identity adoption is not part of Personal.
func (r *userRepository) WithUserProfileIdentityTx(ctx context.Context, fn func(context.Context) error) error {
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(dbent.NewTxContext(ctx, tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *userRepository) ListUserAuthIdentities(ctx context.Context, userID int64) ([]service.UserAuthIdentityRecord, error) {
	identities, err := clientFromContext(ctx, r.client).AuthIdentity.Query().
		Where(authidentity.UserIDEQ(userID), authidentity.ProviderTypeEQ("email")).
		All(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]service.UserAuthIdentityRecord, 0, len(identities))
	for _, identity := range identities {
		records = append(records, service.UserAuthIdentityRecord{
			ProviderType: "email", ProviderKey: strings.TrimSpace(identity.ProviderKey),
			ProviderSubject: strings.TrimSpace(identity.ProviderSubject), VerifiedAt: identity.VerifiedAt,
			Issuer: identity.Issuer, Metadata: copyMetadata(identity.Metadata),
			CreatedAt: identity.CreatedAt, UpdatedAt: identity.UpdatedAt,
		})
	}
	return records, nil
}

func (r *userRepository) UnbindUserAuthProvider(context.Context, int64, string) error {
	return service.ErrIdentityProviderInvalid
}

func (r *userRepository) UpdateUserLastLoginAt(ctx context.Context, userID int64, at time.Time) error {
	_, err := clientFromContext(ctx, r.client).User.UpdateOneID(userID).SetLastLoginAt(at).Save(ctx)
	return err
}

func (r *userRepository) UpdateUserLastActiveAt(ctx context.Context, userID int64, at time.Time) error {
	_, err := clientFromContext(ctx, r.client).User.UpdateOneID(userID).SetLastActiveAt(at).Save(ctx)
	return err
}

func (r *userRepository) GetUserAvatar(ctx context.Context, userID int64) (*service.UserAvatar, error) {
	exec, err := r.userProfileIdentitySQL(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, `SELECT storage_provider,storage_key,url,content_type,byte_size,sha256 FROM user_avatars WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	avatar := &service.UserAvatar{}
	if err := rows.Scan(&avatar.StorageProvider, &avatar.StorageKey, &avatar.URL, &avatar.ContentType, &avatar.ByteSize, &avatar.SHA256); err != nil {
		return nil, err
	}
	return avatar, rows.Err()
}

func (r *userRepository) UpsertUserAvatar(ctx context.Context, userID int64, input service.UpsertUserAvatarInput) (*service.UserAvatar, error) {
	exec, err := r.userProfileIdentitySQL(ctx)
	if err != nil {
		return nil, err
	}
	avatar := &service.UserAvatar{StorageProvider: strings.TrimSpace(input.StorageProvider), StorageKey: strings.TrimSpace(input.StorageKey), URL: strings.TrimSpace(input.URL), ContentType: strings.TrimSpace(input.ContentType), ByteSize: input.ByteSize, SHA256: strings.TrimSpace(input.SHA256)}
	_, err = exec.ExecContext(ctx, `INSERT INTO user_avatars(user_id,storage_provider,storage_key,url,content_type,byte_size,sha256,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,CURRENT_TIMESTAMP) ON CONFLICT(user_id) DO UPDATE SET storage_provider=excluded.storage_provider,storage_key=excluded.storage_key,url=excluded.url,content_type=excluded.content_type,byte_size=excluded.byte_size,sha256=excluded.sha256,updated_at=CURRENT_TIMESTAMP`, userID, avatar.StorageProvider, avatar.StorageKey, avatar.URL, avatar.ContentType, avatar.ByteSize, avatar.SHA256)
	if err != nil {
		return nil, err
	}
	return avatar, nil
}

func (r *userRepository) DeleteUserAvatar(ctx context.Context, userID int64) error {
	exec, err := r.userProfileIdentitySQL(ctx)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `DELETE FROM user_avatars WHERE user_id=$1`, userID)
	return err
}

func copyMetadata(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func txAwareSQLExecutor(ctx context.Context, fallback sqlExecutor, client *dbent.Client) sqlQueryExecutor {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		if exec := sqlExecutorFromEntClient(tx.Client()); exec != nil {
			return exec
		}
	}
	if fallback != nil {
		return fallback
	}
	return sqlExecutorFromEntClient(client)
}

func sqlExecutorFromEntClient(client *dbent.Client) sqlQueryExecutor {
	if client == nil {
		return nil
	}
	clientValue := reflect.ValueOf(client).Elem()
	driverValue := clientValue.FieldByName("config").FieldByName("driver")
	if !driverValue.IsValid() {
		return nil
	}
	driver := reflect.NewAt(driverValue.Type(), unsafe.Pointer(driverValue.UnsafeAddr())).Elem().Interface()
	exec, _ := driver.(sqlQueryExecutor)
	return exec
}

func (r *userRepository) userProfileIdentitySQL(ctx context.Context) (sqlQueryExecutor, error) {
	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return nil, fmt.Errorf("sql executor is not configured")
	}
	return exec, nil
}
