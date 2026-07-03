package a2aconformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/a2a"
)

// ErrCardRegistrarConformanceFailed marks a failed A2A card registrar conformance case.
var ErrCardRegistrarConformanceFailed = errors.New("gopacttest: a2a card registrar conformance failed")

// CardRegistrarConformanceHarness describes one A2A card registrar implementation under test.
type CardRegistrarConformanceHarness struct {
	Registrar a2a.CardRegistrar
	Card      a2a.AgentCard
	TTL       time.Duration
}

// CardRegistrarConformanceResult is the observed result for one card registrar contract case.
type CardRegistrarConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckCardRegistrarConformance runs reusable A2A card registration contract cases for adapters.
func CheckCardRegistrarConformance(ctx context.Context, harness CardRegistrarConformanceHarness) []CardRegistrarConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	ttl := harness.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	card := copyRegistrarCard(harness.Card)

	return []CardRegistrarConformanceResult{
		checkCardRegistrarPresent(harness.Registrar),
		checkCardRegistrarRegisterCanceledContext(harness.Registrar, copyRegistrarCard(card), ttl),
		checkCardRegistrarRegisterRequiresTTL(harness.Registrar, copyRegistrarCard(card)),
		checkCardRegistrarRegisterReturnsExpectedCard(ctx, harness.Registrar, copyRegistrarCard(card), ttl),
		checkCardRegistrarRegisterDoesNotMutateCard(ctx, harness.Registrar, copyRegistrarCard(card), ttl),
		checkCardRegistrarRegisterReturnsDefensiveCopy(ctx, harness.Registrar, copyRegistrarCard(card), ttl),
		checkCardRegistrarHeartbeatCanceledContext(ctx, harness.Registrar, copyRegistrarCard(card), ttl),
		checkCardRegistrarHeartbeatRequiresTTL(ctx, harness.Registrar, copyRegistrarCard(card), ttl),
		checkCardRegistrarHeartbeatRenewsLease(ctx, harness.Registrar, copyRegistrarCard(card), ttl),
		checkCardRegistrarHeartbeatMissingCard(ctx, harness.Registrar, copyRegistrarCard(card), ttl),
	}
}

// RequireCardRegistrarConformance fails the test unless registrar satisfies the A2A card registration contract.
func RequireCardRegistrarConformance(t testing.TB, harness CardRegistrarConformanceHarness) {
	t.Helper()

	for _, result := range CheckCardRegistrarConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("a2a card registrar conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkCardRegistrarPresent(registrar a2a.CardRegistrar) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("has-card-registrar", errors.New("card registrar is nil"))
	}
	return passedCardRegistrarConformance("has-card-registrar")
}

func checkCardRegistrarRegisterCanceledContext(registrar a2a.CardRegistrar, card a2a.AgentCard, ttl time.Duration) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("register-respects-canceled-context", errors.New("card registrar is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := registrar.RegisterCardWithLease(ctx, card, ttl)
	if !errors.Is(err, context.Canceled) {
		return failedCardRegistrarConformance("register-respects-canceled-context", fmt.Errorf("register canceled context error = %v, want context canceled", err))
	}
	return passedCardRegistrarConformance("register-respects-canceled-context")
}

func checkCardRegistrarRegisterRequiresTTL(registrar a2a.CardRegistrar, card a2a.AgentCard) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("register-requires-positive-ttl", errors.New("card registrar is nil"))
	}
	_, err := registrar.RegisterCardWithLease(context.Background(), card, 0)
	if !errors.Is(err, a2a.ErrLeaseTTLRequired) {
		return failedCardRegistrarConformance("register-requires-positive-ttl", fmt.Errorf("register zero ttl error = %v, want %v", err, a2a.ErrLeaseTTLRequired))
	}
	return passedCardRegistrarConformance("register-requires-positive-ttl")
}

