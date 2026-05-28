package usagehistory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Store manages appending usage records to daily JSONL files.
type Store struct {
	mu          sync.Mutex
	dir         string
	currentDate string
	file        *os.File
	encoder     *json.Encoder
}

// NewStore creates a Store that writes JSONL files to dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Write appends a record to the current day's JSONL file.
// It rotates the file when the date changes.
func (s *Store) Write(record JSONLRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	today := record.Timestamp.Format("2006-01-02")
	if today == "" {
		today = time.Now().Format("2006-01-02")
	}

	if s.currentDate != today || s.file == nil {
		if err := s.rotate(today); err != nil {
			return err
		}
	}

	return s.encoder.Encode(&record)
}

// Close closes the current open file.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		s.encoder = nil
		return err
	}
	return nil
}

// rotate closes the current file and opens a new one for the given date.
func (s *Store) rotate(date string) error {
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			log.WithError(err).Warn("usagehistory: failed to close previous file")
		}
	}

	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}

	path := filepath.Join(s.dir, "usage-"+date+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	s.file = f
	s.encoder = json.NewEncoder(f)
	s.currentDate = date
	return nil
}
