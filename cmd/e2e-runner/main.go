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

// This server is a simple webserver that triggers the e2e-test binary.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/google/exposure-notifications-server/pkg/logging"
	"github.com/google/exposure-notifications-server/pkg/server"

	"github.com/google/exposure-notifications-server/pkg/observability"
	"github.com/google/exposure-notifications-verification-server/pkg/buildinfo"
	"github.com/google/exposure-notifications-verification-server/pkg/clients"
	"github.com/google/exposure-notifications-verification-server/pkg/config"
	"github.com/google/exposure-notifications-verification-server/pkg/controller/middleware"
	"github.com/google/exposure-notifications-verification-server/pkg/e2e"
	"github.com/google/exposure-notifications-verification-server/pkg/render"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/sethvargo/go-signalcontext"
)

func main() {
	ctx, done := signalcontext.OnInterrupt()

	debug, _ := strconv.ParseBool(os.Getenv("LOG_DEBUG"))
	logger := logging.NewLogger(debug)
	logger = logger.With("build_id", buildinfo.BuildID)
	logger = logger.With("build_tag", buildinfo.BuildTag)

	ctx = logging.WithLogger(ctx, logger)

	err := realMain(ctx)
	done()

	if err != nil {
		logger.Fatal(err)
	}
	logger.Info("successful shutdown")
}

func realMain(ctx context.Context) error {
	logger := logging.FromContext(ctx)

	// load configs
	e2eConfig, err := config.NewE2ERunnerConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to process e2e-runner config: %w", err)
	}

	// Setup monitoring
	logger.Info("configuring observability exporter")
	oe, err := observability.NewFromEnv(e2eConfig.Observability)
	if err != nil {
		return fmt.Errorf("unable to create ObservabilityExporter provider: %w", err)
	}
	if err := oe.StartExporter(); err != nil {
		return fmt.Errorf("error initializing observability exporter: %w", err)
	}
	defer oe.Close()
	logger.Infow("observability exporter", "config", e2eConfig.Observability)

	// Setup database and authorized apps.
	done, err := e2e.Setup(ctx, e2eConfig)
	if err != nil {
		return fmt.Errorf("failed to setup database and authorized apps: %w", err)
	}
	defer done()

	// Create the renderer
	h, err := render.New(ctx, "", e2eConfig.DevMode)
	if err != nil {
		return fmt.Errorf("failed to create renderer: %w", err)
	}

	// Create the router
	r := mux.NewRouter()

	// Request ID injection
	populateRequestID := middleware.PopulateRequestID(h)
	r.Use(populateRequestID)

	// Logger injection
	populateLogger := middleware.PopulateLogger(logger)
	r.Use(populateLogger)

	r.HandleFunc("/default", defaultHandler(ctx, e2eConfig.TestConfig))
	r.HandleFunc("/revise", reviseHandler(ctx, e2eConfig.TestConfig))

	srv, err := server.New(e2eConfig.Port)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	logger.Infow("server listening", "port", e2eConfig.Port)
	return srv.ServeHTTPHandler(ctx, handlers.CombinedLoggingHandler(os.Stdout, r))
}

// Config is passed by value so that each http hadndler has a separate copy (since they are changing one of the)
// config elements. Previous versions of those code had a race condition where the "DoRevise" status
// could be changed while a handler was executing.
func defaultHandler(ctx context.Context, config config.E2ETestConfig) func(http.ResponseWriter, *http.Request) {
	logger := logging.FromContext(ctx)
	c := &config
	c.DoRevise = false
	return func(w http.ResponseWriter, r *http.Request) {
		if err := clients.RunEndToEnd(ctx, c); err != nil {
			logger.Errorw("could not run default end to end", "error", err)
			http.Error(w, "failed (check server logs for more details): "+err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprint(w, "ok")
	}
}

func reviseHandler(ctx context.Context, config config.E2ETestConfig) func(http.ResponseWriter, *http.Request) {
	logger := logging.FromContext(ctx)
	c := &config
	c.DoRevise = true
	return func(w http.ResponseWriter, r *http.Request) {
		if err := clients.RunEndToEnd(ctx, c); err != nil {
			logger.Errorw("could not run revise end to end", "error", err)
			http.Error(w, "failed (check server logs for more details): "+err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprint(w, "ok")
	}
}
