package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIQTestRepositoryScopesReadsAndTerminalWritesToOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := NewIQTestRepository(db)
	mock.ExpectQuery("FROM user_iq_tests WHERE user_id = \\$1 AND id = \\$2").WithArgs(int64(7), "test-id").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, err = repo.Get(context.Background(), 7, "test-id")
	require.ErrorIs(t, err, service.ErrIQTestNotFound)
	now := time.Now()
	mock.ExpectExec("UPDATE user_iq_tests SET").WithArgs("cancelled", "", "", "", sqlmock.AnyArg(), nil, &now, "test-id", int64(7)).WillReturnResult(sqlmock.NewResult(0, 0))
	changed, err := repo.Finish(context.Background(), &service.IQTest{ID: "test-id", UserID: 7, Status: "cancelled", FinishedAt: &now})
	require.NoError(t, err)
	require.False(t, changed, "terminal results cannot be overwritten by a late completion")
	require.NoError(t, mock.ExpectationsWereMet())
}
