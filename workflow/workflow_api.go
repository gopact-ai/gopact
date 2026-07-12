package workflow

import (
	"context"
	"fmt"
	"iter"

	"github.com/gopact-ai/gopact"
)

// Node registers a typed function node.
func (wf *Workflow[I, O]) Node[NI, NO any](name string, fn func(context.Context, NI) (NO, error)) *Node[NI, NO] {
	if wf == nil {
		return nil
	}
	n := &Node[NI, NO]{
		name: name,
	}
	if fn != nil {
		n.run = func(ctx context.Context, input NI, _ ...gopact.RunOption) (NO, error) {
			return fn(ctx, input)
		}
	}
	wf.registerNode(name, n)
	return n
}

// AddInvokable registers a typed invokable node.
func (wf *Workflow[I, O]) AddInvokable[NI, NO any](name string, inv gopact.Invokable[NI, NO]) *Node[NI, NO] {
	if wf == nil {
		return nil
	}
	n := &Node[NI, NO]{
		name:      name,
		invokable: true,
	}
	if inv != nil {
		n.run = inv.Invoke
	}
	wf.registerNode(name, n)
	return n
}

// Merge registers a node whose input is built from upstream contributions.
func (wf *Workflow[I, O]) Merge[NO any](name string, fn func(context.Context, Inputs) (NO, error)) *Node[Inputs, NO] {
	if wf == nil {
		return nil
	}
	n := &Node[Inputs, NO]{
		name:  name,
		merge: true,
	}
	if fn != nil {
		n.run = func(ctx context.Context, input Inputs, _ ...gopact.RunOption) (NO, error) {
			return fn(ctx, input)
		}
	}
	wf.registerNode(name, n)
	return n
}

// To dispatches this source output once to target.
func (n *Node[I, O]) To[TI, TO any](target *Node[TI, TO]) Dispatch {
	d := n.newDispatch()
	if target != nil {
		d.deliveries = append(d.deliveries, delivery{target: target.endpointName(), useSourceOutput: true})
	}
	return d
}

// Once dispatches one custom payload to target.
func (n *Node[I, O]) Once[TI, TO any](target *Node[TI, TO], payload TI) Dispatch {
	d := n.newDispatch()
	if target != nil {
		d.deliveries = append(d.deliveries, delivery{target: target.endpointName(), payload: payload})
	}
	return d
}

// Each dispatches one custom payload per item to target.
func (n *Node[I, O]) Each[TI, TO any](target *Node[TI, TO], payloads ...TI) Dispatch {
	d := n.newDispatch()
	if target == nil {
		return d
	}
	d.deliveries = make([]delivery, 0, len(payloads))
	for _, payload := range payloads {
		d.deliveries = append(d.deliveries, delivery{target: target.endpointName(), payload: payload})
	}
	return d
}

// EachIter dispatches one custom payload per yielded item.
func (n *Node[I, O]) EachIter[TI, TO any](target *Node[TI, TO], iterFn func(context.Context) iter.Seq2[TI, error], opts ...IterOption[TI]) Dispatch {
	d := n.newDispatch()
	if target == nil {
		return d
	}
	var cfg iterConfig[TI]
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	d.deliveries = append(d.deliveries, delivery{
		target:         target.endpointName(),
		iterCheckpoint: cfg.checkpoint,
		iterErr:        cfg.err,
		iter: func(ctx context.Context) iter.Seq2[any, error] {
			if iterFn == nil {
				return nil
			}
			return eraseIter(iterFn(ctx))
		},
		iterRestore: eraseIterRestore(cfg.restore),
	})
	return d
}

func eraseIter[T any](seq iter.Seq2[T, error]) iter.Seq2[any, error] {
	if seq == nil {
		return nil
	}
	return func(yield func(any, error) bool) {
		for value, err := range seq {
			if !yield(value, err) {
				return
			}
		}
	}
}

func eraseIterRestore[T any](restore func(context.Context, any) iter.Seq2[T, error]) func(context.Context, any) iter.Seq2[any, error] {
	if restore == nil {
		return nil
	}
	return func(ctx context.Context, cursor any) iter.Seq2[any, error] { return eraseIter(restore(ctx, cursor)) }
}

// Entry sets the workflow entry target.
func (wf *Workflow[I, O]) Entry[EI, EO any](target *Node[EI, EO]) {
	wf.setEntry(target)
}

// Edge connects source to target.
func (wf *Workflow[I, O]) Edge[SI, SO, TI, TO any](source *Node[SI, SO], target *Node[TI, TO]) {
	wf.connect(source, target)
}

// Exit marks a source as a workflow output source.
func (wf *Workflow[I, O]) Exit[EI, EO any](source *Node[EI, EO]) {
	wf.addExit(source)
}

// One returns exactly one contribution from source.
func (in Inputs) One[EI, EO any](source *Node[EI, EO]) (EO, error) {
	var zero EO
	value, err := in.one(source)
	if err != nil {
		return zero, err
	}
	typed, ok := value.(EO)
	if !ok {
		return zero, fmt.Errorf("workflow: input from %q has type %T, want %s", source.endpointName(), value, typeOf[EO]())
	}
	return typed, nil
}

// All returns all contributions from source.
func (in Inputs) All[EI, EO any](source *Node[EI, EO]) ([]EO, error) {
	values, err := in.all(source)
	if err != nil {
		return nil, err
	}
	out := make([]EO, 0, len(values))
	for _, value := range values {
		typed, ok := value.(EO)
		if !ok {
			return nil, fmt.Errorf("workflow: input from %q has type %T, want %s", source.endpointName(), value, typeOf[EO]())
		}
		out = append(out, typed)
	}
	return out, nil
}

// Lookup returns an optional single contribution from source.
func (in Inputs) Lookup[EI, EO any](source *Node[EI, EO]) (EO, bool, error) {
	var zero EO
	value, ok, err := in.lookup(source)
	if err != nil || !ok {
		return zero, ok, err
	}
	typed, typeOK := value.(EO)
	if !typeOK {
		return zero, true, fmt.Errorf(
			"workflow: input from %q has type %T, want %s",
			source.endpointName(),
			value,
			typeOf[EO](),
		)
	}
	return typed, true, nil
}

// LookupAll returns optional contributions from source.
func (in Inputs) LookupAll[EI, EO any](source *Node[EI, EO]) ([]EO, bool, error) {
	values, ok, err := in.lookupAll(source)
	if err != nil || !ok {
		return nil, ok, err
	}
	out := make([]EO, 0, len(values))
	for _, value := range values {
		typed, typeOK := value.(EO)
		if !typeOK {
			return nil, true, fmt.Errorf(
				"workflow: input from %q has type %T, want %s",
				source.endpointName(),
				value,
				typeOf[EO](),
			)
		}
		out = append(out, typed)
	}
	return out, true, nil
}
