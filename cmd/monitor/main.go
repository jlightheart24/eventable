// Command monitor polls configured Facebook Pages every 15 minutes and sends
// a Gmail notification for any newly discovered events.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jlightheart24/eventable/event"
	"github.com/jlightheart24/eventable/notifier"
)

const (
	pollInterval  = 15 * time.Minute
	seenEventsFile = "seen_events.json"
)

func main() {
	cfg := loadConfig()

	log.Printf("Eventable monitor starting — watching %d page(s), polling every %s",
		len(cfg.pageIDs), pollInterval)

	seen := loadSeen()

	// Run immediately on startup, then on each tick.
	runOnce(cfg, seen)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for range ticker.C {
		runOnce(cfg, seen)
	}
}

// appConfig holds all runtime configuration.
type appConfig struct {
	accessToken string
	pageIDs     []string
	notifier    notifier.Config
}

func loadConfig() appConfig {
	get := func(key string) string {
		v := os.Getenv(key)
		if v == "" {
			log.Fatalf("required environment variable %s is not set", key)
		}
		return v
	}

	rawIDs := get("FB_PAGE_IDS")
	var ids []string
	for _, id := range strings.Split(rawIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		log.Fatal("FB_PAGE_IDS must contain at least one page ID")
	}

	return appConfig{
		accessToken: get("FB_ACCESS_TOKEN"),
		pageIDs:     ids,
		notifier: notifier.Config{
			GmailUser:     get("GMAIL_USER"),
			GmailPassword: get("GMAIL_APP_PASSWORD"),
			NotifyEmail:   get("NOTIFY_EMAIL"),
		},
	}
}

// seenSet maps event IDs to struct{} for O(1) lookup.
type seenSet map[string]struct{}

func loadSeen() seenSet {
	data, err := os.ReadFile(seenEventsFile)
	if err != nil {
		// File not found on first run — start fresh.
		return make(seenSet)
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		log.Printf("warn: could not parse %s, starting fresh: %v", seenEventsFile, err)
		return make(seenSet)
	}
	s := make(seenSet, len(ids))
	for _, id := range ids {
		s[id] = struct{}{}
	}
	return s
}

func saveSeen(seen seenSet) {
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	data, err := json.Marshal(ids)
	if err != nil {
		log.Printf("error marshalling seen events: %v", err)
		return
	}
	if err := os.WriteFile(seenEventsFile, data, 0o600); err != nil {
		log.Printf("error saving %s: %v", seenEventsFile, err)
	}
}

func runOnce(cfg appConfig, seen seenSet) {
	log.Println("Fetching events...")

	events, err := event.FetchAll(cfg.pageIDs, cfg.accessToken)
	if err != nil {
		// FetchAll returns partial results alongside errors.
		log.Printf("warn: %v", err)
	}

	var newEvents []event.Event
	for _, e := range events {
		if _, ok := seen[e.ID]; !ok {
			newEvents = append(newEvents, e)
			seen[e.ID] = struct{}{}
		}
	}

	log.Printf("Found %d total event(s), %d new", len(events), len(newEvents))

	if len(newEvents) > 0 {
		if err := notifier.Send(cfg.notifier, newEvents); err != nil {
			log.Printf("error sending notification: %v", err)
		} else {
			log.Printf("Notification sent for %d new event(s)", len(newEvents))
		}
		saveSeen(seen)
	}

	fmt.Printf("Next check at %s\n", time.Now().Add(pollInterval).Format("15:04:05"))
}
