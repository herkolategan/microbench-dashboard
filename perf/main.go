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

	"golang.org/x/build/internal/https"
	"golang.org/x/build/perf/app"
)

var (
	victoriaMetricsURL = flag.String("victoriametrics-url", os.Getenv("VICTORIAMETRICS_URL"), "URL of the VictoriaMetrics server")
	grafanaURL         = flag.String("grafana-url", os.Getenv("GRAFANA_URL"), "URL of the Grafana server")
)

func main() {
	https.RegisterFlags(flag.CommandLine)
	flag.Parse()
	
	app, err := app.NewApp()
	if err != nil {
		log.Fatalf("Failed to create app: %v", err)
	}
	app.VictoriaMetricsURL = *victoriaMetricsURL
	app.GrafanaURL = *grafanaURL
	mux := http.NewServeMux()
	mux.Handle("/", http.RedirectHandler("dashboard/", 307))
	app.RegisterOnMux(mux)

	log.Printf("Serving...")

	ctx := context.Background()
	log.Fatal(https.ListenAndServe(ctx, mux))
}
