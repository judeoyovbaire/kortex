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

// Package config provides configuration management with hot-reload support
package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
)

// ConfigChangeHandler is called when configuration changes are detected
type ConfigChangeHandler func(newConfig *KortexConfig)

// KortexConfig represents the main configuration file structure
type KortexConfig struct {
	// Version is the config file version
	Version string `yaml:"version"`

	// Gateway contains proxy server settings
	Gateway GatewayConfig `yaml:"gateway"`

	// SmartRouting contains intelligent routing configuration
	SmartRouting SmartRoutingConfig `yaml:"smartRouting"`

	// Providers contains provider-specific configurations
	Providers map[string]ProviderConfig `yaml:"providers"`

	// RateLimits contains global rate limiting settings
	RateLimits RateLimitConfig `yaml:"rateLimits"`

	// Observability contains tracing and metrics settings
	Observability ObservabilityConfig `yaml:"observability"`
}

// GatewayConfig contains core gateway settings
type GatewayConfig struct {
	// BindAddress is the address to listen on
	BindAddress string `yaml:"bindAddress"`

	// ReadTimeout in seconds
	ReadTimeout int `yaml:"readTimeout"`

	// WriteTimeout in seconds
	WriteTimeout int `yaml:"writeTimeout"`

	// MaxRequestBodySize in bytes
	MaxRequestBodySize int64 `yaml:"maxRequestBodySize"`
}

// SmartRoutingConfig contains intelligent routing settings
type SmartRoutingConfig struct {
	// Enabled enables smart routing
	Enabled bool `yaml:"enabled"`

	// LongContextThreshold is the token count for long-context routing
	LongContextThreshold int `yaml:"longContextThreshold"`

	// FastModelThreshold is the token count for fast-model routing
	FastModelThreshold int `yaml:"fastModelThreshold"`

	// LongContextBackend is the backend for long requests
	LongContextBackend string `yaml:"longContextBackend"`

	// FastModelBackend is the backend for short requests
	FastModelBackend string `yaml:"fastModelBackend"`

	// EnableCostOptimization enables cost-based routing
	EnableCostOptimization bool `yaml:"enableCostOptimization"`
}

// ProviderConfig contains provider-specific settings
type ProviderConfig struct {
	// Enabled determines if this provider is active
	Enabled bool `yaml:"enabled"`

	// BaseURL overrides the default API endpoint
	BaseURL string `yaml:"baseURL"`

	// Timeout in seconds
	Timeout int `yaml:"timeout"`

	// MaxRetries for failed requests
	MaxRetries int `yaml:"maxRetries"`
}

// RateLimitConfig contains rate limiting settings
type RateLimitConfig struct {
	// Enabled enables rate limiting
	Enabled bool `yaml:"enabled"`

	// DefaultRequestsPerMinute is the default rate limit
	DefaultRequestsPerMinute int `yaml:"defaultRequestsPerMinute"`

	// PerUserLimiting enables per-user rate limiting
	PerUserLimiting bool `yaml:"perUserLimiting"`

	// UserHeaderName is the header to identify users
	UserHeaderName string `yaml:"userHeaderName"`
}

// ObservabilityConfig contains tracing and metrics settings
type ObservabilityConfig struct {
	// Tracing configuration
	Tracing TracingConfig `yaml:"tracing"`

	// Metrics configuration
	Metrics MetricsConfig `yaml:"metrics"`
}

// TracingConfig contains OpenTelemetry tracing settings
type TracingConfig struct {
	// Enabled enables tracing
	Enabled bool `yaml:"enabled"`

	// Endpoint is the OTLP collector endpoint
	Endpoint string `yaml:"endpoint"`

	// SampleRate is the fraction of traces to sample (0.0-1.0)
	SampleRate float64 `yaml:"sampleRate"`

	// Insecure disables TLS
	Insecure bool `yaml:"insecure"`
}

// MetricsConfig contains Prometheus metrics settings
type MetricsConfig struct {
	// Enabled enables metrics collection
	Enabled bool `yaml:"enabled"`

	// BindAddress for the metrics server
	BindAddress string `yaml:"bindAddress"`
}

// DefaultConfig returns the default Kortex configuration
func DefaultConfig() *KortexConfig {
	return &KortexConfig{
		Version: "v1",
		Gateway: GatewayConfig{
			BindAddress:        ":8080",
			ReadTimeout:        30,
			WriteTimeout:       120,
			MaxRequestBodySize: 10 * 1024 * 1024, // 10MB
		},
		SmartRouting: SmartRoutingConfig{
			Enabled:              false,
			LongContextThreshold: 4000,
			FastModelThreshold:   500,
		},
		RateLimits: RateLimitConfig{
			Enabled:                  true,
			DefaultRequestsPerMinute: 60,
			PerUserLimiting:          true,
			UserHeaderName:           "x-user-id",
		},
		Observability: ObservabilityConfig{
			Tracing: TracingConfig{
				Enabled:    false,
				Endpoint:   "localhost:4317",
				SampleRate: 1.0,
				Insecure:   true,
			},
			Metrics: MetricsConfig{
				Enabled:     true,
				BindAddress: ":8443",
			},
		},
	}
}

