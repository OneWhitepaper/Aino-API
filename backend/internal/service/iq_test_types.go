package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqtest"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrIQTestNotFound    = infraerrors.NotFound("IQ_TEST_NOT_FOUND", "Test not found")
	ErrIQTestBusy        = infraerrors.TooManyRequests("IQ_TEST_BUSY", "A test is already running; finish or cancel it first")
	ErrIQTestIdempotency = infraerrors.Conflict("IQ_TEST_IDEMPOTENCY", "This request identifier was already used for different settings")
)

type IQTestConnectionRequest struct {
	Source  string `json:"source"`
	KeyID   int64  `json:"key_id,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

type IQTestRequest struct {
	IQTestConnectionRequest
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	Tier      string `json:"service_tier"`
	Stream    bool   `json:"stream"`
}

// IQTestConfig is a strict credential-free persistence/response allowlist.
type IQTestConfig struct {
	Kind    string `json:"kind"`
	Source  string `json:"source"`
	KeyID   int64  `json:"key_id,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	Tier    string `json:"service_tier"`
	Stream  bool   `json:"stream"`
}

type IQTest struct {
	ID               string        `json:"id"`
	UserID           int64         `json:"-"`
	RequestID        string        `json:"-"`
	Fingerprint      string        `json:"-"`
	Config           IQTestConfig  `json:"config"`
	Status           string        `json:"status"`
	Verdict          string        `json:"verdict"`
	Basis            string        `json:"basis"`
	ErrorCode        string        `json:"error_code"`
	Result           iqtest.Result `json:"result"`
	ReferenceCostUSD *float64      `json:"reference_cost_usd"`
	CreatedAt        time.Time     `json:"created_at"`
	DeadlineAt       time.Time     `json:"deadline_at"`
	FinishedAt       *time.Time    `json:"finished_at"`
}

type IQTestRepository interface {
	Create(context.Context, *IQTest) (*IQTest, bool, error)
	Get(context.Context, int64, string) (*IQTest, error)
	GetByRequest(context.Context, int64, string) (*IQTest, error)
	List(context.Context, int64, int, int) ([]*IQTest, error)
	Finish(context.Context, *IQTest) (bool, error)
}

type iqTestRunner interface {
	Run(context.Context, iqtest.RunInput, func(string)) (iqtest.Result, error)
	Models(context.Context, iqtest.Connection) ([]iqtest.Model, error)
}

type iqTestKeys interface {
	GetByID(context.Context, int64) (*APIKey, error)
	List(context.Context, int64, pagination.PaginationParams, APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error)
}

type IQTestKeyOption struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Masked string `json:"masked"`
}
