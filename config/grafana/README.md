# Grafana Dashboard

This directory contains a Grafana dashboard for monitoring AI Inference Gateway.

## Importing the Dashboard

### Option 1: Import via Grafana UI

1. Open Grafana and go to **Dashboards** > **Import**
2. Click **Upload JSON file** and select `dashboard.json`
3. Select your Prometheus datasource
4. Click **Import**

### Option 2: ConfigMap for Kubernetes

Create a ConfigMap to automatically provision the dashboard:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: inference-gateway-dashboard
  namespace: monitoring
  labels:
    grafana_dashboard: "1"
data:
  inference-gateway.json: |
    <contents of dashboard.json>
```

Ensure your Grafana deployment is configured to load dashboards from ConfigMaps.

## Dashboard Panels

### Overview
- **Request Rate**: Current requests per second across all routes
- **Success Rate**: Percentage of successful (2xx) responses
- **P95 Latency**: 95th percentile request duration
- **Total Cost**: Cumulative cost across all backends

### Traffic
- **Request Rate by Route**: Traffic distribution across routes
- **Request Rate by Backend**: Traffic distribution across backends

### Latency
- **Latency Percentiles by Backend**: P50, P95, P99 latency per backend
- **Active Requests by Backend**: Current in-flight requests

### Errors & Rate Limits
- **Errors by Backend**: Error rate broken down by backend and error type
- **Rate Limit Hits by Route**: Rate limiting activity per route

### Backend Health
- **Backend Health Status**: Current health status of each backend
- **Fallback Activations**: Fallback chain activations between backends

### Costs & Tokens
- **Cost Rate by Backend**: Spending rate per backend
- **Token Rate by Backend**: Token consumption rate (input/output)

### A/B Experiments
- **Experiment Assignments**: Traffic distribution across experiment variants

## Metrics Reference

| Metric | Description |
|--------|-------------|
| `inference_gateway_requests_total` | Total requests (labels: route, backend, status) |
| `inference_gateway_request_duration_seconds` | Request duration histogram |
| `inference_gateway_request_errors_total` | Error count (labels: route, backend, error_type) |
| `inference_gateway_backend_health` | Backend health (1=healthy, 0=unhealthy) |
| `inference_gateway_active_requests` | Active requests per backend |
| `inference_gateway_rate_limit_hits_total` | Rate limit rejections |
| `inference_gateway_experiment_assignments_total` | Experiment assignments |
| `inference_gateway_cost_total` | Cumulative cost in USD |
| `inference_gateway_tokens_total` | Tokens processed (labels: type=input/output) |
| `inference_gateway_fallbacks_total` | Fallback chain activations |

## Prerequisites

- Grafana 9.0+
- Prometheus datasource configured
- AI Inference Gateway with metrics enabled
