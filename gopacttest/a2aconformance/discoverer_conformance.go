package a2aconformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact/a2a"
)

// ErrDiscovererConformanceFailed marks a failed A2A discoverer conformance case.
var ErrDiscovererConformanceFailed = errors.New("gopacttest: a2a discoverer conformance failed")

// DiscovererConformanceHarness describes one A2A discoverer implementation under test.
type DiscovererConformanceHarness struct {
	Discoverer       a2a.Discoverer
	Query            a2a.DiscoveryQuery
	ExpectedCard     a2a.AgentCard
	RequireListCards bool
}

// DiscovererConformanceResult is the observed result for one discoverer contract case.
type DiscovererConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckDiscovererConformance runs reusable A2A discovery contract cases for adapters.
func CheckDiscovererConformance(ctx context.Context, harness DiscovererConformanceHarness) []DiscovererConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	results := []DiscovererConformanceResult{
		checkDiscovererPresent(harness.Discoverer),
		checkDiscovererCanceledContext(harness.Discoverer, copyDiscoveryQuery(harness.Query)),
		checkDiscovererReturnsExpectedCard(ctx, harness.Discoverer, copyDiscoveryQuery(harness.Query), harness.ExpectedCard),
		checkDiscovererDoesNotMutateQuery(ctx, harness.Discoverer, copyDiscoveryQuery(harness.Query)),
		checkDiscovererReturnsDefensiveCopy(ctx, harness.Discoverer, copyDiscoveryQuery(harness.Query)),
	}
	if harness.RequireListCards {
		results = append(results,
			checkDiscovererImplementsCardLister(harness.Discoverer),
			checkCardListerCanceledContext(harness.Discoverer),
			checkCardListerIncludesExpectedCard(ctx, harness.Discoverer, harness.ExpectedCard),
			checkCardListerReturnsDefensiveCopy(ctx, harness.Discoverer, harness.ExpectedCard),
		)
	}
	return results
}

