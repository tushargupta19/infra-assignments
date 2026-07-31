package domain

import "fmt"

// ValidationError indicates that a Config failed input validation. Handlers
// can use errors.As to detect this and respond with 400 Bad Request instead
// of a generic 500.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// errInvalidPort is returned when a Config's Port is outside 1-65535.
var errInvalidPort = &ValidationError{Message: "port must be between 1 and 65535"}

// errRequired builds a validation error for a missing required field.
func errRequired(field string) error {
	return &ValidationError{Message: fmt.Sprintf("%s is required", field)}
}
