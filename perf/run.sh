#!/bin/bash

# Exit on error
set -e

# Default VictoriaMetrics URL if not set
export VICTORIAMETRICS_URL=${VICTORIAMETRICS_URL:-"https://roachperf-o11y.observability.testeng.crdb.io/vmselect/select/multitenant/prometheus"}

# Default Grafana URL if not set
export GRAFANA_URL=${GRAFANA_URL:-"https://grafana.testeng.crdb.io"}

# Clean any previous build
rm -f perf-server

# # Build the program
go build -o perf-server

# Run the server
./perf-server -victoriametrics-url="$VICTORIAMETRICS_URL" -listen-http=:8080 -grafana-url="$GRAFANA_URL"