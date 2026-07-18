package eventstore

import (
	"bufio"
	"bytes"
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

	mu          sync.Mutex
	latest      uint64
	closed      bool
	current     *os.File
	fileMaxSeq  map[string]uint64 // closed-file max-sequence cache: skip files entirely behind cursor

	scanMu      sync.Mutex
	tailCursors map[string]tailCursor // current-journal resume cache, keyed by clean path
}

// tailCursor lets a repeated Scan against the still-growing current journal
// resume from where the previous call left off instead of re-reading the
// whole file. It is only a forward-progress optimization: any Scan call
// requesting a fromSeq at or behind lastSeq falls back to reading from the
// start of the file for correctness.
type tailCursor struct {
	offset  int64  // bytes already streamed from this file
	lastSeq uint64 // highest record sequence seen at that offset
}

func NewFileStore(rootDir string) (*FileStore, error) {
	if rootDir == "" {
		return nil, errors.New("rootDir is required")
	}
	store := &FileStore{
		rootDir:    rootDir,
		eventsDir:  filepath.Join(rootDir, eventsDirName),
		stateDir:   filepath.Join(rootDir, stateDirName),
		fileMaxSeq: make(map[string]uint64),
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

	// currentPath is the active journal; its max sequence changes with each Append
	// so we never cache it — always read it fully.
	currentPath := ""
	if s.current != nil {
		currentPath = filepath.Clean(s.current.Name())
	}

	out := make([]Record, 0, min(limit, 128))
	for _, p := range paths {
		if len(out) >= limit {
			break
		}
		// Skip closed files whose cached max sequence is entirely before our cursor.
		cleanP := filepath.Clean(p)
		if cleanP != currentPath {
			if maxSeq, ok := s.fileMaxSeq[cleanP]; ok && maxSeq < fromSequence {
				continue
			}
		}
		recs, err := readRecordsFromFile(p)
		if err != nil {
			return nil, err
		}
		// Populate cache for closed (non-current) files.
		if cleanP != currentPath && len(recs) > 0 {
			s.fileMaxSeq[cleanP] = recs[len(recs)-1].Sequence
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

// Scan streams every record with Sequence >= fromSeq to fn, in order, reading
// each journal file exactly once, line by line, without ever materializing a
// whole file in memory. Peak memory is one record, not one file.
//
// The store mutex is held only long enough to snapshot the journal path list,
// the current-journal identity, and the closed-file skip cache — never across
// the actual file reads, so this does not block concurrent Append calls.
// Journal files are append-only, so reading the already-written bytes of a
// file concurrently with an in-progress Append is safe; a partial trailing
// line from a write still in flight is tolerated and simply not consumed
// until it is complete (mirrors readRecordsFromFile's truncated-line
// tolerance).
func (s *FileStore) Scan(ctx context.Context, fromSeq uint64, fn func(Record) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return os.ErrClosed
	}
	paths, err := s.journalPaths()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	currentPath := ""
	if s.current != nil {
		currentPath = filepath.Clean(s.current.Name())
	}
	skip := make(map[string]bool, len(paths))
	for _, p := range paths {
		cleanP := filepath.Clean(p)
		if cleanP == currentPath {
			continue
		}
		if maxSeq, ok := s.fileMaxSeq[cleanP]; ok && maxSeq < fromSeq {
			skip[cleanP] = true
		}
	}
	s.mu.Unlock()

	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		cleanP := filepath.Clean(p)
		if skip[cleanP] {
			continue
		}
		isCurrent := cleanP == currentPath
		lastSeq, err := s.scanFile(ctx, p, cleanP, isCurrent, fromSeq, fn)
		if err != nil {
			return err
		}
		if !isCurrent && lastSeq > 0 {
			s.mu.Lock()
			s.fileMaxSeq[cleanP] = lastSeq
			s.mu.Unlock()
		}
	}
	return nil
}

// scanFile streams one journal file from the given path, invoking fn for
// every record with Sequence >= fromSeq, and returns the highest sequence
// encountered in the file (0 if none). For the current (still-growing)
// journal, it resumes from a cached byte offset when safe to do so, so a
// repeated tail-style Scan call only reads bytes appended since the last
// call instead of re-reading the whole file.
func (s *FileStore) scanFile(ctx context.Context, path, cleanPath string, isCurrent bool, fromSeq uint64, fn func(Record) error) (uint64, error) {
	startOffset := int64(0)
	if isCurrent {
		s.scanMu.Lock()
		if c, ok := s.tailCursors[cleanPath]; ok && fromSeq > c.lastSeq {
			startOffset = c.offset
		}
		s.scanMu.Unlock()
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("open journal file %s: %w", path, err)
	}
	defer f.Close()

	if startOffset > 0 {
		fi, statErr := f.Stat()
		if statErr != nil {
			return 0, fmt.Errorf("stat journal file %s: %w", path, statErr)
		}
		if fi.Size() < startOffset {
			// File is shorter than our cached position — something rotated or
			// reset it unexpectedly. Fall back to a full read for correctness.
			startOffset = 0
		} else if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
			return 0, fmt.Errorf("seek journal file %s: %w", path, err)
		}
	}

	reader := bufio.NewReaderSize(f, 64*1024)
	offset := startOffset
	var lastSeq uint64
	for {
		if err := ctx.Err(); err != nil {
			return lastSeq, err
		}
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return lastSeq, fmt.Errorf("read journal file %s: %w", path, readErr)
		}
		complete := len(line) > 0 && line[len(line)-1] == '\n'
		if complete {
			offset += int64(len(line))
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 {
				var rec Record
				if uErr := json.Unmarshal(trimmed, &rec); uErr != nil {
					return lastSeq, fmt.Errorf("unmarshal journal record in %s: %w", path, uErr)
				}
				if rec.Sequence > lastSeq {
					lastSeq = rec.Sequence
				}
				if rec.Sequence >= fromSeq {
					if err := fn(rec); err != nil {
						return lastSeq, err
					}
				}
			}
		} else if len(line) > 0 {
			// Partial trailing line — either a write still in flight (current
			// file) or a truncated final line from a hard kill. Either way,
			// stop here without consuming or counting it; offset stays put so
			// the next Scan picks it up once it's complete.
			break
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}

	if isCurrent {
		s.scanMu.Lock()
		if s.tailCursors == nil {
			s.tailCursors = make(map[string]tailCursor)
		}
		existing, ok := s.tailCursors[cleanPath]
		if !ok || offset > existing.offset {
			cached := lastSeq
			if ok && existing.lastSeq > cached {
				cached = existing.lastSeq
			}
			s.tailCursors[cleanPath] = tailCursor{offset: offset, lastSeq: cached}
		}
		s.scanMu.Unlock()
	}

	return lastSeq, nil
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
