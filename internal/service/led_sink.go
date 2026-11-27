package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/ledscreen"
)

const ledSendTimeout = 5 * time.Second

// ledSink pushes lane states to LED screens without blocking the caller.
// Each lane has at most one send in flight; while it runs, newer frames
// replace the pending one, so a slow or offline screen only ever gets the
// latest state.
type ledSink struct {
	newDriver func(addr string) ledscreen.Driver

	mu    sync.Mutex
	lanes map[uuid.UUID]*ledLane
}

type ledLane struct {
	addr    string
	drv     ledscreen.Driver
	pending *ledscreen.Frame
	running bool
}

func newLEDSink(newDriver func(addr string) ledscreen.Driver) *ledSink {
	return &ledSink{newDriver: newDriver, lanes: map[uuid.UUID]*ledLane{}}
}

func (s *ledSink) push(cam *domain.Camera, st DisplayState) {
	if s == nil || cam == nil || cam.DisplayIP == "" {
		return
	}
	frame := ledFrame(st, cam.DisplayRows)

	s.mu.Lock()
	lane, ok := s.lanes[cam.ID]
	if !ok || lane.addr != cam.DisplayIP {
		// A new address gets its own queue; a send still running against the
		// old one finishes on its own.
		lane = &ledLane{addr: cam.DisplayIP, drv: s.newDriver(cam.DisplayIP)}
		s.lanes[cam.ID] = lane
	}
	lane.pending = &frame
	if lane.running {
		s.mu.Unlock()
		return
	}
	lane.running = true
	s.mu.Unlock()

	go s.drain(lane)
}

func (s *ledSink) drain(lane *ledLane) {
	for {
		s.mu.Lock()
		f := lane.pending
		lane.pending = nil
		if f == nil {
			lane.running = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), ledSendTimeout)
		if err := lane.drv.Show(ctx, *f); err != nil {
			log.Printf("[LED %s] yuborilmadi: %v", lane.addr, err)
		}
		cancel()
	}
}
