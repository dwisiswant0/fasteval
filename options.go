package fasteval

import (
	"context"
	"fmt"
)

// ResolveFunc looks up a variable after a direct map miss. found distinguishes
// an explicit nil value from an unknown name.
type ResolveFunc func(ctx context.Context, name string) (value any, found bool, err error)

type evalOptions struct {
	resolver ResolveFunc
}

// EvalOption configures one evaluation call.
type EvalOption func(*evalOptions) error

// WithResolver uses resolve after a direct variables-map miss.
func WithResolver(resolve ResolveFunc) EvalOption {
	return func(options *evalOptions) error {
		if resolve == nil {
			return newDetailError("resolver cannot be nil")
		}

		options.resolver = resolve

		return nil
	}
}

func applyEvalOptions(options []EvalOption) (evalOptions, error) {
	var config evalOptions

	for _, option := range options {
		if option == nil {
			return evalOptions{}, newDetailError("evaluation option cannot be nil")
		}

		err := option(&config)
		if err != nil {
			return evalOptions{}, fmt.Errorf("configure evaluation: %w", err)
		}
	}

	return config, nil
}
