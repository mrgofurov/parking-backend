package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxEventBody: Hikvision ANPR events carry JPEG snapshots.
const maxEventBody = 20 << 20

// Forwarder receives camera / capture-service events on the local network
// and passes them to the backend under the same path. POSTs that cannot be
// delivered (no internet, backend down) are queued on disk and replayed.
type Forwarder struct {
	cfg    Config
	http   *http.Client
	queue  *Queue
	status func() Status
	start  time.Time
	ver    string
}

func NewForwarder(cfg Config, queue *Queue, status func() Status, version string) *Forwarder {
	return &Forwarder{
		cfg:    cfg,
		http:   &http.Client{Timeout: 15 * time.Second},
		queue:  queue,
		status: status,
		start:  time.Now(),
		ver:    version,
	}
}

func (f *Forwarder) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", f.health)
	mux.HandleFunc("/api/v1/anpr/", f.forward)
	return mux
}

func (f *Forwarder) health(w http.ResponseWriter, _ *http.Request) {
	st := f.status()
	w.Header().Set("Content-Type", "application/json")
	if !st.Connected {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":   f.ver,
		"server":    f.cfg.ServerURL,
		"backend":   st,
		"queue":     f.queue.Len(),
		"uptime_s":  int(time.Since(f.start).Seconds()),
		"forwarder": f.cfg.Listen,
	})
}

// QueuedEvent is one undelivered request.
type QueuedEvent struct {
	Method      string    `json:"method"`
	PathQuery   string    `json:"path_query"`
	ContentType string    `json:"content_type"`
	SourceIP    string    `json:"source_ip"`
	ReceivedAt  time.Time `json:"received_at"`
	Body        []byte    `json:"body"`
}

func (f *Forwarder) forward(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxEventBody))
	if err != nil {
		http.Error(w, "body", http.StatusBadRequest)
		return
	}
	src, _, _ := net.SplitHostPort(r.RemoteAddr)
	ev := QueuedEvent{
		Method:      r.Method,
		PathQuery:   r.URL.RequestURI(),
		ContentType: r.Header.Get("Content-Type"),
		SourceIP:    src,
		ReceivedAt:  time.Now(),
		Body:        body,
	}
	if tok := r.Header.Get("X-ANPR-Token"); tok != "" {
		// keep the caller's token: send() only adds ours when none is given
		ev.PathQuery = addToken(ev.PathQuery, tok)
	}

	resp, err := f.send(r.Context(), ev)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 500 {
			if ct := resp.Header.Get("Content-Type"); ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
			return
		}
		err = fmt.Errorf("backend HTTP %d", resp.StatusCode)
	}

	if r.Method != http.MethodPost {
		http.Error(w, "backend bilan aloqa yo'q", http.StatusBadGateway)
		return
	}
	if qerr := f.queue.Put(ev); qerr != nil {
		log.Printf("[agent] hodisani navbatga yozib bo'lmadi: %v\n", qerr)
		http.Error(w, "queue", http.StatusServiceUnavailable)
		return
	}
	log.Printf("[agent] %s yetkazilmadi (%v) — navbatga qo'yildi\n", ev.PathQuery, err)
	// The camera only needs to know the event was taken; it is delivered later.
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"success":true,"queued":true}`))
}

func addToken(pathQuery, token string) string {
	if strings.Contains(pathQuery, "token=") {
		return pathQuery
	}
	sep := "?"
	if strings.Contains(pathQuery, "?") {
		sep = "&"
	}
	return pathQuery + sep + "token=" + token
}

func (f *Forwarder) send(ctx context.Context, ev QueuedEvent) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, ev.Method, f.cfg.ServerURL+ev.PathQuery, bytes.NewReader(ev.Body))
	if err != nil {
		return nil, err
	}
	if ev.ContentType != "" {
		req.Header.Set("Content-Type", ev.ContentType)
	}
	if ev.SourceIP != "" {
		req.Header.Set("X-Forwarded-For", ev.SourceIP)
	}
	if f.cfg.ANPRToken != "" && !strings.Contains(ev.PathQuery, "token=") {
		req.Header.Set("X-ANPR-Token", f.cfg.ANPRToken)
	}
	return f.http.Do(req)
}

// Replay delivers queued events in order until ctx is cancelled. Delivery
// stops at the first failure and resumes on the next tick, so the order of
// entries and exits is kept.
func (f *Forwarder) Replay(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.replayOnce(ctx)
		}
	}
}

func (f *Forwarder) replayOnce(ctx context.Context) {
	for _, name := range f.queue.List() {
		ev, err := f.queue.Get(name)
		if err != nil {
			log.Printf("[agent] navbatdagi fayl o'qilmadi, o'chirildi: %s: %v\n", name, err)
			f.queue.Remove(name)
			continue
		}
		if age := time.Since(ev.ReceivedAt); age > f.cfg.QueueMaxAge {
			log.Printf("[agent] eski hodisa tashlab yuborildi (%s oldin): %s\n", age.Round(time.Second), ev.PathQuery)
			f.queue.Remove(name)
			continue
		}
		resp, err := f.send(ctx, ev)
		if err != nil {
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			return
		}
		log.Printf("[agent] navbatdagi hodisa yetkazildi: %s (HTTP %d, %s kechikish)\n",
			ev.PathQuery, resp.StatusCode, time.Since(ev.ReceivedAt).Round(time.Second))
		f.queue.Remove(name)
	}
}

// Queue is a directory of JSON files, one per event, named so that sorting
// by name gives arrival order.
type Queue struct {
	dir string
	max int
	mu  sync.Mutex
}

func NewQueue(dir string, maxItems int) (*Queue, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Queue{dir: dir, max: maxItems}, nil
}

func (q *Queue) Put(ev QueuedEvent) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	rnd := make([]byte, 4)
	_, _ = rand.Read(rnd)
	name := fmt.Sprintf("%020d-%s.json", ev.ReceivedAt.UnixNano(), hex.EncodeToString(rnd))
	tmp := filepath.Join(q.dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(q.dir, name)); err != nil {
		return err
	}
	// Over the cap: drop the oldest.
	names := q.listLocked()
	for i := 0; len(names)-i > q.max; i++ {
		_ = os.Remove(filepath.Join(q.dir, names[i]))
		log.Printf("[agent] navbat to'ldi, eng eski hodisa o'chirildi: %s\n", names[i])
	}
	return nil
}

func (q *Queue) List() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.listLocked()
}

func (q *Queue) listLocked() []string {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func (q *Queue) Len() int { return len(q.List()) }

func (q *Queue) Get(name string) (QueuedEvent, error) {
	var ev QueuedEvent
	data, err := os.ReadFile(filepath.Join(q.dir, name))
	if err != nil {
		return ev, err
	}
	err = json.Unmarshal(data, &ev)
	return ev, err
}

func (q *Queue) Remove(name string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	_ = os.Remove(filepath.Join(q.dir, name))
}
