package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type upstream struct {
	failing atomic.Bool
	mu      sync.Mutex
	got     []string // "METHOD path?query | content-type | token | body"
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if u.failing.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	b, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.got = append(u.got, r.Method+" "+r.URL.RequestURI()+" | "+r.Header.Get("Content-Type")+" | "+
		r.Header.Get("X-ANPR-Token")+" | "+string(b))
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"success":true}`))
}

func (u *upstream) list() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.got...)
}

func newTestForwarder(t *testing.T, serverURL string) *Forwarder {
	t.Helper()
	q, err := NewQueue(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{ServerURL: serverURL, Token: "x", ANPRToken: "secret", QueueMaxAge: time.Hour, QueueMaxItems: 100}
	return NewForwarder(cfg, q, func() Status { return Status{Connected: true} }, "test")
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestForwarderPassesEventsThrough(t *testing.T) {
	up := &upstream{}
	srv := httptest.NewServer(up)
	defer srv.Close()
	f := newTestForwarder(t, srv.URL)

	rec := post(t, f.Handler(), "/api/v1/anpr/hikvision/cam-exit", "<plate>01A777AA</plate>")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("forward: %d %s", rec.Code, rec.Body.String())
	}
	got := up.list()
	want := "POST /api/v1/anpr/hikvision/cam-exit | application/xml | secret | <plate>01A777AA</plate>"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("upstream got %q, want %q", got, want)
	}
	if f.queue.Len() != 0 {
		t.Fatal("delivered event must not be queued")
	}
}

func TestForwarderQueuesWhileBackendIsDownAndReplaysInOrder(t *testing.T) {
	up := &upstream{}
	up.failing.Store(true)
	srv := httptest.NewServer(up)
	defer srv.Close()
	f := newTestForwarder(t, srv.URL)

	for _, plate := range []string{"ENTRY-1", "EXIT-2"} {
		rec := post(t, f.Handler(), "/api/v1/anpr/hikvision/cam-exit", plate)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"queued":true`) {
			t.Fatalf("queue: %d %s", rec.Code, rec.Body.String())
		}
	}
	if f.queue.Len() != 2 {
		t.Fatalf("queue len = %d, want 2", f.queue.Len())
	}

	f.replayOnce(context.Background()) // still down: nothing delivered, nothing lost
	if f.queue.Len() != 2 || len(up.list()) != 0 {
		t.Fatal("replay while down must keep the queue")
	}

	up.failing.Store(false)
	f.replayOnce(context.Background())
	got := up.list()
	if f.queue.Len() != 0 || len(got) != 2 || !strings.HasSuffix(got[0], "ENTRY-1") || !strings.HasSuffix(got[1], "EXIT-2") {
		t.Fatalf("replay: queue=%d got=%q", f.queue.Len(), got)
	}
}

func TestForwarderDropsStaleEvents(t *testing.T) {
	up := &upstream{}
	srv := httptest.NewServer(up)
	defer srv.Close()
	f := newTestForwarder(t, srv.URL)
	f.cfg.QueueMaxAge = time.Minute

	if err := f.queue.Put(QueuedEvent{Method: http.MethodPost, PathQuery: "/api/v1/anpr/event",
		ReceivedAt: time.Now().Add(-2 * time.Minute), Body: []byte("old")}); err != nil {
		t.Fatal(err)
	}
	f.replayOnce(context.Background())
	if f.queue.Len() != 0 || len(up.list()) != 0 {
		t.Fatalf("stale event must be dropped, not sent: queue=%d sent=%d", f.queue.Len(), len(up.list()))
	}
}

func TestQueueCapDropsOldest(t *testing.T) {
	q, err := NewQueue(t.TempDir(), 2)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	for i, body := range []string{"a", "b", "c"} {
		if err := q.Put(QueuedEvent{ReceivedAt: base.Add(time.Duration(i) * time.Second), Body: []byte(body)}); err != nil {
			t.Fatal(err)
		}
	}
	names := q.List()
	first, _ := q.Get(names[0])
	if len(names) != 2 || string(first.Body) != "b" {
		t.Fatalf("queue after cap: %v first=%q", names, first.Body)
	}
}

func TestWSURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://parking.example.uz":     "wss://parking.example.uz/api/v1/agent/ws",
		"http://10.0.0.5:8085":           "ws://10.0.0.5:8085/api/v1/agent/ws",
		"https://example.uz/parking-api": "wss://example.uz/parking-api/api/v1/agent/ws",
	} {
		if got := (Config{ServerURL: in}).WSURL(); got != want {
			t.Errorf("WSURL(%q) = %q, want %q", in, got, want)
		}
	}
}
