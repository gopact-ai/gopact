package workflow

import (
	"errors"
	"sort"
)

type checkpointIterSource struct {
	ID            string
	SourceID      string
	Target        string
	SourceSet     string
	DeliveryIndex int
	Pulled        int
	Cursor        checkpointValue
	HasCursor     bool
	Replay        bool
	Open          bool
	Cause         string
}

func (state runState) checkpointIterSources() []checkpointIterSource {
	ids := make([]string, 0, len(state.iterSources))
	for id := range state.iterSources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sources := make([]checkpointIterSource, 0, len(ids))
	for _, id := range ids {
		sources = append(sources, state.iterSources[id].checkpoint())
	}
	return sources
}

func (source *iterSource) checkpoint() checkpointIterSource {
	cause := ""
	if source.cause != nil {
		cause = source.cause.Error()
	}
	return checkpointIterSource{
		ID: source.id, SourceID: source.sourceID, Target: source.target, SourceSet: source.sourceSet,
		DeliveryIndex: source.deliveryIndex, Pulled: source.pulled, Cursor: newCheckpointValue(source.cursor),
		HasCursor: source.hasCursor, Replay: source.replay, Open: source.open, Cause: cause,
	}
}

func registerCheckpointIterSources(sources []checkpointIterSource) {
	for _, source := range sources {
		if source.HasCursor {
			source.Cursor.register()
		}
	}
}

func (state *runState) restoreIterSources(sources []checkpointIterSource) {
	for _, encoded := range sources {
		source := encoded.runtime()
		state.iterSources[source.id] = source
	}
	if state.nextIterSeq <= 0 {
		state.nextIterSeq = 1
	}
}

func (encoded checkpointIterSource) runtime() *iterSource {
	var cause error
	if encoded.Cause != "" {
		cause = errors.New(encoded.Cause)
	}
	return &iterSource{
		id: encoded.ID, sourceID: encoded.SourceID, target: encoded.Target, sourceSet: encoded.SourceSet,
		deliveryIndex: encoded.DeliveryIndex, pulled: encoded.Pulled, cursor: encoded.Cursor.runtime(),
		hasCursor: encoded.HasCursor, replay: encoded.Replay, open: encoded.Open, cause: cause,
	}
}