func checkCardRegistrarRegisterReturnsExpectedCard(ctx context.Context, registrar a2a.CardRegistrar, card a2a.AgentCard, ttl time.Duration) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("register-returns-expected-card", errors.New("card registrar is nil"))
	}
	before := time.Now()
	registered, err := registrar.RegisterCardWithLease(ctx, card, ttl)
	if err != nil {
		return failedCardRegistrarConformance("register-returns-expected-card", err)
	}
	if err := checkExpectedCard(registered, card); err != nil {
		return failedCardRegistrarConformance("register-returns-expected-card", err)
	}
	if registered.ExpiresAt.IsZero() {
		return failedCardRegistrarConformance("register-returns-expected-card", errors.New("registered card has no lease expiry"))
	}
	if !registered.ExpiresAt.After(before) {
		return failedCardRegistrarConformance("register-returns-expected-card", fmt.Errorf("registered card expiry = %v, want after %v", registered.ExpiresAt, before))
	}
	return passedCardRegistrarConformance("register-returns-expected-card")
}

func checkCardRegistrarRegisterDoesNotMutateCard(ctx context.Context, registrar a2a.CardRegistrar, card a2a.AgentCard, ttl time.Duration) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("register-does-not-mutate-card", errors.New("card registrar is nil"))
	}
	before := copyRegistrarCard(card)
	if _, err := registrar.RegisterCardWithLease(ctx, card, ttl); err != nil {
		return failedCardRegistrarConformance("register-does-not-mutate-card", err)
	}
	if !reflect.DeepEqual(card, before) {
		return failedCardRegistrarConformance("register-does-not-mutate-card", errors.New("registrar mutated input card"))
	}
	return passedCardRegistrarConformance("register-does-not-mutate-card")
}

func checkCardRegistrarRegisterReturnsDefensiveCopy(ctx context.Context, registrar a2a.CardRegistrar, card a2a.AgentCard, ttl time.Duration) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("register-returns-defensive-copy", errors.New("card registrar is nil"))
	}
	registered, err := registrar.RegisterCardWithLease(ctx, card, ttl)
	if err != nil {
		return failedCardRegistrarConformance("register-returns-defensive-copy", err)
	}
	mutateCard(&registered)

	renewed, err := registrar.HeartbeatCard(ctx, card.Name, renewalTTL(ttl))
	if err != nil {
		return failedCardRegistrarConformance("register-returns-defensive-copy", err)
	}
	if cardHasMutation(renewed) {
		return failedCardRegistrarConformance("register-returns-defensive-copy", errors.New("registrar returned shared card data"))
	}
	return passedCardRegistrarConformance("register-returns-defensive-copy")
}

func checkCardRegistrarHeartbeatCanceledContext(ctx context.Context, registrar a2a.CardRegistrar, card a2a.AgentCard, ttl time.Duration) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("heartbeat-respects-canceled-context", errors.New("card registrar is nil"))
	}
	if _, err := registrar.RegisterCardWithLease(ctx, card, ttl); err != nil {
		return failedCardRegistrarConformance("heartbeat-respects-canceled-context", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := registrar.HeartbeatCard(canceled, card.Name, ttl)
	if !errors.Is(err, context.Canceled) {
		return failedCardRegistrarConformance("heartbeat-respects-canceled-context", fmt.Errorf("heartbeat canceled context error = %v, want context canceled", err))
	}
	return passedCardRegistrarConformance("heartbeat-respects-canceled-context")
}

func checkCardRegistrarHeartbeatRequiresTTL(ctx context.Context, registrar a2a.CardRegistrar, card a2a.AgentCard, ttl time.Duration) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("heartbeat-requires-positive-ttl", errors.New("card registrar is nil"))
	}
	if _, err := registrar.RegisterCardWithLease(ctx, card, ttl); err != nil {
		return failedCardRegistrarConformance("heartbeat-requires-positive-ttl", err)
	}
	_, err := registrar.HeartbeatCard(context.Background(), card.Name, 0)
	if !errors.Is(err, a2a.ErrLeaseTTLRequired) {
		return failedCardRegistrarConformance("heartbeat-requires-positive-ttl", fmt.Errorf("heartbeat zero ttl error = %v, want %v", err, a2a.ErrLeaseTTLRequired))
	}
	return passedCardRegistrarConformance("heartbeat-requires-positive-ttl")
}

