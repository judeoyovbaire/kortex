# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security issue, please report it responsibly.

### How to Report

**Please do NOT report security vulnerabilities through public GitHub issues.**

Instead, please report them via email to: security@judaire.io

Include the following information:

1. **Description:** A clear description of the vulnerability
2. **Impact:** What an attacker could achieve
3. **Steps to reproduce:** Detailed steps to reproduce the issue
4. **Affected versions:** Which versions are affected
5. **Suggested fix:** If you have one

### What to Expect

- **Acknowledgment:** We will acknowledge receipt within 48 hours
- **Initial assessment:** We will provide an initial assessment within 5 business days
- **Resolution timeline:** We aim to resolve critical issues within 30 days
- **Credit:** We will credit reporters in our security advisories (unless you prefer to remain anonymous)

## Security Best Practices

When deploying AI Inference Gateway, follow these security recommendations:

### API Key Management

1. **Use Kubernetes Secrets** for storing API keys
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: openai-credentials
   type: Opaque
   stringData:
     api-key: <your-api-key>
   ```

2. **Enable RBAC** to restrict access to secrets
3. **Rotate API keys** regularly
4. **Use separate keys** for different environments

### Network Security

1. **Use NetworkPolicies** to restrict pod-to-pod communication
2. **Enable TLS** for all external communications
3. **Use internal cluster DNS** for backend services
4. **Consider service mesh** for mTLS between services

### Rate Limiting

1. **Enable per-user rate limiting** to prevent abuse
   ```yaml
   rateLimit:
     requestsPerMinute: 100
     perUser: true
     userHeader: "x-user-id"
   ```

2. **Monitor rate limit metrics** for anomalies

### Request Validation

1. **Set appropriate request body limits** (default: 10MB)
2. **Validate input** at the application layer
3. **Use request timeouts** to prevent resource exhaustion

### Monitoring and Logging

1. **Enable Prometheus metrics** for visibility
2. **Monitor for unusual patterns:**
   - Spike in error rates
   - Unusual request volumes
   - Requests to unknown backends
3. **Set up alerts** for security-relevant events

### RBAC Configuration

Minimal RBAC for the controller:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: inference-gateway-role
rules:
- apiGroups: ["gateway.inference-gateway.io"]
  resources: ["inferenceroutes", "inferencebackends"]
  verbs: ["get", "list", "watch", "update", "patch"]
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get"]  # Read-only access to secrets for API keys
```

### Container Security

1. **Run as non-root user** (configured by default)
2. **Use read-only root filesystem** where possible
3. **Set resource limits** to prevent DoS
4. **Use minimal base images**

### External API Security

When connecting to external LLM providers:

1. **Allowlist specific endpoints** rather than allowing all external traffic
2. **Use egress NetworkPolicies**
3. **Monitor costs** to detect unauthorized usage
4. **Enable provider-side security** (IP allowlists, usage limits)

## Known Security Considerations

### Response Body Capture

Cost tracking requires reading response bodies. This means:
- Response data is temporarily stored in memory
- Large responses may impact memory usage
- Consider disabling cost tracking for sensitive workloads

### Health Check Exposure

Health endpoints may expose backend availability information. Consider:
- Restricting health endpoint access
- Using internal health checks only

### Multi-tenancy

The gateway does not provide multi-tenant isolation by default. For multi-tenant deployments:
- Use namespace-scoped resources
- Implement tenant identification headers
- Use separate gateway instances per tenant

## Security Updates

Security updates will be released as patch versions. We recommend:

1. **Subscribe to releases** on GitHub
2. **Update promptly** when security patches are available
3. **Review changelogs** for security-related changes

## Responsible Disclosure

We follow responsible disclosure practices:

1. We will work with reporters to understand and validate issues
2. We will develop and test fixes before public disclosure
3. We will credit reporters (with permission)
4. We will publish security advisories for significant issues
