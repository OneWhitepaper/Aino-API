package service

import "context"

type apiKeyUsageMetadataReader interface {
	GetUsageMetadata(context.Context, int64) (*APIKey, error)
	VerifyUsageOwnership(context.Context, int64, []int64) ([]int64, error)
}

func (s *APIKeyService) GetUsageMetadata(ctx context.Context, id int64) (*APIKey, error) {
	if repo, ok := s.apiKeyRepo.(apiKeyUsageMetadataReader); ok {
		return repo.GetUsageMetadata(ctx, id)
	}
	key, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	metadata := *key
	metadata.Key = ""
	return &metadata, nil
}

func (s *APIKeyService) VerifyUsageOwnership(ctx context.Context, userID int64, ids []int64) ([]int64, error) {
	if repo, ok := s.apiKeyRepo.(apiKeyUsageMetadataReader); ok {
		return repo.VerifyUsageOwnership(ctx, userID, ids)
	}
	return s.VerifyOwnership(ctx, userID, ids)
}