// RequireDiscovererConformance fails the test unless discoverer satisfies the A2A discovery contract.
func RequireDiscovererConformance(t testing.TB, harness DiscovererConformanceHarness) {
	t.Helper()

	for _, result := range CheckDiscovererConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("a2a discoverer conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkDiscovererPresent(discoverer a2a.Discoverer) DiscovererConformanceResult {
	if discoverer == nil {
		return failedDiscovererConformance("has-discoverer", errors.New("discoverer is nil"))
	}
	return passedDiscovererConformance("has-discoverer")
}

func checkDiscovererCanceledContext(discoverer a2a.Discoverer, query a2a.DiscoveryQuery) DiscovererConformanceResult {
	if discoverer == nil {
		return failedDiscovererConformance("discover-respects-canceled-context", errors.New("discoverer is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := discoverer.Discover(ctx, query)
	if !errors.Is(err, context.Canceled) {
		return failedDiscovererConformance("discover-respects-canceled-context", fmt.Errorf("discover canceled context error = %v, want context canceled", err))
	}
	return passedDiscovererConformance("discover-respects-canceled-context")
}

func checkDiscovererReturnsExpectedCard(ctx context.Context, discoverer a2a.Discoverer, query a2a.DiscoveryQuery, expected a2a.AgentCard) DiscovererConformanceResult {
	if discoverer == nil {
		return failedDiscovererConformance("discover-returns-expected-card", errors.New("discoverer is nil"))
	}
	result, err := discoverer.Discover(ctx, query)
	if err != nil {
		return failedDiscovererConformance("discover-returns-expected-card", err)
	}
	if err := checkExpectedCard(result.Card, expected); err != nil {
		return failedDiscovererConformance("discover-returns-expected-card", err)
	}
	return passedDiscovererConformance("discover-returns-expected-card")
}

func checkDiscovererDoesNotMutateQuery(ctx context.Context, discoverer a2a.Discoverer, query a2a.DiscoveryQuery) DiscovererConformanceResult {
	if discoverer == nil {
		return failedDiscovererConformance("discover-does-not-mutate-query", errors.New("discoverer is nil"))
	}
	before := copyDiscoveryQuery(query)
	if _, err := discoverer.Discover(ctx, query); err != nil {
		return failedDiscovererConformance("discover-does-not-mutate-query", err)
	}
	if !reflect.DeepEqual(query, before) {
		return failedDiscovererConformance("discover-does-not-mutate-query", errors.New("discoverer mutated input query"))
	}
	return passedDiscovererConformance("discover-does-not-mutate-query")
}

func checkDiscovererReturnsDefensiveCopy(ctx context.Context, discoverer a2a.Discoverer, query a2a.DiscoveryQuery) DiscovererConformanceResult {
	if discoverer == nil {
		return failedDiscovererConformance("discover-returns-defensive-copy", errors.New("discoverer is nil"))
	}
	first, err := discoverer.Discover(ctx, query)
	if err != nil {
		return failedDiscovererConformance("discover-returns-defensive-copy", err)
	}
	mutateCard(&first.Card)
	second, err := discoverer.Discover(ctx, copyDiscoveryQuery(query))
	if err != nil {
		return failedDiscovererConformance("discover-returns-defensive-copy", err)
	}
	if cardHasMutation(second.Card) {
		return failedDiscovererConformance("discover-returns-defensive-copy", errors.New("discoverer returned shared card data"))
	}
	return passedDiscovererConformance("discover-returns-defensive-copy")
}

func checkDiscovererImplementsCardLister(discoverer a2a.Discoverer) DiscovererConformanceResult {
	if discoverer == nil {
		return failedDiscovererConformance("implements-card-lister", errors.New("discoverer is nil"))
	}
	if _, ok := discoverer.(a2a.CardLister); !ok {
		return failedDiscovererConformance("implements-card-lister", errors.New("discoverer does not implement CardLister"))
	}
	return passedDiscovererConformance("implements-card-lister")
}

func checkCardListerCanceledContext(discoverer a2a.Discoverer) DiscovererConformanceResult {
	lister, ok := discoverer.(a2a.CardLister)
	if !ok {
		return failedDiscovererConformance("list-respects-canceled-context", errors.New("discoverer does not implement CardLister"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := lister.ListCards(ctx)
	if !errors.Is(err, context.Canceled) {
		return failedDiscovererConformance("list-respects-canceled-context", fmt.Errorf("list canceled context error = %v, want context canceled", err))
	}
	return passedDiscovererConformance("list-respects-canceled-context")
}

func checkCardListerIncludesExpectedCard(ctx context.Context, discoverer a2a.Discoverer, expected a2a.AgentCard) DiscovererConformanceResult {
	lister, ok := discoverer.(a2a.CardLister)
	if !ok {
		return failedDiscovererConformance("list-includes-expected-card", errors.New("discoverer does not implement CardLister"))
	}
	cards, err := lister.ListCards(ctx)
	if err != nil {
		return failedDiscovererConformance("list-includes-expected-card", err)
	}
	for _, card := range cards {
		if checkExpectedCard(card, expected) == nil {
			return passedDiscovererConformance("list-includes-expected-card")
		}
	}
	return failedDiscovererConformance("list-includes-expected-card", errors.New("list does not include expected card"))
}

func checkCardListerReturnsDefensiveCopy(ctx context.Context, discoverer a2a.Discoverer, expected a2a.AgentCard) DiscovererConformanceResult {
	lister, ok := discoverer.(a2a.CardLister)
	if !ok {
		return failedDiscovererConformance("list-returns-defensive-copy", errors.New("discoverer does not implement CardLister"))
	}
	first, err := lister.ListCards(ctx)
	if err != nil {
		return failedDiscovererConformance("list-returns-defensive-copy", err)
	}
	for i := range first {
		if checkExpectedCard(first[i], expected) == nil {
			mutateCard(&first[i])
			break
		}
	}
	second, err := lister.ListCards(ctx)
	if err != nil {
		return failedDiscovererConformance("list-returns-defensive-copy", err)
	}
	for _, card := range second {
		if card.Name == expected.Name && cardHasMutation(card) {
			return failedDiscovererConformance("list-returns-defensive-copy", errors.New("list returned shared card data"))
		}
	}
	return passedDiscovererConformance("list-returns-defensive-copy")
}

func checkExpectedCard(card a2a.AgentCard, expected a2a.AgentCard) error {
	if expected.Name == "" {
		return errors.New("expected card name is empty")
	}
	if card.Name != expected.Name {
		return fmt.Errorf("card name = %q, want %q", card.Name, expected.Name)
	}
	if expected.URL != "" && card.URL != expected.URL {
		return fmt.Errorf("card url = %q, want %q", card.URL, expected.URL)
	}
	if !containsAllStrings(card.Capabilities, expected.Capabilities) {
		return fmt.Errorf("card capabilities = %v, want at least %v", card.Capabilities, expected.Capabilities)
	}
	for key, expectedValue := range expected.Metadata {
		if !reflect.DeepEqual(card.Metadata[key], expectedValue) {
			return fmt.Errorf("card metadata %q = %v, want %v", key, card.Metadata[key], expectedValue)
		}
	}
	return nil
}

func containsAllStrings(values []string, required []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range required {
		if !seen[value] {
			return false
		}
	}
	return true
}

func mutateCard(card *a2a.AgentCard) {
	if card.Metadata == nil {
		card.Metadata = make(map[string]any)
	}
	card.Metadata["gopact_conformance_mutated"] = true
	if len(card.Capabilities) > 0 {
		card.Capabilities[0] = "gopact-conformance-mutated"
	}
}

func cardHasMutation(card a2a.AgentCard) bool {
	if mutated, _ := card.Metadata["gopact_conformance_mutated"].(bool); mutated {
		return true
	}
	for _, capability := range card.Capabilities {
		if capability == "gopact-conformance-mutated" {
			return true
		}
	}
	return false
}

func passedDiscovererConformance(name string) DiscovererConformanceResult {
	return DiscovererConformanceResult{Case: name, Passed: true}
}

func failedDiscovererConformance(name string, err error) DiscovererConformanceResult {
	return DiscovererConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrDiscovererConformanceFailed, err),
	}
}

func copyDiscoveryQuery(query a2a.DiscoveryQuery) a2a.DiscoveryQuery {
	out := query
	out.Require = append([]string(nil), query.Require...)
	out.Metadata = copyAnyMap(query.Metadata)
	return out
}
