// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/compute/metadata"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"golang.org/x/build/internal/influx"
)

const (
	backfillWindow = 30 * 24 * time.Hour // 30 days.
)

func (a *App) influxClient(ctx context.Context) (influxdb2.Client, error) {
	if a.InfluxHost == "" {
		return nil, fmt.Errorf("Influx host unknown (set INFLUX_HOST?)")
	}

	token, err := a.findInfluxToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("error finding Influx token: %w", err)
	}

	return influxdb2.NewClient(a.InfluxHost, token), nil
}

func (a *App) findInfluxToken(ctx context.Context) (string, error) {
	if a.InfluxToken != "" {
		return a.InfluxToken, nil
	}

	var project string
	if a.InfluxProject != "" {
		project = a.InfluxProject
	} else {
		var err error
		project, err = metadata.ProjectID()
		if err != nil {
			return "", fmt.Errorf("error determining GCP project ID (set INFLUX_TOKEN or INFLUX_PROJECT?): %w", err)
		}
	}

	log.Printf("Fetching Influx token from %s...", project)

	token, err := fetchInfluxToken(ctx, project)
	if err != nil {
		return "", fmt.Errorf("error fetching Influx token: %w", err)
	}

	return token, nil
}

func fetchInfluxToken(ctx context.Context, project string) (string, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("error creating secret manager client: %w", err)
	}
	defer client.Close()

	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: "projects/" + project + "/secrets/" + influx.AdminTokenSecretName + "/versions/latest",
	}

	result, err := client.AccessSecretVersion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to access secret version: %w", err)
	}

	return string(result.Payload.Data), nil
}

func latestInfluxTimestamp(ctx context.Context, ifxc influxdb2.Client) (time.Time, error) {
	qc := ifxc.QueryAPI(influx.Org)
	// Find the latest upload in the last month.
	q := fmt.Sprintf(`from(bucket:%q)
		|> range(start: -%dh)
		|> filter(fn: (r) => r["_measurement"] == "benchmark-result")
		|> filter(fn: (r) => r["_field"] == "upload-time")
		|> group()
		|> sort(columns: ["_value"], desc: true)
		|> limit(n: 1)`, influx.Bucket, backfillWindow/time.Hour)
	result, err := influxQuery(ctx, qc, q)
	if err != nil {
		return time.Time{}, err
	}
	for result.Next() {
		// Except for the point timestamp, all other timestamps are stored as strings, specifically
		// as the RFC3339Nano format.
		//
		// We only care about the first result, and there should be just one.
		return time.Parse(time.RFC3339Nano, result.Record().Value().(string))
	}
	return time.Time{}, result.Err()
}
