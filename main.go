package main

import (
	"fmt"
	"log"
	"os/user"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

const (
	refreshInterval = 2 * time.Second
	historyLimit    = 10
	killThreshold   = 90.0 // Percent
)

type ProcStats struct {
	PID        int32
	Name       string
	MemoryMB   float64
	MemoryPct  float32
	RiseRate   float64 // MB per second
	History    []float64
	LastUpdate time.Time
}

type KillEvent struct {
	Time   time.Time
	PID    int32
	Name   string
	Reason string
}

type model struct {
	table           table.Model
	candidates      []*ProcStats
	killHistory     []KillEvent
	totalMem        float64
	totalMemHistory []float64
	currentUser     string
	stats           map[int32]*ProcStats
	width, height   int
	showHelp        bool
	killing         map[int32]bool
	killThreshold   float64
}

type tickMsg time.Time
type killMsg struct{ event KillEvent }

func (m model) Init() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = true
		case "esc":
			m.showHelp = false
		case "=", "+":
			m.killThreshold += 1.0
			if m.killThreshold > 100.0 {
				m.killThreshold = 100.0
			}
		case "-":
			m.killThreshold -= 1.0
			if m.killThreshold < 0.0 {
				m.killThreshold = 0.0
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetWidth(msg.Width - 2)
		m.table.SetHeight(m.height/2 - 4)
	case tickMsg:
		m.updateStats()
		killCmd := m.checkAndKill()
		if killCmd != nil {
			return m, tea.Batch(m.Init(), killCmd)
		}
		return m, m.Init()
	case killMsg:
		m.killHistory = append(m.killHistory, msg.event)
		if len(m.killHistory) > 10 {
			m.killHistory = m.killHistory[1:]
		}
		delete(m.killing, msg.event.PID)
		return m, nil
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *model) updateStats() {
	v, _ := mem.VirtualMemory()
	m.totalMem = v.UsedPercent
	m.totalMemHistory = append(m.totalMemHistory, v.UsedPercent)
	if len(m.totalMemHistory) > 60 { // Keep last 60 points for the graph
		m.totalMemHistory = m.totalMemHistory[len(m.totalMemHistory)-60:]
	}

	procs, _ := process.Processes()
	u, _ := user.Current()
	m.currentUser = u.Username

	currentPIDs := make(map[int32]bool)

	for _, p := range procs {
		username, _ := p.Username()
		if username != m.currentUser {
			continue
		}

		pid := p.Pid
		currentPIDs[pid] = true
		memInfo, _ := p.MemoryInfo()
		if memInfo == nil {
			continue
		}

		name, _ := p.Name()
		memMB := float64(memInfo.RSS) / 1024 / 1024
		memPct, _ := p.MemoryPercent()

		stat, ok := m.stats[pid]
		if !ok {
			stat = &ProcStats{
				PID:        pid,
				Name:       name,
				LastUpdate: time.Now(),
				History:    []float64{memMB},
			}
			m.stats[pid] = stat
		} else {
			now := time.Now()
			dt := now.Sub(stat.LastUpdate).Seconds()
			if dt > 0 {
				stat.RiseRate = (memMB - stat.MemoryMB) / dt
			}
			stat.MemoryMB = memMB
			stat.MemoryPct = memPct
			stat.LastUpdate = now
			stat.History = append(stat.History, memMB)
			if len(stat.History) > historyLimit {
				stat.History = stat.History[1:]
			}
		}
		stat.MemoryMB = memMB
		stat.MemoryPct = memPct
	}

	// Clean up dead processes
	for pid := range m.stats {
		if !currentPIDs[pid] {
			delete(m.stats, pid)
		}
	}

	// Update table rows
	var rows []table.Row
	var sortedStats []*ProcStats
	for _, s := range m.stats {
		sortedStats = append(sortedStats, s)
	}
	sort.Slice(sortedStats, func(i, j int) bool {
		return sortedStats[i].MemoryMB > sortedStats[j].MemoryMB
	})

	for _, s := range sortedStats {
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", s.PID),
			s.Name,
			fmt.Sprintf("%.1f MB", s.MemoryMB),
			fmt.Sprintf("%.1f%%", s.MemoryPct),
			fmt.Sprintf("%.2f MB/s", s.RiseRate),
		})
	}
	m.table.SetRows(rows)

	// Update candidates
	m.candidates = nil
	for _, s := range m.stats {
		if s.RiseRate > 0.5 { // Only consider processes rising more than 0.5MB/s
			m.candidates = append(m.candidates, s)
		}
	}
	sort.Slice(m.candidates, func(i, j int) bool {
		return m.candidates[i].RiseRate > m.candidates[j].RiseRate
	})
}

