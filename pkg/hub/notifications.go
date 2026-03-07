// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/messages"
	"github.com/ptone/scion-agent/pkg/store"
)

// NotificationDispatcher listens for agent status events, matches them against
// notification subscriptions, stores notification records, and dispatches
// messages to subscriber agents.
type NotificationDispatcher struct {
	store         store.Store
	events        *ChannelEventPublisher
	getDispatcher func() AgentDispatcher // lazy getter; dispatcher may be set after startup
	log           *slog.Logger
	messageLog    *slog.Logger // dedicated message audit logger (nil = disabled)
	stopCh        chan struct{}
	stopOnce      sync.Once
	wg            sync.WaitGroup
}

// NewNotificationDispatcher creates a new NotificationDispatcher.
// The getDispatcher function is called at dispatch time to resolve the current
// AgentDispatcher, allowing the dispatcher to be set up after the notification
// system starts (e.g. in combined hub+web mode).
func NewNotificationDispatcher(s store.Store, events *ChannelEventPublisher, getDispatcher func() AgentDispatcher, log *slog.Logger) *NotificationDispatcher {
	return &NotificationDispatcher{
		store:         s,
		events:        events,
		getDispatcher: getDispatcher,
		log:           log,
		stopCh:        make(chan struct{}),
	}
}

// Start subscribes to agent status events and spawns a goroutine to process them.
func (nd *NotificationDispatcher) Start() {
	ch, unsubscribe := nd.events.Subscribe("grove.>.agent.status")

	nd.wg.Add(1)
	go func() {
		defer nd.wg.Done()
		defer unsubscribe()
		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					return
				}
				nd.handleEvent(evt)
			case <-nd.stopCh:
				return
			}
		}
	}()

	nd.log.Info("Notification dispatcher started")
}

// Stop signals the dispatcher goroutine to exit and waits for it to finish.
// It is safe to call multiple times.
func (nd *NotificationDispatcher) Stop() {
	nd.stopOnce.Do(func() {
		close(nd.stopCh)
		nd.wg.Wait()
		nd.log.Info("Notification dispatcher stopped")
	})
}

