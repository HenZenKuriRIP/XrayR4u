package api

import "errors"

// HTTPStatusError is a panel HTTP failure. Status is safe to log.
// The wrapped error may include a truncated body and must not be assumed
// free of secrets; callers that log it should drop the body.
type HTTPStatusError struct {
	Status int
	Err    error
}

func (e *HTTPStatusError) Error() string {
	if e == nil || e.Err == nil {
		return "panel request failed"
	}
	return e.Err.Error()
}

func (e *HTTPStatusError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// StatusOf reports the HTTP status carried by err, if any.
func StatusOf(err error) (int, bool) {
	var status *HTTPStatusError
	if errors.As(err, &status) && status != nil && status.Status > 0 {
		return status.Status, true
	}
	return 0, false
}
