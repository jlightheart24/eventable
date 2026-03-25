// Command monitor polls configured Facebook Pages every 15 minutes and sends
// a Gmail notification for any newly discovered events. A web UI on :8080
// (or $WEB_PORT) lets you add and remove monitored pages at runtime.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jlightheart24/eventable/event"
	"github.com/jlightheart24/eventable/notifier"
	"github.com/jlightheart24/eventable/store"
	"github.com/jlightheart24/eventable/web"
)

const (
	pollInterval   = 15 * time.Minute
	seenEventsFile = "seen_events.json"
)

func main() {
	accessToken := mustEnv("FB_ACCESS_TOKEN")
	notifyCfg := notifier.Config{
		GmailUser:     mustEnv("GMAIL_USER"),
		GmailPassword: mustEnv("GMAIL_APP_PASSWORD"),
		NotifyEmail:   mustEnv("NOTIFY_EMAIL"),
	}
	webPort := envOr("WEB_PORT", "8080")

	pageStore, err := store.New(store.DefaultPath)
	if err != nil {
		log.Fatalf("loading page store: %v", err)
	}

	status := &web.Status{}
	srv := web.New(pageStore, accessToken, status)

	go func() {
		addr := ":" + webPort
		log.Printf("Web UI → http://localhost%s", addr)
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			log.Fatalf("web server: %v", err)
		}
	}()

	seen := loadSeen()
	log.Printf("Eventable monitor starting — polling every %s", pollInterval)

	// Run immediately, then on each tick.
	runOnce(accessToken, notifyCfg, pageStore, seen, status)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for range ticker.C {
		runOnce(accessToken, notifyCfg, pageStore, seen, status)
	}
}

// runOnce performs a single poll cycle.
func runOnce(
	accessToken string,
	notifyCfg notifier.Config,
	pageStore *store.Store,
	seen seenSet,
	status *web.Status,
) {
	pages := pageStore.List()
	if len(pages) == 0 {
		log.Println("No pages configured — add some via the web UI")
		status.Set(time.Now(), 0)
		fmt.Printf("Next check at %s\n", time.Now().Add(pollInterval).Format("15:04:05"))
		return
	}

	log.Printf("Polling %d page(s) via batch API...", len(pages))

	pageInfos := make([]event.PageInfo, len(pages))
	for i, p := range pages {
		pageInfos[i] = event.PageInfo{ID: p.ID, Name: p.Name}
	}

	events, err := event.FetchAllBatch(pageInfos, accessToken)
	if err != nil {
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
	status.Set(time.Now(), len(newEvents))

	if len(newEvents) > 0 {
		if err := notifier.Send(notifyCfg, newEvents); err != nil {
			log.Printf("error sending notification: %v", err)
		} else {
			log.Printf("Notification sent for %d new event(s)", len(newEvents))
		}
		saveSeen(seen)
	}

	fmt.Printf("Next check at %s\n", time.Now().Add(pollInterval).Format("15:04:05"))
}

// --- Seen-events persistence -------------------------------------------

type seenSet map[string]struct{}

func loadSeen() seenSet {
	data, err := os.ReadFile(seenEventsFile)
	if err != nil {
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

// --- Env helpers -------------------------------------------------------

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
