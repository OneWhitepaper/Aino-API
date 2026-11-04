package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var apiKeyUsageMetadataFields = []string{
	apikey.FieldID, apikey.FieldUserID, apikey.FieldName, apikey.FieldDesktopManaged,
	apikey.FieldGroupID, apikey.FieldStatus, apikey.FieldExpiresAt, apikey.FieldCreatedAt, apikey.FieldUpdatedAt,
}

func apiKeyUsageMetadataPredicate() predicate.APIKey {
	return apikey.Or(apikey.DeletedAtIsNil(), apikey.DesktopManagedEQ(true))
}

// This read-only seam keeps original key IDs usable in billing history without
// widening normal key lookup or authentication, and never selects a credential.
func (r *apiKeyRepository) GetUsageMetadata(ctx context.Context, id int64) (*service.APIKey, error) {
	key, err := r.client.APIKey.Query().Where(apikey.IDEQ(id), apiKeyUsageMetadataPredicate()).
		Select(apiKeyUsageMetadataFields...).Only(mixins.SkipSoftDelete(ctx))
	if dbent.IsNotFound(err) {
		return nil, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return apiKeyEntityToService(key), nil
}

func (r *apiKeyRepository) VerifyUsageOwnership(ctx context.Context, userID int64, ids []int64) ([]int64, error) {
	return r.client.APIKey.Query().Where(apikey.UserIDEQ(userID), apikey.IDIn(ids...), apiKeyUsageMetadataPredicate()).
		IDs(mixins.SkipSoftDelete(ctx))
}
