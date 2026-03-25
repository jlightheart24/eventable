// Package web provides the HTTP UI for managing monitored Facebook Pages.
package web

import (
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jlightheart24/eventable/event"
	"github.com/jlightheart24/eventable/store"
)

// Status holds live information about the most recent poll run.
// It is safe for concurrent access.
type Status struct {
	mu        sync.RWMutex
	lastPoll  time.Time
	newEvents int
}

// Set records the result of a completed poll.
func (s *Status) Set(t time.Time, newEvents int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPoll = t
	s.newEvents = newEvents
}

func (s *Status) snapshot() (time.Time, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastPoll, s.newEvents
}

// Server serves the management UI.
type Server struct {
	store       *store.Store
	accessToken string
	status      *Status
	tmpl        *template.Template
}

// New creates a Server. status may be updated concurrently by the monitor.
func New(st *store.Store, accessToken string, status *Status) *Server {
	return &Server{
		store:       st,
		accessToken: accessToken,
		status:      status,
		tmpl:        template.Must(template.New("ui").Parse(uiHTML)),
	}
}

// Handler returns the HTTP mux for the web UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/add", s.handleAdd)
	mux.HandleFunc("/remove", s.handleRemove)
	return mux
}

// --- template data types -----------------------------------------------

type pageData struct {
	Pages     []store.Page
	LastPoll  string
	NewEvents int
	Flash     flashMsg
}

type flashMsg struct {
	Text string
	Kind string // "success" or "error"
}

// --- handlers ----------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	lastPoll, newEvents := s.status.snapshot()
	pollStr := "Not yet"
	if !lastPoll.IsZero() {
		pollStr = lastPoll.Format("Jan 2 at 3:04:05 PM")
	}

	data := pageData{
		Pages:     s.store.List(),
		LastPoll:  pollStr,
		NewEvents: newEvents,
	}

	q := r.URL.Query()
	if msg := q.Get("msg"); msg != "" {
		data.Flash = flashMsg{Text: msg, Kind: q.Get("kind")}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, data); err != nil {
		log.Printf("web: template error: %v", err)
	}
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pageID := strings.TrimSpace(r.FormValue("page_id"))
	if pageID == "" {
		redirect(w, r, "Page ID is required", "error")
		return
	}

	name, err := event.ResolveName(pageID, s.accessToken)
	if err != nil {
		redirect(w, r, "Could not find page: "+err.Error(), "error")
		return
	}

	if err := s.store.Add(store.Page{ID: pageID, Name: name}); err != nil {
		redirect(w, r, err.Error(), "error")
		return
	}

	redirect(w, r, name+" added successfully", "success")
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pageID := r.FormValue("page_id")
	if err := s.store.Remove(pageID); err != nil {
		redirect(w, r, err.Error(), "error")
		return
	}
	redirect(w, r, "Page removed", "success")
}

func redirect(w http.ResponseWriter, r *http.Request, msg, kind string) {
	http.Redirect(w, r,
		"/?msg="+url.QueryEscape(msg)+"&kind="+kind,
		http.StatusSeeOther)
}

