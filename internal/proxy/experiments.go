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

package proxy

import (
	"hash/fnv"
	"net/http"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
)

const (
	// VariantControl is the control variant name
	VariantControl = "control"
	// VariantTreatment is the treatment variant name
	VariantTreatment = "treatment"

	// DefaultUserIDHeader is the default header for user identification
	DefaultUserIDHeader = "X-User-ID"
)

// ExperimentResult contains the result of experiment assignment
type ExperimentResult struct {
	Backend    string
	Variant    string
	Experiment string
}

// ExperimentManager handles A/B testing experiment assignment
type ExperimentManager struct {
	metrics *MetricsRecorder
}

// NewExperimentManager creates a new experiment manager
func NewExperimentManager(metrics *MetricsRecorder) *ExperimentManager {
	return &ExperimentManager{
		metrics: metrics,
	}
}

// GetBackend determines which backend to use based on experiment configuration.
// It uses consistent hashing to ensure the same user always gets the same variant.
func (e *ExperimentManager) GetBackend(
	experiment *gatewayv1alpha1.ABExperiment,
	req *http.Request,
) ExperimentResult {
	if experiment == nil {
		return ExperimentResult{}
	}

	// Get user identifier for consistent hashing
	userID := e.getUserID(req)

	// Calculate bucket using consistent hash
	bucket := e.calculateBucket(userID, experiment.Name)

	// Determine variant based on traffic percentage
	trafficPercent := experiment.TrafficPercent
	if trafficPercent <= 0 {
		trafficPercent = 10 // default 10% to treatment
	}

	var backend, variant string
	if bucket < int(trafficPercent) {
		backend = experiment.Treatment
		variant = VariantTreatment
	} else {
		backend = experiment.Control
		variant = VariantControl
	}

	// Record the assignment
	if e.metrics != nil {
		e.metrics.RecordExperimentAssignment(experiment.Name, variant)
	}

	return ExperimentResult{
		Backend:    backend,
		Variant:    variant,
		Experiment: experiment.Name,
	}
}

// ShouldApplyExperiment checks if an experiment applies to the selected backend
func (e *ExperimentManager) ShouldApplyExperiment(
	experiment *gatewayv1alpha1.ABExperiment,
	selectedBackend string,
) bool {
	if experiment == nil {
		return false
	}
	return selectedBackend == experiment.Control || selectedBackend == experiment.Treatment
}

// getUserID extracts the user identifier from the request
func (e *ExperimentManager) getUserID(req *http.Request) string {
	// Try X-User-ID header first
	userID := req.Header.Get(DefaultUserIDHeader)
	if userID != "" {
		return userID
	}

	// Try Authorization header (for API keys)
	auth := req.Header.Get("Authorization")
	if auth != "" {
		return auth
	}

	// Fall back to client IP for anonymous users
	// This provides some consistency for users without explicit ID
	return req.RemoteAddr
}

// calculateBucket computes a consistent hash bucket (0-99) for a user and experiment
func (e *ExperimentManager) calculateBucket(userID, experimentName string) int {
	h := fnv.New32a()
	// Combine user ID and experiment name for the hash
	// This ensures different experiments can have different assignments for the same user
	_, _ = h.Write([]byte(userID + ":" + experimentName))
	return int(h.Sum32() % 100)
}

// FindApplicableExperiment finds the first applicable experiment for the selected backend
func (e *ExperimentManager) FindApplicableExperiment(
	experiments []gatewayv1alpha1.ABExperiment,
	selectedBackend string,
) *gatewayv1alpha1.ABExperiment {
	for i := range experiments {
		exp := &experiments[i]
		if e.ShouldApplyExperiment(exp, selectedBackend) {
			return exp
		}
	}
	return nil
}

// ApplyExperiment applies experiment routing if applicable and returns the result
func (e *ExperimentManager) ApplyExperiment(
	experiments []gatewayv1alpha1.ABExperiment,
	selectedBackend string,
	req *http.Request,
) (string, *ExperimentResult) {
	// Find applicable experiment
	exp := e.FindApplicableExperiment(experiments, selectedBackend)
	if exp == nil {
		return selectedBackend, nil
	}

	// Get experiment assignment
	result := e.GetBackend(exp, req)
	return result.Backend, &result
}

// SetExperimentHeaders adds experiment tracking headers to the response
func (e *ExperimentManager) SetExperimentHeaders(w http.ResponseWriter, result *ExperimentResult) {
	if result == nil {
		return
	}
	w.Header().Set("X-Experiment", result.Experiment)
	w.Header().Set("X-Variant", result.Variant)
}
