package gopact

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

const (
	// EventMetadataEffectReplayPlan is the event metadata key carrying an EffectReplayPlan.
	EventMetadataEffectReplayPlan = "effect_replay_plan"
)

// EffectReplayAction is the runtime decision for a recorded effect during resume.
type EffectReplayAction string

const (
	EffectReplayActionRecordOnly EffectReplayAction = "record_only"
	EffectReplayActionReplay     EffectReplayAction = "replay"
	EffectReplayActionSkip       EffectReplayAction = "skip"
)

// EffectReplayPlan is a deterministic resume plan for the effects in a step snapshot.
type EffectReplayPlan struct {
	StepID          string                 `json:"step_id,omitempty"`
	Step            int                    `json:"step,omitempty"`
	Node            string                 `json:"node,omitempty"`
	Decisions       []EffectReplayDecision `json:"decisions,omitempty"`
	ReplayCount     int                    `json:"replay_count,omitempty"`
	SkipCount       int                    `json:"skip_count,omitempty"`
	RecordOnlyCount int                    `json:"record_only_count,omitempty"`
}

// EffectReplayDecision describes how one recorded effect should be handled on resume.
type EffectReplayDecision struct {
	Effect         EffectRecord       `json:"effect"`
	Action         EffectReplayAction `json:"action"`
	ReplayPolicy   EffectReplayPolicy `json:"replay_policy"`
	IdempotencyKey string             `json:"idempotency_key,omitempty"`
	Reason         string             `json:"reason,omitempty"`
}

// EffectReplayResult describes the outcome of applying one replay decision.
type EffectReplayResult struct {
	EffectID     string             `json:"effect_id,omitempty"`
	Action       EffectReplayAction `json:"action"`
	ReplayPolicy EffectReplayPolicy `json:"replay_policy,omitempty"`
	Effect       EffectRecord       `json:"effect"`
	Metadata     map[string]any     `json:"metadata,omitempty"`
}

// RunEffectNode is one effect placed in a run-level effect graph.
type RunEffectNode struct {
	Effect EffectRecord `json:"effect"`
	StepID string       `json:"step_id,omitempty"`
	Step   int          `json:"step,omitempty"`
	Node   string       `json:"node,omitempty"`
	Index  int          `json:"index"`
}

// RunEffectEdge is a dependency between two effect records in a run.
type RunEffectEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// RunEffectGraph describes effect dependencies across all stable steps in a run export.
type RunEffectGraph struct {
	RunID    string          `json:"run_id,omitempty"`
	ThreadID string          `json:"thread_id,omitempty"`
	Nodes    []RunEffectNode `json:"nodes,omitempty"`
	Edges    []RunEffectEdge `json:"edges,omitempty"`
	Ordered  []RunEffectNode `json:"ordered,omitempty"`
}

// RunEffectReplayPlan is a deterministic replay plan across all stable steps in a run.
type RunEffectReplayPlan struct {
	RunID           string                    `json:"run_id,omitempty"`
	ThreadID        string                    `json:"thread_id,omitempty"`
	Decisions       []RunEffectReplayDecision `json:"decisions,omitempty"`
	ReplayCount     int                       `json:"replay_count,omitempty"`
	SkipCount       int                       `json:"skip_count,omitempty"`
	RecordOnlyCount int                       `json:"record_only_count,omitempty"`
}

// RunEffectReplayDecision keeps step identity with one effect replay decision.
type RunEffectReplayDecision struct {
	StepID   string               `json:"step_id,omitempty"`
	Step     int                  `json:"step,omitempty"`
	Node     string               `json:"node,omitempty"`
	Index    int                  `json:"index"`
	Decision EffectReplayDecision `json:"decision"`
}

// RunEffectReplayResult keeps step identity with one replay decision result.
type RunEffectReplayResult struct {
	StepID string             `json:"step_id,omitempty"`
	Step   int                `json:"step,omitempty"`
	Node   string             `json:"node,omitempty"`
	Index  int                `json:"index"`
	Result EffectReplayResult `json:"result"`
}

