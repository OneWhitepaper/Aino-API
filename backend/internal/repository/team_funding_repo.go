package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type teamFundingRepository struct{ db *sql.DB }

func NewTeamFundingRepository(_ *dbent.Client, db *sql.DB) service.TeamFundingRepository {
	return &teamFundingRepository{db: db}
}

type teamFundingQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func teamFundingTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
func teamOwner(ctx context.Context, q teamFundingQuery, actor, team int64, lock bool) (int64, error) {
	var owner int64
	query := "SELECT owner_user_id FROM teams WHERE id=$1 AND status<>'deleted'"
	if lock {
		query += " FOR UPDATE"
	}
	e := q.QueryRowContext(ctx, query, team).Scan(&owner)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, service.ErrTeamFundingNotFound
	}
	if e != nil {
		return 0, e
	}
	if owner != actor {
		return owner, service.ErrTeamFundingForbidden
	}
	return owner, nil
}
func teamReadRole(ctx context.Context, q teamFundingQuery, user, team int64, history bool) (string, error) {
	var owner int64
	var state string
	if e := q.QueryRowContext(ctx, "SELECT owner_user_id,status FROM teams WHERE id=$1", team).Scan(&owner, &state); errors.Is(e, sql.ErrNoRows) {
		return "", service.ErrTeamFundingNotFound
	} else if e != nil {
		return "", e
	}
	if !history && state == "deleted" {
		return "", service.ErrTeamFundingNotFound
	}
	if owner == user {
		return "owner", nil
	}
	var status string
	e := q.QueryRowContext(ctx, "SELECT status FROM team_members WHERE team_id=$1 AND user_id=$2", team, user).Scan(&status)
	if errors.Is(e, sql.ErrNoRows) || (!history && status == "removed") {
		return "", service.ErrTeamFundingForbidden
	}
	if e != nil {
		return "", e
	}
	return "member", nil
}

