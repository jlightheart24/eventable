// Package store manages the persisted list of monitored Facebook Pages.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

const DefaultPath = "pages.json"

// Page represents a monitored Facebook Page.
type Page struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Store is a thread-safe, JSON-backed list of monitored pages.
type Store struct {
	mu       sync.RWMutex
	pages    []Page
	filePath string
}

// New loads (or creates) a Store backed by the given file path.
func New(filePath string) (*Store, error) {
	s := &Store{filePath: filePath}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if os.IsNotExist(err) {
		return nil // start empty on first run
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", s.filePath, err)
	}
	return json.Unmarshal(data, &s.pages)
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.pages, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0o600)
}

// List returns a copy of the current page list.
func (s *Store) List() []Page {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Page, len(s.pages))
	copy(out, s.pages)
	return out
}

// Add appends a page, returning an error if it is already present.
func (s *Store) Add(page Page) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.pages {
		if p.ID == page.ID {
			return fmt.Errorf("%s (%s) is already being monitored", page.Name, page.ID)
		}
	}
	s.pages = append(s.pages, page)
	return s.save()
}

// Remove deletes the page with the given ID.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.pages {
		if p.ID == id {
			s.pages = append(s.pages[:i], s.pages[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("page %s not found", id)
}
