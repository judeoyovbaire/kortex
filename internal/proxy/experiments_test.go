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
	"net/http/httptest"
	"testing"

	gatewayv1alpha1 "github.com/judeoyovbaire/kortex/api/v1alpha1"
)

func TestExperimentManager_GetBackend_NilExperiment(t *testing.T) {
	em := NewExperimentManager(nil)
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	result := em.GetBackend(nil, req)

	if result.Backend != "" || result.Variant != "" || result.Experiment != "" {
		t.Error("expected empty result for nil experiment")
	}
}

func TestExperimentManager_GetBackend_ConsistentAssignment(t *testing.T) {
	em := NewExperimentManager(nil)
	experiment := &gatewayv1alpha1.ABExperiment{
		Name:           "test-experiment",
		Control:        "control-backend",
		Treatment:      "treatment-backend",
		TrafficPercent: 50,
	}

	// Create request with user ID
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("X-User-ID", "consistent-user-123")

	// Get assignment multiple times
	var firstBackend, firstVariant string
	for i := 0; i < 10; i++ {
		result := em.GetBackend(experiment, req)
		if i == 0 {
			firstBackend = result.Backend
			firstVariant = result.Variant
		} else {
			if result.Backend != firstBackend {
				t.Errorf("inconsistent backend assignment: got %s, expected %s", result.Backend, firstBackend)
			}
			if result.Variant != firstVariant {
				t.Errorf("inconsistent variant assignment: got %s, expected %s", result.Variant, firstVariant)
			}
		}
	}
}

func TestExperimentManager_GetBackend_TrafficSplit(t *testing.T) {
	em := NewExperimentManager(nil)
	experiment := &gatewayv1alpha1.ABExperiment{
		Name:           "traffic-test",
		Control:        "control-backend",
		Treatment:      "treatment-backend",
		TrafficPercent: 50,
	}

	// Run many assignments to verify approximate split
	controlCount := 0
	treatmentCount := 0
	totalUsers := 1000

	for i := 0; i < totalUsers; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req.Header.Set("X-User-ID", string(rune('a'+i%26))+string(rune('0'+i%10)))

		result := em.GetBackend(experiment, req)
		if result.Variant == VariantControl {
			controlCount++
		} else if result.Variant == VariantTreatment {
			treatmentCount++
		}
	}

	// Verify rough split (allow 15% variance for randomness)
	expectedPercent := 50.0
	treatmentPercent := float64(treatmentCount) / float64(totalUsers) * 100

	if treatmentPercent < expectedPercent-15 || treatmentPercent > expectedPercent+15 {
		t.Errorf("traffic split too far from expected: got %.1f%% treatment, expected ~%.1f%%",
			treatmentPercent, expectedPercent)
	}
}

func TestExperimentManager_GetBackend_DefaultTrafficPercent(t *testing.T) {
	em := NewExperimentManager(nil)
	experiment := &gatewayv1alpha1.ABExperiment{
		Name:           "default-traffic-test",
		Control:        "control-backend",
		Treatment:      "treatment-backend",
		TrafficPercent: 0, // Should default to 10%
	}

	treatmentCount := 0
	totalUsers := 500

	for i := 0; i < totalUsers; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req.Header.Set("X-User-ID", string(rune('x'+i%3))+string(rune('0'+i%10)))

		result := em.GetBackend(experiment, req)
		if result.Variant == VariantTreatment {
			treatmentCount++
		}
	}

	// Should be around 10% treatment (allow variance)
	treatmentPercent := float64(treatmentCount) / float64(totalUsers) * 100

	if treatmentPercent > 25 {
		t.Errorf("default traffic percent should be ~10%%, got %.1f%%", treatmentPercent)
	}
}

func TestExperimentManager_ShouldApplyExperiment(t *testing.T) {
	em := NewExperimentManager(nil)
	experiment := &gatewayv1alpha1.ABExperiment{
		Name:      "test-exp",
		Control:   "backend-a",
		Treatment: "backend-b",
	}

	tests := []struct {
		name            string
		selectedBackend string
		expected        bool
	}{
		{"control backend", "backend-a", true},
		{"treatment backend", "backend-b", true},
		{"unrelated backend", "backend-c", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := em.ShouldApplyExperiment(experiment, tt.selectedBackend)
			if result != tt.expected {
				t.Errorf("ShouldApplyExperiment(%s) = %v, expected %v",
					tt.selectedBackend, result, tt.expected)
			}
		})
	}
}

func TestExperimentManager_ShouldApplyExperiment_NilExperiment(t *testing.T) {
	em := NewExperimentManager(nil)

	result := em.ShouldApplyExperiment(nil, "any-backend")
	if result {
		t.Error("nil experiment should not apply")
	}
}

