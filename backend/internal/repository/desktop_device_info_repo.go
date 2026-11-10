package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *desktopCredentialRepository) UpsertDeviceInfo(ctx context.Context, userID int64, deviceID string, info service.DesktopDeviceInfo) (*service.DesktopDeviceInfo, error) {
	client := clientFromContext(ctx, r.client)
	// The existence predicate and metadata write share one statement. Revoked,
	// expired and soft-deleted authorizations still identify this user's device.
	rows, err := client.QueryContext(ctx, `
		INSERT INTO desktop_device_info (user_id,device_id,installation_name,os,client_type,client_version)
		SELECT $1,$2::uuid,$3,$4,$5,$6
		WHERE EXISTS (SELECT 1 FROM api_keys
			WHERE user_id=$1 AND desktop_device_id=$2::text AND desktop_credential_type='device')
		OR EXISTS (SELECT 1 FROM desktop_model_credentials WHERE user_id=$1 AND device_id=$2::text)
		ON CONFLICT (user_id,device_id) DO UPDATE SET
			installation_name=EXCLUDED.installation_name,os=EXCLUDED.os,
			client_type=EXCLUDED.client_type,client_version=EXCLUDED.client_version,updated_at=NOW()
		RETURNING installation_name,os,client_type,client_version`,
		userID, deviceID, info.InstallationName, info.OS, info.ClientType, info.ClientVersion)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrDesktopCredentialNotFound
	}
	var saved service.DesktopDeviceInfo
	if err := rows.Scan(&saved.InstallationName, &saved.OS, &saved.ClientType, &saved.ClientVersion); err != nil {
		return nil, err
	}
	return &saved, rows.Err()
}

func (r *desktopCredentialRepository) ListDeviceInfo(ctx context.Context, userID int64) (map[string]service.DesktopDeviceInfo, error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `
		SELECT device_id::text,installation_name,os,client_type,client_version
		FROM desktop_device_info WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := map[string]service.DesktopDeviceInfo{}
	for rows.Next() {
		var deviceID string
		var info service.DesktopDeviceInfo
		if err := rows.Scan(&deviceID, &info.InstallationName, &info.OS, &info.ClientType, &info.ClientVersion); err != nil {
			return nil, err
		}
		result[deviceID] = info
	}
	return result, rows.Err()
}
