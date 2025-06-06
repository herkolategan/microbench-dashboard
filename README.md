# RoachPerf Server

A performance monitoring server for CockroachDB that integrates with VictoriaMetrics and Grafana.

## Prerequisites

- Go 1.21 or later
- Docker (for containerized builds)
- Make (optional, but recommended)

## Environment Variables

The server uses the following environment variables:

- `VICTORIAMETRICS_URL`: URL for VictoriaMetrics (default: "https://roachperf-o11y.observability.testeng.crdb.io/vmselect/select/0/prometheus")
- `GRAFANA_URL`: URL for Grafana

## Running Locally

1. Build the binary:
```bash
make build
```

2. Run the server:
```bash
./perf-server -victoriametrics-url="https://roachperf-o11y.observability.testeng.crdb.io/vmselect/select/0/prometheus" -listen-http=:8080 -grafana-url="http://35.227.63.94:3000"
```

Or use the run script:
```bash
./perf/run.sh
```

## Building and Running with Docker

### Building the Docker Image

1. Build the Docker image:
```bash
gcloud auth print-access-token | docker login -u oauth2accesstoken --password-stdin https://us-central1-docker.pkg.dev
make build-docker
```

This will create an image with the following tags:
- Latest version: `us-central1-docker.pkg.dev/cockroach-testeng-infra/crl-roachperf/roachperf-server:latest`
- Git SHA: `us-central1-docker.pkg.dev/cockroach-testeng-infra/crl-roachperf/roachperf-server:<git-sha>`
- Version tag: `us-central1-docker.pkg.dev/cockroach-testeng-infra/crl-roachperf/roachperf-server:<version>`

### Running the Docker Container

```bash
docker run -p 8080:8080 \
  -e VICTORIAMETRICS_URL="https://roachperf-o11y.observability.testeng.crdb.io/vmselect/select/0/prometheus" \
  -e GRAFANA_URL="http://<grafana_url>:3000" \
  us-central1-docker.pkg.dev/cockroach-testeng-infra/crl-roachperf/roachperf-server:latest
```

### Pushing to Artifact Registry

To push the built image to the Artifact Registry:

```bash
make push-docker
```

## Version Management

The Makefile supports versioning in several ways:

1. Using git tags:
```bash
git tag v1.0.0
make build-docker push-docker
```

2. Specifying a custom version:
```bash
make build-docker push-docker VERSION=v1.0.0
```

## Available Make Commands

- `make build`: Build the binary locally
- `make build-docker`: Build the Docker image
- `make push-docker`: Push the Docker image to registry
- `make clean`: Remove built binary
- `make version`: Show current version
- `make help`: Show all available commands

## Development

### Project Structure

- `perf/`: Contains the main server code
- `Dockerfile`: Multi-stage Docker build configuration
- `Makefile`: Build and deployment automation

### Building for Different Platforms

The project is configured to build for Linux AMD64 by default, which is required for Cloud Run. If you need to build for a different platform, you can modify the `PLATFORM` variable in the Makefile. 