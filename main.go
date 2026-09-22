package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

const (
	refreshInterval = 2 * time.Second
	historyLimit    = 10
	killThreshold   = 90.0 // Percent
	// swapKillThreshold is the default swap-only occupancy, in percent, above
	// which a sharp riser is killed even if the combined RAM + swap pool is
	// still below killThreshold. Heavy swap use means thrashing, which the
	// combined figure hides on machines with a large swap area.
	swapKillThreshold = 90.0 // Percent
)

type ProcStats struct {
	PID        int32
	Name       string
	MemoryMB   float64
	MemoryPct  float32
	RiseRate   float64 // MB per second
	History    []float64
	LastUpdate time.Time
	StartTime  time.Time
}

type KillEvent struct {
	Time   time.Time
	PID    int32
	Name   string
	Reason string
}

// memSample holds one sample of system memory occupancy. When the system has
// swap configured, Occupancy spans the combined RAM + swap pool, since that is
// the pool the kernel must exhaust before the OOM killer steps in.
type memSample struct {
	// Occupancy is the percentage the kill threshold is compared against:
	// combined RAM + swap when swap is present, RAM alone otherwise.
	Occupancy float64
	// RAMPct is the RAM-only occupancy, in percent.
	RAMPct float64
	// SwapPct is the swap-only occupancy, in percent. Zero when there is no swap.
	SwapPct float64
	// HasSwap is true when the system has a non-empty swap area.
	HasSwap bool
}

// combinedMemory folds a virtual memory reading and a swap reading into a
// single occupancy figure. Either argument may be nil, and a swap area of zero
// size is treated as "no swap available", in which case the result is the plain
// RAM occupancy and the previous (swap-less) behaviour is preserved exactly.
func combinedMemory(v *mem.VirtualMemoryStat, s *mem.SwapMemoryStat) memSample {
	var out memSample
	var ramUsed, ramTotal uint64
	if v != nil && v.Total > 0 {
		ramTotal = v.Total
		// Stay consistent with gopsutil's UsedPercent, which counts everything
		// that is not "available" (i.e. reclaimable cache does not count as used).
		if v.Available > 0 && v.Available <= v.Total {
			ramUsed = v.Total - v.Available
		} else {
			ramUsed = v.Used
		}
		out.RAMPct = v.UsedPercent
	}

	var swapUsed, swapTotal uint64
	if s != nil && s.Total > 0 {
		swapTotal = s.Total
		swapUsed = s.Used
		if swapUsed > swapTotal {
			swapUsed = swapTotal
		}
		out.HasSwap = true
		out.SwapPct = float64(swapUsed) / float64(swapTotal) * 100
	}

	if total := ramTotal + swapTotal; total > 0 {
		out.Occupancy = float64(ramUsed+swapUsed) / float64(total) * 100
	}
	return out
}

