package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/desktopmodelcredential"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// The caller holds the user row lock used by both issuance paths. A UUID is an
// application installation, even when it obtains another grant or purpose.
// Keep archived associations authoritative so revocation never relabels history.
func desktopApplicationForInstallation(ctx context.Context, client *dbent.Client, userID int64, deviceID, requested string) (string, error) {
	rows, err := client.APIKey.Query().Where(
		apikey.UserIDEQ(userID), apikey.DesktopManagedEQ(true),
		apikey.Or(
			apikey.And(apikey.DesktopCredentialTypeEQ(service.DesktopCredentialTypeDevice), apikey.DesktopDeviceIDEQ(deviceID)),
			apikey.HasDesktopModelCredentialsWith(desktopmodelcredential.DeviceIDEQ(deviceID)),
		),
	).Select(apikey.FieldDesktopApp, apikey.FieldDesktopManaged).All(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return "", err
	}
	app := requested
	for _, row := range rows {
		stored := service.DesktopAppForKey(&service.APIKey{DesktopManaged: row.DesktopManaged, DesktopApp: row.DesktopApp})
		if app != "" && app != stored {
			return "", service.ErrDesktopAppConflict
		}
		app = stored
	}
	return app, nil
}
