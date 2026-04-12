package ai

import (
	"testing"

	"github.com/photonest/photonest/internal/library"
)

func TestSelectProviderPrefersHealthyLocalCandidate(t *testing.T) {
	decision, err := SelectProvider(RouteRequest{
		Capability: CapabilityOCR,
		Policy:     library.DefaultPolicy(),
		Candidates: []Candidate{
			{
				Name:         "remote-a",
				Boundary:     BoundaryRemoteService,
				Capabilities: []Capability{CapabilityOCR},
				Healthy:      true,
			},
			{
				Name:         "local-b",
				Boundary:     BoundaryLocalSidecar,
				Capabilities: []Capability{CapabilityOCR},
				Healthy:      true,
			},
		},
	})
	if err != nil {
		t.Fatalf("select provider: %v", err)
	}
	if decision.ProviderName != "local-b" {
		t.Fatalf("expected local provider to win lexical tie, got %+v", decision)
	}
}

func TestSelectProviderRespectsPrivacyPolicy(t *testing.T) {
	policy := library.DefaultPolicy()
	policy.AllowRemoteOCR = false

	_, err := SelectProvider(RouteRequest{
		Capability: CapabilityOCR,
		Policy:     policy,
		Candidates: []Candidate{
			{
				Name:         "remote-a",
				Boundary:     BoundaryRemoteService,
				Capabilities: []Capability{CapabilityOCR},
				Healthy:      true,
			},
		},
	})
	if err == nil {
		t.Fatal("expected policy blocked error")
	}

	classified, ok := err.(ClassifiedError)
	if !ok {
		t.Fatalf("expected classified error, got %T", err)
	}
	if classified.Kind != ErrorKindPolicyBlocked {
		t.Fatalf("expected policy_blocked, got %+v", classified)
	}
}