func (r *teamFundingRepository) GetFunding(ctx context.Context, user int64) (*service.FundingView, error) {
	v := &service.FundingView{FundingPreferences: service.FundingPreferences{Mode: "personal_first", AllowPersonalBalance: true, AllowPersonalSubscription: true}, PersonalSubscriptions: []service.FundingSubscription{}, Teams: []service.TeamSummary{}}
	e := r.db.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1 AND deleted_at IS NULL", user).Scan(&v.PersonalBalance)
	if e != nil {
		return nil, e
	}
	e = r.db.QueryRowContext(ctx, "SELECT mode,team_id,allow_personal_balance,allow_personal_subscription FROM funding_preferences WHERE user_id=$1", user).Scan(&v.Mode, &v.TeamID, &v.AllowPersonalBalance, &v.AllowPersonalSubscription)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	v.Configured = e == nil
	v.PersonalSubscriptions, e = teamSubscriptions(ctx, r.db, user, nil)
	if e != nil {
		return nil, e
	}
	teams, e := r.ListTeams(ctx, user)
	if e != nil {
		return nil, e
	}
	v.Teams = append(v.Teams, teams.Owned...)
	v.Teams = append(v.Teams, teams.Joined...)
	return v, nil
}
func (r *teamFundingRepository) UpdateFunding(ctx context.Context, user int64, v *service.FundingUpdateCommand) (*service.FundingView, error) {
	if e := service.ValidateFundingUpdate(v); e != nil {
		return nil, e
	}
	e := teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		if v.TeamID != nil {
			// Match the team -> member -> preferences order used by reservations
			// and deletion, so selecting a team cannot race its revocation.
			var state string
			if e := tx.QueryRowContext(ctx, "SELECT status FROM teams WHERE id=$1 AND owner_user_id<>$2 FOR SHARE", *v.TeamID, user).Scan(&state); e != nil || state == "deleted" {
				return service.ErrTeamFundingForbidden
			}
			var status string
			if e := tx.QueryRowContext(ctx, "SELECT status FROM team_members WHERE team_id=$1 AND user_id=$2 FOR SHARE", *v.TeamID, user).Scan(&status); e != nil || status == "removed" {
				return service.ErrTeamFundingForbidden
			}
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO funding_preferences(user_id,mode,team_id,allow_personal_balance,allow_personal_subscription) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id) DO UPDATE SET mode=EXCLUDED.mode,team_id=EXCLUDED.team_id,allow_personal_balance=EXCLUDED.allow_personal_balance,allow_personal_subscription=EXCLUDED.allow_personal_subscription,updated_at=NOW()`, user, v.Mode, v.TeamID, v.AllowPersonalBalance, v.AllowPersonalSubscription)
		return e
	})
	if e != nil {
		return nil, e
	}
	return r.GetFunding(ctx, user)
}

const teamSummarySQL = `SELECT t.id,t.owner_user_id,t.name,CASE WHEN t.owner_user_id=$1 THEN 'owner' ELSE 'member' END,CASE WHEN t.owner_user_id=$1 THEN t.status ELSE m.status END,COALESCE(m.shared_mode,t.shared_mode),COALESCE(m.suggested_mode,t.suggested_mode),u.balance::text,CASE WHEN t.owner_user_id=$1 THEN COALESCE((SELECT SUM(spent) FROM team_members WHERE team_id=t.id),0)::numeric(20,8)::text ELSE m.spent::text END,CASE WHEN t.owner_user_id=$1 THEN NULL ELSE m.spend_limit::text END,CASE WHEN t.owner_user_id=$1 OR m.spend_limit IS NULL THEN NULL ELSE GREATEST(m.spend_limit-m.spent-m.reserved,0)::numeric(20,8)::text END FROM teams t JOIN users u ON u.id=t.owner_user_id LEFT JOIN team_members m ON m.team_id=t.id AND m.user_id=$1 WHERE t.status<>'deleted' AND (t.owner_user_id=$1 OR (m.user_id=$1 AND m.status<>'removed'))`

func scanTeamSummary(row interface{ Scan(...any) error }) (v service.TeamSummary, e error) {
	e = row.Scan(&v.ID, &v.OwnerUserID, &v.Name, &v.Role, &v.Status, &v.SharedMode, &v.SuggestedMode, &v.AvailableBalance, &v.Spent, &v.SpendLimit, &v.RemainingLimit)
	return
}
func (r *teamFundingRepository) ListTeams(ctx context.Context, user int64) (*service.FundingTeams, error) {
	out := &service.FundingTeams{Owned: []service.TeamSummary{}, Joined: []service.TeamSummary{}}
	rows, e := r.db.QueryContext(ctx, teamSummarySQL+" ORDER BY t.id", user)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		v, e := scanTeamSummary(rows)
		if e != nil {
			return nil, e
		}
		if v.Role == "owner" {
			out.Owned = append(out.Owned, v)
		} else {
			out.Joined = append(out.Joined, v)
		}
	}
	return out, rows.Err()
}
func (r *teamFundingRepository) CreateTeam(ctx context.Context, user int64, v *service.TeamCreateCommand) (*service.TeamDetail, error) {
	if v == nil || strings.TrimSpace(v.Name) == "" || len([]rune(v.Name)) > 100 {
		return nil, service.ErrTeamFundingInvalid
	}
	var id int64
	e := r.db.QueryRowContext(ctx, "INSERT INTO teams(owner_user_id,name) VALUES($1,$2) RETURNING id", user, strings.TrimSpace(v.Name)).Scan(&id)
	if e != nil {
		return nil, e
	}
	return r.GetTeam(ctx, user, id)
}
func (r *teamFundingRepository) GetTeam(ctx context.Context, user, team int64) (*service.TeamDetail, error) {
	role, e := teamReadRole(ctx, r.db, user, team, false)
	if e != nil {
		return nil, e
	}
	summary, e := scanTeamSummary(r.db.QueryRowContext(ctx, teamSummarySQL+" AND t.id=$2", user, team))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, service.ErrTeamFundingNotFound
	}
	if e != nil {
		return nil, e
	}
	v := &service.TeamDetail{TeamSummary: summary, SharedSubscriptionIDs: []int64{}, Subscriptions: []service.FundingSubscription{}, Members: []service.TeamMember{}, Invitations: []service.Invitation{}}
	rows, e := r.db.QueryContext(ctx, "SELECT subscription_id FROM team_shared_subscriptions WHERE team_id=$1 ORDER BY subscription_id", team)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			_ = rows.Close()
			return nil, e
		}
		v.SharedSubscriptionIDs = append(v.SharedSubscriptionIDs, id)
	}
	e = rows.Err()
	_ = rows.Close()
	if e != nil {
		return nil, e
	}
	v.Subscriptions, e = teamSubscriptions(ctx, r.db, summary.OwnerUserID, &team)
	if e != nil {
		return nil, e
	}
	if role == "owner" {
		v.Members, e = teamMembers(ctx, r.db, team, 0)
		if e != nil {
			return nil, e
		}
		v.Invitations, e = teamInvitations(ctx, r.db, team, 0, false)
	} else {
		v.Members, e = teamMembers(ctx, r.db, team, user)
	}
	return v, e
}
func teamMembers(ctx context.Context, q teamFundingQuery, team, user int64) ([]service.TeamMember, error) {
	rows, e := q.QueryContext(ctx, `SELECT m.user_id,u.username,m.status,m.spend_limit::text,m.spent::text,m.reserved::text,m.shared_mode,m.suggested_mode FROM team_members m JOIN users u ON u.id=m.user_id WHERE m.team_id=$1 AND ($2::bigint=0 OR m.user_id=$2) ORDER BY m.user_id`, team, user)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []service.TeamMember{}
	for rows.Next() {
		var m service.TeamMember
		if e = rows.Scan(&m.UserID, &m.Username, &m.Status, &m.SpendLimit, &m.Spent, &m.Reserved, &m.SharedMode, &m.SuggestedMode); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func teamSubscriptions(ctx context.Context, q teamFundingQuery, user int64, team *int64) ([]service.FundingSubscription, error) {
	rows, e := q.QueryContext(ctx, `SELECT s.id,s.group_id,g.name,s.expires_at FROM user_subscriptions s JOIN groups g ON g.id=s.group_id WHERE s.user_id=$1 AND s.deleted_at IS NULL AND g.deleted_at IS NULL AND g.status='active' AND s.status='active' AND s.starts_at<=NOW() AND s.expires_at>NOW() AND ($2::bigint IS NULL OR EXISTS(SELECT 1 FROM team_shared_subscriptions ts WHERE ts.team_id=$2 AND ts.subscription_id=s.id)) ORDER BY s.id`, user, team)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []service.FundingSubscription{}
	for rows.Next() {
		var s service.FundingSubscription
		if e = rows.Scan(&s.ID, &s.GroupID, &s.GroupName, &s.ExpiresAt); e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *teamFundingRepository) UpdateTeam(ctx context.Context, user, team int64, v *service.TeamUpdateCommand) (*service.TeamDetail, error) {
	if e := service.ValidateTeamUpdate(v); e != nil {
		return nil, e
	}
	e := teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		if _, e := teamOwner(ctx, tx, user, team, true); e != nil {
			return e
		}
		if v.SharedSubscriptionIDs != nil {
			ids := *v.SharedSubscriptionIDs
			seen := map[int64]bool{}
			for _, id := range ids {
				if id <= 0 || seen[id] {
					return service.ErrTeamFundingInvalid
				}
				seen[id] = true
				var ok bool
				if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_subscriptions s JOIN groups g ON g.id=s.group_id WHERE s.id=$1 AND s.user_id=$2 AND s.deleted_at IS NULL AND g.deleted_at IS NULL AND g.status='active' AND s.status='active' AND s.starts_at<=NOW() AND s.expires_at>NOW())`, id, user).Scan(&ok); e != nil {
					return e
				}
				if !ok {
					return service.ErrTeamFundingForbidden
				}
			}
			if _, e := tx.ExecContext(ctx, "DELETE FROM team_shared_subscriptions WHERE team_id=$1", team); e != nil {
				return e
			}
			for _, id := range ids {
				if _, e := tx.ExecContext(ctx, "INSERT INTO team_shared_subscriptions(team_id,subscription_id) VALUES($1,$2)", team, id); e != nil {
					return e
				}
			}
		}
		_, e := tx.ExecContext(ctx, "UPDATE teams SET name=COALESCE($2,name),shared_mode=COALESCE($3,shared_mode),suggested_mode=COALESCE($4,suggested_mode),updated_at=NOW() WHERE id=$1", team, v.Name, v.SharedMode, v.SuggestedMode)
		return e
	})
	if e != nil {
		return nil, e
	}
	return r.GetTeam(ctx, user, team)
}

