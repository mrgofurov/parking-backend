package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/ledscreen"
)

// LED screen rows: DS-TVL224-4-5Y has 4, the "(2 Rows)" variant has 2.
const (
	LEDRows4 = 4
	LEDRows2 = 2
)

// NormalizeLEDRows maps anything other than 2 to the 4-row default.
func NormalizeLEDRows(rows int) int {
	if rows == LEDRows2 {
		return LEDRows2
	}
	return LEDRows4
}

// ledFrame turns a lane display state into LED lines (max 8 Latin chars each).
// Uzbek plates ("01A123BC") are exactly 8 characters, so they get a line of
// their own.
func ledFrame(st DisplayState, rows int) ledscreen.Frame {
	y := ledscreen.Yellow
	g := ledscreen.Green
	r := ledscreen.Red
	plate := st.PlateNumber

	var four []ledscreen.Line
	switch st.State {
	case DisplayWelcome:
		four = lines(plate, y, "XUSH", g, "KELIBSIZ", g, clock(st), g)
	case DisplayPaymentRequired:
		four = lines(plate, y, ledDuration(st.DurationMin), g, ledAmount(st.Amount), r, "SO'M", r)
	case DisplayPaid:
		four = lines(plate, y, "TO'LANDI", g, "OQ YO'L!", g, "", g)
	case DisplayFree:
		four = lines(plate, y, "BEPUL", g, "OQ YO'L!", g, "", g)
	case DisplayNoSession, DisplayError:
		four = lines(plate, y, "KASSAGA", r, "MUROJAAT", r, "QILING", r)
	default: // IDLE
		if st.Direction == domain.DirectionExit {
			four = lines("", g, "OQ YO'L!", g, "", g, "", g)
		} else {
			four = lines("", g, "XUSH", g, "KELIBSIZ", g, "", g)
		}
	}

	if NormalizeLEDRows(rows) == LEDRows4 {
		return ledscreen.Frame{Lines: four}
	}
	// 2 rows: the plate (or first line) and the one line that matters most.
	second := four[1]
	switch st.State {
	case DisplayPaymentRequired:
		second = four[2] // amount
	case DisplayIdle, "":
		return ledscreen.Frame{Lines: []ledscreen.Line{four[1], four[2]}}
	}
	return ledscreen.Frame{Lines: []ledscreen.Line{four[0], second}}
}

func lines(t1 string, c1 ledscreen.Color, t2 string, c2 ledscreen.Color,
	t3 string, c3 ledscreen.Color, t4 string, c4 ledscreen.Color) []ledscreen.Line {
	return []ledscreen.Line{
		{Text: ledscreen.Fit(t1), Color: c1},
		{Text: ledscreen.Fit(t2), Color: c2},
		{Text: ledscreen.Fit(t3), Color: c3},
		{Text: ledscreen.Fit(t4), Color: c4},
	}
}

func clock(st DisplayState) string {
	if st.EntryTime == nil {
		return ""
	}
	return st.EntryTime.Format("15:04")
}

// ledDuration: "45 MIN", "2:35", "3K 04:10" (K = kun).
func ledDuration(min int) string {
	if min < 0 {
		min = 0
	}
	if min < 60 {
		return fmt.Sprintf("%d MIN", min)
	}
	days, rest := min/(24*60), min%(24*60)
	hm := fmt.Sprintf("%d:%02d", rest/60, rest%60)
	if days == 0 {
		return hm
	}
	if s := fmt.Sprintf("%dK %02d:%02d", days, rest/60, rest%60); len(s) <= ledscreen.CharsPerLine {
		return s
	}
	return fmt.Sprintf("%d KUN", days)
}

// ledAmount groups thousands with spaces when it still fits: "12 000".
func ledAmount(amount float64) string {
	digits := strconv.FormatInt(int64(amount+0.5), 10)
	var b strings.Builder
	for i, ch := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(ch)
	}
	if b.Len() <= ledscreen.CharsPerLine {
		return b.String()
	}
	return digits
}
