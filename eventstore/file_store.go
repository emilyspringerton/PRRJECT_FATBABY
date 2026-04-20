package eventstore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	eventsDirName    = "events"
	stateDirName     = "state"
	latestSeqStateFn = "latest-sequence"
)

type FileStore struct {
	rootDir   string
	eventsDir string
	stateDir  string
	clock     func() time.Time

	mu      sync.Mutex
	latest  uint64
	closed  bool
	current *os.File
}

func NewFileStore(rootDir string) (*FileStore, error) {
	if rootDir == "" {
		return nil, errors.New("rootDir is required")
	}
	store := &FileStore{
		rootDir:   rootDir,
		eventsDir: filepath.Join(rootDir, eventsDirName),
		stateDir:  filepath.Join(rootDir, stateDirName),
		clock: func() time.Time {
			return time.Now().UTC()
		},
	}

	if err := os.MkdirAll(store.eventsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create events dir: %w", err)
	}
	if err := os.MkdirAll(store.stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	latest, err := store.recoverLatestSequence()
	if err != nil {
		return nil, err
	}
	store.latest = latest
	if err := store.persistLatestSequence(latest); err != nil {
		return nil, err
	}

	if err := store.openTodayJournal(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileStore) Append(ctx context.Context, events ...Event) ([]Record, error) {
	if len(events) == 0 {
		return nil, ErrEmptyAppend
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	normalized := make([]Event, len(events))
	for i, ev := range events {
		ne, err := normalizeAndValidateEvent(ev)
		if err != nil {
			return nil, err
		}
		normalized[i] = ne
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	if err := s.rotateIfDateChanged(); err != nil {
		return nil, err
	}

	records := make([]Record, len(normalized))
	for i, ev := range normalized {
		s.latest++
		records[i] = Record{Sequence: s.latest, Event: ev, AppendedAt: s.clock()}
		line, err := json.Marshal(records[i])
		if err != nil {
			s.latest--
			return nil, fmt.Errorf("marshal record: %w", err)
		}
		if _, err := s.current.Write(append(line, '\n')); err != nil {
			s.latest--
			return nil, fmt.Errorf("write record: %w", err)
		}
	}
	if err := s.current.Sync(); err != nil {
		return nil, fmt.Errorf("sync journal: %w", err)
	}
	if err := s.persistLatestSequence(s.latest); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *FileStore) ReadFrom(ctx context.Context, fromSequence uint64, limit int) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 0 {
		return nil, errors.New("limit must be >= 0")
	}
	if limit == 0 {
		return []Record{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}

	paths, err := s.journalPaths()
	if err != nil {
		return nil, err
	}

	out := make([]Record, 0, min(limit, 128))
	for _, p := range paths {
		if len(out) >= limit {
			break
		}
		recs, err := readRecordsFromFile(p)
		if err != nil {
			return nil, err
		}
		for _, rec := range recs {
			if rec.Sequence >= fromSequence {
				out = append(out, rec)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out, nil
}

func (s *FileStore) LatestSequence(ctx context.Context) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	return s.latest, nil
}

func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.current == nil {
		return nil
	}
	return s.current.Close()
}

func (s *FileStore) recoverLatestSequence() (uint64, error) {
	stateVal, err := s.readLatestFromState()
	if err == nil {
		return stateVal, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	return s.scanLatestFromJournals()
}

func (s *FileStore) readLatestFromState() (uint64, error) {
	b, err := os.ReadFile(filepath.Join(s.stateDir, latestSeqStateFn))
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse latest sequence state: %w", err)
	}
	return v, nil
}

func (s *FileStore) scanLatestFromJournals() (uint64, error) {
	paths, err := s.journalPaths()
	if err != nil {
		return 0, err
	}
	var latest uint64
	for _, p := range paths {
		recs, err := readRecordsFromFile(p)
		if err != nil {
			return 0, err
		}
		if len(recs) > 0 && recs[len(recs)-1].Sequence > latest {
			latest = recs[len(recs)-1].Sequence
		}
	}
	return latest, nil
}

func (s *FileStore) journalPaths() ([]string, error) {
	entries, err := os.ReadDir(s.eventsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read events dir: %w", err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".ndjson") {
			continue
		}
		paths = append(paths, filepath.Join(s.eventsDir, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *FileStore) persistLatestSequence(latest uint64) error {
	tmpPath := filepath.Join(s.stateDir, latestSeqStateFn+".tmp")
	finalPath := filepath.Join(s.stateDir, latestSeqStateFn)
	if err := os.WriteFile(tmpPath, []byte(strconv.FormatUint(latest, 10)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write latest sequence temp file: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename latest sequence state file: %w", err)
	}
	return nil
}

func (s *FileStore) journalPathForDate(date time.Time) string {
	return filepath.Join(s.eventsDir, date.Format("2006-01-02")+".ndjson")
}

func (s *FileStore) openTodayJournal() error {
	path := s.journalPathForDate(s.clock())
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open journal file: %w", err)
	}
	s.current = f
	return nil
}

func (s *FileStore) rotateIfDateChanged() error {
	if s.current == nil {
		return s.openTodayJournal()
	}
	want := s.journalPathForDate(s.clock())
	if filepath.Clean(s.current.Name()) == filepath.Clean(want) {
		return nil
	}
	if err := s.current.Close(); err != nil {
		return err
	}
	s.current = nil
	return s.openTodayJournal()
}

func readRecordsFromFile(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open journal file %s: %w", path, err)
	}
	defer f.Close()

	recs := make([]Record, 0)
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = []byte(strings.TrimSpace(string(line)))
			if len(line) > 0 {
				var rec Record
				if uErr := json.Unmarshal(line, &rec); uErr != nil {
					if errors.Is(err, io.EOF) {
						// Truncated or partial trailing line. Ignore it for restart-safety.
						break
					}
					return nil, fmt.Errorf("unmarshal journal record in %s: %w", path, uErr)
				}
				recs = append(recs, rec)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read journal file %s: %w", path, err)
		}
	}
	return recs, nil
}