func checkCardRegistrarHeartbeatRenewsLease(ctx context.Context, registrar a2a.CardRegistrar, card a2a.AgentCard, ttl time.Duration) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("heartbeat-renews-lease", errors.New("card registrar is nil"))
	}
	registered, err := registrar.RegisterCardWithLease(ctx, card, ttl)
	if err != nil {
		return failedCardRegistrarConformance("heartbeat-renews-lease", err)
	}
	renewed, err := registrar.HeartbeatCard(ctx, card.Name, renewalTTL(ttl))
	if err != nil {
		return failedCardRegistrarConformance("heartbeat-renews-lease", err)
	}
	if err := checkExpectedCard(renewed, card); err != nil {
		return failedCardRegistrarConformance("heartbeat-renews-lease", err)
	}
	if !renewed.ExpiresAt.After(registered.ExpiresAt) {
		return failedCardRegistrarConformance("heartbeat-renews-lease", fmt.Errorf("renewed expiry = %v, want after %v", renewed.ExpiresAt, registered.ExpiresAt))
	}
	return passedCardRegistrarConformance("heartbeat-renews-lease")
}

func checkCardRegistrarHeartbeatMissingCard(ctx context.Context, registrar a2a.CardRegistrar, card a2a.AgentCard, ttl time.Duration) CardRegistrarConformanceResult {
	if registrar == nil {
		return failedCardRegistrarConformance("heartbeat-rejects-missing-card", errors.New("card registrar is nil"))
	}
	missingName := card.Name + "-missing"
	_, err := registrar.HeartbeatCard(ctx, missingName, ttl)
	if !errors.Is(err, a2a.ErrAgentNotFound) {
		return failedCardRegistrarConformance("heartbeat-rejects-missing-card", fmt.Errorf("heartbeat missing card error = %v, want %v", err, a2a.ErrAgentNotFound))
	}
	return passedCardRegistrarConformance("heartbeat-rejects-missing-card")
}

func renewalTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 2 * time.Minute
	}
	return ttl + time.Minute
}

func passedCardRegistrarConformance(name string) CardRegistrarConformanceResult {
	return CardRegistrarConformanceResult{Case: name, Passed: true}
}

func failedCardRegistrarConformance(name string, err error) CardRegistrarConformanceResult {
	return CardRegistrarConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrCardRegistrarConformanceFailed, err),
	}
}

func copyRegistrarCard(card a2a.AgentCard) a2a.AgentCard {
	out := card
	out.Protocols = append([]a2a.ProtocolBinding(nil), card.Protocols...)
	out.Skills = copyRegistrarSkills(card.Skills)
	out.Capabilities = append([]string(nil), card.Capabilities...)
	out.Tags = append([]string(nil), card.Tags...)
	out.InputSchema = copyRegistrarJSONSchema(card.InputSchema)
	out.OutputSchema = copyRegistrarJSONSchema(card.OutputSchema)
	out.Auth = copyRegistrarAuthRequirement(card.Auth)
	out.Health = copyRegistrarHealthHints(card.Health)
	out.Metadata = copyAnyMap(card.Metadata)
	return out
}

func copyRegistrarSkills(skills []a2a.AgentSkill) []a2a.AgentSkill {
	if len(skills) == 0 {
		return nil
	}
	out := make([]a2a.AgentSkill, len(skills))
	for i, skill := range skills {
		out[i] = skill
		out[i].InputSchema = copyRegistrarJSONSchema(skill.InputSchema)
		out[i].OutputSchema = copyRegistrarJSONSchema(skill.OutputSchema)
		out[i].Metadata = copyAnyMap(skill.Metadata)
	}
	return out
}

func copyRegistrarAuthRequirement(auth *a2a.AuthRequirement) *a2a.AuthRequirement {
	if auth == nil {
		return nil
	}
	out := *auth
	out.Schemes = append([]string(nil), auth.Schemes...)
	out.Scopes = append([]string(nil), auth.Scopes...)
	out.Metadata = copyAnyMap(auth.Metadata)
	return &out
}

func copyRegistrarHealthHints(hints *a2a.HealthHints) *a2a.HealthHints {
	if hints == nil {
		return nil
	}
	out := *hints
	return &out
}

func copyRegistrarJSONSchema(in gopact.JSONSchema) gopact.JSONSchema {
	if len(in) == 0 {
		return nil
	}
	out := make(gopact.JSONSchema, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
