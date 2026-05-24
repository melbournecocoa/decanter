package activity

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
)

// keepalive records a heartbeat every interval until the returned cancel
// func is called. Wrap any subprocess whose output might go silent for
// stretches longer than the workflow's HeartbeatTimeout — ffmpeg
// silencedetect passes, long ffmpeg copies, etc. Pair with line-based
// heartbeats: those give richer telemetry while output flows, this guards
// the silent phases.
//
//	defer keepalive(ctx, 30*time.Second)()
func keepalive(ctx context.Context, interval time.Duration) func() {
	keepCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-keepCtx.Done():
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "keepalive")
			}
		}
	}()
	return cancel
}
