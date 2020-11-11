// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/exposure-notifications-server/pkg/logging"
	"github.com/google/exposure-notifications-verification-server/pkg/config"
	"github.com/google/exposure-notifications-verification-server/pkg/database"
)

const (
	realmName       = "e2e-test-realm"
	realmRegionCode = "e2e-test"
	adminKeyName    = "e2e-admin-key."
	deviceKeyName   = "e2e-device-key."
)

// Generate random string of 32 characters in length
func randomString() (string, error) {
	b := make([]byte, 512)
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to generate random: %v", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b[:])), nil
}

// Setup sets up the test environment (database and authorized apps) for an E2E test.
// The returned done function must be called to clean up the environment.
func Setup(ctx context.Context, cfg *config.E2ERunnerConfig) (func(), error) {
	ready := make(chan error)
	done := make(chan struct{})

	go func() {
		logger := logging.FromContext(ctx)
		db, err := cfg.Database.Load(ctx)
		if err != nil {
			ready <- fmt.Errorf("failed to load database config: %w", err)
			return
		}
		if err := db.Open(ctx); err != nil {
			ready <- fmt.Errorf("failed to connect to database: %w", err)
			return
		}
		defer db.Close()

		// Create or reuse the existing realm
		realm, err := db.FindRealmByName(realmName)
		if err != nil {
			if !database.IsNotFound(err) {
				ready <- fmt.Errorf("error when finding the realm %q: %w", realmName, err)
				return
			}
			realm = database.NewRealmWithDefaults(realmName)
			realm.RegionCode = realmRegionCode
			if err := db.SaveRealm(realm, database.System); err != nil {
				ready <- fmt.Errorf("failed to create realm %+v: %w: %v", realm, err, realm.ErrorMessages())
				return
			}
		}

		// Create new API keys
		suffix, err := randomString()
		if err != nil {
			ready <- fmt.Errorf("failed to create suffix string for API keys: %w", err)
			return
		}

		adminKey, err := realm.CreateAuthorizedApp(db, &database.AuthorizedApp{
			Name:       adminKeyName + suffix,
			APIKeyType: database.APIKeyTypeAdmin,
		}, database.System)
		if err != nil {
			ready <- fmt.Errorf("error trying to create a new Admin API Key: %w", err)
			return
		}

		defer func() {
			app, err := db.FindAuthorizedAppByAPIKey(adminKey)
			if err != nil {
				logger.Errorf("admin API key cleanup failed: %w", err)
			}
			now := time.Now().UTC()
			app.DeletedAt = &now
			if err := db.SaveAuthorizedApp(app, database.System); err != nil {
				logger.Errorf("admin API key disable failed: %w", err)
			}
			logger.Info("successfully cleaned up e2e test admin key")
		}()

		deviceKey, err := realm.CreateAuthorizedApp(db, &database.AuthorizedApp{
			Name:       deviceKeyName + suffix,
			APIKeyType: database.APIKeyTypeDevice,
		}, database.System)
		if err != nil {
			ready <- fmt.Errorf("error trying to create a new Device API Key: %w", err)
			return
		}

		defer func() {
			app, err := db.FindAuthorizedAppByAPIKey(deviceKey)
			if err != nil {
				logger.Errorf("device API key cleanup failed: %w", err)
				return
			}
			now := time.Now().UTC()
			app.DeletedAt = &now
			if err := db.SaveAuthorizedApp(app, database.System); err != nil {
				logger.Errorf("device API key disable failed: %w", err)
			}
			logger.Info("successfully cleaned up e2e test device key")
		}()

		cfg.TestConfig.VerificationAdminAPIKey = adminKey
		cfg.TestConfig.VerificationAPIServerKey = deviceKey

		ready <- nil
		select {
		case <-done:
		case <-ctx.Done():
		}
	}()

	if err := <-ready; err != nil {
		close(done)
		return nil, err
	}
	return func() { close(done) }, nil
}
