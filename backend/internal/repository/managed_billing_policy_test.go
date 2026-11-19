package repository

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBalanceOnlyPersonalReservationUsesPersistedKeyConsent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT user_id,billing_policy,desktop_managed FROM api_keys").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"user_id", "billing_policy", "desktop_managed"}).AddRow(22, "balance_only", true))
	mock.ExpectQuery("SELECT subscription_type FROM groups").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"subscription_type"}).AddRow("standard"))
	mock.ExpectQuery("SELECT id FROM users").WithArgs(int64(22)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(22))
	mock.ExpectQuery("SELECT EXISTS.*personal_funding_reservations").WithArgs(int64(22)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT id,request_id.*personal_funding_reservations").WithArgs("policy-fixture", int64(7)).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("UPDATE users SET balance=balance").WithArgs(int64(22), "2.00000000").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO personal_funding_reservations").WithArgs("policy-fixture", int64(7), int64(22), "personal_balance", nil, "2.00000000", nil, nil, nil).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectCommit()
	repo := &teamFundingRepository{db: db}
	hold, err := repo.ReservePersonalFunding(t.Context(), &service.PersonalFundingReserveCommand{RequestID: "policy-fixture", APIKeyID: 7, MemberUserID: 22, GroupID: 3, Source: "personal_balance", Amount: "2"})
	require.NoError(t, err)
	require.Equal(t, int64(22), hold.MemberUserID)
	require.Equal(t, "personal_balance", hold.Source)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBalanceOnlyReservationRejectsSubscriptionAndForgedOwnership(t *testing.T) {
	for _, test := range []struct {
		name    string
		owner   int64
		managed bool
		source  string
	}{{"subscription", 22, true, "personal_subscription"}, {"other actor", 11, true, "personal_balance"}, {"unmanaged", 22, false, "personal_balance"}} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT user_id,billing_policy,desktop_managed FROM api_keys").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"user_id", "billing_policy", "desktop_managed"}).AddRow(test.owner, "balance_only", test.managed))
			mock.ExpectRollback()
			_, err = (&teamFundingRepository{db: db}).ReservePersonalFunding(t.Context(), &service.PersonalFundingReserveCommand{RequestID: "rejected-policy", APIKeyID: 7, MemberUserID: 22, GroupID: 3, Source: test.source, Amount: "2"})
			require.ErrorIs(t, err, service.ErrTeamFundingForbidden)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