// EffectReplayExecutor replays idempotent effects for ExecuteEffectReplay.
type EffectReplayExecutor interface {
	ReplayEffect(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error)
}

// EffectReplayFunc adapts a function into an EffectReplayExecutor.
type EffectReplayFunc func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error)

// ReplayEffect implements EffectReplayExecutor.
func (f EffectReplayFunc) ReplayEffect(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
	return f(ctx, decision)
}

// PlanEffectReplay creates a deterministic replay plan for a step snapshot's effects.
func PlanEffectReplay(snapshot StepSnapshot) (EffectReplayPlan, error) {
	if err := snapshot.Validate(); err != nil {
		return EffectReplayPlan{}, fmt.Errorf("gopact: plan effect replay: %w", err)
	}

	plan := EffectReplayPlan{
		StepID: snapshot.ID,
		Step:   snapshot.Step,
		Node:   snapshot.Node,
	}
	if len(snapshot.Effects) == 0 {
		return plan, nil
	}

	ordered, err := orderEffectRecords(snapshot.Effects)
	if err != nil {
		return EffectReplayPlan{}, fmt.Errorf("gopact: plan effect replay: %w", err)
	}
	plan.Decisions = make([]EffectReplayDecision, 0, len(ordered))
	for _, effect := range ordered {
		decision := effectReplayDecision(effect)
		switch decision.Action {
		case EffectReplayActionReplay:
			plan.ReplayCount++
		case EffectReplayActionSkip:
			plan.SkipCount++
		case EffectReplayActionRecordOnly:
			plan.RecordOnlyCount++
		}
		plan.Decisions = append(plan.Decisions, decision)
	}
	return plan, nil
}

