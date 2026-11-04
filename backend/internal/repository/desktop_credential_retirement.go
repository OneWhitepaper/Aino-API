package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/desktopmodelcredential"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Retirement shares issuance's user lock, so a renewed live lease cannot be
// removed using an expiry read before its renewal committed.
func (r *desktopCredentialRepository) RetireInactiveKeys(ctx context.Context, userID int64) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.User.Query().Where(user.IDEQ(userID), user.DeletedAtIsNil()).ForUpdate().Only(ctx); err != nil {
		return err
	}
	if err = retireInactiveDesktopKeys(ctx, tx.Client(), userID, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func retireInactiveDesktopKeys(ctx context.Context, client *dbent.Client, userID int64, now time.Time) error {
	keys, err := client.APIKey.Query().Where(
		apikey.UserIDEQ(userID), apikey.DesktopManagedEQ(true), apikey.DeletedAtIsNil(),
		apikey.Or(apikey.ExpiresAtLTE(now), apikey.HasDesktopModelCredentialsWith(
			desktopmodelcredential.UserIDEQ(userID),
			desktopmodelcredential.Or(desktopmodelcredential.RevokedAtNotNil(), desktopmodelcredential.ExpiresAtLTE(now)),
		)),
	).All(ctx)
	if err != nil || len(keys) == 0 {
		return err
	}
	ids := make([]int64, len(keys))
	for i, key := range keys {
		ids[i] = key.ID
	}
	// Preserve explicit revocation reasons: they fence the entire parent family.
	if _, err = client.DesktopModelCredential.Update().Where(
		desktopmodelcredential.UserIDEQ(userID), desktopmodelcredential.APIKeyIDIn(ids...), desktopmodelcredential.RevokedAtIsNil(),
	).SetRevokedAt(now).SetRevokeReason("expired").Save(ctx); err != nil {
		return err
	}
	if _, err = client.APIKey.Update().Where(apikey.IDIn(ids...)).SetStatus(service.StatusAPIKeyDisabled).Save(ctx); err != nil {
		return err
	}
	keyRepo := NewAPIKeyRepository(client, nil)
	for _, id := range ids {
		// Reuse key deletion's tombstone path while retaining usage and lease FKs.
		if err = keyRepo.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
