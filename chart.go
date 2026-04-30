package main

import (
	"fmt"
	"image"
	"strings"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// renderBrailleChart renders the supplied data as a termui Plot widget using
// braille markers. The rendered widget is rasterised into a string with
// embedded ANSI escape sequences so that it can be embedded directly in the
// bubbletea View output. The data series is drawn in green.
func renderBrailleChart(data []float64, width, height int) string {
	if width < 4 || height < 3 || len(data) == 0 {
		return ""
	}

	plot := widgets.NewPlot()
	plot.Data = [][]float64{data}
	plot.Marker = widgets.MarkerBraille
	plot.PlotType = widgets.LineChart
	plot.LineColors = []ui.Color{ui.ColorGreen}
	plot.AxesColor = ui.ColorWhite
	plot.MaxVal = 100
	plot.ShowAxes = true
	plot.Border = false
	plot.HorizontalScale = 1
	plot.SetRect(0, 0, width, height)

	buf := ui.NewBuffer(image.Rect(0, 0, width, height))
	plot.Draw(buf)

	return bufferToANSI(buf, width, height)
}

// bufferToANSI converts a termui rendered buffer into a string with ANSI
// escape sequences encoding foreground color. Background, modifier, and
// 256-color codes are intentionally omitted to keep the output compatible
// with the surrounding bubbletea / lipgloss styling.
func bufferToANSI(buf *ui.Buffer, width, height int) string {
	var sb strings.Builder
	const reset = "\x1b[0m"
	lastFg := ui.ColorClear

	for y := 0; y < height; y++ {
		if y > 0 {
			sb.WriteString(reset)
			sb.WriteByte('\n')
			lastFg = ui.ColorClear
		}
		for x := 0; x < width; x++ {
			cell := buf.GetCell(image.Pt(x, y))
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			fg := cell.Style.Fg
			if fg != lastFg {
				sb.WriteString(ansiFg(fg))
				lastFg = fg
			}
			sb.WriteRune(r)
		}
	}
	sb.WriteString(reset)
	return sb.String()
}

// ansiFg maps a termui Color into the ANSI SGR escape sequence that selects
// it as the foreground color. Unknown colors fall back to the terminal's
// default foreground.
func ansiFg(c ui.Color) string {
	switch c {
	case ui.ColorBlack:
		return "\x1b[30m"
	case ui.ColorRed:
		return "\x1b[31m"
	case ui.ColorGreen:
		return "\x1b[32m"
	case ui.ColorYellow:
		return "\x1b[33m"
	case ui.ColorBlue:
		return "\x1b[34m"
	case ui.ColorMagenta:
		return "\x1b[35m"
	case ui.ColorCyan:
		return "\x1b[36m"
	case ui.ColorWhite:
		return "\x1b[37m"
	case ui.ColorClear:
		return "\x1b[39m"
	default:
		if c >= 0 && c <= 255 {
			return fmt.Sprintf("\x1b[38;5;%dm", int(c))
		}
		return "\x1b[39m"
	}
}