func TestExperimentManager_FindApplicableExperiment(t *testing.T) {
	em := NewExperimentManager(nil)
	experiments := []gatewayv1alpha1.ABExperiment{
		{Name: "exp1", Control: "backend-a", Treatment: "backend-b"},
		{Name: "exp2", Control: "backend-c", Treatment: "backend-d"},
	}

	tests := []struct {
		name            string
		selectedBackend string
		expectedExp     string
	}{
		{"matches first exp control", "backend-a", "exp1"},
		{"matches first exp treatment", "backend-b", "exp1"},
		{"matches second exp control", "backend-c", "exp2"},
		{"matches second exp treatment", "backend-d", "exp2"},
		{"no match", "backend-x", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := em.FindApplicableExperiment(experiments, tt.selectedBackend)
			if tt.expectedExp == "" {
				if result != nil {
					t.Errorf("expected nil, got experiment %s", result.Name)
				}
			} else {
				if result == nil {
					t.Error("expected non-nil experiment")
				} else if result.Name != tt.expectedExp {
					t.Errorf("expected experiment %s, got %s", tt.expectedExp, result.Name)
				}
			}
		})
	}
}

func TestExperimentManager_ApplyExperiment(t *testing.T) {
	em := NewExperimentManager(nil)
	experiments := []gatewayv1alpha1.ABExperiment{
		{
			Name:           "model-test",
			Control:        "gpt4-backend",
			Treatment:      "claude-backend",
			TrafficPercent: 50,
		},
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("X-User-ID", "test-user")

	// Apply with matching backend
	backend, result := em.ApplyExperiment(experiments, "gpt4-backend", req)

	if result == nil {
		t.Error("expected experiment result for matching backend")
	}
	if backend != result.Backend {
		t.Errorf("returned backend %s doesn't match result backend %s", backend, result.Backend)
	}
	if result.Experiment != "model-test" {
		t.Errorf("expected experiment name 'model-test', got %s", result.Experiment)
	}
}

func TestExperimentManager_ApplyExperiment_NoMatch(t *testing.T) {
	em := NewExperimentManager(nil)
	experiments := []gatewayv1alpha1.ABExperiment{
		{Name: "exp1", Control: "backend-a", Treatment: "backend-b"},
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("X-User-ID", "test-user")

	// Apply with non-matching backend
	backend, result := em.ApplyExperiment(experiments, "unrelated-backend", req)

	if result != nil {
		t.Error("expected nil result for non-matching backend")
	}
	if backend != "unrelated-backend" {
		t.Errorf("expected original backend 'unrelated-backend', got %s", backend)
	}
}

func TestExperimentManager_SetExperimentHeaders(t *testing.T) {
	em := NewExperimentManager(nil)
	recorder := httptest.NewRecorder()

	result := &ExperimentResult{
		Backend:    "treatment-backend",
		Variant:    VariantTreatment,
		Experiment: "test-experiment",
	}

	em.SetExperimentHeaders(recorder, result)

	if recorder.Header().Get("X-Experiment") != "test-experiment" {
		t.Errorf("expected X-Experiment header 'test-experiment', got %s",
			recorder.Header().Get("X-Experiment"))
	}
	if recorder.Header().Get("X-Variant") != VariantTreatment {
		t.Errorf("expected X-Variant header '%s', got %s",
			VariantTreatment, recorder.Header().Get("X-Variant"))
	}
}

func TestExperimentManager_SetExperimentHeaders_NilResult(t *testing.T) {
	em := NewExperimentManager(nil)
	recorder := httptest.NewRecorder()

	// Should not panic
	em.SetExperimentHeaders(recorder, nil)

	if recorder.Header().Get("X-Experiment") != "" {
		t.Error("expected no X-Experiment header for nil result")
	}
}

func TestExperimentManager_GetUserID_Fallbacks(t *testing.T) {
	em := NewExperimentManager(nil)

	tests := []struct {
		name          string
		userIDHeader  string
		authHeader    string
		remoteAddr    string
		expectedMatch string
	}{
		{"uses X-User-ID", "user123", "", "1.2.3.4:1234", "user123"},
		{"falls back to Authorization", "", "Bearer token123", "1.2.3.4:1234", "Bearer token123"},
		{"falls back to RemoteAddr", "", "", "1.2.3.4:1234", "1.2.3.4:1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			if tt.userIDHeader != "" {
				req.Header.Set("X-User-ID", tt.userIDHeader)
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			req.RemoteAddr = tt.remoteAddr

			userID := em.getUserID(req)
			if userID != tt.expectedMatch {
				t.Errorf("expected userID %s, got %s", tt.expectedMatch, userID)
			}
		})
	}
}