// --- HTML template -----------------------------------------------------

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Eventable</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #f0f2f5;
      color: #1c1e21;
      line-height: 1.5;
    }
    header {
      background: #1877f2;
      color: #fff;
      padding: 0.9rem 2rem;
    }
    header h1 { font-size: 1.4rem; font-weight: 700; letter-spacing: -0.02em; }
    header p  { font-size: 0.8rem; opacity: 0.8; margin-top: 0.1rem; }
    main { max-width: 880px; margin: 2rem auto; padding: 0 1rem; }

    .flash {
      padding: 0.75rem 1rem;
      border-radius: 6px;
      margin-bottom: 1.25rem;
      font-size: 0.9rem;
      font-weight: 500;
    }
    .flash.success { background: #d4edda; color: #155724; border: 1px solid #b8dfc6; }
    .flash.error   { background: #f8d7da; color: #721c24; border: 1px solid #f1b8bc; }

    .card {
      background: #fff;
      border-radius: 8px;
      box-shadow: 0 1px 3px rgba(0,0,0,0.08);
      padding: 1.5rem;
      margin-bottom: 1.5rem;
    }
    .card-title {
      font-size: 0.75rem;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.07em;
      color: #65676b;
      margin-bottom: 1rem;
    }

    .stats { display: flex; gap: 2.5rem; }
    .stat-value { font-size: 1.6rem; font-weight: 700; color: #1877f2; }
    .stat-label { font-size: 0.78rem; color: #65676b; margin-top: 0.1rem; }

    table { width: 100%; border-collapse: collapse; }
    th {
      text-align: left;
      font-size: 0.75rem;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.06em;
      color: #65676b;
      padding: 0.5rem 0.75rem;
      border-bottom: 2px solid #f0f2f5;
    }
    td { padding: 0.75rem; border-bottom: 1px solid #f0f2f5; vertical-align: middle; }
    tr:last-child td { border-bottom: none; }
    .name { font-weight: 600; }
    .pid  { font-family: monospace; font-size: 0.85rem; color: #65676b; }
    .fb-link { font-size: 0.8rem; color: #1877f2; text-decoration: none; }
    .fb-link:hover { text-decoration: underline; }

    .btn-remove {
      background: none;
      border: 1px solid #ddd;
      color: #fa383e;
      padding: 0.3rem 0.7rem;
      border-radius: 5px;
      cursor: pointer;
      font-size: 0.8rem;
      white-space: nowrap;
    }
    .btn-remove:hover { background: #fff0f0; border-color: #fa383e; }

    .empty { text-align: center; padding: 2.5rem 1rem; color: #aaa; font-size: 0.9rem; }

    .add-row { display: flex; gap: 0.75rem; align-items: flex-end; flex-wrap: wrap; }
    .field   { display: flex; flex-direction: column; flex: 1; min-width: 220px; }
    label    { font-size: 0.82rem; font-weight: 600; color: #444; margin-bottom: 0.35rem; }
    input[type="text"] {
      border: 1px solid #ccd0d5;
      border-radius: 6px;
      padding: 0.6rem 0.85rem;
      font-size: 0.95rem;
    }
    input[type="text"]:focus {
      outline: none;
      border-color: #1877f2;
      box-shadow: 0 0 0 3px rgba(24,119,242,0.15);
    }
    .hint { font-size: 0.75rem; color: #8a8d91; margin-top: 0.3rem; }
    .btn-add {
      background: #1877f2;
      color: #fff;
      border: none;
      padding: 0.62rem 1.3rem;
      border-radius: 6px;
      font-size: 0.95rem;
      font-weight: 600;
      cursor: pointer;
      white-space: nowrap;
    }
    .btn-add:hover { background: #1664d8; }
  </style>
</head>
<body>
<header>
  <h1>Eventable</h1>
  <p>Facebook Page Event Monitor</p>
</header>
<main>

  {{if .Flash.Text}}
  <div class="flash {{.Flash.Kind}}">{{.Flash.Text}}</div>
  {{end}}

  <div class="card">
    <div class="card-title">Monitor Status</div>
    <div class="stats">
      <div>
        <div class="stat-value">{{len .Pages}}</div>
        <div class="stat-label">Pages monitored</div>
      </div>
      <div>
        <div class="stat-value">{{.LastPoll}}</div>
        <div class="stat-label">Last poll</div>
      </div>
      <div>
        <div class="stat-value">{{.NewEvents}}</div>
        <div class="stat-label">New events (last poll)</div>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-title">Monitored Pages ({{len .Pages}})</div>
    {{if .Pages}}
    <table>
      <thead>
        <tr>
          <th>Page Name</th>
          <th>Page ID</th>
          <th>Link</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {{range .Pages}}
        <tr>
          <td class="name">{{.Name}}</td>
          <td class="pid">{{.ID}}</td>
          <td>
            <a class="fb-link" href="https://www.facebook.com/{{.ID}}" target="_blank" rel="noopener">
              View Page
            </a>
          </td>
          <td>
            <form method="POST" action="/remove"
                  onsubmit="return confirm('Stop monitoring {{.Name}}?')">
              <input type="hidden" name="page_id" value="{{.ID}}">
              <button type="submit" class="btn-remove">Remove</button>
            </form>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
    <div class="empty">No pages added yet. Use the form below to start monitoring.</div>
    {{end}}
  </div>

  <div class="card">
    <div class="card-title">Add a Page</div>
    <form method="POST" action="/add" class="add-row">
      <div class="field">
        <label for="page_id">Facebook Page ID or username</label>
        <input type="text" id="page_id" name="page_id"
               placeholder="e.g. 123456789012345 or cocacola" required>
        <span class="hint">
          Find the numeric ID via the Graph API Explorer, or enter the page's
          username from its URL (facebook.com/username).
        </span>
      </div>
      <button type="submit" class="btn-add">Add Page</button>
    </form>
  </div>

</main>
</body>
</html>`
