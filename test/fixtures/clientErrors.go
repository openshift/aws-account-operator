package fixtures

// For use with the generated crclient mocks, these can be used as .Return() values
// when the code path expects an error condition.

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// clientError will answer APIStatus questions like IsNotFound according to its `status`.
type clientError struct {
	reason metav1.StatusReason
}

// Status implements APIStatus
func (ce clientError) Status() metav1.Status {
	return metav1.Status{Reason: ce.reason}
}

// Error implements error
func (ce clientError) Error() string {
	return string(ce.reason)
}

// A couple of error stubs that can be returned from the Client.

// NotFound stub API response
var NotFound error = clientError{reason: metav1.StatusReasonNotFound}

// AlreadyExists stub API response
var AlreadyExists error = clientError{reason: metav1.StatusReasonAlreadyExists}

// Conflict stub API response
var Conflict error = clientError{reason: metav1.StatusReasonConflict}
