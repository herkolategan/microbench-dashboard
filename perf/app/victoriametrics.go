// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// VictoriaMetricsClient represents a client for interacting with VictoriaMetrics.
type VictoriaMetricsClient struct {
	baseURL string
}

// NewVictoriaMetricsClient creates a new VictoriaMetrics client.
func NewVictoriaMetricsClient(baseURL string) *VictoriaMetricsClient {
	return &VictoriaMetricsClient{
		baseURL: baseURL,
	}
}

// VictoriaMetricsError represents an error from VictoriaMetrics operations
type VictoriaMetricsError struct {
	Code    int    // HTTP status code if applicable
	Type    string // Error type from VictoriaMetrics
	Message string // Error message
	Query   string // The query that caused the error
	Err     error  // Original error if any
}

func (e *VictoriaMetricsError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("VictoriaMetrics error (type=%s, code=%d): %s: %v", e.Type, e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("VictoriaMetrics error (type=%s, code=%d): %s", e.Type, e.Code, e.Message)
}

func (e *VictoriaMetricsError) Unwrap() error {
	return e.Err
}

// Query executes a PromQL query against VictoriaMetrics.
func (c *VictoriaMetricsClient) Query(ctx context.Context, query string, start, end time.Time) ([]byte, error) {
	// Construct the query URL
	url := fmt.Sprintf("%s/api/v1/query_range", c.baseURL)

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, &VictoriaMetricsError{
			Type:    "request_creation",
			Message: "failed to create request",
			Query:   query,
			Err:     err,
		}
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("query", query)
	q.Add("start", fmt.Sprintf("%d", start.Unix()))
	q.Add("end", fmt.Sprintf("%d", end.Unix()))
	q.Add("step", "1d") // 1 day resolution
	req.URL.RawQuery = q.Encode()

	// Log the full query URL for debugging
	fullURL := req.URL.String()
	log.Printf("Executing VictoriaMetrics query: %s", fullURL)
	log.Printf("Time range: %s to %s", start.Format(time.RFC3339), end.Format(time.RFC3339))

	// Make the request
	client := &http.Client{
		Timeout: 30 * time.Second, // Set a reasonable timeout
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &VictoriaMetricsError{
			Type:    "request_execution",
			Message: "failed to execute query",
			Query:   query,
			Err:     err,
		}
	}
	defer resp.Body.Close()

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &VictoriaMetricsError{
			Type:    "response_read",
			Message: "failed to read response body",
			Query:   query,
			Err:     err,
		}
	}

	// Check if the response body is empty
	if len(body) == 0 {
		return nil, &VictoriaMetricsError{
			Type:    "empty_response",
			Message: "empty response body from VictoriaMetrics",
			Query:   query,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &VictoriaMetricsError{
			Code:    resp.StatusCode,
			Type:    "http_error",
			Message: fmt.Sprintf("unexpected status code: %s", string(body)),
			Query:   query,
		}
	}

	// Parse the response to check for VictoriaMetrics-specific errors
	var response struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string        `json:"resultType"`
			Result     []interface{} `json:"result"`
		} `json:"data"`
		ErrorType string `json:"errorType"`
		Error     string `json:"error"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		// Log the parsing error but continue since we'll return the raw body
		log.Printf("Warning: Could not parse VictoriaMetrics response: %v", err)
	} else {
		// Check for error in the response
		if response.Status == "error" {
			return nil, &VictoriaMetricsError{
				Type:    response.ErrorType,
				Message: response.Error,
				Query:   query,
			}
		}

		// Log response details
		log.Printf("Response status: %s, result type: %s, result count: %d",
			response.Status, response.Data.ResultType, len(response.Data.Result))

		// Handle empty results
		if response.Status == "success" && len(response.Data.Result) == 0 {
			log.Printf("Warning: Query returned successfully but with empty results. Query: %s", query)
			log.Printf("Time range: %s to %s", start.Format(time.RFC3339), end.Format(time.RFC3339))

			// Try to get diagnostic information
			if err := c.logDiagnosticInfo(ctx, client); err != nil {
				log.Printf("Warning: Failed to get diagnostic info: %v", err)
			}
		}
	}

	return body, nil
}

// logDiagnosticInfo logs available tests and metrics for debugging
func (c *VictoriaMetricsClient) logDiagnosticInfo(ctx context.Context, client *http.Client) error {
	// Check available tests
	if err := c.logLabelValues(ctx, client, "test"); err != nil {
		return err
	}

	// Check available metrics
	return c.logLabelValues(ctx, client, "__name__")
}

// logLabelValues logs the values for a given label
func (c *VictoriaMetricsClient) logLabelValues(ctx context.Context, client *http.Client, label string) error {
	url := fmt.Sprintf("%s/api/v1/label/%s/values", c.baseURL, label)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d for label %s", resp.StatusCode, label)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var response struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}

	if len(response.Data) > 0 {
		log.Printf("Available %s values in VictoriaMetrics (first 10): %v", 
			label, response.Data[:min(10, len(response.Data))])
	}
	return nil
}

// min returns the smaller of x or y.
func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}
