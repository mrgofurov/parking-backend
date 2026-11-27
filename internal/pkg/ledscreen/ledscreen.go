// Package ledscreen drives Hikvision DS-TVL224 entrance/exit LED screens
// (64x64 or 64x32 px, P4.75, red/green/yellow, GB2312 16x16 font → 8 Latin
// characters per line, controlled over LAN).
//
// The wire protocol is not public. Until it is confirmed (Wireshark capture
// of "LED Test Tool" or the vendor document), NewDriver returns LogDriver,
// which only logs what would be shown. Everything above the driver — layouts,
// per-lane queueing, camera binding — does not depend on the protocol.
package ledscreen

import (
	"context"
	"log"
	"strings"
)

// CharsPerLine is how many Latin characters (8x16 half-width glyphs) fit on a
// 64 px wide line.
const CharsPerLine = 8

type Color int

const (
	Red Color = iota + 1
	Green
	Yellow // red + green
)

func (c Color) String() string {
	switch c {
	case Red:
		return "red"
	case Green:
		return "green"
	case Yellow:
		return "yellow"
	}
	return "?"
}

// Line is one 16 px text row.
type Line struct {
	Text  string
	Color Color
}

// Frame is the full content of the screen at one moment, top line first.
type Frame struct {
	Lines []Line
}

func (f Frame) String() string {
	parts := make([]string, len(f.Lines))
	for i, l := range f.Lines {
		parts[i] = l.Color.String() + ":" + l.Text
	}
	return "[" + strings.Join(parts, " | ") + "]"
}

// Driver pushes frames to one physical screen.
type Driver interface {
	Show(ctx context.Context, f Frame) error
}

// NewDriver returns the driver for the screen at addr ("ip" or "ip:port").
func NewDriver(addr string) Driver {
	return LogDriver{Addr: addr}
}

// LogDriver logs frames instead of sending them. Placeholder until the
// DS-TVL224 wire protocol is confirmed.
type LogDriver struct{ Addr string }

func (d LogDriver) Show(_ context.Context, f Frame) error {
	log.Printf("[LED %s] %s (protokol hali ulanmagan, faqat log)", d.Addr, f)
	return nil
}

// Fit makes s displayable on one line: Uzbek apostrophes become ASCII ',
// other non-ASCII runes are dropped (the font is GB2312; ASCII is the only
// safe Latin subset), and the result is cut to CharsPerLine.
func Fit(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case 'ʻ', 'ʼ', '‘', '’', '`':
			r = '\''
		}
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > CharsPerLine {
		out = out[:CharsPerLine]
	}
	return out
}
