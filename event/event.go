// Package event fetches upcoming calendar events from Facebook Pages
// via the Facebook Graph API v19.0.
package event

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	graphBase     = "https://graph.facebook.com/v19.0"
	eventFields   = "id,name,start_time,end_time,place,description"
	timeFormat    = "2006-01-02T15:04:05-0700"
	displayFormat = "Mon, Jan 2 2006 at 3:04 PM"
	batchSize     = 50
)

// PageInfo associates a Facebook Page ID with its display name.
// Use this when the name has already been resolved (e.g. from the store)
// so FetchAllBatch does not need an extra API call per page.
type PageInfo struct {
	ID   string
	Name string
}

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

// --- JSON shapes -------------------------------------------------------

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

type pageNameResponse struct {
	Name  string `json:"name"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type batchRequest struct {
	Method      string `json:"method"`
	RelativeURL string `json:"relative_url"`
}

type batchResponseItem struct {
	Code int    `json:"code"`
	Body string `json:"body"`
}

// --- Helpers -----------------------------------------------------------

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(timeFormat, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

func toEvent(ge graphEvent, pageID, pageName string) Event {
	e := Event{
		ID:          ge.ID,
		PageID:      pageID,
		PageName:    pageName,
		Title:       ge.Name,
		StartTime:   parseTime(ge.StartTime),
		EndTime:     parseTime(ge.EndTime),
		Description: ge.Description,
		Link:        fmt.Sprintf("https://www.facebook.com/events/%s", ge.ID),
	}
	if ge.Place != nil {
		e.Location = ge.Place.Name
	}
	return e
}

// FormatTime returns a human-readable representation of an event time.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "TBD"
	}
	return t.Format(displayFormat)
}

// --- Name resolution ---------------------------------------------------

// ResolveName looks up the display name of a Facebook Page by its ID or
// username, validating that it exists and is accessible with the given token.
func ResolveName(pageID, accessToken string) (string, error) {
	u := fmt.Sprintf("%s/%s?fields=name&access_token=%s",
		graphBase, pageID, url.QueryEscape(accessToken))

	resp, err := http.Get(u) //nolint:noctx
	if err != nil {
		return "", fmt.Errorf("contacting Graph API: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var pr pageNameResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return "", fmt.Errorf("decoding page response: %w", err)
	}
	if pr.Error != nil {
		return "", fmt.Errorf("Graph API: %s", pr.Error.Message)
	}
	if pr.Name == "" {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	return pr.Name, nil
}

// resolveName is the internal version that falls back to pageID on error.
func resolveName(pageID, accessToken string) string {
	name, err := ResolveName(pageID, accessToken)
	if err != nil {
		return pageID
	}
	return name
}

// --- Single-page fetch (used internally and for one-off checks) --------

// FetchPage retrieves all upcoming events for a single Facebook Page.
func FetchPage(pageID, accessToken string) ([]Event, error) {
	name := resolveName(pageID, accessToken)
	return fetchPageWithName(pageID, name, accessToken)
}

func fetchPageWithName(pageID, name, accessToken string) ([]Event, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	nextURL := fmt.Sprintf(
		"%s/%s/events?fields=%s&since=%s&limit=100&access_token=%s",
		graphBase,
		pageID,
		url.QueryEscape(eventFields),
		url.QueryEscape(now),
		url.QueryEscape(accessToken),
	)

	var events []Event
	for nextURL != "" {
		resp, err := http.Get(nextURL) //nolint:noctx
		if err != nil {
			return nil, fmt.Errorf("fetching events for page %s: %w", pageID, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("graph API %d for page %s: %s", resp.StatusCode, pageID, body)
		}

		var er eventsResponse
		if err := json.Unmarshal(body, &er); err != nil {
			return nil, fmt.Errorf("decoding events for page %s: %w", pageID, err)
		}
		for _, ge := range er.Data {
			events = append(events, toEvent(ge, pageID, name))
		}

		nextURL = ""
		if er.Paging != nil {
			nextURL = er.Paging.Next
		}
	}
	return events, nil
}

// FetchAll fetches events sequentially. For large numbers of pages prefer
// FetchAllBatch, which is far more efficient.
func FetchAll(pageIDs []string, accessToken string) ([]Event, error) {
	var all []Event
	var errs []string
	for _, id := range pageIDs {
		events, err := FetchPage(id, accessToken)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		all = append(all, events...)
	}
	if len(errs) > 0 {
		return all, fmt.Errorf("errors fetching pages: %s", strings.Join(errs, "; "))
	}
	return all, nil
}

// --- Batch fetch -------------------------------------------------------

// FetchAllBatch retrieves upcoming events for all pages using the Graph API
// batch endpoint (up to 50 sub-requests per HTTP call). Page names are read
// from PageInfo so no extra API call per page is needed.
func FetchAllBatch(pages []PageInfo, accessToken string) ([]Event, error) {
	if len(pages) == 0 {
		return nil, nil
	}

	now := url.QueryEscape(time.Now().UTC().Format(time.RFC3339))
	fields := url.QueryEscape(eventFields)

	var all []Event
	var errs []string

	for i := 0; i < len(pages); i += batchSize {
		end := i + batchSize
		if end > len(pages) {
			end = len(pages)
		}
		chunk := pages[i:end]

		batch := make([]batchRequest, len(chunk))
		for j, p := range chunk {
			batch[j] = batchRequest{
				Method:      "GET",
				RelativeURL: fmt.Sprintf("%s/events?fields=%s&since=%s&limit=100", p.ID, fields, now),
			}
		}

		batchJSON, err := json.Marshal(batch)
		if err != nil {
			return all, fmt.Errorf("marshalling batch: %w", err)
		}

		form := url.Values{}
		form.Set("access_token", accessToken)
		form.Set("include_headers", "false")
		form.Set("batch", string(batchJSON))

		resp, err := http.PostForm(graphBase, form)
		if err != nil {
			errs = append(errs, fmt.Sprintf("batch POST failed: %v", err))
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var items []batchResponseItem
		if err := json.Unmarshal(body, &items); err != nil {
			errs = append(errs, fmt.Sprintf("decoding batch response: %v", err))
			continue
		}

		for j, item := range items {
			if j >= len(chunk) {
				break
			}
			page := chunk[j]
			if item.Code != http.StatusOK {
				errs = append(errs, fmt.Sprintf("page %s returned HTTP %d", page.ID, item.Code))
				continue
			}
			var er eventsResponse
			if err := json.Unmarshal([]byte(item.Body), &er); err != nil {
				errs = append(errs, fmt.Sprintf("decoding events for page %s: %v", page.ID, err))
				continue
			}
			for _, ge := range er.Data {
				all = append(all, toEvent(ge, page.ID, page.Name))
			}
		}
	}

	if len(errs) > 0 {
		return all, fmt.Errorf("batch errors: %s", strings.Join(errs, "; "))
	}
	return all, nil
}
