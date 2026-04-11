package ai

import (
	"fmt"
	"sort"

	"github.com/photonest/photonest/internal/library"
)

type Candidate struct {
	Name         string
	Boundary     Boundary
	Capabilities []Capability
	Healthy      bool
	FailureCount int
}

type RouteRequest struct {
	Capability Capability
	Policy     library.Policy
	RetryCount int
	Candidates []Candidate
}

type RouteDecision struct {
	ProviderName string
	Boundary     Boundary
	Degraded     bool
	Reason       string
}

func SelectProvider(request RouteRequest) (RouteDecision, error) {
	eligible := make([]Candidate, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		if !supportsCapability(candidate.Capabilities, request.Capability) {
			continue
		}
		if candidate.Boundary == BoundaryRemoteService && !request.Policy.AllowsCapability(string(request.Capability)) {
			continue
		}
		eligible = append(eligible, candidate)
	}

	if len(eligible) == 0 {
		return RouteDecision{}, ClassifiedError{
			Kind:      ErrorKindPolicyBlocked,
			Boundary:  BoundaryRemoteService,
			Message:   fmt.Sprintf("no provider can satisfy capability=%s under current privacy policy", request.Capability),
			Retryable: false,
			Temporary: false,
		}
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].Healthy != eligible[j].Healthy {
			return eligible[i].Healthy
		}
		if request.RetryCount > 0 && eligible[i].FailureCount != eligible[j].FailureCount {
			return eligible[i].FailureCount < eligible[j].FailureCount
		}
		return eligible[i].Name < eligible[j].Name
	})

	for _, candidate := range eligible {
		if !candidate.Healthy {
			continue
		}
		return RouteDecision{
			ProviderName: candidate.Name,
			Boundary:     candidate.Boundary,
			Degraded:     request.RetryCount > 0 || candidate.FailureCount > 0,
			Reason:       routeReason(request, candidate),
		}, nil
	}

	return RouteDecision{}, ClassifiedError{
		Kind:      ErrorKindProviderUnavailable,
		Boundary:  eligible[0].Boundary,
		Message:   fmt.Sprintf("all providers for capability=%s are currently unhealthy", request.Capability),
		Retryable: true,
		Temporary: true,
	}
}

func routeReason(request RouteRequest, candidate Candidate) string {
	switch {
	case request.RetryCount > 0:
		return "retry routing selected the currently healthiest candidate"
	case candidate.Boundary == BoundaryLocalSidecar:
		return "privacy policy prefers local sidecar execution"
	default:
		return "selected first healthy provider that satisfies capability and policy"
	}
}

func supportsCapability(capabilities []Capability, target Capability) bool {
	for _, capability := range capabilities {
		if capability == target {
			return true
		}
	}
	return false
}
