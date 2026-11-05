//go:build integration && nativeconsumer

package repository_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeviceOperationAcceptedResponseLostHTTP(t *testing.T) {
	f := newDevicePreflight(t, t.TempDir())
	f.startServers(t)
	operationFaultSeedWindows(t, f)
	token := f.login(t)
	identity := f.device(t, token, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	otherKey := f.device(t, token, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	for _, option := range operationFaultOptions {
		t.Run(option, func(t *testing.T) {
			request := newOperationFaultRequest(t, option)
			before := subscriptionLifecycleSnapshot(t, f)
			front, completed := operationFaultDropResponse(t, f.origins[0])
			lost := operationFaultDispatch(front.URL, identity.APIKey, request)
			require.Error(t, lost.err, "client must lose the accepted API response")
			select {
			case original := <-completed:
				require.NoError(t, original.err)
				require.Equal(t, http.StatusOK, original.status, string(original.body))
			case <-time.After(5 * time.Second):
				t.Fatal("response-loss front did not observe completed API response")
			}
			operationFaultSettle(t, f, request)
			var wg sync.WaitGroup
			duplicates := make(chan operationFaultResponse, 4)
			for i := range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					duplicates <- operationFaultDispatch(f.origins[i%2], identity.APIKey, operationFaultRetry(request))
				}()
			}
			wg.Wait()
			close(duplicates)
			t.Logf("OPERATION_RESPONSE_LOST_OBSERVED route=%s provider_delta=%d", option, subscriptionLifecycleSnapshot(t, f).UpstreamCalls-before.UpstreamCalls)
			for response := range duplicates {
				operationFaultUnknown(t, response)
			}
			conflict := operationFaultRetry(request)
			conflict.body = append(append([]byte(nil), request.body...), ' ')
			response := operationFaultDispatch(f.origins[1], identity.APIKey, conflict)
			require.NoError(t, response.err)
			require.Equal(t, http.StatusConflict, response.status, string(response.body))
			require.Contains(t, string(response.body), "IDEMPOTENCY_KEY_CONFLICT")
			otherOption := "fixture-text-subscription"
			if option == "fixture-text-subscription" {
				otherOption = "fixture-tool-model"
			} else if option == nativeImageCatalogID {
				otherOption = nativeSubscriptionImageCatalogID
			} else if option == nativeSubscriptionImageCatalogID {
				otherOption = nativeImageCatalogID
			}
			catalogConflict := operationFaultRetry(request)
			catalogConflict.headers["X-Aino-Model-ID"] = otherOption
			response = operationFaultDispatch(f.origins[1], identity.APIKey, catalogConflict)
			require.NoError(t, response.err)
			require.Equal(t, http.StatusConflict, response.status, string(response.body))
			require.Contains(t, string(response.body), "IDEMPOTENCY_KEY_CONFLICT")
			operationFaultAssertCharge(t, f, before, option, true)
			fresh := newOperationFaultRequest(t, option)
			beforeFresh := subscriptionLifecycleSnapshot(t, f)
			response = operationFaultDispatch(f.origins[1], identity.APIKey, fresh)
			require.NoError(t, response.err)
			require.Equal(t, http.StatusOK, response.status, string(response.body))
			operationFaultSettle(t, f, fresh)
			operationFaultAssertCharge(t, f, beforeFresh, option, true)
			beforeKey := subscriptionLifecycleSnapshot(t, f)
			keyScoped := operationFaultRetry(request)
			response = operationFaultDispatch(f.origins[1], otherKey.APIKey, keyScoped)
			require.NoError(t, response.err)
			require.Equal(t, http.StatusOK, response.status, string(response.body))
			operationFaultSettle(t, f, keyScoped)
			operationFaultAssertCharge(t, f, beforeKey, option, true)
			operationFaultFirstClaimRace(t, f, identity.APIKey, option)
			t.Logf("OPERATION_RESPONSE_LOST route=%s original_executions=1 settled_debits=1 concurrent_duplicates=4 body_conflicts=1 catalog_conflicts=1 fresh_operation_executions=1 independent_key_executions=1 first_claim_race_winners=1 client_response_lost=true", option)
		})
	}
}