// ExecuteEffectReplay applies an effect replay plan using the supplied executor for replay actions.
func ExecuteEffectReplay(ctx context.Context, plan EffectReplayPlan, executor EffectReplayExecutor) ([]EffectReplayResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	results := make([]EffectReplayResult, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		result, err := executeEffectReplayDecision(ctx, decision, executor)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// ExecuteRunEffectReplay applies a run-level replay plan while preserving step identity.
func ExecuteRunEffectReplay(ctx context.Context, plan RunEffectReplayPlan, executor EffectReplayExecutor) ([]RunEffectReplayResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	results := make([]RunEffectReplayResult, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		result, err := executeEffectReplayDecision(ctx, decision.Decision, executor)
		if err != nil {
			return results, err
		}
		results = append(results, RunEffectReplayResult{
			StepID: decision.StepID,
			Step:   decision.Step,
			Node:   decision.Node,
			Index:  decision.Index,
			Result: result,
		})
	}
	return results, nil
}

// BuildRunEffectGraph builds a deterministic effect graph across a complete run export.
func BuildRunEffectGraph(export RunExport) (RunEffectGraph, error) {
	if err := export.Validate(); err != nil {
		return RunEffectGraph{}, fmt.Errorf("gopact: build run effect graph: %w", err)
	}

	graph := RunEffectGraph{
		RunID:    export.IDs.RunID,
		ThreadID: export.IDs.ThreadID,
	}
	idIndex := make(map[string]int)
	for _, step := range export.Steps {
		if step.Phase == StepPending || step.Phase == StepRunning {
			if len(step.Effects) > 0 {
				return RunEffectGraph{}, fmt.Errorf("gopact: step %q phase %q is not stable for effect graph", step.ID, step.Phase)
			}
			continue
		}
		for _, effect := range step.Effects {
			if effect.ID == "" {
				return RunEffectGraph{}, fmt.Errorf("gopact: step %q effect id is required", step.ID)
			}
			if _, ok := idIndex[effect.ID]; ok {
				return RunEffectGraph{}, fmt.Errorf("gopact: effect id %q is duplicated in run", effect.ID)
			}
			node := RunEffectNode{
				Effect: copyEffectRecord(effect),
				StepID: step.ID,
				Step:   step.Step,
				Node:   step.Node,
				Index:  len(graph.Nodes),
			}
			idIndex[effect.ID] = len(graph.Nodes)
			graph.Nodes = append(graph.Nodes, node)
		}
	}

	ordered, edges, err := orderRunEffectNodes(graph.Nodes, idIndex)
	if err != nil {
		return RunEffectGraph{}, fmt.Errorf("gopact: build run effect graph: %w", err)
	}
	graph.Edges = edges
	graph.Ordered = ordered
	return graph, nil
}

// PlanRunEffectReplay creates a deterministic replay plan across all stable steps in a run export.
func PlanRunEffectReplay(export RunExport) (RunEffectReplayPlan, error) {
	graph, err := BuildRunEffectGraph(export)
	if err != nil {
		return RunEffectReplayPlan{}, fmt.Errorf("gopact: plan run effect replay: %w", err)
	}

	plan := RunEffectReplayPlan{
		RunID:    graph.RunID,
		ThreadID: graph.ThreadID,
	}
	plan.Decisions = make([]RunEffectReplayDecision, 0, len(graph.Ordered))
	for _, node := range graph.Ordered {
		decision := effectReplayDecision(node.Effect)
		switch decision.Action {
		case EffectReplayActionReplay:
			plan.ReplayCount++
		case EffectReplayActionSkip:
			plan.SkipCount++
		case EffectReplayActionRecordOnly:
			plan.RecordOnlyCount++
		}
		plan.Decisions = append(plan.Decisions, RunEffectReplayDecision{
			StepID:   node.StepID,
			Step:     node.Step,
			Node:     node.Node,
			Index:    node.Index,
			Decision: decision,
		})
	}
	return plan, nil
}

func executeEffectReplayDecision(ctx context.Context, decision EffectReplayDecision, executor EffectReplayExecutor) (EffectReplayResult, error) {
	switch decision.Action {
	case EffectReplayActionReplay:
		if executor == nil {
			return EffectReplayResult{}, fmt.Errorf("gopact: effect %q requires replay executor", decision.Effect.ID)
		}
		result, err := executor.ReplayEffect(ctx, decision)
		if err != nil {
			return EffectReplayResult{}, fmt.Errorf("gopact: replay effect %q: %w", decision.Effect.ID, err)
		}
		return normalizeEffectReplayResult(decision, result), nil
	case EffectReplayActionSkip, EffectReplayActionRecordOnly:
		return defaultEffectReplayResult(decision), nil
	default:
		return EffectReplayResult{}, fmt.Errorf("gopact: effect %q replay action %q is invalid", decision.Effect.ID, decision.Action)
	}
}

func effectReplayDecision(effect EffectRecord) EffectReplayDecision {
	policy := effect.ReplayPolicy
	if policy == EffectReplayUnspecified {
		policy = EffectReplayRecordOnly
	}
	decision := EffectReplayDecision{
		Effect:         copyEffectRecord(effect),
		ReplayPolicy:   policy,
		IdempotencyKey: effect.IdempotencyKey,
	}
	switch policy {
	case EffectReplayIdempotent:
		decision.Action = EffectReplayActionReplay
		decision.Reason = "effect is idempotent and may be replayed"
	case EffectReplaySkip:
		decision.Action = EffectReplayActionSkip
		decision.Reason = "recorded effect may be reused without replay"
	default:
		decision.Action = EffectReplayActionRecordOnly
		decision.Reason = "effect is evidence only and requires caller handling"
	}
	return decision
}

func normalizeEffectReplayResult(decision EffectReplayDecision, result EffectReplayResult) EffectReplayResult {
	if result.EffectID == "" {
		result.EffectID = decision.Effect.ID
	}
	if result.Action == "" {
		result.Action = decision.Action
	}
	if result.ReplayPolicy == EffectReplayUnspecified {
		result.ReplayPolicy = decision.ReplayPolicy
	}
	if result.Effect.ID == "" {
		result.Effect = copyEffectRecord(decision.Effect)
	} else {
		result.Effect = copyEffectRecord(result.Effect)
	}
	result.Metadata = copyAnyMap(result.Metadata)
	return result
}

func defaultEffectReplayResult(decision EffectReplayDecision) EffectReplayResult {
	return EffectReplayResult{
		EffectID:     decision.Effect.ID,
		Action:       decision.Action,
		ReplayPolicy: decision.ReplayPolicy,
		Effect:       copyEffectRecord(decision.Effect),
	}
}

func orderEffectRecords(effects []EffectRecord) ([]EffectRecord, error) {
	idIndex := make(map[string]int, len(effects))
	for i, effect := range effects {
		if effect.ID != "" {
			idIndex[effect.ID] = i
		}
	}

	indegree := make([]int, len(effects))
	children := make([][]int, len(effects))
	for i, effect := range effects {
		seen := make(map[int]struct{}, len(effect.DependsOn))
		for _, dep := range effect.DependsOn {
			depIndex, ok := idIndex[dep]
			if !ok {
				return nil, fmt.Errorf("effect %q depends on missing effect %q", effect.ID, dep)
			}
			if _, ok := seen[depIndex]; ok {
				continue
			}
			seen[depIndex] = struct{}{}
			indegree[i]++
			children[depIndex] = append(children[depIndex], i)
		}
	}

	queue := make([]int, 0, len(effects))
	for i, count := range indegree {
		if count == 0 {
			queue = append(queue, i)
		}
	}

	ordered := make([]EffectRecord, 0, len(effects))
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		ordered = append(ordered, effects[index])
		for _, child := range children[index] {
			indegree[child]--
			if indegree[child] == 0 {
				queue = append(queue, child)
				sort.Ints(queue)
			}
		}
	}
	if len(ordered) != len(effects) {
		return nil, errors.New("effect dependency graph contains a cycle")
	}
	return ordered, nil
}

func orderRunEffectNodes(nodes []RunEffectNode, idIndex map[string]int) ([]RunEffectNode, []RunEffectEdge, error) {
	indegree := make([]int, len(nodes))
	children := make([][]int, len(nodes))
	var edges []RunEffectEdge

	for i, node := range nodes {
		seen := make(map[int]struct{}, len(node.Effect.DependsOn))
		for _, dep := range node.Effect.DependsOn {
			depIndex, ok := idIndex[dep]
			if !ok {
				return nil, nil, fmt.Errorf("effect %q depends on missing effect %q", node.Effect.ID, dep)
			}
			depNode := nodes[depIndex]
			if depNode.Step > node.Step {
				return nil, nil, fmt.Errorf("effect %q depends on future effect %q", node.Effect.ID, dep)
			}
			if _, ok := seen[depIndex]; ok {
				continue
			}
			seen[depIndex] = struct{}{}
			indegree[i]++
			children[depIndex] = append(children[depIndex], i)
			edges = append(edges, RunEffectEdge{From: dep, To: node.Effect.ID})
		}
	}

	queue := make([]int, 0, len(nodes))
	for i, count := range indegree {
		if count == 0 {
			queue = append(queue, i)
		}
	}

	ordered := make([]RunEffectNode, 0, len(nodes))
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		ordered = append(ordered, copyRunEffectNode(nodes[index]))
		for _, child := range children[index] {
			indegree[child]--
			if indegree[child] == 0 {
				queue = append(queue, child)
				sort.Ints(queue)
			}
		}
	}
	if len(ordered) != len(nodes) {
		return nil, nil, errors.New("run effect dependency graph contains a cycle")
	}
	return ordered, edges, nil
}

func copyRunEffectNode(in RunEffectNode) RunEffectNode {
	out := in
	out.Effect = copyEffectRecord(in.Effect)
	return out
}