func (m *model) checkAndKill() tea.Cmd {
	if m.totalMem < m.killThreshold {
		return nil
	}
	if len(m.candidates) == 0 {
		return nil
	}

	target := m.candidates[0]

	if m.killing[target.PID] {
		return nil
	}
	m.killing[target.PID] = true

	pid := target.PID
	name := target.Name
	reasonBase := fmt.Sprintf("Spike: %.2f MB/s", target.RiseRate)

	return func() tea.Msg {
		p, err := process.NewProcess(pid)
		if err != nil {
			return killMsg{KillEvent{time.Now(), pid, name, "Process not found: " + err.Error()}}
		}

		err = p.SendSignal(syscall.SIGTERM)
		reason := reasonBase
		if err != nil {
			p.Kill()
			reason += " (SIGKILL immediate)"
		} else {
			killed := false
			for i := 0; i < 6; i++ {
				time.Sleep(500 * time.Millisecond)
				exists, _ := process.PidExists(pid)
				if !exists {
					killed = true
					break
				}
			}
			if !killed {
				p.Kill()
				reason += " (SIGKILL applied)"
			} else {
				reason += " (SIGTERM successful)"
			}
		}

		return killMsg{KillEvent{
			Time:   time.Now(),
			PID:    pid,
			Name:   name,
			Reason: reason,
		}}
	}
}

var (
	baseStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			MarginBottom(1)
)

func (m model) helpView() string {
	helpText := "MKILL - Help\n\n" +
		"Keyboard Shortcuts:\n" +
		"  q, ctrl+c   Quit application\n" +
		"  ?           Show this help panel\n" +
		"  esc         Close help panel\n" +
		"  up/down     Navigate process list\n\n" +
		"Press 'esc' to return to the main view."

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 4).
		Render(helpText)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	if m.showHelp {
		return m.helpView()
	}

	memColor := "#00FF00" // Green
	if m.totalMem >= m.killThreshold {
		memColor = "#FF0000" // Red
	} else if m.totalMem >= m.killThreshold*0.9 {
		memColor = "#FFA500" // Orange
	}
	memStatus := lipgloss.NewStyle().Foreground(lipgloss.Color(memColor)).Render(fmt.Sprintf("Total Memory: %.1f%%", m.totalMem))
	header := titleStyle.Render("MKILL - Memory Watchdog") + " " + memStatus + " (Threshold: " + fmt.Sprintf("%.1f%%", m.killThreshold) + ")\n"

	topPane := baseStyle.Width(m.width - 2).Render(m.table.View())

	candidateView := "No sharp risers detected."
	if len(m.candidates) > 0 {
		var lines []string
		for i, c := range m.candidates {
			if i >= 10 {
				break
			}
			lines = append(lines, fmt.Sprintf("%d. PID %d (%s): +%.2f MB/s", i+1, c.PID, c.Name, c.RiseRate))
		}
		candidateView = strings.Join(lines, "\n")
	}

	historyView := "No processes killed yet."
	if len(m.killHistory) > 0 {
		var lines []string
		for i := len(m.killHistory) - 1; i >= 0; i-- {
			h := m.killHistory[i]
			lines = append(lines, fmt.Sprintf("[%s] %s (%d)\n  └ %s", h.Time.Format("15:04:05"), h.Name, h.PID, h.Reason))
		}
		historyView = strings.Join(lines, "\n")
	}

	bottomHeight := m.height - (m.height / 2) - 6
	leftPane := baseStyle.Width((m.width - 4) / 2).Height(bottomHeight).Render(headerStyle.Render("Kill Candidates") + "\n" + candidateView)

	// Build Memory Graph View
	graphView := "Gathering data..."
	if len(m.totalMemHistory) > 0 {
		// Calculate available width and height for graph
		graphWidth := (m.width-4)/2 - 10
		if graphWidth < 10 {
			graphWidth = 10
		}
		graphHeight := bottomHeight/2 - 2
		if graphHeight < 3 {
			graphHeight = 3
		}

		graph := asciigraph.Plot(m.totalMemHistory, asciigraph.Height(graphHeight), asciigraph.Width(graphWidth))
		graphView = graph
	}

	rightTopHeight := bottomHeight / 2
	rightBottomHeight := bottomHeight - rightTopHeight

	rightTopPane := lipgloss.NewStyle().Height(rightTopHeight).Render(headerStyle.Render("Kill History") + "\n" + historyView)
	rightBottomPane := lipgloss.NewStyle().Height(rightBottomHeight).Render(headerStyle.Render("System Memory Occupancy (%)") + "\n" + graphView)

	rightPane := baseStyle.Width((m.width - 4) / 2).Height(bottomHeight).Render(lipgloss.JoinVertical(lipgloss.Left, rightTopPane, rightBottomPane))

	bottomPane := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(" Press '?' for help • 'q' to quit • Refresh: " + refreshInterval.String())

	return lipgloss.JoinVertical(lipgloss.Left, header, topPane, bottomPane, footer)
}

func main() {
	columns := []table.Column{
		{Title: "PID", Width: 8},
		{Title: "Name", Width: 20},
		{Title: "Memory", Width: 12},
		{Title: "Mem%", Width: 8},
		{Title: "Rise Rate", Width: 15},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	m := model{
		table:           t,
		stats:           make(map[int32]*ProcStats),
		killing:         make(map[int32]bool),
		killThreshold:   90.0,
		totalMemHistory: make([]float64, 60),
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
