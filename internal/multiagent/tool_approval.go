package multiagent

import (
	"context"
	"errors"
	"strings"
)

type hitlInterceptorKey struct{}

type HITLToolInterceptor func(ctx context.Context, toolName, arguments string) (string, error)

type humanRejectError struct {
	reason string
}

func (e *humanRejectError) Error() string {
	if strings.TrimSpace(e.reason) == "" {
		return "rejected by user"
	}
	return "rejected by user: " + strings.TrimSpace(e.reason)
}

func NewHumanRejectError(reason string) error {
	return &humanRejectError{reason: strings.TrimSpace(reason)}
}

func IsHumanRejectError(err error) bool {
	var target *humanRejectError
	return errors.As(err, &target)
}

func WithHITLToolInterceptor(ctx context.Context, fn HITLToolInterceptor) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, hitlInterceptorKey{}, fn)
}
