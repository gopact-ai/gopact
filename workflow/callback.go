package workflow

import "fmt"

func invokeCallback[T any](kind string, callback func() (T, error)) (value T, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = callbackPanicError(kind, recovered)
		}
	}()
	return callback()
}

func invokeCallbackError(kind string, callback func() error) error {
	_, err := invokeCallback(kind, func() (struct{}, error) {
		return struct{}{}, callback()
	})
	return err
}

func callbackPanicError(kind string, recovered any) error {
	if cause, ok := recovered.(error); ok {
		return fmt.Errorf("workflow: %s panic: %w", kind, cause)
	}
	return fmt.Errorf("workflow: %s panic: %v", kind, recovered)
}
