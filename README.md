# AI Inference Gateway

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.32+-326CE5?style=flat&logo=kubernetes)](https://kubernetes.io/)
[![KServe](https://img.shields.io/badge/KServe-0.15+-purple?style=flat)](https://kserve.github.io/website/)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

A Kubernetes-native inference gateway for intelligent routing, A/B testing, and failover across multiple LLM and ML model backends.

## The Problem

Organizations deploying AI/ML models face significant challenges:

- **Vendor Lock-in**: Tight coupling to a single LLM provider (OpenAI, Anthropic, etc.)
- **No Intelligent Failover**: When one model goes down, the entire application fails
- **Blind A/B Testing**: No standardized way to test new models against production traffic
- **Cost Opacity**: No visibility into per-request costs across different providers
- **Complex Routing**: Need to route different request types to appropriate models

## The Solution

AI Inference Gateway provides a unified control plane for all your inference traffic:

```
                         ┌─────────────────────────────────┐
                         │     AI Inference Gateway        │
                         │                                 │
  User Request ─────────►│  • Intelligent Routing          │
                         │  • A/B Testing                  │
                         │  • Automatic Failover           │
                         │  • Cost Tracking                │
                         │  • Rate Limiting                │
                         └───────────┬─────────────────────┘
                                     │
           ┌─────────────────────────┼─────────────────────────┐
           │                         │                         │
           ▼                         ▼                         ▼
    ┌─────────────┐          ┌─────────────┐          ┌─────────────┐
    │   KServe    │          │   OpenAI    │          │  Anthropic  │
    │  (Mistral)  │          │   (GPT-4)   │          │  (Claude)   │
    └─────────────┘          └─────────────┘          └─────────────┘
```

## Features

### Core Capabilities

| Feature | Description | Status |
|---------|-------------|--------|
| **Multi-Backend Routing** | Route requests to KServe, external APIs, or K8s services | Implemented |
| **Weighted Traffic Split** | Distribute traffic across backends by percentage | Implemented |
| **Fallback Chains** | Automatic failover through ordered backend list | Implemented |
| **A/B Testing** | Consistent hashing for deterministic user assignment | Implemented |
| **Rate Limiting** | Per-route and per-user token bucket rate limiting | Implemented |
| **Cost Tracking** | Track cost per request with token-based pricing | Implemented |
| **Health Checks** | Automatic backend health monitoring with thresholds | Implemented |
| **Prometheus Metrics** | Request counts, latency histograms, error tracking | Implemented |

### Supported Backends

- **KServe InferenceServices** - Native integration with CNCF KServe
- **External APIs** - OpenAI, Anthropic, Cohere, and custom endpoints
- **Kubernetes Services** - Any K8s service (vLLM, Ollama, etc.)

## Quick Start

### Prerequisites

- Kubernetes cluster v1.32+ (tested with v1.34)
- kubectl configured
- (Optional) KServe v0.15+ for KServe backend support

### Installation

```bash
# Install CRDs
kubectl apply -f https://raw.githubusercontent.com/judeoyovbaire/inference-gateway/main/config/crd/bases/gateway.inference-gateway.io_inferenceroutes.yaml
kubectl apply -f https://raw.githubusercontent.com/judeoyovbaire/inference-gateway/main/config/crd/bases/gateway.inference-gateway.io_inferencebackends.yaml

# Install controller
kubectl apply -f https://raw.githubusercontent.com/judeoyovbaire/inference-gateway/main/config/default/manager.yaml
```

### Example: Multi-Provider Setup with Fallback

```yaml
# Define backends
apiVersion: gateway.inference-gateway.io/v1alpha1
kind: InferenceBackend
metadata:
  name: openai-gpt4
spec:
  type: external
  external:
    url: https://api.openai.com/v1
    provider: openai
    model: gpt-4
    apiKeySecret:
      name: openai-credentials
      key: api-key
  cost:
    inputTokenCost: "0.03"
    outputTokenCost: "0.06"
  timeoutSeconds: 30
---
apiVersion: gateway.inference-gateway.io/v1alpha1
kind: InferenceBackend
metadata:
  name: anthropic-claude
spec:
  type: external
  external:
    url: https://api.anthropic.com/v1
    provider: anthropic
    model: claude-3-sonnet
    apiKeySecret:
      name: anthropic-credentials
      key: api-key
  cost:
    inputTokenCost: "0.003"
    outputTokenCost: "0.015"
  priority: 1  # Higher priority = tried first in fallback
---
apiVersion: gateway.inference-gateway.io/v1alpha1
kind: InferenceBackend
metadata:
  name: local-mistral
spec:
  type: kserve
  kserve:
    serviceName: mistral-7b
  priority: 2  # Lowest cost, highest priority
---
# Create route with fallback chain
apiVersion: gateway.inference-gateway.io/v1alpha1
kind: InferenceRoute
metadata:
  name: chatbot-route
spec:
  # Default routing
  defaultBackend:
    name: local-mistral

  # Premium users get GPT-4
  rules:
    - match:
        headers:
          x-user-tier: "premium"
      backends:
        - name: openai-gpt4

  # Fallback chain if primary fails
  fallback:
    backends:
      - local-mistral
      - anthropic-claude
      - openai-gpt4
    timeoutSeconds: 10

  # Rate limiting
  rateLimit:
    requestsPerMinute: 100
    perUser: true

  # Enable cost tracking
  costTracking: true
```

### Example: A/B Testing New Models

```yaml
apiVersion: gateway.inference-gateway.io/v1alpha1
kind: InferenceRoute
metadata:
  name: model-experiment
spec:
  defaultBackend:
    name: anthropic-claude

  experiments:
    - name: claude-3-5-test
      control: anthropic-claude      # Current model
      treatment: anthropic-claude-35  # New model to test
      trafficPercent: 10              # 10% to new model
      metric: latency_p95             # Track P95 latency
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                           │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                 Inference Gateway Controller              │  │
│  │                                                          │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │  │
│  │  │   Route     │  │  Backend    │  │   Metrics   │      │  │
│  │  │  Reconciler │  │  Reconciler │  │  Collector  │      │  │
│  │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘      │  │
│  │         │                │                │              │  │
│  │         └────────────────┴────────────────┘              │  │
│  │                          │                               │  │
│  └──────────────────────────┼───────────────────────────────┘  │
│                             │                                   │
│  ┌──────────────────────────▼───────────────────────────────┐  │
│  │                    Gateway Proxy                          │  │
│  │                                                          │  │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐     │  │
│  │  │ Router  │  │A/B Test │  │Fallback │  │  Cost   │     │  │
│  │  │         │  │ Manager │  │ Handler │  │ Tracker │     │  │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘     │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## CRDs

### InferenceRoute

Defines routing rules for directing inference requests to backends.

| Field | Description |
|-------|-------------|
| `rules` | List of routing rules with match conditions and backends |
| `defaultBackend` | Backend to use when no rules match |
| `fallback` | Ordered list of backends for automatic failover |
| `rateLimit` | Rate limiting configuration |
| `experiments` | A/B testing experiments |
| `costTracking` | Enable per-request cost tracking |

### InferenceBackend

Defines a backend service for inference requests.

| Field | Description |
|-------|-------------|
| `type` | Backend type: `kserve`, `external`, or `kubernetes` |
| `kserve` | KServe InferenceService configuration |
| `external` | External API configuration (OpenAI, Anthropic, etc.) |
| `kubernetes` | Kubernetes Service configuration |
| `healthCheck` | Health check settings |
| `cost` | Token-based cost configuration |

## Roadmap

### Phase 1: Core Routing - Complete
- [x] Project scaffolding with Kubebuilder
- [x] InferenceRoute and InferenceBackend CRD design
- [x] Route and Backend controllers with reconciliation
- [x] KServe, External API, and Kubernetes backend support
- [x] Embedded reverse proxy with request routing

### Phase 2: Intelligent Features - Complete
- [x] Weighted traffic splitting across backends
- [x] Fallback chain with per-attempt timeouts
- [x] Health check integration with failure thresholds
- [x] Token bucket rate limiting (per-route and per-user)

### Phase 3: Advanced Capabilities - Complete
- [x] A/B testing with consistent hashing
- [x] Cost tracking with provider-specific token parsing
- [x] Prometheus metrics (requests, latency, errors, costs)
- [ ] Grafana dashboards

### Phase 4: Production Ready - In Progress
- [ ] Semantic caching
- [ ] Request/response logging
- [ ] Circuit breaker pattern
- [ ] Multi-cluster support
- [ ] CNCF Sandbox submission

## Integration with Other Projects

AI Inference Gateway is designed to work seamlessly with:

- **[AI FinOps Platform](https://github.com/judeoyovbaire/ai-finops-platform)** - Cost data from Gateway feeds into FinOps dashboards
- **[MLOps Platform](https://github.com/judeoyovbaire/mlops-platform)** - Reference architecture showing full integration

## Development

### Prerequisites

- Go 1.24+
- Docker
- kubectl
- Access to a Kubernetes cluster v1.32+

### Setup

```bash
# Clone the repository
git clone https://github.com/judeoyovbaire/inference-gateway.git
cd inference-gateway

# Install dependencies
go mod download

# Run tests
make test

# Generate manifests
make manifests

# Build
make build

# Run locally (requires kubeconfig)
make run
```

### Deploy to Cluster

```bash
# Build and push image
make docker-build docker-push IMG=<your-registry>/inference-gateway:latest

# Install CRDs
make install

# Deploy controller
make deploy IMG=<your-registry>/inference-gateway:latest
```

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

### Areas for Contribution

- Backend implementations (new providers)
- Metrics and observability
- Documentation
- Testing

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

## Author

**Jude Oyovbaire** - Senior Platform & DevOps Engineer

- Website: [judaire.io](https://judaire.io)
- GitHub: [@judeoyovbaire](https://github.com/judeoyovbaire)
- LinkedIn: [judeoyovbaire](https://linkedin.com/in/judeoyovbaire)

---

*Building AI infrastructure in the open. Targeting CNCF Sandbox.*
