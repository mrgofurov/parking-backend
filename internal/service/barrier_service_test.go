package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/repository"
	"gorm.io/gorm"
)

// fakeISAPI records every request a barrier sends to the "camera".
type fakeISAPI struct {
	mu   sync.Mutex
	hits []string // "METHOD path body"
	fail bool
}

func (f *fakeISAPI) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.hits = append(f.hits, r.Method+" "+r.URL.Path+" "+string(b))
	fail := f.fail
	f.mu.Unlock()
	if fail {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(`<ResponseStatus><statusCode>1</statusCode></ResponseStatus>`))
}

func (f *fakeISAPI) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hits) == 0 {
		return ""
	}
	return f.hits[len(f.hits)-1]
}

func (f *fakeISAPI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hits)
}

func newBarrierTestSvc(t *testing.T) (BarrierService, repository.BarrierRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Barrier{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewBarrierRepository(db)
	return NewBarrierService(repo, NewWebSocketHub()), repo
}

func addBarrier(t *testing.T, repo repository.BarrierRepository, b domain.Barrier) uuid.UUID {
	t.Helper()
	b.ID = uuid.New()
	b.Name = "test"
	b.Direction = domain.DirectionExit
	b.Status = domain.BarrierClosed
	if err := repo.Create(&b); err != nil {
		t.Fatal(err)
	}
	return b.ID
}

func status(t *testing.T, repo repository.BarrierRepository, id uuid.UUID) domain.BarrierStatus {
	b, err := repo.GetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return b.Status
}

func TestBarrierIOOpenAndCarPassedSendCommands(t *testing.T) {
	dev := &fakeISAPI{}
	srv := httptest.NewServer(http.HandlerFunc(dev.handler))
	defer srv.Close()
	svc, repo := newBarrierTestSvc(t)
	id := addBarrier(t, repo, domain.Barrier{ControlType: domain.BarrierControlHikvisionIO, DeviceHost: srv.URL, OutputID: 1, CloseOutputID: 2})

	if err := svc.OpenBarrier(id, "operator"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dev.last(), "PUT /ISAPI/System/IO/outputs/1/trigger") || status(t, repo, id) != domain.BarrierOpen {
		t.Fatalf("open: last=%q status=%s", dev.last(), status(t, repo, id))
	}

	res, err := svc.CarPassed(id)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HardwareSent || !strings.HasPrefix(dev.last(), "PUT /ISAPI/System/IO/outputs/2/trigger") || status(t, repo, id) != domain.BarrierClosed {
		t.Fatalf("car-passed: res=%+v last=%q status=%s", res, dev.last(), status(t, repo, id))
	}
}

func TestBarrierIOWithoutCloseOutputOnlyUpdatesStatus(t *testing.T) {
	dev := &fakeISAPI{}
	srv := httptest.NewServer(http.HandlerFunc(dev.handler))
	defer srv.Close()
	svc, repo := newBarrierTestSvc(t)
	id := addBarrier(t, repo, domain.Barrier{ControlType: domain.BarrierControlHikvisionIO, DeviceHost: srv.URL, OutputID: 1})

	_ = svc.OpenBarrier(id, "operator")
	before := dev.count()
	res, err := svc.CarPassed(id)
	if err != nil {
		t.Fatal(err)
	}
	if res.HardwareSent || dev.count() != before || status(t, repo, id) != domain.BarrierClosed {
		t.Fatalf("expected status-only close: res=%+v hits=%d->%d", res, before, dev.count())
	}
}

func TestBarrierGateCloseUsesCtrlMode(t *testing.T) {
	dev := &fakeISAPI{}
	srv := httptest.NewServer(http.HandlerFunc(dev.handler))
	defer srv.Close()
	svc, repo := newBarrierTestSvc(t)
	id := addBarrier(t, repo, domain.Barrier{ControlType: domain.BarrierControlHikvisionGate, DeviceHost: srv.URL, OutputID: 1})

	if _, err := svc.CloseBarrier(id, "operator"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dev.last(), "/ISAPI/Parking/channels/1/barrierGate") || !strings.Contains(dev.last(), "<ctrlMode>close</ctrlMode>") {
		t.Fatalf("last=%q", dev.last())
	}
}

func TestBarrierDeviceErrorKeepsStatusAndReturnsError(t *testing.T) {
	dev := &fakeISAPI{fail: true}
	srv := httptest.NewServer(http.HandlerFunc(dev.handler))
	defer srv.Close()
	svc, repo := newBarrierTestSvc(t)
	id := addBarrier(t, repo, domain.Barrier{ControlType: domain.BarrierControlHikvisionIO, DeviceHost: srv.URL, OutputID: 1})

	if err := svc.OpenBarrier(id, "operator"); err == nil {
		t.Fatal("expected error when device fails")
	}
	if status(t, repo, id) != domain.BarrierClosed {
		t.Fatal("status must stay CLOSED when the device did not accept the command")
	}
}
