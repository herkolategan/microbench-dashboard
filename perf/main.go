// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// perf runs an HTTP server for benchmark analysis.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/build/internal/https"
	"golang.org/x/build/perf/app"
)

var (
	influxHost      = flag.String("influx-host", os.Getenv("INFLUX_HOST"), "URL of the InfluxDB instance")
	influxToken     = flag.String("influx-token", os.Getenv("INFLUX_TOKEN"), "Authentication token for the InfluxDB instance")
	influxTokenFile = flag.String("influx-token-file", os.Getenv("INFLUX_TOKEN_FILE"), "File containing the Authentication token for the InfluxDB instance")
	influxProject   = flag.String("influx-project", os.Getenv("INFLUX_PROJECT"), "GCP project ID for the InfluxDB instance. If empty, defaults to the project this service is running as. If -influx-token is not set, the token is fetched from Secret Manager in the project.")
	authCronEmail   = flag.String("auth-cron-email", "", "If set, requests to /cron/syncinflux must be authenticated as the passed service account.")
)

func main() {
	https.RegisterFlags(flag.CommandLine)
	flag.Parse()

	// If the token is not set, but the token file is, read the token from the file.
	if *influxToken == "" && *influxTokenFile != "" {
		tokenData, err := os.ReadFile(*influxTokenFile)
		if err != nil {
			log.Fatalf("Failed to read token file: %v", err)
		}
		*influxToken = strings.TrimSpace(string(tokenData))
	}

	app := &app.App{
		InfluxHost:    *influxHost,
		InfluxToken:   *influxToken,
		InfluxProject: *influxProject,
		AuthCronEmail: *authCronEmail,
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.RedirectHandler("dashboard/", 307))
	app.RegisterOnMux(mux)

	log.Printf("Serving...")

	ctx := context.Background()
	log.Fatal(https.ListenAndServe(ctx, mux))
}
