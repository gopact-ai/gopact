package gopact

import (
	"context"
	"iter"
	"testing"
)

func TestRunnableFuncInvokesTypedInputAndOutput(t *testing.T) {
	var runnable Runnable[string, int] = RunnableFunc[string, int](
		func(_ context.Context, input string, _ ...RunOption) (int, error) {
			return len(input), nil
		},
	)

	got, err := runnable.Invoke(context.Background(), "gopact")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != 6 {
		t.Fatalf("Invoke() = %d, want 6", got)
	}
}

func TestStateRunnableUsesSameInputAndOutputType(t *testing.T) {
	var runnable StateRunnable[int] = RunnableFunc[int, int](
		func(_ context.Context, input int, _ ...RunOption) (int, error) {
			return input + 1, nil
		},
	)

	got, err := runnable.Invoke(context.Background(), 41)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != 42 {
		t.Fatalf("Invoke() = %d, want 42", got)
	}
}

func TestRunnerEmitsEventsToSink(t *testing.T) {
	var got []Event
	runner, err := NewRunner(eventRunnableFunc(func(_ context.Context, _ any, _ ...RunOption) iter.Seq2[Event, error] {
		return func(yield func(Event, error) bool) {
			yield(Event{Type: EventRunStarted}, nil)
			yield(Event{Type: EventRunCompleted}, nil)
		}
	}))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	for _, err := range runner.Run(context.Background(), nil, WithEvents(EventSinkFunc(func(_ context.Context, event Event) error {
		got = append(got, event)
		return nil
	}))) {
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	}

	if len(got) != 2 {
		t.Fatalf("sink events = %d, want 2", len(got))
	}
	if got[0].Type != EventRunStarted || got[1].Type != EventRunCompleted {
		t.Fatalf("sink event types = %s, %s", got[0].Type, got[1].Type)
	}
}

type eventRunnableFunc func(context.Context, any, ...RunOption) iter.Seq2[Event, error]

func (f eventRunnableFunc) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return f(ctx, input, opts...)
}
