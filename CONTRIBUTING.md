# Contributing to AI Inference Gateway

Thank you for your interest in contributing to AI Inference Gateway! This document provides guidelines and instructions for contributing.

## Code of Conduct

By participating in this project, you agree to maintain a respectful and inclusive environment for everyone.

## Getting Started

### Prerequisites

- Go 1.24+
- Docker
- kubectl
- Access to a Kubernetes cluster v1.32+ (kind, minikube, or remote)
- Make

### Development Setup

1. **Fork and clone the repository**
   ```bash
   git clone https://github.com/YOUR_USERNAME/inference-gateway.git
   cd inference-gateway
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Install development tools**
   ```bash
   # Install controller-gen for CRD generation
   go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

   # Install golangci-lint for linting
   go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
   ```

4. **Generate manifests and code**
   ```bash
   make manifests generate
   ```

5. **Run tests**
   ```bash
   make test
   ```

## Development Workflow

### Making Changes

1. Create a new branch from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes following the code style guidelines below.

3. Run tests and linting:
   ```bash
   make test
   make lint
   ```

4. Commit your changes with a clear message:
   ```bash
   git commit -m "Add feature: description of changes"
   ```

### Running Locally

1. **Install CRDs to your cluster**
   ```bash
   make install
   ```

2. **Run the controller locally**
   ```bash
   make run
   ```

3. **Apply sample resources**
   ```bash
   kubectl apply -f config/samples/
   ```

### Building and Deploying

```bash
# Build the binary
make build

# Build and push Docker image
make docker-build docker-push IMG=<your-registry>/inference-gateway:tag

# Deploy to cluster
make deploy IMG=<your-registry>/inference-gateway:tag
```

## Code Style Guidelines

### Go Code

- Follow standard Go conventions and idioms
- Use `gofmt` for formatting (automatically enforced)
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions focused and concise
- Handle errors explicitly - don't ignore them

### Kubernetes Resources

- Use consistent naming conventions for CRD fields
- Add validation markers where appropriate
- Include helpful descriptions in CRD schemas

### Testing

- Write unit tests for new functionality
- Use table-driven tests where appropriate
- Aim for meaningful test coverage
- Test edge cases and error conditions

Example test structure:
```go
func TestFeature(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"basic case", "input", "expected"},
        {"edge case", "", ""},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := Feature(tt.input)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

## Pull Request Process

1. **Before submitting:**
   - Ensure all tests pass: `make test`
   - Run linting: `make lint`
   - Update documentation if needed
   - Add tests for new functionality

2. **PR Title:** Use a clear, descriptive title that summarizes the change.

3. **PR Description:** Include:
   - What the change does
   - Why it's needed
   - How to test it
   - Any breaking changes

4. **Review:** Address any feedback from reviewers promptly.

5. **Merge:** Once approved, the PR will be merged by a maintainer.

## Areas for Contribution

We welcome contributions in these areas:

### Backend Implementations
- New provider support (Azure OpenAI, AWS Bedrock, Google Vertex AI)
- Custom backend types
- Provider-specific optimizations

### Features
- Semantic caching implementation
- Request/response logging
- Circuit breaker pattern
- Multi-cluster support

### Observability
- Grafana dashboard templates
- Custom metrics and alerting rules

### Documentation
- API documentation
- Architecture diagrams
- Tutorials and guides
- Example configurations

### Testing
- Unit test coverage
- Integration tests
- End-to-end tests
- Performance benchmarks

## Reporting Issues

When reporting issues, please include:

1. **Description:** Clear description of the problem
2. **Steps to reproduce:** How to trigger the issue
3. **Expected behavior:** What should happen
4. **Actual behavior:** What actually happens
5. **Environment:** Go version, Kubernetes version, OS
6. **Logs:** Relevant error messages or logs

## Getting Help

- Open an issue for questions
- Check existing issues for similar problems
- Review the documentation in the README

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
