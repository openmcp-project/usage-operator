package runnable

import (
	"context"
	"time"

	"github.com/openmcp-project/usage-operator/internal/usage"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const interval = 60 * time.Minute

type UsageRunnable struct {
	client       client.Client
	usageTracker *usage.UsageTracker
}

func NewUsageRunnable(client client.Client, usageTracker *usage.UsageTracker) UsageRunnable {
	return UsageRunnable{
		client:       client,
		usageTracker: usageTracker,
	}
}

func (u *UsageRunnable) NeedLeaderElection() bool {
	return true
}

func (u *UsageRunnable) Start(ctx context.Context) error {
	ch := time.Tick(interval)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ch:
			err := u.loop(ctx)
			if err != nil {
				return err
			}
		}
	}
}

func (u *UsageRunnable) loop(ctx context.Context) error {
	err := u.usageTracker.ScheduledEvent(ctx)
	if err != nil {
		return err
	}

	return nil
}