// Watcher monitors a configuration file for changes and reloads it
type Watcher struct {
	configPath string
	watcher    *fsnotify.Watcher
	log        logr.Logger
	handlers   []ConfigChangeHandler
	config     *KortexConfig
	configMu   sync.RWMutex
	debounce   time.Duration
}

// NewWatcher creates a new configuration file watcher
func NewWatcher(configPath string, log logr.Logger) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		configPath: configPath,
		watcher:    fsWatcher,
		log:        log.WithName("config-watcher"),
		handlers:   make([]ConfigChangeHandler, 0),
		debounce:   500 * time.Millisecond, // Debounce rapid changes
	}

	// Load initial configuration
	if err := w.loadConfig(); err != nil {
		fsWatcher.Close()
		return nil, err
	}

	return w, nil
}

// OnChange registers a handler to be called when config changes
func (w *Watcher) OnChange(handler ConfigChangeHandler) {
	w.handlers = append(w.handlers, handler)
}

// GetConfig returns the current configuration
func (w *Watcher) GetConfig() *KortexConfig {
	w.configMu.RLock()
	defer w.configMu.RUnlock()
	return w.config
}

// Start begins watching the configuration file
func (w *Watcher) Start(ctx context.Context) error {
	// Watch the directory containing the config file
	configDir := filepath.Dir(w.configPath)
	if err := w.watcher.Add(configDir); err != nil {
		return err
	}

	w.log.Info("Started watching configuration file",
		"path", w.configPath,
		"directory", configDir,
	)

	// Debounce timer for rapid changes
	var debounceTimer *time.Timer
	var debounceMu sync.Mutex

	go func() {
		for {
			select {
			case <-ctx.Done():
				w.log.Info("Stopping config watcher")
				return

			case event, ok := <-w.watcher.Events:
				if !ok {
					return
				}

				// Check if this event is for our config file
				if filepath.Base(event.Name) != filepath.Base(w.configPath) {
					continue
				}

				// Handle write and create events
				if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
					w.log.V(1).Info("Config file change detected", "event", event.Op.String())

					// Debounce rapid changes
					debounceMu.Lock()
					if debounceTimer != nil {
						debounceTimer.Stop()
					}
					debounceTimer = time.AfterFunc(w.debounce, func() {
						w.reloadConfig()
					})
					debounceMu.Unlock()
				}

			case err, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
				w.log.Error(err, "Config watcher error")
			}
		}
	}()

	return nil
}

// Stop stops the configuration watcher
func (w *Watcher) Stop() error {
	return w.watcher.Close()
}

// loadConfig loads the configuration from disk
func (w *Watcher) loadConfig() error {
	data, err := os.ReadFile(w.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Use default config if file doesn't exist
			w.configMu.Lock()
			w.config = DefaultConfig()
			w.configMu.Unlock()
			w.log.Info("Config file not found, using defaults", "path", w.configPath)
			return nil
		}
		return err
	}

	var config KortexConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return err
	}

	w.configMu.Lock()
	w.config = &config
	w.configMu.Unlock()

	w.log.Info("Configuration loaded", "path", w.configPath, "version", config.Version)
	return nil
}

// reloadConfig reloads the configuration and notifies handlers
func (w *Watcher) reloadConfig() {
	if err := w.loadConfig(); err != nil {
		w.log.Error(err, "Failed to reload configuration")
		return
	}

	// Notify all handlers
	config := w.GetConfig()
	for _, handler := range w.handlers {
		go handler(config)
	}

	w.log.Info("Configuration reloaded successfully")
}

// ValidateConfig performs validation on a configuration
func ValidateConfig(config *KortexConfig) []string {
	var errors []string

	if config.Gateway.BindAddress == "" {
		errors = append(errors, "gateway.bindAddress is required")
	}

	if config.SmartRouting.Enabled {
		if config.SmartRouting.LongContextThreshold <= 0 {
			errors = append(errors, "smartRouting.longContextThreshold must be positive when enabled")
		}
		if config.SmartRouting.FastModelThreshold <= 0 {
			errors = append(errors, "smartRouting.fastModelThreshold must be positive when enabled")
		}
		if config.SmartRouting.LongContextThreshold <= config.SmartRouting.FastModelThreshold {
			errors = append(errors, "smartRouting.longContextThreshold must be greater than fastModelThreshold")
		}
	}

	if config.Observability.Tracing.Enabled && config.Observability.Tracing.Endpoint == "" {
		errors = append(errors, "observability.tracing.endpoint is required when tracing is enabled")
	}

	if config.Observability.Tracing.SampleRate < 0 || config.Observability.Tracing.SampleRate > 1 {
		errors = append(errors, "observability.tracing.sampleRate must be between 0.0 and 1.0")
	}

	return errors
}