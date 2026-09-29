// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package exporter

import (
	"context"
	"log"
	"strings"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// otherResourceBucket is the resource label value used when a key does not have enough
// segments to determine its resource kind.
const otherResourceBucket = "__other__"

// resourceFromKey extracts the etcd resource kind from a raw key. etcd keys written by the
// Kubernetes apiserver look like "/registry/<resource>/<namespace>/<name>". Splitting such a
// key on "/" yields ["", "registry", "<resource>", ...], so the resource kind lives at index 2.
// If the key has fewer than three segments, otherResourceBucket is returned.
func resourceFromKey(key string) string {
	segments := strings.Split(key, "/")
	if len(segments) < 3 {
		return otherResourceBucket
	}
	resource := segments[2]
	if resource == "" {
		return otherResourceBucket
	}
	return resource
}

// eventTypeLabel maps an mvccpb event type to its metric label value.
func eventTypeLabel(t mvccpb.Event_EventType) string {
	if t == mvccpb.DELETE {
		return "DELETE"
	}
	return "PUT"
}

// minBackoff is the floor applied to the (re)connect backoff to avoid a busy-loop when the
// configured backoff is zero or very small and etcd is unreachable.
const minBackoff = 1 * time.Second

// runWatch opens a persistent watch on the entire etcd keyspace and increments the resource-event
// counter for every observed event. The watch reconnects with exponential backoff whenever the
// channel closes or errors, until the supplied context is cancelled.
//
// In etcd v3, WithRev(0) means "start from the current revision", not "from the beginning of
// history". On the very first connect we therefore start with WithRev(0). On every subsequent
// reconnect we resume from the last observed revision + 1 (tracked via the watch response header
// and per-event ModRevision) so that events already counted are not re-observed and double-counted.
func runWatch(ctx context.Context, cli *clientv3.Client, m *metrics, baseBackoff time.Duration) {
	const maxBackoff = 2 * time.Minute
	// Clamp the base backoff to a sane minimum so a zero/tiny value cannot cause a busy-loop.
	if baseBackoff < minBackoff {
		baseBackoff = minBackoff
	}
	backoff := baseBackoff

	// lastRev tracks the highest revision observed so far. A value of 0 means we have not observed
	// any revision yet, in which case we start the watch from the current revision (WithRev(0)).
	var lastRev int64

	for {
		if ctx.Err() != nil {
			return
		}

		// On the first connect start from the current revision; on reconnect resume just after the
		// last revision we already processed to avoid re-counting historical events.
		startRev := int64(0)
		if lastRev > 0 {
			startRev = lastRev + 1
		}
		watchChan := cli.Watch(ctx, "", clientv3.WithPrefix(), clientv3.WithRev(startRev))

		// Consume events until the channel closes (connection dropped) or the context is cancelled.
		for watchResp := range watchChan {
			// Advance lastRev from the response header so reconnects resume correctly even for
			// progress-notification responses that carry no events.
			if watchResp.Header.Revision > lastRev {
				lastRev = watchResp.Header.Revision
			}
			if watchResp.Canceled {
				log.Printf("etcd watch canceled: %v; will reconnect", watchResp.Err())
				break
			}
			if err := watchResp.Err(); err != nil {
				log.Printf("etcd watch error: %v; will reconnect", err)
				break
			}
			// A successful response resets the backoff.
			backoff = baseBackoff
			for _, ev := range watchResp.Events {
				if ev != nil && ev.Kv != nil && ev.Kv.ModRevision > lastRev {
					lastRev = ev.Kv.ModRevision
				}
				m.recordEvent(ev)
			}
		}

		if ctx.Err() != nil {
			return
		}

		log.Printf("etcd watch channel closed; reconnecting in %s", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Exponential backoff, capped.
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// recordEvent increments the resource-event counter for a single etcd event.
func (m *metrics) recordEvent(ev *clientv3.Event) {
	if ev == nil || ev.Kv == nil {
		return
	}
	resource := resourceFromKey(string(ev.Kv.Key))
	m.resourceEvents.WithLabelValues(resource, eventTypeLabel(ev.Type)).Inc()
}
