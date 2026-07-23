package workflow

import "fmt"

func invokeCallback[T any](kind, name string, callback func() (T, error)) (value T, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = callbackPanicError(kind, name, recovered)
		}
	}()
	return callback()
}

func invokeCallbackError(kind, name string, callback func() error) error {
	_, err := invokeCallback(kind, name, func() (struct{}, error) {
		return struct{}{}, callback()
	})
	return err
}

func callbackPanicError(kind, name string, recovered any) error {
	if name != "" {
		kind = fmt.Sprintf("%s %q", kind, name)
	}
	if cause, ok := recovered.(error); ok {
		return fmt.Errorf("workflow: %s panic: %w", kind, cause)
	}
	return fmt.Errorf("workflow: %s panic: %v", kind, recovered)
}
