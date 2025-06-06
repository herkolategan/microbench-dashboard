// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package app implements the performance data analysis server.
package app

import (
	"net/http"
	"os"
)

// App is the main application struct.
type App struct {
	// VictoriaMetricsURL is the base URL for the VictoriaMetrics instance.
	VictoriaMetricsURL string

	// GrafanaURL is the base URL for the Grafana instance
	GrafanaURL string

	// vmClient is the VictoriaMetrics client.
	vmClient *VictoriaMetricsClient

	// AuthCronEmail is the service account email for /cron/syncinflux authentication.
	AuthCronEmail string

	// MetricsAllowlist holds the parsed metrics allowlist in memory
	MetricsAllowlist *MetricsAllowlist
}

// NewApp creates a new App instance.
func NewApp() (*App, error) {
	vmURL := os.Getenv("VICTORIA_METRICS_URL")
	if vmURL == "" {
		vmURL = "http://localhost:8428" // default VictoriaMetrics URL
	}

	app := &App{
		VictoriaMetricsURL: vmURL,
	}

	// Initialize the VictoriaMetrics client
	app.vmClient = NewVictoriaMetricsClient(app.VictoriaMetricsURL)

	// Load metrics allowlist at startup
	allowlist, err := loadMetricsAllowlist()
	if err != nil {
		return nil, err
	}
	app.MetricsAllowlist = allowlist

	return app, nil
}

// RegisterOnMux registers all HTTP handlers on mux.
func (a *App) RegisterOnMux(mux *http.ServeMux) {
	a.dashboardRegisterOnMux(mux)
}

// Close cleans up any resources used by the App.
func (a *App) Close() error {
	return nil
}