// DeleteTeam revokes future sharing while retaining payer identities and escrow
// for work that was already accepted. Team locking serializes it with admission.
func (r *teamFundingRepository) DeleteTeam(ctx context.Context, user, team int64) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		var owner int64
		var state string
		e := tx.QueryRowContext(ctx, "SELECT owner_user_id,status FROM teams WHERE id=$1 FOR UPDATE", team).Scan(&owner, &state)
		if errors.Is(e, sql.ErrNoRows) {
			return service.ErrTeamFundingNotFound
		}
		if e != nil {
			return e
		}
		if owner != user {
			return service.ErrTeamFundingForbidden
		}
		if state == "deleted" {
			return nil
		}
		for _, query := range []string{
			"UPDATE teams SET status='deleted',updated_at=NOW() WHERE id=$1",
			"UPDATE team_members SET status='removed',updated_at=NOW() WHERE team_id=$1 AND status<>'removed'",
			"UPDATE team_invitations SET status='revoked',updated_at=NOW() WHERE team_id=$1 AND status='pending'",
			// Preserve the member's mode and personal authorizations. In particular,
			// team_only must never become permission to charge a personal wallet.
			"UPDATE funding_preferences SET team_id=NULL,updated_at=NOW() WHERE team_id=$1",
		} {
			if _, e = tx.ExecContext(ctx, query, team); e != nil {
				return e
			}
		}
		return nil
	})
}

