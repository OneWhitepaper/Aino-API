package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

const (
	FundingModePersonalFirst        = "personal_first"
	FundingModePersonalOnly         = "personal_only"
	FundingModeTeamFirst            = "team_first"
	FundingModeTeamOnly             = "team_only"
	FundingModeSubscriptionFirst    = "subscription_first"
	FundingModeSubscriptionOnly     = "subscription_only"
	TeamSharedModeBalanceFirst      = "balance_first"
	TeamSharedModeSubscriptionFirst = "subscription_first"
	TeamSharedModeBalanceOnly       = "balance_only"
	TeamSharedModeSubscriptionOnly  = "subscription_only"
	TeamMemberActive                = "active"
	TeamMemberPaused                = "paused"
	TeamMemberPending               = "pending"
	TeamInvitationPending           = "pending"
	TeamInvitationAccepted          = "accepted"
	TeamInvitationDeclined          = "declined"
	TeamInvitationRevoked           = "revoked"
	TeamFundingSourceBalance        = "team_balance"
	TeamFundingSourceSubscription   = "team_subscription"
)

var (
	ErrTeamFundingNotFound      = infraerrors.NotFound("TEAM_FUNDING_NOT_FOUND", "team funding record not found")
	ErrTeamFundingForbidden     = infraerrors.Forbidden("TEAM_FUNDING_FORBIDDEN", "you do not have permission to manage this team funding record")
	ErrTeamFundingConflict      = infraerrors.Conflict("TEAM_FUNDING_CONFLICT", "team funding operation conflicts with current state")
	ErrTeamFundingInvalid       = infraerrors.BadRequest("TEAM_FUNDING_INVALID", "invalid team funding request")
	ErrTeamFundingCostUnbounded = infraerrors.BadRequest("TEAM_FUNDING_COST_UNBOUNDED", "shared funding requires a verifiable request cost bound")
)

// FundingPreferences are intentionally separate from the team owner policy: only the
// member can write these fields. A nil TeamID means no selected team.
type FundingPreferences struct {
	Configured                bool   `json:"configured"`
	Mode                      string `json:"mode"`
	TeamID                    *int64 `json:"team_id"`
	AllowPersonalBalance      bool   `json:"allow_personal_balance"`
	AllowPersonalSubscription bool   `json:"allow_personal_subscription"`
}
type FundingSubscription struct {
	ID        int64     `json:"id"`
	GroupID   int64     `json:"group_id"`
	GroupName string    `json:"group_name"`
	ExpiresAt time.Time `json:"expires_at"`
}
type FundingView struct {
	FundingPreferences
	PersonalBalance       string                `json:"personal_balance"`
	PersonalSubscriptions []FundingSubscription `json:"personal_subscriptions"`
	Teams                 []TeamSummary         `json:"teams"`
}
type TeamSummary struct {
	ID               int64   `json:"id"`
	OwnerUserID      int64   `json:"owner_user_id"`
	Name             string  `json:"name"`
	Role             string  `json:"role"`
	Status           string  `json:"status"`
	SharedMode       string  `json:"shared_mode"`
	SuggestedMode    string  `json:"suggested_mode"`
	AvailableBalance string  `json:"available_balance"`
	Spent            string  `json:"spent"`
	SpendLimit       *string `json:"spend_limit"`
	RemainingLimit   *string `json:"remaining_limit"`
}
type TeamDetail struct {
	TeamSummary
	SharedSubscriptionIDs []int64               `json:"shared_subscription_ids"`
	Subscriptions         []FundingSubscription `json:"subscriptions"`
	Members               []TeamMember          `json:"members"`
	Invitations           []Invitation          `json:"invitations"`
}
type TeamMember struct {
	UserID        int64   `json:"user_id"`
	Username      string  `json:"username"`
	Status        string  `json:"status"`
	SpendLimit    *string `json:"spend_limit"`
	Spent         string  `json:"spent"`
	Reserved      string  `json:"reserved"`
	SharedMode    *string `json:"shared_mode"`
	SuggestedMode *string `json:"suggested_mode"`
}
type Invitation struct {
	ID          int64     `json:"id"`
	TeamID      int64     `json:"team_id"`
	TeamName    string    `json:"team_name"`
	OwnerUserID int64     `json:"owner_user_id"`
	UserID      int64     `json:"user_id"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}
type TeamLedgerEntry struct {
	ID                 int64     `json:"id"`
	RequestID          string    `json:"request_id"`
	UserID             int64     `json:"user_id"`
	PayerUserID        int64     `json:"payer_user_id"`
	TeamID             int64     `json:"-"`
	Source             string    `json:"source"`
	SubscriptionID     *int64    `json:"subscription_id"`
	Amount             string    `json:"amount"`
	UnauthorizedAmount string    `json:"unauthorized_amount"`
	CreatedAt          time.Time `json:"created_at"`
}
type TeamLedgerPage struct {
	Items    []TeamLedgerEntry `json:"items"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}
