package workflow

import "sort"

type checkpointJoinBucket struct {
	Target        string
	Correlation   CorrelationKey
	Contributions map[string][]any
	Expectations  map[string]map[string]int
}

func checkpointJoinBuckets(in map[joinBucketKey]*joinBucket) []checkpointJoinBucket {
	keys := make([]joinBucketKey, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return joinBucketLess(keys[left], keys[right]) })
	buckets := make([]checkpointJoinBucket, 0, len(keys))
	for _, key := range keys {
		bucket := in[key]
		buckets = append(buckets, checkpointJoinBucket{
			Target: key.target, Correlation: key.correlation,
			Contributions: copyContributions(bucket.contributions), Expectations: copyNestedIntMap(bucket.expectations),
		})
	}
	return buckets
}

func joinBucketLess(left, right joinBucketKey) bool {
	if left.correlation.ID != right.correlation.ID {
		return left.correlation.ID < right.correlation.ID
	}
	if left.correlation.Epoch != right.correlation.Epoch {
		return left.correlation.Epoch < right.correlation.Epoch
	}
	return left.target < right.target
}

func registerCheckpointJoinBuckets(buckets []checkpointJoinBucket) {
	for _, bucket := range buckets {
		registerCheckpointSources(bucket.Contributions)
	}
}

func (state *runState) restoreJoinBuckets(buckets []checkpointJoinBucket) {
	for _, encoded := range buckets {
		bucket := state.joinBucket(encoded.Target, encoded.Correlation)
		bucket.contributions = copyContributions(encoded.Contributions)
		bucket.expectations = copyNestedIntMap(encoded.Expectations)
	}
}

func copyCorrelationCounts(in map[CorrelationKey]map[string]int) map[CorrelationKey]map[string]int {
	out := make(map[CorrelationKey]map[string]int, len(in))
	for correlation, counts := range in {
		out[correlation] = copyIntMap(counts)
	}
	return out
}
