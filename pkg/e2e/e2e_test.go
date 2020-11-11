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
	"testing"

	"github.com/google/exposure-notifications-verification-server/pkg/clients"
	"github.com/google/exposure-notifications-verification-server/pkg/config"
)

func TestE2E(t *testing.T) {
	ctx := context.Background()
	e2eConfig, err := config.NewE2ERunnerConfig(ctx)
	if err != nil {
		t.Fatalf("failed to process e2e-runner config: %v", err)
	}

	close, err := Setup(ctx, e2eConfig)
	defer close()

	cases := []struct {
		Name   string
		Revise bool
	}{
		{"default", false},
		{"revise", true},
	}

	for _, tc := range cases {
		tc := tc
		cfg := e2eConfig.TestConfig
		cfg.DoRevise = tc.Revise
		t.Run(tc.Name, func(t *testing.T) {
			if err := clients.RunEndToEnd(ctx, &cfg); err != nil {
				t.Errorf("End to end test failed: %v", err)
			}
		})
	}
}
