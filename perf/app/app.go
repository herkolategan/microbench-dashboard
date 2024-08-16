// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package app implements the performance data analysis server.
package app

import (
	"net/http"
)

// App manages the analysis server logic.
// Construct an App instance and call RegisterOnMux to connect it with an HTTP server.
type App struct {
	// BaseDir is the directory containing the "template" directory.
	// If empty, the current directory will be used.
	BaseDir string

	// InfluxHost is the host URL of the perf InfluxDB server.
	InfluxHost string

	// InfluxToken is the Influx auth token for connecting to InfluxHost.
	//
	// If empty, we attempt to fetch the token from Secret Manager using
	// InfluxProject.
	InfluxToken string

	// InfluxProject is the GCP project ID containing the InfluxDB secrets.
	//
	// If empty, this defaults to the project this service is running as.
	//
	// Only used if InfluxToken is empty.
	InfluxProject string

	// AuthCronEmail is the service account email which requests to
	// /cron/syncinflux must contain an OICD authentication token for, with
	// audience "/cron/syncinflux".
	//
	// If empty, no authentication is required.
	AuthCronEmail string
}

// RegisterOnMux registers the app's URLs on mux.
func (a *App) RegisterOnMux(mux *http.ServeMux) {
	a.dashboardRegisterOnMux(mux)
}
