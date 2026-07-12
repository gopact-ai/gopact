package workflow

import "fmt"

// CorrelationKey identifies one logical workflow dataflow epoch.
type CorrelationKey struct {
	ID    string `json:"id"`
	Epoch int    `json:"epoch"`
}

type joinBucketKey struct {
	target      string
	correlation CorrelationKey
}

func (state *runState) joinBucket(target string, correlation CorrelationKey) *joinBucket {
	if state.buckets == nil {
		state.buckets = map[joinBucketKey]*joinBucket{}
	}
	key := joinBucketKey{target: target, correlation: correlation}
	bucket := state.buckets[key]
	if bucket != nil {
		return bucket
	}
	bucket = &joinBucket{contributions: map[string][]any{}, expectations: map[string]map[string]int{}}
	state.buckets[key] = bucket
	return bucket
}

func (c *compiled[I, O]) closeJoinExpectations(state *runState, source activation, dispatch Dispatch) error {
	selected := c.selectedJoinTargets(dispatch)
	for _, target := range c.edges[source.node] {
		if !c.isJoinTarget(target) {
			continue
		}
		correlation := c.nextCorrelation(source, target)
		if err := state.joinBucket(target, correlation).close(source, selected[target]); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiled[I, O]) selectedJoinTargets(dispatch Dispatch) map[string]int {
	selected := map[string]int{}
	for _, item := range dispatch.deliveries {
		if item.useSourceOutput && c.isJoinTarget(item.target) {
			selected[item.target]++
		}
	}
	return selected
}

func (bucket *joinBucket) close(source activation, expected int) error {
	byActivation := bucket.expectations[source.node]
	if byActivation == nil {
		byActivation = map[string]int{}
		bucket.expectations[source.node] = byActivation
	}
	current, closed := byActivation[source.id]
	if closed && current != expected {
		return fmt.Errorf("workflow: join expectation for activation %q changed from %d to %d", source.id, current, expected)
	}
	byActivation[source.id] = expected
	return nil
}

func (bucket *joinBucket) sourceReady(source string, pending int) bool {
	byActivation := bucket.expectations[source]
	if len(byActivation) != pending {
		return false
	}
	expected := 0
	for _, count := range byActivation {
		expected += count
	}
	return len(bucket.contributions[source]) == expected
}

func (c *compiled[I, O]) pendingJoinActivations(state *runState, key joinBucketKey, source string) int {
	correlation := key.correlation
	if _, back := c.backEdges[topologyEdge{source: source, target: key.target}]; back {
		correlation.Epoch--
	}
	return state.correlations[correlation][source]
}

func (state *runState) trackCorrelation(item activation) {
	if state.correlations == nil {
		state.correlations = map[CorrelationKey]map[string]int{}
	}
	byNode := state.correlations[item.correlation]
	if byNode == nil {
		byNode = map[string]int{}
		state.correlations[item.correlation] = byNode
	}
	byNode[item.node]++
}

func (state *runState) restoreReadyCorrelations() {
	if len(state.correlations) > 0 {
		return
	}
	for _, item := range state.queue {
		state.trackCorrelation(item)
	}
}

func copyNestedIntMap(in map[string]map[string]int) map[string]map[string]int {
	out := make(map[string]map[string]int, len(in))
	for key, values := range in {
		out[key] = copyIntMap(values)
	}
	return out
}