const invitationSQL = `SELECT i.id,i.team_id,t.name,t.owner_user_id,i.user_id,i.status,i.expires_at,i.created_at FROM team_invitations i JOIN teams t ON t.id=i.team_id`

func scanInvitation(row interface{ Scan(...any) error }) (v service.Invitation, e error) {
	e = row.Scan(&v.ID, &v.TeamID, &v.TeamName, &v.OwnerUserID, &v.UserID, &v.Status, &v.ExpiresAt, &v.CreatedAt)
	return
}
func teamInvitations(ctx context.Context, q teamFundingQuery, team, user int64, pending bool) ([]service.Invitation, error) {
	query := invitationSQL + " WHERE ($1::bigint=0 OR i.team_id=$1) AND ($2::bigint=0 OR i.user_id=$2)"
	if pending {
		query += " AND t.status<>'deleted' AND i.status='pending' AND i.expires_at>NOW()"
	}
	rows, e := q.QueryContext(ctx, query+" ORDER BY i.id DESC", team, user)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []service.Invitation{}
	for rows.Next() {
		v, e := scanInvitation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *teamFundingRepository) CreateInvitation(ctx context.Context, user, team int64, v *service.InvitationCreateCommand) (*service.Invitation, error) {
	if v == nil || v.UserID <= 0 {
		return nil, service.ErrTeamFundingInvalid
	}
	var id int64
	e := teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		if _, e := teamOwner(ctx, tx, user, team, true); e != nil {
			return e
		}
		if v.UserID == user {
			return service.ErrTeamFundingInvalid
		}
		var exists bool
		if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active')", v.UserID).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return service.ErrUserNotFound
		}
		if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM team_members WHERE team_id=$1 AND user_id=$2 AND status<>'removed')", team, v.UserID).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return service.ErrTeamFundingConflict
		}
		if _, e := tx.ExecContext(ctx, "UPDATE team_invitations SET status='revoked',updated_at=NOW() WHERE team_id=$1 AND user_id=$2 AND status='pending' AND expires_at<=NOW()", team, v.UserID); e != nil {
			return e
		}
		return tx.QueryRowContext(ctx, `INSERT INTO team_invitations(team_id,user_id) VALUES($1,$2) ON CONFLICT(team_id,user_id) WHERE status='pending' DO UPDATE SET user_id=EXCLUDED.user_id RETURNING id`, team, v.UserID).Scan(&id)
	})
	if e != nil {
		return nil, e
	}
	v2, e := scanInvitation(r.db.QueryRowContext(ctx, invitationSQL+" WHERE i.id=$1", id))
	return &v2, e
}
func (r *teamFundingRepository) ListInvitations(ctx context.Context, user int64) ([]service.Invitation, error) {
	return teamInvitations(ctx, r.db, 0, user, true)
}
func (r *teamFundingRepository) AcceptInvitation(ctx context.Context, user, id int64) error {
	return r.respondInvitation(ctx, user, id, true)
}
func (r *teamFundingRepository) DeclineInvitation(ctx context.Context, user, id int64) error {
	return r.respondInvitation(ctx, user, id, false)
}
func (r *teamFundingRepository) respondInvitation(ctx context.Context, user, id int64, accept bool) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		var team, target int64
		e := tx.QueryRowContext(ctx, "SELECT team_id,user_id FROM team_invitations WHERE id=$1", id).Scan(&team, &target)
		if errors.Is(e, sql.ErrNoRows) {
			return service.ErrTeamFundingNotFound
		}
		if e != nil {
			return e
		}
		if target != user {
			return service.ErrTeamFundingForbidden
		}
		var state string
		if e := tx.QueryRowContext(ctx, "SELECT status FROM teams WHERE id=$1 FOR UPDATE", team).Scan(&state); e != nil {
			return e
		}
		if state == "deleted" {
			return service.ErrTeamFundingNotFound
		}
		v, e := scanInvitation(tx.QueryRowContext(ctx, invitationSQL+" WHERE i.id=$1 FOR UPDATE OF i", id))
		if e != nil {
			return e
		}
		desired := "declined"
		if accept {
			desired = "accepted"
		}
		if v.Status == desired {
			return nil
		}
		if v.Status != "pending" || !v.ExpiresAt.After(time.Now().UTC()) {
			return service.ErrTeamFundingConflict
		}
		if state != "active" {
			return service.ErrTeamFundingConflict
		}
		if accept {
			if _, e := tx.ExecContext(ctx, `INSERT INTO team_members(team_id,user_id) VALUES($1,$2) ON CONFLICT(team_id,user_id) DO UPDATE SET status='active',updated_at=NOW()`, team, user); e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, "UPDATE team_invitations SET status=$2,updated_at=NOW() WHERE id=$1", id, desired)
		return e
	})
}
func (r *teamFundingRepository) RevokeInvitation(ctx context.Context, user, team, id int64) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		if _, e := teamOwner(ctx, tx, user, team, true); e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, "UPDATE team_invitations SET status='revoked',updated_at=NOW() WHERE id=$1 AND team_id=$2 AND status='pending'", id, team)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if n == 0 {
			return service.ErrTeamFundingConflict
		}
		return e
	})
}
func (r *teamFundingRepository) UpdateMember(ctx context.Context, user, team, member int64, v *service.TeamMemberPatch) (*service.TeamMember, error) {
	if e := service.ValidateTeamMemberPatch(v); e != nil {
		return nil, e
	}
	e := teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		if _, e := teamOwner(ctx, tx, user, team, true); e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `UPDATE team_members SET status=CASE WHEN $3='' THEN status ELSE $3 END,spend_limit=CASE WHEN $4 THEN $5::numeric ELSE spend_limit END,shared_mode=CASE WHEN $6 THEN $7::text ELSE shared_mode END,suggested_mode=CASE WHEN $8 THEN $9::text ELSE suggested_mode END,updated_at=NOW() WHERE team_id=$1 AND user_id=$2 AND status<>'removed'`, team, member, v.Status, v.SpendLimit.Set, v.SpendLimit.Value, v.SharedMode.Set, v.SharedMode.Value, v.SuggestedMode.Set, v.SuggestedMode.Value)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if n == 0 {
			return service.ErrTeamFundingNotFound
		}
		return e
	})
	if e != nil {
		return nil, e
	}
	members, e := teamMembers(ctx, r.db, team, member)
	if e != nil {
		return nil, e
	}
	if len(members) != 1 {
		return nil, service.ErrTeamFundingNotFound
	}
	return &members[0], nil
}
func (r *teamFundingRepository) RemoveMember(ctx context.Context, user, team, member int64) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		var owner int64
		if e := tx.QueryRowContext(ctx, "SELECT owner_user_id FROM teams WHERE id=$1 AND status<>'deleted' FOR UPDATE", team).Scan(&owner); errors.Is(e, sql.ErrNoRows) {
			return service.ErrTeamFundingNotFound
		} else if e != nil {
			return e
		}
		if owner != user && user != member {
			return service.ErrTeamFundingForbidden
		}
		res, e := tx.ExecContext(ctx, "UPDATE team_members SET status='removed',updated_at=NOW() WHERE team_id=$1 AND user_id=$2", team, member)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if n == 0 {
			return service.ErrTeamFundingNotFound
		}
		return e
	})
}
func (r *teamFundingRepository) ListLedger(ctx context.Context, user, team int64, page, size int) (*service.TeamLedgerPage, error) {
	role, e := teamReadRole(ctx, r.db, user, team, true)
	if e != nil {
		return nil, e
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	filterUser := int64(0)
	if role != "owner" {
		filterUser = user
	}
	out := &service.TeamLedgerPage{Items: []service.TeamLedgerEntry{}, Page: page, PageSize: size}
	if e := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM team_funding_ledger WHERE team_id=$1 AND ($2::bigint=0 OR user_id=$2)", team, filterUser).Scan(&out.Total); e != nil {
		return nil, e
	}
	rows, e := r.db.QueryContext(ctx, `SELECT id,request_id,user_id,payer_user_id,team_id,source,subscription_id,(amount-unauthorized_amount)::numeric(20,8)::text,unauthorized_amount::text,created_at FROM team_funding_ledger WHERE team_id=$1 AND ($2::bigint=0 OR user_id=$2) ORDER BY id DESC LIMIT $3 OFFSET $4`, team, filterUser, size, (page-1)*size)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var v service.TeamLedgerEntry
		if e := rows.Scan(&v.ID, &v.RequestID, &v.UserID, &v.PayerUserID, &v.TeamID, &v.Source, &v.SubscriptionID, &v.Amount, &v.UnauthorizedAmount, &v.CreatedAt); e != nil {
			return nil, e
		}
		out.Items = append(out.Items, v)
	}
	return out, rows.Err()
}
