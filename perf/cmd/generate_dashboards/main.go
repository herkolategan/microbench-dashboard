package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	victoriaMetricsURL = flag.String("victoriametrics-url", os.Getenv("VICTORIAMETRICS_URL"), "URL of the VictoriaMetrics server")
	grafanaURL         = flag.String("grafana-url", os.Getenv("GRAFANA_URL"), "URL of the Grafana server")
	grafanaToken       = flag.String("grafana-token", os.Getenv("GRAFANA_TOKEN"), "Grafana service account token")
	grafanaDatasourceUID = flag.String("grafana-datasource-uid", "den7d9f2jiwhsa", "UID of the Grafana Prometheus datasource")
	folderUID          = flag.String("folder-uid", "", "UID of the Grafana folder to create dashboards in")
)

type VictoriaMetricsClient struct {
	baseURL string
	client  *http.Client
}

func NewVictoriaMetricsClient(baseURL string) *VictoriaMetricsClient {
	return &VictoriaMetricsClient{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

func (c *VictoriaMetricsClient) GetTestNames(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/api/v1/label/test/values", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Add time range for last year
	q := req.URL.Query()
	q.Add("start", fmt.Sprintf("%d", time.Now().AddDate(-1, 0, 0).Unix()))
	q.Add("end", fmt.Sprintf("%d", time.Now().Unix()))
	req.URL.RawQuery = q.Encode()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("unexpected status: %s", result.Status)
	}

	return result.Data, nil
}

func (c *VictoriaMetricsClient) GetMetricsForTest(ctx context.Context, testName string) ([]map[string]string, error) {
	// Get metric names and their labels
	url := fmt.Sprintf("%s/api/v1/series", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	q := req.URL.Query()
	// Query for metrics that either have quantile label or end with _count
	q.Add("match[]", fmt.Sprintf(`{test="%s", __name__=~"openmetric_.*", quantile=~".+"}`, testName))
	q.Add("match[]", fmt.Sprintf(`{test="%s", __name__=~"openmetric_.*_count"}`, testName))
	q.Add("match[]", fmt.Sprintf(`{test="%s", __name__=~"openmetric_.*", unit=~".+"}`, testName))
	// Add time range for last year
	q.Add("start", fmt.Sprintf("%d", time.Now().AddDate(-1, 0, 0).Unix()))
	q.Add("end", fmt.Sprintf("%d", time.Now().Unix()))
	req.URL.RawQuery = q.Encode()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("unexpected status: %s", result.Status)
	}

	// Create a map to store unique metrics with their labels
	uniqueMetrics := make(map[string]map[string]string)
	for _, series := range result.Data {
		name := series["__name__"]
		if !strings.HasPrefix(name, "openmetric_") {
			continue
		}

		// If we haven't seen this metric before, add it
		if _, exists := uniqueMetrics[name]; !exists {
			uniqueMetrics[name] = series
		}
	}

	// Convert map to slice
	metrics := make([]map[string]string, 0, len(uniqueMetrics))
	for _, metric := range uniqueMetrics {
		metrics = append(metrics, metric)
	}

	return metrics, nil
}

type GrafanaClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewGrafanaClient(baseURL, token string) *GrafanaClient {
	return &GrafanaClient{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{},
	}
}

func (c *GrafanaClient) CreateDashboard(ctx context.Context, dashboard map[string]interface{}, folderUid string) error {
	url := fmt.Sprintf("%s/api/dashboards/db", c.baseURL)
	
	payload := map[string]interface{}{
		"dashboard": dashboard,
		"overwrite": true,
	}
	if folderUid != "" {
		payload["folderUid"] = folderUid
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling dashboard: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error messages
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// Success case
		var result struct {
			ID      int    `json:"id"`
			Slug    string `json:"slug"`
			Status  string `json:"status"`
			UID     string `json:"uid"`
			URL     string `json:"url"`
			Version int    `json:"version"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("unmarshaling response: %w", err)
		}
		return nil
	case http.StatusPreconditionFailed:
		// Handle specific error cases
		var result struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("unmarshaling error response: %w", err)
		}
		return fmt.Errorf("precondition failed: %s (status: %s)", result.Message, result.Status)
	case http.StatusBadRequest:
		return fmt.Errorf("invalid request: %s", string(body))
	case http.StatusUnauthorized:
		return fmt.Errorf("unauthorized: invalid service account token")
	case http.StatusForbidden:
		return fmt.Errorf("forbidden: insufficient permissions")
	default:
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}
}

func createSummaryPanel(metrics []map[string]string, testName string) []map[string]interface{} {
	// Create a single panel for all metrics
	panel := map[string]interface{}{
		"datasource": map[string]string{
			"type": "prometheus",
			"uid":  *grafanaDatasourceUID,
		},
		"fieldConfig": map[string]interface{}{
			"defaults": map[string]interface{}{
				"color": map[string]string{
					"mode": "palette-classic",
				},
				"custom": map[string]interface{}{
					"axisBorderShow": false,
					"axisCenteredZero": false,
					"axisColorMode": "text",
					"axisLabel": "",
					"axisPlacement": "right",
					"barAlignment": 0,
					"drawStyle": "line",
					"fillOpacity": 0,
					"gradientMode": "none",
					"hideFrom": map[string]bool{
						"legend": false,
						"tooltip": false,
						"viz": false,
					},
					"insertNulls": false,
					"lineInterpolation": "linear",
					"lineWidth": 1,
					"lineStyle": map[string]string{
						"fill": "dotted",
					},
					"pointSize": 5,
					"scaleDistribution": map[string]string{
						"type": "linear",
					},
					"showPoints": "auto",
					"spanNulls": true,
					"stacking": map[string]interface{}{
						"group": "A",
						"mode": "none",
					},
					"thresholdsStyle": map[string]string{
						"mode": "off",
					},
				},
				"mappings": []interface{}{},
				"thresholds": map[string]interface{}{
					"mode": "absolute",
					"steps": []map[string]interface{}{
						{
							"color": "green",
							"value": nil,
						},
					},
				},
				"unit": "ns",
			},
			"overrides": []map[string]interface{}{
				{
					"matcher": map[string]interface{}{
						"id": "byRegexp",
						"options": "/.*_count.*/",
					},
					"properties": []map[string]interface{}{
						{
							"id": "custom.axisPlacement",
							"value": "left",
						},
						{
							"id": "unit",
							"value": "",
						},
						{
							"id": "custom.lineStyle",
							"value": map[string]string{
								"fill": "solid",
							},
						},
						{
							"id": "custom.lineWidth",
							"value": 2,
						},
					},
				},
			},
		},
		"gridPos": map[string]int{
			"h": 12,
			"w": 24,
			"x": 0,
			"y": 0,
		},
		"options": map[string]interface{}{
			"legend": map[string]interface{}{
				"calcs": []string{"min", "max", "mean"},
				"displayMode": "table",
				"placement": "bottom",
				"showLegend": true,
				"width": 150,
			},
			"tooltip": map[string]interface{}{
				"mode": "single",
				"sort": "none",
			},
		},
		"targets": []map[string]interface{}{},
		"title": "All Metrics",
		"type": "timeseries",
		"minStep": "1s",
		"interval": "1s",
	}

	countMetrics := 0

	// Add all metrics to the panel
	for _, metric := range metrics {
		name := metric["__name__"]
		if _, ok := metric["unit"]; ok {
			continue
		}
		if !strings.HasPrefix(name, "openmetric_") {
			continue
		}

		expr := fmt.Sprintf(`avg(%s{test="%s", cloud="$cloud", goos="$goos", goarch="$goarch", branch="$branch", commit=~"$commit|"`, name, testName)
		if strings.Contains(testName, "tpccbench") {
			expr += `, warehouses=~"$warehouses"`
		}
		if !strings.HasSuffix(name, "_count") {
			expr += `, quantile="$quantile"`
		}
		expr += `})`

		countMetrics++

		panel["targets"] = append(panel["targets"].([]map[string]interface{}), map[string]interface{}{
			"datasource": map[string]string{
				"type": "prometheus",
				"uid":  *grafanaDatasourceUID,
			},
			"expr": expr,
			"legendFormat": name,
			"refId": name,
		})
	}
	
	if countMetrics == 0 {
		return []map[string]interface{}{}
	}

	return []map[string]interface{}{panel}
}

func createGaugePanel(metric map[string]string, testName string) map[string]interface{} {
	name := metric["__name__"]
	if !strings.HasPrefix(name, "openmetric_") {
		return nil
	}

	unit := metric["unit"]
	if unit == "" {
		return nil
	}

	// Check if this is a TPCC benchmark special metric
	isTPCCSpecialMetric := strings.Contains(testName, "tpccbench") && (strings.HasSuffix(name, "tpmc") || strings.HasSuffix(name, "efficiency"))

	if isTPCCSpecialMetric {
		return map[string]interface{}{
			"datasource": map[string]string{
				"type": "prometheus",
				"uid":  *grafanaDatasourceUID,
			},
			"fieldConfig": map[string]interface{}{
				"defaults": map[string]interface{}{
					"color": map[string]string{
						"mode": "palette-classic",
					},
					"custom": map[string]interface{}{
						"axisBorderShow": false,
						"axisCenteredZero": false,
						"axisColorMode": "text",
						"axisLabel": "",
						"axisPlacement": "auto",
						"barAlignment": 0,
						"drawStyle": "line",
						"fillOpacity": 0,
						"gradientMode": "none",
						"hideFrom": map[string]bool{
							"legend": false,
							"tooltip": false,
							"viz": false,
						},
						"insertNulls": false,
						"lineInterpolation": "linear",
						"lineWidth": 1,
						"pointSize": 5,
						"scaleDistribution": map[string]string{
							"type": "linear",
						},
						"showPoints": "auto",
						"spanNulls": false,
						"stacking": map[string]interface{}{
							"group": "A",
							"mode": "none",
						},
						"thresholdsStyle": map[string]string{
							"mode": "off",
						},
					},
					"mappings": []interface{}{},
					"thresholds": map[string]interface{}{
						"mode": "absolute",
						"steps": []map[string]interface{}{
							{
								"color": "green",
								"value": nil,
							},
						},
					},
					"unit": unit,
				},
				"overrides": []map[string]interface{}{
					{
						"matcher": map[string]interface{}{
							"id": "byName",
							"options": "warehouses",
						},
						"properties": []map[string]interface{}{
							{
								"id": "unit",
							},
						},
					},
				},
			},
			"gridPos": map[string]int{
				"h": 12,
				"w": 24,
				"x": 0,
				"y": 0,
			},
			"options": map[string]interface{}{
				"legend": map[string]interface{}{
					"calcs": []interface{}{},
					"displayMode": "list",
					"placement": "bottom",
					"showLegend": true,
				},
				"tooltip": map[string]interface{}{
					"mode": "single",
					"sort": "none",
				},
				"xField": "warehouses",
			},
			"targets": []map[string]interface{}{
				{
					"datasource": map[string]string{
						"type": "prometheus",
						"uid":  *grafanaDatasourceUID,
					},
					"disableTextWrap": false,
					"editorMode": "builder",
					"expr": fmt.Sprintf(`avg by(warehouses) (%s{test="%s", cloud="$cloud", goos="$goos", goarch="$goarch", branch="$branch", commit=~"$commit|"})`, name, testName),
					"fullMetaSearch": false,
					"includeNullMetadata": true,
					"legendFormat": "__auto",
					"range": true,
					"refId": "A",
					"useBackend": false,
				},
			},
			"title": fmt.Sprintf("%s (%s)", name, unit),
			"transformations": []map[string]interface{}{
				{
					"id": "labelsToFields",
					"options": map[string]interface{}{},
				},
				{
					"id": "merge",
					"options": map[string]interface{}{},
				},
				{
					"id": "convertFieldType",
					"options": map[string]interface{}{},
				},
				{
					"id": "groupBy",
					"options": map[string]interface{}{
						"fields": map[string]interface{}{
							"Value": map[string]interface{}{
								"aggregations": []string{"mean"},
								"operation": "aggregate",
							},
							"warehouses": map[string]interface{}{
								"aggregations": []interface{}{},
								"operation": "groupby",
							},
						},
					},
				},
			},
			"type": "trend",
		}
	}

	// Regular gauge panel for other metrics
	expr := fmt.Sprintf(`avg(%s{test="%s", cloud="$cloud", goos="$goos", goarch="$goarch", branch="$branch", commit=~"$commit|"`, name, testName)
	if name == "openmetric_tpce_latency" {
		expr += `, quantile="$quantile"`
	}
	expr += `})`

	return map[string]interface{}{
		"datasource": map[string]string{
			"type": "prometheus",
			"uid":  *grafanaDatasourceUID,
		},
		"fieldConfig": map[string]interface{}{
			"defaults": map[string]interface{}{
				"color": map[string]string{
					"mode": "palette-classic",
				},
				"custom": map[string]interface{}{
					"axisBorderShow": false,
					"axisCenteredZero": false,
					"axisColorMode": "text",
					"axisLabel": "",
					"axisPlacement": "auto",
					"barAlignment": 0,
					"drawStyle": "line",
					"fillOpacity": 0,
					"gradientMode": "none",
					"hideFrom": map[string]bool{
						"legend": false,
						"tooltip": false,
						"viz": false,
					},
					"insertNulls": false,
					"lineInterpolation": "linear",
					"lineWidth": 1,
					"pointSize": 5,
					"scaleDistribution": map[string]string{
						"type": "linear",
					},
					"showPoints": "auto",
					"spanNulls": true,
					"stacking": map[string]interface{}{
						"group": "A",
						"mode": "none",
					},
					"thresholdsStyle": map[string]string{
						"mode": "off",
					},
				},
				"mappings": []interface{}{},
				"thresholds": map[string]interface{}{
					"mode": "absolute",
					"steps": []map[string]interface{}{
						{
							"color": "green",
							"value": nil,
						},
					},
				},
				"unit": unit,
			},
		},
		"gridPos": map[string]int{
			"h": 12,
			"w": 24,
			"x": 0,
			"y": 0,
		},
		"options": map[string]interface{}{
			"legend": map[string]interface{}{
				"calcs": []string{"mean"},
				"displayMode": "table",
				"placement": "bottom",
				"showLegend": true,
			},
			"tooltip": map[string]interface{}{
				"mode": "single",
				"sort": "none",
			},
		},
		"targets": []map[string]interface{}{
			{
				"datasource": map[string]string{
					"type": "prometheus",
					"uid":  *grafanaDatasourceUID,
				},
				"expr": expr,
				"legendFormat": "__auto",
				"refId": "A",
			},
		},
		"title": fmt.Sprintf("%s (%s)", name, unit),
		"type": "timeseries",
	}
}

// sanitizeUID removes or replaces characters that are not allowed in Grafana UIDs
func sanitizeUID(s string) string {
	// Replace forward slashes and other special characters with hyphens
	re := regexp.MustCompile(`[^a-zA-Z0-9-_]`)
	return re.ReplaceAllString(s, "-")
}

func main() {
	flag.Parse()

	if *victoriaMetricsURL == "" {
		log.Fatal("victoriametrics-url is required")
	}
	if *grafanaURL == "" {
		log.Fatal("grafana-url is required")
	}
	if *grafanaToken == "" {
		log.Fatal("grafana-token is required")
	}
	if *grafanaDatasourceUID == "" {
		log.Fatal("grafana-datasource-uid is required")
	}

	vmClient := NewVictoriaMetricsClient(*victoriaMetricsURL)
	grafanaClient := NewGrafanaClient(*grafanaURL, *grafanaToken)

	ctx := context.Background()

	// Get all test names
	testNames, err := vmClient.GetTestNames(ctx)
	if err != nil {
		log.Fatalf("Failed to get test names: %v", err)
	}

	for _, testName := range testNames {
		// Get metrics for this test
		metrics, err := vmClient.GetMetricsForTest(ctx, testName)
		if err != nil {
			log.Printf("Failed to get metrics for test %s: %v", testName, err)
			continue
		}

		if len(metrics) == 0 {
			log.Printf("No metrics found for test %s", testName)
			continue
		}

		// Create dashboard
		dashboard := map[string]interface{}{
			"annotations": map[string]interface{}{
				"list": []map[string]interface{}{
					{
						"builtIn": 1,
						"datasource": map[string]string{
							"type": "grafana",
							"uid":  "-- Grafana --",
						},
						"enable": true,
						"hide": true,
						"iconColor": "rgba(0, 211, 255, 1)",
						"name": "Annotations & Alerts",
						"type": "dashboard",
					},
				},
			},
			"editable": true,
			"fiscalYearStartMonth": 0,
			"graphTooltip": 0,
			"id": nil,
			"links": []interface{}{},
			"liveNow": false,
			"panels": []map[string]interface{}{},
			"refresh": "5s",
			"schemaVersion": 38,
			"style": "dark",
			"tags": []interface{}{},
			"templating": map[string]interface{}{
				"list": []map[string]interface{}{
					{
						"current": map[string]interface{}{
							"selected": true,
							"text": "gce",
							"value": "gce",
						},
						"datasource": map[string]string{
							"type": "prometheus",
							"uid":  *grafanaDatasourceUID,
						},
						"definition": fmt.Sprintf(`label_values(%s{test="%s"}, cloud)`, metrics[0]["__name__"], testName),
						"hide": 0,
						"includeAll": false,
						"label": "Cloud",
						"multi": false,
						"name": "cloud",
						"options": []interface{}{},
						"query": map[string]interface{}{
							"query": fmt.Sprintf(`label_values(%s{test="%s"}, cloud)`, metrics[0]["__name__"], testName),
							"refId": "StandardVariableQuery",
						},
						"refresh": 2,
						"regex": "",
						"skipUrlSync": false,
						"sort": 1,
						"type": "query",
					},
					{
						"current": map[string]interface{}{
							"selected": true,
							"text": "linux",
							"value": "linux",
						},
						"datasource": map[string]string{
							"type": "prometheus",
							"uid":  *grafanaDatasourceUID,
						},
						"definition": fmt.Sprintf(`label_values(%s{test="%s"}, goos)`, metrics[0]["__name__"], testName),
						"hide": 0,
						"includeAll": true,
						"label": "Operating System",
						"multi": false,
						"name": "goos",
						"options": []interface{}{},
						"query": map[string]interface{}{
							"query": fmt.Sprintf(`label_values(%s{test="%s"}, goos)`, metrics[0]["__name__"], testName),
							"refId": "StandardVariableQuery",
						},
						"refresh": 2,
						"regex": "",
						"skipUrlSync": false,
						"sort": 1,
						"type": "query",
					},
					{
						"current": map[string]interface{}{
							"selected": true,
							"text": "amd64",
							"value": "amd64",
						},
						"datasource": map[string]string{
							"type": "prometheus",
							"uid":  *grafanaDatasourceUID,
						},
						"definition": fmt.Sprintf(`label_values(%s{test="%s"}, goarch)`, metrics[0]["__name__"], testName),
						"hide": 0,
						"includeAll": true,
						"label": "Architecture",
						"multi": false,
						"name": "goarch",
						"options": []interface{}{},
						"query": map[string]interface{}{
							"query": fmt.Sprintf(`label_values(%s{test="%s"}, goarch)`, metrics[0]["__name__"], testName),
							"refId": "StandardVariableQuery",
						},
						"refresh": 2,
						"regex": "",
						"skipUrlSync": false,
						"sort": 1,
						"type": "query",
					},
					{
						"current": map[string]interface{}{
							"selected": true,
							"text": "master",
							"value": "master",
						},
						"datasource": map[string]string{
							"type": "prometheus",
							"uid":  *grafanaDatasourceUID,
						},
						"definition": fmt.Sprintf(`label_values(%s{test="%s", cloud="$cloud"}, branch)`, metrics[0]["__name__"], testName),
						"hide": 0,
						"includeAll": false,
						"label": "Branch",
						"multi": false,
						"name": "branch",
						"options": []interface{}{},
						"query": map[string]interface{}{
							"query": fmt.Sprintf(`label_values(%s{test="%s", cloud="$cloud"}, branch)`, metrics[0]["__name__"], testName),
							"refId": "StandardVariableQuery",
						},
						"refresh": 2,
						"regex": "",
						"skipUrlSync": false,
						"sort": 1,
						"type": "query",
					},
					{
						"current": map[string]interface{}{
							"selected": false,
						},
						"datasource": map[string]string{
							"type": "prometheus",
							"uid":  *grafanaDatasourceUID,
						},
						"definition": fmt.Sprintf(`label_values(%s{test="%s", cloud="$cloud", branch="$branch"}, commit)`, metrics[0]["__name__"], testName),
						"hide": 0,
						"includeAll": true,
						"label": "Commit",
						"multi": false,
						"name": "commit",
						"options": []interface{}{},
						"query": map[string]interface{}{
							"query": fmt.Sprintf(`label_values(%s{test="%s", cloud="$cloud", branch="$branch"}, commit)`, metrics[0]["__name__"], testName),
							"refId": "StandardVariableQuery",
						},
						"refresh": 2,
						"regex": "",
						"skipUrlSync": false,
						"sort": 1,
						"type": "query",
					},
					{
						"current": map[string]interface{}{
							"selected": false,
							"text": "0.5",
							"value": "0.5",
						},
						"datasource": map[string]string{
							"type": "prometheus",
							"uid":  *grafanaDatasourceUID,
						},
						"definition": "0.5,0.9,0.95,0.99",
						"hide": 0,
						"includeAll": false,
						"label": "Quantile",
						"multi": false,
						"name": "quantile",
						"options": []map[string]interface{}{
							{"text": "0.5", "value": "0.5"},
							{"text": "0.9", "value": "0.9"},
							{"text": "0.95", "value": "0.95"},
							{"text": "0.99", "value": "0.99"},
						},
						"query": "0.5,0.9,0.95,0.99",
						"refresh": 2,
						"regex": "",
						"skipUrlSync": false,
						"sort": 0,
						"type": "custom",
					},
				},
			},
			"time": map[string]interface{}{
				"from": "now-1y",
				"to": "now",
			},
			"timepicker": map[string]interface{}{},
			"timezone": "",
			"title": testName,
			"version": 1,
			"weekStart": "",
		}

		// Add warehouses variable for TPCC benchmark tests
		if strings.Contains(testName, "tpccbench") {
			warehousesVar := map[string]interface{}{
				"current": map[string]interface{}{
					"selected": false,
				},
				"datasource": map[string]string{
					"type": "prometheus",
					"uid":  *grafanaDatasourceUID,
				},
				"definition": fmt.Sprintf(`label_values(%s{test="%s", cloud="$cloud", goos="$goos", goarch="$goarch", branch="$branch", commit=~"$commit|"}, warehouses)`, metrics[0]["__name__"], testName),
				"hide": 0,
				"includeAll": true,
				"label": "Warehouses",
				"multi": true,
				"name": "warehouses",
				"options": []interface{}{},
				"query": fmt.Sprintf(`label_values(%s{test="%s", cloud="$cloud", goos="$goos", goarch="$goarch", branch="$branch", commit=~"$commit|"}, warehouses)`, metrics[0]["__name__"], testName),
				"refresh": 2,
				"regex": "",
				"skipUrlSync": false,
				"sort": 1,
				"type": "query",
			}
			
			// Get the current templating list
			templating := dashboard["templating"].(map[string]interface{})
			list := templating["list"].([]map[string]interface{})
			
			// Append the new variable
			list = append(list, warehousesVar)
			
			// Update the templating list
			templating["list"] = list
			dashboard["templating"] = templating
		}

		// Add gauge panels first
		yPos := 0
		if strings.Contains(testName, "tpccbench") {
			// First add max warehouse metric
			for _, metric := range metrics {
				name := metric["__name__"]
				if strings.HasSuffix(name, "max_warehouse") {
					if panel := createGaugePanel(metric, testName); panel != nil {
						panel["gridPos"].(map[string]int)["y"] = yPos
						dashboard["panels"] = append(dashboard["panels"].([]map[string]interface{}), panel)
						yPos += 12
					}
					break
				}
			}
			// Then add tpmc and efficiency metrics
			for _, metric := range metrics {
				name := metric["__name__"]
				if strings.HasSuffix(name, "tpmc") || strings.HasSuffix(name, "efficiency") {
					if panel := createGaugePanel(metric, testName); panel != nil {
						panel["gridPos"].(map[string]int)["y"] = yPos
						dashboard["panels"] = append(dashboard["panels"].([]map[string]interface{}), panel)
						yPos += 12
					}
				}
			}
		} else if strings.HasPrefix(testName, "sysbench") {
			// For sysbench, create a single panel with all metrics
			panel := map[string]interface{}{
				"datasource": map[string]string{
					"type": "prometheus",
					"uid":  *grafanaDatasourceUID,
				},
				"fieldConfig": map[string]interface{}{
					"defaults": map[string]interface{}{
						"color": map[string]string{
							"mode": "palette-classic",
						},
						"custom": map[string]interface{}{
							"axisBorderShow": false,
							"axisCenteredZero": false,
							"axisColorMode": "text",
							"axisLabel": "",
							"axisPlacement": "auto",
							"barAlignment": 0,
							"drawStyle": "line",
							"fillOpacity": 0,
							"gradientMode": "none",
							"hideFrom": map[string]bool{
								"legend": false,
								"tooltip": false,
								"viz": false,
							},
							"insertNulls": false,
							"lineInterpolation": "linear",
							"lineWidth": 1,
							"pointSize": 5,
							"scaleDistribution": map[string]string{
								"type": "linear",
							},
							"showPoints": "auto",
							"spanNulls": true,
							"stacking": map[string]interface{}{
								"group": "A",
								"mode": "none",
							},
							"thresholdsStyle": map[string]string{
								"mode": "off",
							},
						},
						"mappings": []interface{}{},
						"thresholds": map[string]interface{}{
							"mode": "absolute",
							"steps": []map[string]interface{}{
								{
									"color": "green",
									"value": nil,
								},
							},
						},
						"unit": "ops/s",
					},
				},
				"gridPos": map[string]int{
					"h": 12,
					"w": 24,
					"x": 0,
					"y": yPos,
				},
				"options": map[string]interface{}{
					"legend": map[string]interface{}{
						"calcs": []string{"mean"},
						"displayMode": "table",
						"placement": "bottom",
						"showLegend": true,
					},
					"tooltip": map[string]interface{}{
						"mode": "single",
						"sort": "none",
					},
				},
				"targets": []map[string]interface{}{},
				"title": "Sysbench Metrics",
				"type": "timeseries",
			}

			// Add all metrics to the panel
			refId := 'A'
			for _, metric := range metrics {
				name := metric["__name__"]
				if !strings.HasPrefix(name, "openmetric_") {
					continue
				}

				unit := metric["unit"]
				if unit == "" {
					continue
				}

				expr := fmt.Sprintf(`avg(%s{test="%s", cloud="$cloud", branch="$branch", commit=~"$commit|"})`, name, testName)
				panel["targets"] = append(panel["targets"].([]map[string]interface{}), map[string]interface{}{
					"datasource": map[string]string{
						"type": "prometheus",
						"uid":  *grafanaDatasourceUID,
					},
					"expr": expr,
					"legendFormat": "__auto",
					"refId": string(refId),
				})
				refId++
			}

			if len(panel["targets"].([]map[string]interface{})) > 0 {
				dashboard["panels"] = append(dashboard["panels"].([]map[string]interface{}), panel)
				yPos += 12
			}
		} else {
			// For non-TPCC and non-sysbench tests, keep original order
			for _, metric := range metrics {
				if panel := createGaugePanel(metric, testName); panel != nil {
					panel["gridPos"].(map[string]int)["y"] = yPos
					dashboard["panels"] = append(dashboard["panels"].([]map[string]interface{}), panel)
					yPos += 12
				}
			}
		}

		// Add summary panels
		summaryPanels := createSummaryPanel(metrics, testName)

		for _, panel := range summaryPanels {
			panel["gridPos"].(map[string]int)["y"] = yPos
			dashboard["panels"] = append(dashboard["panels"].([]map[string]interface{}), panel)
				yPos += 12
			}
		

		// Create dashboard in Grafana
		if err := grafanaClient.CreateDashboard(ctx, dashboard, *folderUID); err != nil {
			log.Printf("Failed to create dashboard for test %s: %v", testName, err)
			continue
		}

		log.Printf("Created dashboard for test %s", testName)
	}
} 