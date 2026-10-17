package domain

import "errors"

var (
	ErrInvalidSignal = errors.New("invalid signal")
	// ErrUnavailable means the data could not be accepted right now and the
	// client should retry (OTLP: HTTP 503 / gRPC Unavailable).
	ErrUnavailable = errors.New("temporarily unavailable")

	ErrInvalidQuery = errors.New("invalid query")
	ErrNotFound     = errors.New("not found")
)
