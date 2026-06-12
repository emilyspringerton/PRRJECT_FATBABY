package feedserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/prrject-fatbaby/broker"
	"github.com/example/prrject-fatbaby/eventstore"
)

type SessionConfig struct {
	Conn                                                            net.Conn
	Store                                                           eventstore.EventStore
	Registry                                                        *broker.Registry
	Logger                                                          Logger
	HandshakeTimeout, WriteTimeout, HeartbeatInterval, PollInterval time.Duration
	MaxBufferDepth                                                  int
}
type Session struct {
	cfg                    SessionConfig
	id, tenant             string
	from                   uint64
	frames, bytes, lastAck atomic.Int64
	cancel                 context.CancelFunc
	ctx                    context.Context
	wg                     sync.WaitGroup
	out                    chan []byte
	disconnect             string
	lastSent               atomic.Int64
}

func NewSession(cfg SessionConfig) *Session {
	if cfg.HandshakeTimeout == 0 {
		cfg.HandshakeTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 5 * time.Second
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 15 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.MaxBufferDepth == 0 {
		cfg.MaxBufferDepth = 512
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{cfg: cfg, ctx: ctx, cancel: cancel, out: make(chan []byte, cfg.MaxBufferDepth), disconnect: "clean"}
}

func (s *Session) Run() error {
	start := time.Now()
	if err := s.handshake(); err != nil {
		return err
	}
	s.wg.Add(3)
	go s.writeLoop()
	go s.readLoop()
	go s.tailLoop()
	s.wg.Wait()
	_ = s.cfg.Conn.Close()
	appendFeedSessionEvent(s.cfg.Store, FeedSessionEvent{SessionID: s.id, TenantID: s.tenant, FromSeq: s.from, LastAckedSeq: uint64(s.lastAck.Load()), FramesSent: s.frames.Load(), BytesSent: s.bytes.Load(), DurationMS: time.Since(start).Milliseconds(), DisconnectCode: s.disconnect})
	return nil
}
func (s *Session) Close() { s.cancel(); s.wg.Wait(); _ = s.cfg.Conn.Close() }

func (s *Session) handshake() error {
	_ = s.cfg.Conn.SetDeadline(time.Now().Add(s.cfg.HandshakeTimeout))
	ft, p, err := ReadFrame(bufio.NewReaderSize(s.cfg.Conn, 8192))
	if err != nil {
		return err
	}
	if ft != TypeHello {
		return s.sendError("bad_handshake", "expected hello")
	}
	var h struct {
		Key     string      `json:"key"`
		FromSeq json.Number `json:"from_seq"`
		Filters []string    `json:"filters"`
	}
	if err := json.Unmarshal(p, &h); err != nil {
		return s.sendError("bad_hello", err.Error())
	}
	var tenant broker.RouteTenant
	tenant, err = s.cfg.Registry.ResolveKey(h.Key)
	if err != nil {
		return s.sendError("unauthorized", "invalid key")
	}
	s.tenant = tenant.ID
	latest, _ := s.cfg.Store.LatestSequence(s.ctx)
	mode := "resume"
	next := uint64(1)
	fv, _ := h.FromSeq.Int64()
	switch {
	case fv == 0:
		mode = "backfill"
		next = 1
	case fv == -1 || uint64(fv) == FromSeqRealtimeOnly:
		mode = "realtime"
		next = latest + 1
	default:
		seq := uint64(fv)
		if seq >= latest {
			next = latest + 1
		} else {
			next = seq + 1
		}
	}
	s.from = next
	s.id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	w, _ := json.Marshal(map[string]any{"session_id": s.id, "server_ts": time.Now().UTC().Format(time.RFC3339Nano), "mode": mode, "from_seq": next, "latest_seq": latest})
	_ = s.cfg.Conn.SetDeadline(time.Time{})
	return WriteFrame(s.cfg.Conn, TypeWelcome, w)
}
func (s *Session) sendError(code, msg string) error {
	b, _ := json.Marshal(map[string]any{"code": code, "message": msg})
	_ = WriteFrame(s.cfg.Conn, TypeError, b)
	_ = s.cfg.Conn.Close()
	return fmt.Errorf("%s", msg)
}
func (s *Session) tailLoop() {
	defer s.wg.Done()
	next := s.from
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	filters := map[string]bool{}
	_ = filters
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			recs, _ := s.cfg.Store.ReadFrom(s.ctx, next, 256)
			if len(recs) == 0 {
				if time.Since(time.Unix(0, s.lastSent.Load())) >= s.cfg.HeartbeatInterval {
					hb, _ := json.Marshal(map[string]any{"server_ts": time.Now().UTC().Format(time.RFC3339Nano), "latest_seq": next - 1})
					s.enqueue(TypeHeartbeat, hb)
				}
				continue
			}
			for _, r := range recs {
				payload, _ := json.Marshal(map[string]any{"seq": r.Sequence, "event_type": r.Event.Type, "appended_at": r.AppendedAt.Format(time.RFC3339Nano), "data": json.RawMessage(r.Event.Data)})
				if !s.enqueue(TypeRecord, payload) {
					return
				}
				next = r.Sequence + 1
			}
		}
	}
}
func (s *Session) enqueue(ft FrameType, p []byte) bool {
	var b []byte
	buf := new(bytes.Buffer)
	_ = WriteFrame(buf, ft, p)
	b = buf.Bytes()
	select {
	case s.out <- append([]byte(nil), b...):
		return true
	default:
		s.disconnect = "behind"
		bh, _ := json.Marshal(map[string]any{"seq": s.from, "buffer_depth": len(s.out), "reason": "buffer_full"})
		_ = WriteFrame(s.cfg.Conn, TypeBehind, bh)
		s.cancel()
		_ = s.cfg.Conn.Close()
		return false
	}
}
func (s *Session) writeLoop() {
	defer s.wg.Done()
	w := bufio.NewWriterSize(s.cfg.Conn, 8192)
	for {
		select {
		case <-s.ctx.Done():
			return
		case fr := <-s.out:
			_ = s.cfg.Conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
			if _, err := w.Write(fr); err != nil {
				s.disconnect = "error"
				s.cancel()
				return
			}
			_ = w.Flush()
			s.frames.Add(1)
			s.bytes.Add(int64(len(fr)))
			s.lastSent.Store(time.Now().UnixNano())
		}
	}
}
func (s *Session) readLoop() {
	defer s.wg.Done()
	r := bufio.NewReaderSize(s.cfg.Conn, 8192)
	for {
		ft, p, err := ReadFrame(r)
		if err != nil {
			s.cancel()
			return
		}
		switch ft {
		case TypeAck:
			var a struct {
				Seq uint64 `json:"seq"`
			}
			_ = json.Unmarshal(p, &a)
			s.lastAck.Store(int64(a.Seq))
		case TypePing:
			_ = s.enqueue(TypePong, []byte(`{}`))
		case TypeGoodbye:
			s.cancel()
			return
		default:
			s.disconnect = "error"
			_ = s.sendError("unknown_type", "unknown frame")
			s.cancel()
			return
		}
	}
}
