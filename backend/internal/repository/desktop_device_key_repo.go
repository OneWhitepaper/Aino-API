package repository

import (
	"context"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func apiKeyCredentialPredicate(secret string) predicate.APIKey {
	return apikey.Or(
		apikey.And(apikey.KeyEQ(secret), apikey.KeyHashTypeEQ("")),
		apikey.And(apikey.KeyEQ(service.HashDesktopDeviceSecret(secret)), apikey.KeyHashTypeEQ("sha256"), apikey.DesktopCredentialTypeEQ(service.DesktopCredentialTypeDevice)),
	)
}

func NewDesktopDeviceKeyRepository(client *dbent.Client) service.DesktopDeviceKeyRepository {
	return &apiKeyRepository{client: client}
}

func (r *apiKeyRepository) ListAuthCacheIndexesByUserID(ctx context.Context, userID int64) ([]string, error) {
	return r.authCacheIndexes(ctx, apikey.UserIDEQ(userID))
}

func (r *apiKeyRepository) ListAuthCacheIndexesByGroupID(ctx context.Context, groupID int64) ([]string, error) {
	return r.authCacheIndexes(ctx, apikey.GroupIDEQ(groupID))
}

func (r *apiKeyRepository) authCacheIndexes(ctx context.Context, scope predicate.APIKey) ([]string, error) {
	keys, err := r.activeQuery().Where(scope).Select(apikey.FieldKey, apikey.FieldKeyHashType).All(ctx)
	if err != nil {
		return nil, err
	}
	indexes := make([]string, 0, len(keys))
	for _, key := range keys {
		indexes = append(indexes, service.APIKeyAuthCacheIndex(key.Key, key.KeyHashType))
	}
	return indexes, nil
}

func (r *apiKeyRepository) GetStoredKeyHashType(ctx context.Context, id int64) (string, error) {
	key, err := r.activeQuery().Where(apikey.IDEQ(id)).Select(apikey.FieldKeyHashType).Only(ctx)
	if err != nil {
		return "", err
	}
	return key.KeyHashType, nil
}

func deviceIdentityScope(userID int64, deviceID string) []predicate.APIKey {
	return []predicate.APIKey{apikey.UserIDEQ(userID), apikey.DesktopDeviceIDEQ(deviceID), apikey.DesktopCredentialTypeEQ(service.DesktopCredentialTypeDevice)}
}

func (r *apiKeyRepository) GetDeviceKey(ctx context.Context, userID int64, deviceID string) (*service.APIKey, error) {
	key, err := r.client.APIKey.Query().Where(deviceIdentityScope(userID, deviceID)...).Only(mixins.SkipSoftDelete(ctx))
	if dbent.IsNotFound(err) {
		return nil, service.ErrDesktopDeviceKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return apiKeyEntityToService(key), nil
}

func (r *apiKeyRepository) ListDeviceKeys(ctx context.Context, userID int64) ([]service.APIKey, error) {
	rows, err := r.client.APIKey.Query().Where(apikey.UserIDEQ(userID), apikey.DesktopCredentialTypeEQ(service.DesktopCredentialTypeDevice)).All(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return nil, err
	}
	keys := make([]service.APIKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, *apiKeyEntityToService(row))
	}
	return keys, nil
}

func (r *apiKeyRepository) EnsureDeviceKey(ctx context.Context, candidate *service.APIKey, expected *int, rotate, reauthorize bool) (*service.APIKey, bool, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	owner, err := tx.User.Query().Where(user.IDEQ(candidate.UserID), user.DeletedAtIsNil()).ForUpdate().Only(ctx)
	if err != nil {
		return nil, false, err
	}
	if owner.Status != service.StatusActive {
		return nil, false, service.ErrUserNotActive
	}
	// Serialize the absent-row case as well as rotation/revocation. This lock is
	// installation-scoped, not a user/global model-call lock.
	lockID := strconv.FormatInt(candidate.UserID, 10) + ":" + candidate.DesktopDeviceID
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", lockID); err != nil {
		return nil, false, err
	}
	row, err := tx.APIKey.Query().Where(deviceIdentityScope(candidate.UserID, candidate.DesktopDeviceID)...).ForUpdate().Only(mixins.SkipSoftDelete(ctx))
	if dbent.IsNotFound(err) {
		if rotate || reauthorize {
			return nil, false, service.ErrDesktopDeviceKeyNotFound
		}
		repo := &apiKeyRepository{client: tx.Client()}
		if err = repo.Create(ctx, candidate); err != nil {
			return nil, false, err
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		return candidate, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	active := row.Status == service.StatusActive && row.DeletedAt == nil
	if !rotate && !reauthorize {
		if !active {
			return nil, false, service.ErrDesktopDeviceKeyRevoked
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		return apiKeyEntityToService(row), false, nil
	}
	if expected == nil || *expected != row.DesktopKeyVersion {
		return nil, false, service.ErrDesktopDeviceKeyVersion
	}
	if !active && !reauthorize {
		return nil, false, service.ErrDesktopDeviceKeyRevoked
	}
	if active && reauthorize {
		return nil, false, service.ErrDesktopDeviceKeyVersion
	}
	updated, err := tx.APIKey.UpdateOneID(row.ID).SetKey(candidate.Key).AddDesktopKeyVersion(1).SetStatus(service.StatusActive).ClearDeletedAt().Save(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return apiKeyEntityToService(updated), true, nil
}

func (r *apiKeyRepository) RevokeDeviceKey(ctx context.Context, userID int64, deviceID string) error {
	count, err := r.client.APIKey.Update().Where(append(deviceIdentityScope(userID, deviceID), apikey.StatusNEQ(service.StatusAPIKeyDisabled))...).SetStatus(service.StatusAPIKeyDisabled).AddDesktopKeyVersion(1).SetUpdatedAt(time.Now()).Save(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return err
	}
	if count == 0 {
		_, err = r.GetDeviceKey(ctx, userID, deviceID)
	}
	return err
}
