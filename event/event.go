// Package event fetches upcoming calendar events from Facebook Pages
// via the Facebook Graph API v19.0.
package event

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	graphBase    = "https://graph.facebook.com/v19.0"
	eventFields  = "id,name,start_time,end_time,place,description,cover"
	timeFormat   = "2006-01-02T15:04:05-0700"
	displayFormat = "Mon, Jan 2 2006 at 3:04 PM"
)

// Event represents a single Facebook calendar event.
type Event struct {
	ID          string
	PageID      string
	PageName    string
	Title       string
	StartTime   time.Time
	EndTime     time.Time
	Location    string
	Description string
	Link        string
}

// graphEvent is the raw JSON shape returned by the Graph API.
type graphEvent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	Description string `json:"description"`
	Place       *struct {
		Name string `json:"name"`
	} `json:"place"`
}

type eventsResponse struct {
	Data   []graphEvent `json:"data"`
	Paging *struct {
		Next string `json:"next"`
	} `json:"paging"`
}

type pageResponse struct {
	Name string `json:"name"`
}

// parseTime attempts to parse a Facebook timestamp string, which may or may
// not include a time component.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// Try full datetime first.
	if t, err := time.Parse(timeFormat, s); err == nil {
		return t
	}
	// Fall back to date-only.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

// pageName resolves a Facebook Page ID to its display name.
func pageName(pageID, accessToken string) string {
	u := fmt.Sprintf("%s/%s?fields=name&access_token=%s", graphBase, pageID, url.QueryEscape(accessToken))
	resp, err := http.Get(u) //nolint:noctx
	if err != nil {
		return pageID
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var pr pageResponse
	if err := json.Unmarshal(body, &pr); err != nil || pr.Name == "" {
		return pageID
	}
	return pr.Name
}

// FetchPage retrieves all upcoming events for a single Facebook Page.
// It follows pagination cursors until all events have been collected.
func FetchPage(pageID, accessToken string) ([]Event, error) {
	name := pageName(pageID, accessToken)

	now := time.Now().UTC().Format(time.RFC3339)
	startURL := fmt.Sprintf(
		"%s/%s/events?fields=%s&since=%s&access_token=%s",
		graphBase,
		pageID,
		url.QueryEscape(eventFields),
		url.QueryEscape(now),
		url.QueryEscape(accessToken),
	)

	var events []Event
	nextURL := startURL

	for nextURL != "" {
		resp, err := http.Get(nextURL) //nolint:noctx
		if err != nil {
			return nil, fmt.Errorf("fetching events for page %s: %w", pageID, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("graph API returned %d for page %s: %s", resp.StatusCode, pageID, body)
		}

		var er eventsResponse
		if err := json.Unmarshal(body, &er); err != nil {
			return nil, fmt.Errorf("decoding events for page %s: %w", pageID, err)
		}

		for _, ge := range er.Data {
			e := Event{
				ID:          ge.ID,
				PageID:      pageID,
				PageName:    name,
				Title:       ge.Name,
				StartTime:   parseTime(ge.StartTime),
				EndTime:     parseTime(ge.EndTime),
				Description: ge.Description,
				Link:        fmt.Sprintf("https://www.facebook.com/events/%s", ge.ID),
			}
			if ge.Place != nil {
				e.Location = ge.Place.Name
			}
			events = append(events, e)
		}

		nextURL = ""
		if er.Paging != nil {
			nextURL = er.Paging.Next
		}
	}

	return events, nil
}

// FetchAll retrieves upcoming events from all provided Facebook Page IDs.
// Errors for individual pages are collected and returned together; successfully
// fetched pages are still included in the result.
func FetchAll(pageIDs []string, accessToken string) ([]Event, error) {
	var (
		all    []Event
		errs   []error
	)
	for _, id := range pageIDs {
		events, err := FetchPage(id, accessToken)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		all = append(all, events...)
	}
	if len(errs) > 0 {
		msgs := ""
		for _, e := range errs {
			msgs += e.Error() + "; "
		}
		return all, fmt.Errorf("errors fetching pages: %s", msgs)
	}
	return all, nil
}

// FormatTime returns a human-readable representation of a parsed event time.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "TBD"
	}
	return t.Format(displayFormat)
}