type FundingTeams struct {
	Owned  []TeamSummary `json:"owned"`
	Joined []TeamSummary `json:"joined"`
}

type TeamCreateCommand struct {
	Name string `json:"name"`
}
type TeamUpdateCommand struct {
	Name                  *string  `json:"name"`
	SharedMode            *string  `json:"shared_mode"`
	SharedSubscriptionIDs *[]int64 `json:"shared_subscription_ids"`
	SuggestedMode         *string  `json:"suggested_mode"`
}
type InvitationCreateCommand struct {
	UserID int64 `json:"user_id"`
}
type TeamMemberPatch struct {
	Status        string                `json:"status"`
	SpendLimit    OptionalFundingAmount `json:"spend_limit"`
	SharedMode    OptionalString        `json:"shared_mode"`
	SuggestedMode OptionalString        `json:"suggested_mode"`
}
type OptionalString struct {
	Set   bool
	Value *string
}

func (o *OptionalString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

type OptionalFundingAmount struct {
	Set   bool
	Value *string
}

func (o *OptionalFundingAmount) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	a, e := ParseTeamFundingAmount(v)
	if e != nil {
		return e
	}
	s := a.StringFixed(8)
	o.Value = &s
	return nil
}

type FundingUpdateCommand struct {
	Mode                      string `json:"mode"`
	TeamID                    *int64 `json:"team_id"`
	AllowPersonalBalance      bool   `json:"allow_personal_balance"`
	AllowPersonalSubscription bool   `json:"allow_personal_subscription"`
}
type FundingContext struct {
	ImplicitOwner bool
	Preferences   FundingPreferences
	Team          *TeamFundingCandidate
}
type TeamFundingCandidate struct {
	TeamID                            int64
	MemberUserID                      int64
	PayerUserID                       int64
	SharedMode                        string
	SubscriptionIDs                   []int64
	SpendLimit                        *string
	Spent, Reserved, AvailableBalance string
	Status                            string
}
type TeamFundingReserveCommand struct {
	RequestID      string
	APIKeyID       int64
	GroupID        int64
	MemberUserID   int64
	PayerUserID    int64
	TeamID         int64
	Source         string
	SubscriptionID *int64
	Amount         string
}
type TeamFundingReservation struct {
	ID                 int64     `json:"id"`
	RequestID          string    `json:"request_id"`
	APIKeyID           int64     `json:"api_key_id"`
	TeamID             int64     `json:"team_id"`
	MemberUserID       int64     `json:"member_user_id"`
	PayerUserID        int64     `json:"payer_user_id"`
	Source             string    `json:"source"`
	SubscriptionID     *int64    `json:"subscription_id,omitempty"`
	Amount             string    `json:"amount"`
	ReservedAmount     string    `json:"reserved_amount"`
	SettledAmount      string    `json:"settled_amount"`
	UnauthorizedAmount string    `json:"unauthorized_amount"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
}
type TeamFundingSettlement struct {
	ChargedAmount      string
	UnauthorizedAmount string
	NewBalance         *float64
	AlreadySettled     bool
}

// TeamFundingRepository is the persistence boundary for both management APIs and billing.
type TeamFundingRepository interface {
	GetFunding(context.Context, int64) (*FundingView, error)
	UpdateFunding(context.Context, int64, *FundingUpdateCommand) (*FundingView, error)
	ListTeams(context.Context, int64) (*FundingTeams, error)
	CreateTeam(context.Context, int64, *TeamCreateCommand) (*TeamDetail, error)
	GetTeam(context.Context, int64, int64) (*TeamDetail, error)
	UpdateTeam(context.Context, int64, int64, *TeamUpdateCommand) (*TeamDetail, error)
	CreateInvitation(context.Context, int64, int64, *InvitationCreateCommand) (*Invitation, error)
	ListInvitations(context.Context, int64) ([]Invitation, error)
	AcceptInvitation(context.Context, int64, int64) error
	DeclineInvitation(context.Context, int64, int64) error
	RevokeInvitation(context.Context, int64, int64, int64) error
	UpdateMember(context.Context, int64, int64, int64, *TeamMemberPatch) (*TeamMember, error)
	RemoveMember(context.Context, int64, int64, int64) error
	ListLedger(context.Context, int64, int64, int, int) (*TeamLedgerPage, error)
	GetFundingContext(context.Context, int64) (*FundingContext, error)
	Reserve(context.Context, *TeamFundingReserveCommand) (*TeamFundingReservation, error)
	Release(context.Context, int64, int64) error
	MarkDispatched(context.Context, int64, int64) error
	SettleTeamFundingReservationTx(context.Context, *sql.Tx, int64, string, int64, string) (*TeamFundingSettlement, error)
}

type TeamFundingService struct{ repo TeamFundingRepository }

func NewTeamFundingService(repo TeamFundingRepository) *TeamFundingService {
	return &TeamFundingService{repo: repo}
}
func (s *TeamFundingService) GetFunding(c context.Context, u int64) (*FundingView, error) {
	return s.repo.GetFunding(c, u)
}
func (s *TeamFundingService) UpdateFunding(c context.Context, u int64, v *FundingUpdateCommand) (*FundingView, error) {
	if err := ValidateFundingUpdate(v); err != nil {
		return nil, err
	}
	return s.repo.UpdateFunding(c, u, v)
}
func (s *TeamFundingService) ListTeams(c context.Context, u int64) (*FundingTeams, error) {
	return s.repo.ListTeams(c, u)
}
func (s *TeamFundingService) CreateTeam(c context.Context, u int64, v *TeamCreateCommand) (*TeamDetail, error) {
	if v == nil || strings.TrimSpace(v.Name) == "" || len([]rune(v.Name)) > 100 {
		return nil, ErrTeamFundingInvalid
	}
	return s.repo.CreateTeam(c, u, v)
}
func (s *TeamFundingService) GetTeam(c context.Context, u, id int64) (*TeamDetail, error) {
	return s.repo.GetTeam(c, u, id)
}
func (s *TeamFundingService) UpdateTeam(c context.Context, u, id int64, v *TeamUpdateCommand) (*TeamDetail, error) {
	if err := ValidateTeamUpdate(v); err != nil {
		return nil, err
	}
	return s.repo.UpdateTeam(c, u, id, v)
}
func (s *TeamFundingService) CreateInvitation(c context.Context, u, id int64, v *InvitationCreateCommand) (*Invitation, error) {
	if v == nil || v.UserID <= 0 {
		return nil, ErrTeamFundingInvalid
	}
	return s.repo.CreateInvitation(c, u, id, v)
}
func (s *TeamFundingService) ListInvitations(c context.Context, u int64) ([]Invitation, error) {
	return s.repo.ListInvitations(c, u)
}
func (s *TeamFundingService) AcceptInvitation(c context.Context, u, id int64) error {
	return s.repo.AcceptInvitation(c, u, id)
}
func (s *TeamFundingService) DeclineInvitation(c context.Context, u, id int64) error {
	return s.repo.DeclineInvitation(c, u, id)
}
func (s *TeamFundingService) RevokeInvitation(c context.Context, u, team, id int64) error {
	return s.repo.RevokeInvitation(c, u, team, id)
}
func (s *TeamFundingService) UpdateMember(c context.Context, u, team, member int64, v *TeamMemberPatch) (*TeamMember, error) {
	if err := ValidateTeamMemberPatch(v); err != nil {
		return nil, err
	}
	return s.repo.UpdateMember(c, u, team, member, v)
}
func (s *TeamFundingService) RemoveMember(c context.Context, u, team, member int64) error {
	return s.repo.RemoveMember(c, u, team, member)
}
func (s *TeamFundingService) ListLedger(c context.Context, u, team int64, page, size int) (*TeamLedgerPage, error) {
	return s.repo.ListLedger(c, u, team, page, size)
}
func (s *TeamFundingService) GetFundingContext(c context.Context, u int64) (*FundingContext, error) {
	return s.repo.GetFundingContext(c, u)
}
func (s *TeamFundingService) Reserve(c context.Context, v *TeamFundingReserveCommand) (*TeamFundingReservation, error) {
	if err := ValidateReserve(v); err != nil {
		return nil, err
	}
	return s.repo.Reserve(c, v)
}
func (s *TeamFundingService) Release(c context.Context, id, u int64) error {
	return s.repo.Release(c, id, u)
}
func (s *TeamFundingService) MarkDispatched(c context.Context, id, u int64) error {
	return s.repo.MarkDispatched(c, id, u)
}

func ParseTeamFundingAmount(s string) (decimal.Decimal, error) {
	if strings.TrimSpace(s) != s || s == "" || strings.ContainsAny(s, "eE+") {
		return decimal.Zero, ErrTeamFundingInvalid
	}
	d, e := decimal.NewFromString(s)
	if e != nil || d.IsNegative() || d.Exponent() < -8 || d.GreaterThan(decimal.RequireFromString("999999999999.99999999")) {
		return decimal.Zero, ErrTeamFundingInvalid
	}
	return d, nil
}
func ValidateFundingPreferences(v FundingPreferences) error {
	switch v.Mode {
	case FundingModePersonalFirst, FundingModePersonalOnly, FundingModeTeamFirst, FundingModeTeamOnly, FundingModeSubscriptionFirst, FundingModeSubscriptionOnly:
	default:
		return ErrTeamFundingInvalid
	}
	if v.TeamID != nil && *v.TeamID <= 0 {
		return ErrTeamFundingInvalid
	}
	return nil
}
func ValidateFundingUpdate(v *FundingUpdateCommand) error {
	if v == nil {
		return ErrTeamFundingInvalid
	}
	return ValidateFundingPreferences(FundingPreferences{Mode: v.Mode, TeamID: v.TeamID, AllowPersonalBalance: v.AllowPersonalBalance, AllowPersonalSubscription: v.AllowPersonalSubscription})
}
func ValidateTeamUpdate(v *TeamUpdateCommand) error {
	if v == nil {
		return ErrTeamFundingInvalid
	}
	if v.SharedMode != nil && !validSharedMode(*v.SharedMode) {
		return ErrTeamFundingInvalid
	}
	if v.SuggestedMode != nil && *v.SuggestedMode != "" && !validFundingMode(*v.SuggestedMode) {
		return ErrTeamFundingInvalid
	}
	if v.Name != nil && (strings.TrimSpace(*v.Name) == "" || len([]rune(*v.Name)) > 100) {
		return ErrTeamFundingInvalid
	}
	return nil
}
func ValidateTeamMemberPatch(v *TeamMemberPatch) error {
	if v == nil {
		return ErrTeamFundingInvalid
	}
	if v.Status != "" && v.Status != TeamMemberActive && v.Status != TeamMemberPaused {
		return ErrTeamFundingInvalid
	}
	if v.SpendLimit.Set && v.SpendLimit.Value != nil {
		if _, e := ParseTeamFundingAmount(*v.SpendLimit.Value); e != nil {
			return e
		}
	}
	if v.SharedMode.Set && v.SharedMode.Value != nil && !validSharedMode(*v.SharedMode.Value) {
		return ErrTeamFundingInvalid
	}
	if v.SuggestedMode.Set && v.SuggestedMode.Value != nil && !validFundingMode(*v.SuggestedMode.Value) {
		return ErrTeamFundingInvalid
	}
	return nil
}
func ValidateReserve(v *TeamFundingReserveCommand) error {
	if v == nil || strings.TrimSpace(v.RequestID) == "" || v.APIKeyID <= 0 || v.TeamID <= 0 || v.MemberUserID <= 0 || v.PayerUserID <= 0 {
		return ErrTeamFundingInvalid
	}
	if v.Source != TeamFundingSourceBalance && v.Source != TeamFundingSourceSubscription {
		return ErrTeamFundingInvalid
	}
	if v.Source == TeamFundingSourceSubscription && (v.SubscriptionID == nil || *v.SubscriptionID <= 0) {
		return ErrTeamFundingInvalid
	}
	if _, e := ParseTeamFundingAmount(v.Amount); e != nil {
		return e
	}
	return nil
}
func validFundingMode(s string) bool {
	switch s {
	case FundingModePersonalFirst, FundingModePersonalOnly, FundingModeTeamFirst, FundingModeTeamOnly, FundingModeSubscriptionFirst, FundingModeSubscriptionOnly:
		return true
	}
	return false
}
func validSharedMode(s string) bool {
	switch s {
	case TeamSharedModeBalanceFirst, TeamSharedModeSubscriptionFirst, TeamSharedModeBalanceOnly, TeamSharedModeSubscriptionOnly:
		return true
	}
	return false
}

// These methods make management and billing contracts easy to use by integrations.
func (s *TeamFundingService) SettleTeamFundingReservationTx(c context.Context, tx *sql.Tx, id int64, r string, key int64, a string) (*TeamFundingSettlement, error) {
	return s.repo.SettleTeamFundingReservationTx(c, tx, id, r, key, a)
}