type model struct {
	table           table.Model
	candidates      []*ProcStats
	killHistory     []KillEvent
	totalMem        float64
	memStats        memSample
	totalMemHistory []float64
	currentUser     string
	stats           map[int32]*ProcStats
	width, height   int
	showHelp        bool
	confirmKillPID  int32
	confirmKillName string
	killing         map[int32]bool
	killThreshold   float64
	swapThreshold   float64
	protected       map[string]bool
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
		case "]":
			m.swapThreshold += 1.0
			if m.swapThreshold > 100.0 {
				m.swapThreshold = 100.0
			}
		case "[":
			m.swapThreshold -= 1.0
			if m.swapThreshold < 0.0 {
				m.swapThreshold = 0.0
			}
		case "p":
			if row := m.table.SelectedRow(); row != nil {
				var pid int32
				fmt.Sscanf(row[0], "%d", &pid)
				if stat, ok := m.stats[pid]; ok {
					if m.protected[stat.Name] {
						delete(m.protected, stat.Name)
					} else {
						m.protected[stat.Name] = true
					}
					m.updateStats()
				}
			}
		case "k":
			if m.confirmKillPID != 0 {
				// Confirm kill
				cmd = killProcessCmd(m.confirmKillPID, m.confirmKillName, "Manual kill")
				m.killing[m.confirmKillPID] = true
				m.confirmKillPID = 0
				m.confirmKillName = ""
				return m, cmd
			} else {
				// Initiate kill confirmation
				if row := m.table.SelectedRow(); row != nil {
					var pid int32
					fmt.Sscanf(row[0], "%d", &pid)
					if stat, ok := m.stats[pid]; ok {
						m.confirmKillPID = pid
						m.confirmKillName = stat.Name
					}
				}
			}
		case "P":
			_ = saveConfig(m.protected, m.swapThreshold)
		}

		if m.confirmKillPID != 0 && msg.String() != "k" {
			// Any other key cancels the confirmation
			m.confirmKillPID = 0
			m.confirmKillName = ""
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
	sw, _ := mem.SwapMemory()
	m.memStats = combinedMemory(v, sw)
	m.totalMem = m.memStats.Occupancy
	m.totalMemHistory = append(m.totalMemHistory, m.totalMem)
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

		createTimeMs, err := p.CreateTime()
		var startTime time.Time
		if err == nil {
			startTime = time.UnixMilli(createTimeMs)
		} else {
			startTime = time.Now()
		}

		stat, ok := m.stats[pid]
		if !ok {
			stat = &ProcStats{
				PID:        pid,
				Name:       name,
				LastUpdate: time.Now(),
				History:    []float64{memMB},
				StartTime:  startTime,
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
		pStr := ""
		if m.protected[s.Name] {
			pStr = "p"
		}

		age := time.Since(s.StartTime).Round(time.Second)
		ageStr := age.String()

		rows = append(rows, table.Row{
			fmt.Sprintf("%d", s.PID),
			pStr,
			s.Name,
			ageStr,
			fmt.Sprintf("%.1f MB", s.MemoryMB),
			fmt.Sprintf("%.1f%%", s.MemoryPct),
			fmt.Sprintf("%.2f MB/s", s.RiseRate),
		})
	}
	m.table.SetRows(rows)

	// Update candidates
	m.candidates = nil
	for _, s := range m.stats {
		if s.RiseRate > 0.5 && !m.protected[s.Name] { // Only consider processes rising more than 0.5MB/s and not protected
			m.candidates = append(m.candidates, s)
		}
	}
	sort.Slice(m.candidates, func(i, j int) bool {
		return m.candidates[i].RiseRate > m.candidates[j].RiseRate
	})
}

// killTrigger reports whether the system is over a kill threshold, and which
// one tripped. Swap occupancy is checked separately from the combined pool:
// on a machine with a large swap area the combined figure stays low while the
// system thrashes, so a full swap must be able to trigger a kill on its own.
func (m *model) killTrigger() (string, bool) {
	if m.totalMem >= m.killThreshold {
		return fmt.Sprintf("mem %.1f%%", m.totalMem), true
	}
	if m.memStats.HasSwap && m.memStats.SwapPct >= m.swapThreshold {
		return fmt.Sprintf("swap %.1f%%", m.memStats.SwapPct), true
	}
	return "", false
}

func (m *model) checkAndKill() tea.Cmd {
	trigger, over := m.killTrigger()
	if !over {
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

	reasonBase := fmt.Sprintf("Spike: %.2f MB/s, %s", target.RiseRate, trigger)
	return killProcessCmd(target.PID, target.Name, reasonBase)
}

func killProcessCmd(pid int32, name string, reasonBase string) tea.Cmd {
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

// memGraphTitle names the memory occupancy graph, making it explicit when the
// plotted series covers the combined RAM + swap pool.
func (m model) memGraphTitle() string {
	if m.memStats.HasSwap {
		return "System Memory Occupancy, RAM+Swap (%)"
	}
	return "System Memory Occupancy (%)"
}

func (m model) helpView() string {
	helpText := "MKILL - Help\n\n" +
		"Keyboard Shortcuts:\n" +
		"  q, ctrl+c   Quit application\n" +
		"  ?           Show this help panel\n" +
		"  esc         Close help panel\n" +
		"  up/down, j/k Navigate process list\n" +
		"  pgup/pgdn   Page up/down process list\n" +
		"  home/end, g/G Go to top/bottom of list\n" +
		"  +, =        Increase memory kill threshold\n" +
		"  -           Decrease memory kill threshold\n" +
		"  ]           Increase swap kill threshold\n" +
		"  [           Decrease swap kill threshold\n" +
		"  p           Toggle 'protect' status of selected process\n" +
		"  k           Kill highlighted process (requires confirmation)\n" +
		"  P           Save protected processes to config\n\n" +
		"When the system has swap, the kill threshold is compared against the\n" +
		"combined RAM + swap occupancy; the header also breaks out RAM and swap.\n" +
		"A kill is also triggered when swap occupancy alone reaches the separate\n" +
		"swap kill threshold, which 'P' saves along with the protected list.\n\n" +
		"Press 'esc' to return to the main view."
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 4).
		Render(helpText)

	return box
}

func placeOverlay(x, y int, fg, bg string) string {
	fgLines := strings.Split(fg, "\n")
	bgLines := strings.Split(bg, "\n")
	fgHeight := len(fgLines)

	for i := 0; i < fgHeight; i++ {
		bgY := y + i
		if bgY < 0 || bgY >= len(bgLines) {
			continue
		}

		bgLine := bgLines[bgY]
		fgLine := fgLines[i]
		fgWidth := lipgloss.Width(fgLine)

		leftPart := ansi.Truncate(bgLine, x, "")
		rightPart := ansi.Cut(bgLine, x+fgWidth, 9999)

		bgLines[bgY] = leftPart + fgLine + rightPart
	}

	return strings.Join(bgLines, "\n")
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	var modalBox string
	if m.showHelp {
		modalBox = m.helpView()
	} else if m.confirmKillPID != 0 {
		confirmText := fmt.Sprintf("Are you sure you want to kill process %d (%s)?\n\nPress 'k' to confirm or any other key to cancel.", m.confirmKillPID, m.confirmKillName)
		modalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")).
			Padding(1, 4).
			Render(confirmText)
	}

	swapOver := m.memStats.HasSwap && m.memStats.SwapPct >= m.swapThreshold
	memColor := "#00FF00" // Green
	if m.totalMem >= m.killThreshold || swapOver {
		memColor = "#FF0000" // Red
	} else if m.totalMem >= m.killThreshold*0.9 || (m.memStats.HasSwap && m.memStats.SwapPct >= m.swapThreshold*0.9) {
		memColor = "#FFA500" // Orange
	}
	memText := fmt.Sprintf("Total Memory: %.1f%%", m.totalMem)
	if m.memStats.HasSwap {
		memText += fmt.Sprintf(" (RAM: %.1f%% | Swap: %.1f%%)", m.memStats.RAMPct, m.memStats.SwapPct)
	}
	memStatus := lipgloss.NewStyle().Foreground(lipgloss.Color(memColor)).Render(memText)
	thresholdText := fmt.Sprintf(" (Threshold: %.1f%%", m.killThreshold)
	if m.memStats.HasSwap {
		thresholdText += fmt.Sprintf(" | Swap: %.1f%%", m.swapThreshold)
	}
	thresholdText += ")\n"
	header := titleStyle.Render("MKILL - Memory Watchcat") + " " + memStatus + thresholdText
	topPane := baseStyle.Width(m.width - 2).Render(m.table.View())

	bottomHeight := m.height - (m.height / 2) - 6
	leftTopHeight := bottomHeight / 2
	leftBottomHeight := bottomHeight - leftTopHeight
	rightTopHeight := bottomHeight / 2
	rightBottomHeight := bottomHeight - rightTopHeight

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
			lines = append(lines, fmt.Sprintf("[%s] %s (%d)", h.Time.Format("15:04:05"), h.Name, h.PID))
			lines = append(lines, fmt.Sprintf("  └ %s", h.Reason))
		}

		maxLines := rightTopHeight - 3 // header takes 3 lines
		if maxLines < 0 {
			maxLines = 0
		}
		if len(lines) > maxLines {
			lines = lines[:maxLines]
		}
		historyView = strings.Join(lines, "\n")
	}

	leftTopPane := lipgloss.NewStyle().Height(leftTopHeight).Render(headerStyle.Render("Kill Candidates") + "\n" + candidateView)

	protectedList := "No protected processes."
	if len(m.protected) > 0 {
		var pNames []string
		for name := range m.protected {
			pNames = append(pNames, name)
		}
		sort.Strings(pNames)
		protectedList = strings.Join(pNames, "\n")
	}
	leftBottomPane := lipgloss.NewStyle().Height(leftBottomHeight).Render(headerStyle.Render("Protected Processes") + "\n" + protectedList)

	leftPane := baseStyle.Width((m.width - 4) / 2).Height(bottomHeight).Render(lipgloss.JoinVertical(lipgloss.Left, leftTopPane, leftBottomPane))
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

		graphView = renderBrailleChart(m.totalMemHistory, graphWidth, graphHeight)
	}

	rightTopPane := lipgloss.NewStyle().Height(rightTopHeight).Render(headerStyle.Render("Kill History") + "\n" + historyView)
	rightBottomPane := lipgloss.NewStyle().Height(rightBottomHeight).Render(headerStyle.Render(m.memGraphTitle()) + "\n" + graphView)

	rightPane := baseStyle.Width((m.width - 4) / 2).Height(bottomHeight).Render(lipgloss.JoinVertical(lipgloss.Left, rightTopPane, rightBottomPane))

	bottomPane := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(" Press '?' for help • 'q' to quit • Refresh: " + refreshInterval.String())

	mainView := lipgloss.JoinVertical(lipgloss.Left, header, topPane, bottomPane, footer)
	if modalBox != "" {
		modalWidth := lipgloss.Width(modalBox)
		modalHeight := lipgloss.Height(modalBox)
		x := (m.width - modalWidth) / 2
		y := (m.height - modalHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		return placeOverlay(x, y, modalBox, mainView)
	}
	return mainView
}

