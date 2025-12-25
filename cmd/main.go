/*
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
*/

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
	"github.com/judeoyovbaire/kortex/internal/cache"
	"github.com/judeoyovbaire/kortex/internal/controller"
	"github.com/judeoyovbaire/kortex/internal/health"
	"github.com/judeoyovbaire/kortex/internal/proxy"
	"github.com/judeoyovbaire/kortex/internal/tracing"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(gatewayv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var proxyAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var enableTracing bool
	var otlpEndpoint string
	var enableSmartRouting bool
	var smartRoutingLongContextThreshold int
	var smartRoutingFastModelThreshold int
	var smartRoutingLongContextBackend string
	var smartRoutingFastModelBackend string
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&proxyAddr, "proxy-bind-address", ":8080", "The address the inference proxy binds to.")
	flag.BoolVar(&enableTracing, "enable-tracing", false, "Enable OpenTelemetry tracing.")
	flag.StringVar(&otlpEndpoint, "otlp-endpoint", "localhost:4317", "OTLP collector endpoint for tracing.")
	flag.BoolVar(&enableSmartRouting, "enable-smart-routing", false, "Enable smart routing based on request characteristics.")
	flag.IntVar(&smartRoutingLongContextThreshold, "smart-routing-long-context-threshold", 4000, "Token count threshold for long-context routing.")
	flag.IntVar(&smartRoutingFastModelThreshold, "smart-routing-fast-model-threshold", 500, "Token count threshold for fast model routing.")
	flag.StringVar(&smartRoutingLongContextBackend, "smart-routing-long-context-backend", "", "Backend name for long-context requests.")
	flag.StringVar(&smartRoutingFastModelBackend, "smart-routing-fast-model-backend", "", "Backend name for short/fast requests.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production. For production, configure cert-manager:
	// - Enable [METRICS-WITH-CERTS] in config/default/kustomization.yaml
	// - Enable [PROMETHEUS-WITH-CERTS] in config/prometheus/kustomization.yaml
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "b59fb625.kortex.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Create shared components for controllers and proxy
	routeCache := cache.NewStore()
	healthChecker := health.NewChecker()

	// Setup InferenceBackend controller
	if err := (&controller.InferenceBackendReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		HealthChecker: healthChecker,
		Cache:         routeCache,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "InferenceBackend")
		os.Exit(1)
	}

	// Setup InferenceRoute controller
	if err := (&controller.InferenceRouteReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Cache:  routeCache,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "InferenceRoute")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	// Create P2/P3 components for proxy server
	metricsRecorder := proxy.NewMetricsRecorder()
	rateLimiter := proxy.NewRateLimiter()
	experimentManager := proxy.NewExperimentManager(metricsRecorder)
	costTracker := proxy.NewCostTracker(metricsRecorder)

	// Initialize OpenTelemetry tracer if enabled
	var tracer *tracing.Tracer
	if enableTracing {
		tracingConfig := tracing.Config{
			Enabled:        true,
			Endpoint:       otlpEndpoint,
			ServiceName:    "kortex-gateway",
			ServiceVersion: "v0.1.0",
			SampleRate:     1.0,
			Insecure:       true,
		}
		var err error
		tracer, err = tracing.NewTracer(tracingConfig)
		if err != nil {
			setupLog.Error(err, "failed to initialize tracer")
			os.Exit(1)
		}
		defer func() {
			if err := tracer.Shutdown(context.Background()); err != nil {
				setupLog.Error(err, "failed to shutdown tracer")
			}
		}()
		setupLog.Info("OpenTelemetry tracing enabled", "endpoint", otlpEndpoint)
	}

	// Initialize SmartRouter if enabled
	var smartRouter *proxy.SmartRouter
	if enableSmartRouting {
		smartRouterConfig := proxy.SmartRouterConfig{
			LongContextThreshold:   smartRoutingLongContextThreshold,
			FastModelThreshold:     smartRoutingFastModelThreshold,
			LongContextBackend:     smartRoutingLongContextBackend,
			FastModelBackend:       smartRoutingFastModelBackend,
			EnableCostOptimization: false,
		}
		smartRouter = proxy.NewSmartRouter(smartRouterConfig, ctrl.Log)
		setupLog.Info("Smart routing enabled",
			"long-context-threshold", smartRoutingLongContextThreshold,
			"fast-model-threshold", smartRoutingFastModelThreshold,
			"long-context-backend", smartRoutingLongContextBackend,
			"fast-model-backend", smartRoutingFastModelBackend,
		)
	}

	setupLog.Info("initialized proxy components",
		"metrics", metricsRecorder != nil,
		"rate-limiter", rateLimiter != nil,
		"experiments", experimentManager != nil,
		"cost-tracker", costTracker != nil,
		"tracer", tracer != nil,
		"smart-router", smartRouter != nil,
	)

	// Setup inference proxy server with all features
	proxyConfig := proxy.DefaultConfig()
	proxyConfig.Addr = proxyAddr
	proxyServer := proxy.NewServer(
		proxyConfig,
		routeCache,
		mgr.GetClient(),
		ctrl.Log.WithName("proxy"),
		proxy.WithMetrics(metricsRecorder),
		proxy.WithRateLimiter(rateLimiter),
		proxy.WithExperiments(experimentManager),
		proxy.WithCostTracker(costTracker),
		proxy.WithTracer(tracer),
		proxy.WithServerSmartRouter(smartRouter),
	)

	// Add proxy server to manager as a runnable
	if err := mgr.Add(proxyServer); err != nil {
		setupLog.Error(err, "unable to add proxy server to manager")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager",
		"proxy-addr", proxyAddr,
		"health-probe-addr", probeAddr,
	)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
