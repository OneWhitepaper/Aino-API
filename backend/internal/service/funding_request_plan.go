package service

// FundingRequestPlan is the serializable cost envelope attached to an admitted
// operation. Body is request-local; durable snapshots retain only its bounds.
// NeedsModelBounds asks the selected-account adapter to supply documented model
// limits before dispatch. ProviderBound means those limits also cover server-side
// state and protocols that do not preserve the client's explicit output limit.
type FundingRequestPlan struct {
	Body                   []byte   `json:"-"`
	ContentType            string   `json:"content_type,omitempty"`
	Kind                   string   `json:"kind"`
	Endpoint               string   `json:"endpoint,omitempty"`
	Model                  string   `json:"model,omitempty"`
	InputTokens            int      `json:"input_tokens,omitempty"`
	OutputTokens           int      `json:"output_tokens,omitempty"`
	Count                  int      `json:"count,omitempty"`
	ImageSize              string   `json:"image_size,omitempty"`
	ImageEndpoint          bool     `json:"image_endpoint,omitempty"`
	ImageTokenPricing      bool     `json:"image_token_pricing,omitempty"`
	ImageCountLimit        int      `json:"image_count_limit,omitempty"`
	BatchAccountMultiplier *float64 `json:"batch_account_multiplier,omitempty"`
	VideoResolution        string   `json:"video_resolution,omitempty"`
	VideoDurationSeconds   int      `json:"video_duration_seconds,omitempty"`
	AudioMode              string   `json:"audio_mode,omitempty"`
	AudioUnits             float64  `json:"audio_units,omitempty"`
	MaxDurationSeconds     float64  `json:"max_duration_seconds,omitempty"`
	MediaSHA256            string   `json:"media_sha256,omitempty"`
	SearchCalls            int      `json:"search_calls,omitempty"`
	CacheTokenHours        float64  `json:"cache_token_hours,omitempty"`
	CacheTTLSeconds        int64    `json:"cache_ttl_seconds,omitempty"`
	CachedContentName      string   `json:"cached_content_name,omitempty"`
	CachedContentTokens    int64    `json:"cached_content_tokens,omitempty"`
	NeedsModelBounds       bool     `json:"needs_model_bounds,omitempty"`
	FullContext            bool     `json:"full_context,omitempty"`
	ProviderBound          bool     `json:"provider_bound,omitempty"`
	BillingModels          []string `json:"billing_models,omitempty"`
	MaxToolCalls           int      `json:"max_tool_calls,omitempty"`
	HostedSearch           bool     `json:"hosted_search,omitempty"`
	HostedImages           bool     `json:"hosted_images,omitempty"`
	BodyBytes              int      `json:"body_bytes,omitempty"`
}

const (
	FundingRequestText         = "text"
	FundingRequestImage        = "image"
	FundingRequestBatchImage   = "batch_image"
	FundingRequestVideo        = "video"
	FundingRequestAudio        = "audio"
	FundingRequestSearch       = "search"
	FundingRequestAlphaSearch  = "alpha_search"
	FundingRequestCache        = "cache"
	FundingRequestCacheStorage = "cache_storage"
	FundingRequestControl      = "control"
)
