package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/ledscreen"
)

func texts(f ledscreen.Frame) []string {
	out := make([]string, len(f.Lines))
	for i, l := range f.Lines {
		out[i] = l.Text
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLEDFrameLayouts(t *testing.T) {
	entry := time.Date(2026, 11, 26, 9, 5, 0, 0, time.UTC)
	cases := []struct {
		name string
		st   DisplayState
		rows int
		want []string
	}{
		{"payment 4 rows", DisplayState{State: DisplayPaymentRequired, PlateNumber: "01A123BC", DurationMin: 155, Amount: 12000}, 4,
			[]string{"01A123BC", "2:35", "12 000", "SO'M"}},
		{"payment 2 rows", DisplayState{State: DisplayPaymentRequired, PlateNumber: "01A123BC", DurationMin: 155, Amount: 12000}, 2,
			[]string{"01A123BC", "12 000"}},
		{"payment large amount drops spaces", DisplayState{State: DisplayPaymentRequired, PlateNumber: "01A123BC", Amount: 1250000}, 4,
			[]string{"01A123BC", "0 MIN", "1250000", "SO'M"}},
		{"welcome", DisplayState{State: DisplayWelcome, PlateNumber: "01A123BC", EntryTime: &entry}, 4,
			[]string{"01A123BC", "XUSH", "KELIBSIZ", "09:05"}},
		{"paid 2 rows", DisplayState{State: DisplayPaid, PlateNumber: "01A123BC"}, 2,
			[]string{"01A123BC", "TO'LANDI"}},
		{"no session", DisplayState{State: DisplayNoSession, PlateNumber: "01A123BC"}, 4,
			[]string{"01A123BC", "KASSAGA", "MUROJAAT", "QILING"}},
		{"idle exit 2 rows", DisplayState{State: DisplayIdle, Direction: domain.DirectionExit}, 2,
			[]string{"OQ YO'L!", ""}},
		{"unknown rows default to 4", DisplayState{State: DisplayFree, PlateNumber: "01A123BC"}, 0,
			[]string{"01A123BC", "BEPUL", "OQ YO'L!", ""}},
	}
	for _, c := range cases {
		got := texts(ledFrame(c.st, c.rows))
		if !eq(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		for _, line := range got {
			if len(line) > ledscreen.CharsPerLine {
				t.Errorf("%s: line %q longer than %d", c.name, line, ledscreen.CharsPerLine)
			}
		}
	}
}

func TestLEDDuration(t *testing.T) {
	for min, want := range map[int]string{0: "0 MIN", 45: "45 MIN", 60: "1:00", 155: "2:35", 1500: "1K 01:00", 15000: "10 KUN", 150000: "104 KUN"} {
		if got := ledDuration(min); got != want {
			t.Errorf("ledDuration(%d) = %q, want %q", min, got, want)
		}
	}
}

func TestFitSanitizes(t *testing.T) {
	for in, want := range map[string]string{
		"TOʻLANDI":        "TO'LANDI",
		"Привет 01A":      "01A",
		"  OQ YO’L  ":     "OQ YO'L",
		"MUROJAAT QILING": "MUROJAAT",
	} {
		if got := ledscreen.Fit(in); got != want {
			t.Errorf("Fit(%q) = %q, want %q", in, got, want)
		}
	}
}

// blockingDriver holds the first Show until released, recording every frame.
type blockingDriver struct {
	mu      sync.Mutex
	shown   [][]string
	release chan struct{}
	started chan struct{}
	once    sync.Once
}

func (d *blockingDriver) Show(_ context.Context, f ledscreen.Frame) error {
	d.once.Do(func() { close(d.started); <-d.release })
	d.mu.Lock()
	d.shown = append(d.shown, texts(f))
	d.mu.Unlock()
	return nil
}

func TestLEDSinkSendsOnlyLatestWhileBusy(t *testing.T) {
	drv := &blockingDriver{release: make(chan struct{}), started: make(chan struct{})}
	sink := newLEDSink(func(string) ledscreen.Driver { return drv })
	cam := &domain.Camera{ID: uuid.New(), DisplayIP: "192.168.1.99", DisplayRows: 2}

	sink.push(cam, DisplayState{State: DisplayPaymentRequired, PlateNumber: "AAA", Amount: 1000})
	<-drv.started // first frame is in flight
	sink.push(cam, DisplayState{State: DisplayPaid, PlateNumber: "BBB"})
	sink.push(cam, DisplayState{State: DisplayFree, PlateNumber: "CCC"})
	close(drv.release)

	deadline := time.Now().Add(2 * time.Second)
	for {
		drv.mu.Lock()
		n := len(drv.shown)
		drv.mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // make sure nothing else arrives

	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.shown) != 2 {
		t.Fatalf("got %d frames %q, want 2 (first + latest)", len(drv.shown), drv.shown)
	}
	if drv.shown[1][0] != "CCC" {
		t.Errorf("second frame = %q, want the latest (CCC)", drv.shown[1])
	}
}

func TestLEDSinkSkipsLaneWithoutScreen(t *testing.T) {
	called := false
	sink := newLEDSink(func(string) ledscreen.Driver { called = true; return ledscreen.LogDriver{} })
	sink.push(&domain.Camera{ID: uuid.New()}, DisplayState{State: DisplayIdle})
	if called {
		t.Error("driver created for a camera without DisplayIP")
	}
}
