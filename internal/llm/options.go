package llm

import "time"

// Option is a functional option for New().
type Option func(Driver)

// WithWorkDir sets the working directory for the subprocess.
func WithWorkDir(dir string) Option {
	return func(d Driver) {
		if wc, ok := d.(WorkDirConfigurable); ok {
			wc.SetWorkDir(dir)
		}
	}
}

// WithTimeout sets the per-call timeout.
func WithTimeout(d time.Duration) Option {
	return func(drv Driver) {
		if tc, ok := drv.(TimeoutConfigurable); ok {
			tc.SetTimeout(d)
		}
	}
}

// WithThinking sets extended thinking mode.
func WithThinking(v string) Option {
	return func(drv Driver) {
		if tc, ok := drv.(ThinkingConfigurable); ok {
			tc.SetThinking(v)
		}
	}
}

// NewWithOptions creates a Driver with functional options applied.
func NewWithOptions(driver, model string, opts ...Option) (Driver, error) {
	d, err := New(driver, model)
	if err != nil {
		return nil, err
	}
	for _, o := range opts {
		o(d)
	}
	return d, nil
}