func operationFaultFirstClaimRace(t *testing.T, f *devicePreflight, key, option string) {
	t.Helper()
	request := newOperationFaultRequest(t, option)
	before := subscriptionLifecycleSnapshot(t, f)
	type outcome struct {
		request  operationFaultRequest
		response operationFaultResponse
	}
	ready := make(chan struct{})
	results := make(chan outcome, 2)
	for instance := range 2 {
		go func() {
			<-ready
			attempt := operationFaultRetry(request)
			results <- outcome{request: attempt, response: operationFaultDispatch(f.origins[instance], key, attempt)}
		}()
	}
	close(ready)
	winners := 0
	for range 2 {
		result := <-results
		require.NoError(t, result.response.err)
		if result.response.status == http.StatusOK {
			winners++
			operationFaultSettle(t, f, result.request)
		} else {
			operationFaultUnknown(t, result.response)
		}
	}
	require.Equal(t, 1, winners)
	operationFaultAssertCharge(t, f, before, option, true)
}

func TestDeviceOperationProviderAcceptedCrashRestartHTTP(t *testing.T) {
	for _, option := range operationFaultOptions {
		t.Run(option, func(t *testing.T) {
			f := newDevicePreflight(t, t.TempDir())
			accepted, release := operationFaultUpstream(t, f, true)
			defer close(release)
			f.startServers(t)
			operationFaultSeedWindows(t, f)
			identity := f.device(t, f.login(t), service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
			request := newOperationFaultRequest(t, option)
			before := subscriptionLifecycleSnapshot(t, f)
			original := make(chan operationFaultResponse, 1)
			go func() { original <- operationFaultDispatch(f.origins[0], identity.APIKey, request) }()
			select {
			case <-accepted:
			case <-time.After(10 * time.Second):
				t.Fatal("controlled provider never accepted the original request")
			}
			operationFaultUnknown(t, operationFaultDispatch(f.origins[1], identity.APIKey, operationFaultRetry(request)))
			select {
			case result := <-original:
				t.Fatalf("original ended before the intended API crash: status=%d error=%v", result.status, result.err)
			default:
			}
			operationFaultRestart(t, f, 0)
			select {
			case <-original:
			case <-time.After(5 * time.Second):
				t.Fatal("crashed API request did not terminate")
			}
			for instance := range 2 {
				operationFaultUnknown(t, operationFaultDispatch(f.origins[instance], identity.APIKey, operationFaultRetry(request)))
			}
			db := repository.GetIntegrationDB()
			hash := service.HashIdempotencyKey(request.operation)
			result, err := db.ExecContext(f.r.ctx, "UPDATE idempotency_records SET locked_until=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE idempotency_key_hash=$1", hash)
			require.NoError(t, err)
			rows, err := result.RowsAffected()
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)
			_, err = repository.NewIdempotencyRepository(f.r.client, db).DeleteExpired(f.r.ctx, time.Now(), 500)
			require.NoError(t, err)
			for instance := range 2 {
				operationFaultUnknown(t, operationFaultDispatch(f.origins[instance], identity.APIKey, operationFaultRetry(request)))
			}
			// The provider executed once, but the API died before receiving usage.
			before.UpstreamCalls++
			operationFaultAssertCharge(t, f, before, option, false)
			t.Logf("OPERATION_CRASH route=%s provider_executions=1 settled_debits=0 duplicate_unknowns=5 process_restart=true expired_claim_cleanup=true synthetic_backdating=true", option)
		})
	}
}

func TestDeviceOperationProviderResponseLostHTTP(t *testing.T) {
	for _, option := range operationFaultOptions {
		t.Run(option, func(t *testing.T) {
			f := newDevicePreflight(t, t.TempDir())
			_, release := operationFaultUpstream(t, f, false)
			defer close(release)
			f.startServers(t)
			operationFaultSeedWindows(t, f)
			identity := f.device(t, f.login(t), service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
			request := newOperationFaultRequest(t, option)
			before := subscriptionLifecycleSnapshot(t, f)
			response := operationFaultDispatch(f.origins[0], identity.APIKey, request)
			t.Logf("OPERATION_PROVIDER_LOSS_OBSERVED route=%s initial_status=%d provider_delta=%d", option, response.status, subscriptionLifecycleSnapshot(t, f).UpstreamCalls-before.UpstreamCalls)
			require.NoError(t, response.err)
			require.NotEqual(t, http.StatusOK, response.status, string(response.body))
			operationFaultUnknown(t, operationFaultDispatch(f.origins[1], identity.APIKey, operationFaultRetry(request)))
			before.UpstreamCalls++
			operationFaultAssertCharge(t, f, before, option, false)
			t.Logf("OPERATION_PROVIDER_RESPONSE_LOST route=%s provider_executions=1 settled_debits=0 internal_failover_reexecution=false duplicate_unknowns=1", option)
		})
	}
}