// handleEvent processes a single agent status event.
func (nd *NotificationDispatcher) handleEvent(evt Event) {
	var statusEvt AgentStatusEvent
	if err := json.Unmarshal(evt.Data, &statusEvt); err != nil {
		nd.log.Error("Failed to unmarshal agent status event", "error", err)
		return
	}

	ctx := context.Background()

	nd.log.Debug("Notification dispatcher received event",
		"agentID", statusEvt.AgentID, "activity", statusEvt.Activity, "phase", statusEvt.Phase)

	subs, err := nd.store.GetNotificationSubscriptions(ctx, statusEvt.AgentID)
	if err != nil {
		nd.log.Error("Failed to get notification subscriptions",
			"agentID", statusEvt.AgentID, "error", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	// Use activity for matching (notifications trigger on activity changes).
	// Fall back to phase when activity is empty (e.g. phase "error" has no activity).
	matchStatus := statusEvt.Activity
	if matchStatus == "" {
		matchStatus = statusEvt.Phase
	}

	nd.log.Debug("Notification dispatcher checking subscriptions",
		"agentID", statusEvt.AgentID, "activity", matchStatus, "subscriptionCount", len(subs))

	for i := range subs {
		sub := &subs[i]
		if !sub.MatchesActivity(matchStatus) {
			continue
		}

		// Dedup: check if the last notification for this subscription already has this status
		lastStatus, err := nd.store.GetLastNotificationStatus(ctx, sub.ID)
		if err != nil {
			nd.log.Error("Failed to get last notification status",
				"subscriptionID", sub.ID, "error", err)
			continue
		}
		if strings.EqualFold(lastStatus, matchStatus) {
			continue
		}

		nd.storeAndDispatch(ctx, sub, statusEvt)
	}
}

// storeAndDispatch creates a notification record and dispatches it to the subscriber.
func (nd *NotificationDispatcher) storeAndDispatch(ctx context.Context, sub *store.NotificationSubscription, evt AgentStatusEvent) {
	agent, err := nd.store.GetAgent(ctx, evt.AgentID)
	if err != nil {
		nd.log.Error("Failed to get agent for notification",
			"agentID", evt.AgentID, "error", err)
		return
	}

	// Use activity for matching/display; fall back to phase when activity is empty.
	effectiveStatus := evt.Activity
	if effectiveStatus == "" {
		effectiveStatus = evt.Phase
	}

	message := formatNotificationMessage(agent, effectiveStatus)

	notif := &store.Notification{
		ID:             api.NewUUID(),
		SubscriptionID: sub.ID,
		AgentID:        evt.AgentID,
		GroveID:        sub.GroveID,
		SubscriberType: sub.SubscriberType,
		SubscriberID:   sub.SubscriberID,
		Status:         strings.ToUpper(effectiveStatus),
		Message:        message,
		CreatedAt:      time.Now(),
	}

	if err := nd.store.CreateNotification(ctx, notif); err != nil {
		nd.log.Error("Failed to create notification",
			"subscriptionID", sub.ID, "agentID", evt.AgentID, "error", err)
		return
	}

	nd.log.Info("Notification created",
		"notificationID", notif.ID, "agentID", evt.AgentID, "subscriber", sub.SubscriberType+":"+sub.SubscriberID, "status", notif.Status)

	switch sub.SubscriberType {
	case store.SubscriberTypeAgent:
		nd.dispatchToAgent(ctx, sub, notif, agent.Slug)
	case store.SubscriberTypeUser:
		nd.events.PublishNotification(ctx, notif)
		nd.log.Info("Notification dispatched to user via SSE",
			"subscriberID", sub.SubscriberID, "notificationID", notif.ID)
	default:
		nd.log.Warn("Unknown subscriber type", "type", sub.SubscriberType)
	}
}

// dispatchToAgent sends a notification message to a subscriber agent as a
// structured message. The sender is the watched agent (agent:<slug>), and
// the type is state-change or input-needed based on the notification status.
func (nd *NotificationDispatcher) dispatchToAgent(ctx context.Context, sub *store.NotificationSubscription, notif *store.Notification, watchedSlug string) {
	subscriber, err := nd.store.GetAgentBySlug(ctx, sub.GroveID, sub.SubscriberID)
	if err != nil {
		nd.log.Warn("Subscriber agent not found, skipping dispatch",
			"subscriberID", sub.SubscriberID, "groveID", sub.GroveID, "error", err)
		return
	}

	dispatcher := nd.getDispatcher()
	if dispatcher == nil {
		nd.log.Warn("No dispatcher available, skipping notification dispatch",
			"subscriberID", sub.SubscriberID)
		// Mark dispatched anyway (best-effort)
		if err := nd.store.MarkNotificationDispatched(ctx, notif.ID); err != nil {
			nd.log.Error("Failed to mark notification dispatched", "notificationID", notif.ID, "error", err)
		}
		return
	}

	if subscriber.RuntimeBrokerID == "" {
		nd.log.Warn("Subscriber agent has no runtime broker, skipping dispatch",
			"subscriberID", sub.SubscriberID)
		if err := nd.store.MarkNotificationDispatched(ctx, notif.ID); err != nil {
			nd.log.Error("Failed to mark notification dispatched", "notificationID", notif.ID, "error", err)
		}
		return
	}

	// Build structured message for the notification
	msgType := notificationMessageType(notif.Status)
	structuredMsg := messages.NewNotification(
		"agent:"+watchedSlug,
		"agent:"+subscriber.Slug,
		notif.Message,
		msgType,
	)

	if err := dispatcher.DispatchAgentMessage(ctx, subscriber, notif.Message, false, structuredMsg); err != nil {
		nd.log.Error("Failed to dispatch notification to agent",
			"subscriberID", sub.SubscriberID, "error", err)
	} else {
		nd.log.Info("Notification dispatched to agent",
			"subscriberID", sub.SubscriberID, "notificationID", notif.ID, "brokerID", subscriber.RuntimeBrokerID)
		// Log to dedicated message audit log
		if nd.messageLog != nil {
			logAttrs := []any{
				"agent_id", subscriber.ID,
				"agent_name", subscriber.Name,
				"notification_id", notif.ID,
			}
			logAttrs = append(logAttrs, structuredMsg.LogAttrs()...)
			nd.messageLog.Info("notification message dispatched", logAttrs...)
		}
	}

	// Mark dispatched regardless of success (best-effort)
	if err := nd.store.MarkNotificationDispatched(ctx, notif.ID); err != nil {
		nd.log.Error("Failed to mark notification dispatched", "notificationID", notif.ID, "error", err)
	}
}

// notificationMessageType returns the structured message type for a notification status.
func notificationMessageType(status string) string {
	if strings.EqualFold(status, "WAITING_FOR_INPUT") {
		return messages.TypeInputNeeded
	}
	return messages.TypeStateChange
}

// formatNotificationMessage formats a notification message based on agent state and status.
func formatNotificationMessage(agent *store.Agent, status string) string {
	upper := strings.ToUpper(status)
	switch upper {
	case "COMPLETED":
		msg := fmt.Sprintf("%s has reached a state of COMPLETED", agent.Slug)
		if agent.TaskSummary != "" {
			msg += ": " + agent.TaskSummary
		}
		return msg
	case "WAITING_FOR_INPUT":
		msg := fmt.Sprintf("%s is WAITING_FOR_INPUT", agent.Slug)
		if agent.Message != "" {
			msg += ": " + agent.Message
		}
		return msg
	case "LIMITS_EXCEEDED":
		msg := fmt.Sprintf("%s has reached a state of LIMITS_EXCEEDED", agent.Slug)
		if agent.Message != "" {
			msg += ": " + agent.Message
		}
		return msg
	case "STALLED":
		msg := fmt.Sprintf("%s has STALLED", agent.Slug)
		if agent.StalledFromActivity != "" {
			msg += " (was " + agent.StalledFromActivity + ")"
		}
		if agent.Message != "" {
			msg += ": " + agent.Message
		}
		return msg
	case "ERROR":
		msg := fmt.Sprintf("%s has reached a state of ERROR", agent.Slug)
		if agent.Message != "" {
			msg += ": " + agent.Message
		}
		return msg
	default:
		return fmt.Sprintf("%s has reached status: %s", agent.Slug, upper)
	}
}
