package periodic

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Runner is a periodic runner which enqueues Pods for reconciliation on regular
// intervals.
type Runner struct {
	client          client.Client
	interval        time.Duration
	TargetNamespace string
	eventCh         chan event.GenericEvent
}

// Option is a function which configures the [Runner].
type Option func(c *Runner) error

// New creates a new periodic runner and configures it using the provided
// options.
func New(opts ...Option) (*Runner, error) {
	r := &Runner{}
	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, err
		}
	}

	return r, nil
}

// WithClient configures the [Runner] with the given client.
func WithClient(c client.Client) Option {
	opt := func(r *Runner) error {
		r.client = c
		return nil
	}

	return opt
}

// WithInterval configures the [Runner] with the given interval.
func WithInterval(interval time.Duration) Option {
	opt := func(r *Runner) error {
		r.interval = interval
		return nil
	}

	return opt
}

// WithTargetNamespace configures the [Runner] to watch the given namespace.
func WithTargetNamespace(ns string) Option {
	opt := func(r *Runner) error {
		r.TargetNamespace = ns
		return nil
	}

	return opt
}

// WithEventChannel configures the [Runner] to use the given channel for
// enqueuing.
func WithEventChannel(ch chan event.GenericEvent) Option {
	opt := func(r *Runner) error {
		r.eventCh = ch
		return nil
	}

	return opt
}

// Start implements the
// [sigs.k8s.io/controller-runtime/pkg/manager.Runnable] interface.
func (r *Runner) Start(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	logger := log.FromContext(ctx)
	defer ticker.Stop()
	defer close(r.eventCh)

	for {
		select {
		case <-ticker.C:
			if err := r.genericEventChannel(ctx); err != nil {
				logger.Error(err, "failed to enqueue pods")
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// genericEventChannel enqueues a generic event to trigger reconciliation
func (r *Runner) genericEventChannel(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("Enqueuing periodic event")

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "periodic-cabundle-enqueue",
			Namespace: r.TargetNamespace,
		},
	}

	event := event.GenericEvent{
		Object: cm,
	}
	r.eventCh <- event
	return nil
}
