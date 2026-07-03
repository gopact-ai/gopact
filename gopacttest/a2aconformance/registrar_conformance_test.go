package a2aconformance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact/a2a"
)

func TestRequireCardRegistrarConformanceAcceptsRegistry(t *testing.T) {
	RequireCardRegistrarConformance(t, CardRegistrarConformanceHarness{
		Registrar: a2a.NewRegistry(),
		Card:      registrarExpectedCard(),
		TTL:       time.Minute,
	})
}

func TestCheckCardRegistrarConformanceReportsFailures(t *testing.T) {
	results := CheckCardRegistrarConformance(context.Background(), CardRegistrarConformanceHarness{
		Registrar: &badCardRegistrar{},
		Card:      registrarExpectedCard(),
		TTL:       time.Minute,
	})

	failures := failedRegistrarCases(t, results)
	for _, want := range []string{
		"register-respects-canceled-context",
		"register-requires-positive-ttl",
		"register-returns-expected-card",
		"register-does-not-mutate-card",
		"heartbeat-requires-positive-ttl",
	} {
		if !failures[want] {
			t.Fatalf("failures = %v, want case %q to fail", failures, want)
		}
	}
}

func TestCheckCardRegistrarConformanceReportsSharedCardMutation(t *testing.T) {
	results := CheckCardRegistrarConformance(context.Background(), CardRegistrarConformanceHarness{
		Registrar: &sharedCardRegistrar{},
		Card:      registrarExpectedCard(),
		TTL:       time.Minute,
	})

	failures := failedRegistrarCases(t, results)
	if !failures["register-returns-defensive-copy"] {
		t.Fatalf("failures = %v, want register-returns-defensive-copy to fail", failures)
	}
}

func failedRegistrarCases(t *testing.T, results []CardRegistrarConformanceResult) map[string]bool {
	t.Helper()

	failures := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failures[result.Case] = true
			if !errors.Is(result.Err, ErrCardRegistrarConformanceFailed) {
				t.Fatalf("case %q error = %v, want ErrCardRegistrarConformanceFailed", result.Case, result.Err)
			}
		}
	}
	return failures
}

func registrarExpectedCard() a2a.AgentCard {
	return a2a.AgentCard{
		Name:         "reviewer",
		URL:          "http://127.0.0.1:8080",
		Capabilities: []string{"code.review"},
		Tags:         []string{"code"},
		Metadata:     map[string]any{"region": "local"},
	}
}

type badCardRegistrar struct{}

func (r *badCardRegistrar) RegisterCardWithLease(_ context.Context, card a2a.AgentCard, _ time.Duration) (a2a.AgentCard, error) {
	if card.Metadata != nil {
		card.Metadata["mutated"] = true
	}
	return a2a.AgentCard{Name: "wrong"}, nil
}

func (r *badCardRegistrar) HeartbeatCard(context.Context, string, time.Duration) (a2a.AgentCard, error) {
	return a2a.AgentCard{Name: "wrong"}, nil
}

type sharedCardRegistrar struct {
	mu   sync.Mutex
	card a2a.AgentCard
}

func (r *sharedCardRegistrar) RegisterCardWithLease(ctx context.Context, card a2a.AgentCard, ttl time.Duration) (a2a.AgentCard, error) {
	if err := ctx.Err(); err != nil {
		return a2a.AgentCard{}, err
	}
	if ttl <= 0 {
		return a2a.AgentCard{}, a2a.ErrLeaseTTLRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	card.ExpiresAt = time.Now().Add(ttl)
	r.card = card
	return r.card, nil
}

func (r *sharedCardRegistrar) HeartbeatCard(ctx context.Context, name string, ttl time.Duration) (a2a.AgentCard, error) {
	if err := ctx.Err(); err != nil {
		return a2a.AgentCard{}, err
	}
	if ttl <= 0 {
		return a2a.AgentCard{}, a2a.ErrLeaseTTLRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.card.Name != name {
		return a2a.AgentCard{}, a2a.ErrAgentNotFound
	}
	r.card.ExpiresAt = time.Now().Add(ttl)
	return r.card, nil
}