func main() {
	protected, swapThresholdCfg := loadConfig()

	columns := []table.Column{
		{Title: "PID", Width: 8},
		{Title: "P", Width: 3},
		{Title: "Name", Width: 20},
		{Title: "Age", Width: 10},
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
		killThreshold:   killThreshold,
		swapThreshold:   swapThresholdCfg,
		totalMemHistory: make([]float64, 60),
		protected:       protected,
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
func getConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "mkill", "config.json"), nil
}

// configFile is the on-disk shape of ~/.config/mkill/config.json.
// SwapThreshold is a pointer so that a config written before the setting
// existed (or one that simply omits it) falls back to the default rather than
// being read as a swap threshold of 0%, which would kill on any swap use.
type configFile struct {
	Protected     []string `json:"protected"`
	SwapThreshold *float64 `json:"swap_threshold,omitempty"`
}

// loadConfig returns the protected process names and the swap kill threshold,
// falling back to the defaults when the config file is missing or unreadable.
func loadConfig() (map[string]bool, float64) {
	defaultProtected := map[string]bool{
		"chrome-remote-desktop":      true,
		"chrome-remote-desktop-host": true,
		"chrome":                     true,
	}
	path, err := getConfigPath()
	if err != nil {
		return defaultProtected, swapKillThreshold
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultProtected, swapKillThreshold
	}
	var cfg configFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultProtected, swapKillThreshold
	}
	protected := make(map[string]bool)
	for _, p := range cfg.Protected {
		protected[p] = true
	}
	swap := float64(swapKillThreshold)
	if cfg.SwapThreshold != nil {
		swap = *cfg.SwapThreshold
	}
	return protected, swap
}

func saveConfig(protected map[string]bool, swapThreshold float64) error {
	path, err := getConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	cfg := configFile{SwapThreshold: &swapThreshold}
	for p := range protected {
		cfg.Protected = append(cfg.Protected, p)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
