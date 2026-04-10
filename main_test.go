package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

func createTestModel() model {
	columns := []table.Column{
		{Title: "PID", Width: 8},
		{Title: "P", Width: 3},
		{Title: "Name", Width: 20},
		{Title: "Memory", Width: 12},
		{Title: "Mem%", Width: 8},
		{Title: "Rise Rate", Width: 15},
	}

	t := table.New(table.WithColumns(columns))
	m := model{
		table:           t,
		stats:           make(map[int32]*ProcStats),
		killing:         make(map[int32]bool),
		killThreshold:   90.0,
		protected:       map[string]bool{"chrome-remote-desktop": true, "chrome-remote-desktop-host": true, "chrome": true},
		totalMemHistory: make([]float64, 60)}
	return m
}

func TestTUI_HelpViewToggle(t *testing.T) {
	m := createTestModel()
	m.width = 100
	m.height = 100

	// Initial view shouldn't have help
	view := m.View()
	if strings.Contains(view, "Keyboard Shortcuts:") {
		t.Errorf("Expected normal view, but help text was present")
	}

	// Press '?'
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	newModel, ok := m2.(model)
	if !ok {
		t.Fatalf("Update did not return a main.model")
	}

	if !newModel.showHelp {
		t.Errorf("Expected showHelp to be true after pressing '?'")
	}

	// Now view should contain help
	helpView := newModel.View()
	if !strings.Contains(helpView, "Keyboard Shortcuts:") {
		t.Errorf("Expected help view, but help text was missing")
	}

	// Press 'esc'
	m3, _ := newModel.Update(tea.KeyMsg{Type: tea.KeyEsc})
	newModel3, _ := m3.(model)

	if newModel3.showHelp {
		t.Errorf("Expected showHelp to be false after pressing 'esc'")
	}
}

func TestTUI_Quit(t *testing.T) {
	m := createTestModel()

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatalf("Expected quit command, got nil")
	}

	// It's hard to compare commands directly, but we know it's a Quit command
	// Let's just ensure we return tea.Quit (by running it and checking the msg type)
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("Expected tea.QuitMsg, got %T", msg)
	}

	// Check ctrl+c as well
	_, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	msg = cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("Expected tea.QuitMsg for ctrl+c, got %T", msg)
	}
}

func TestTUI_WindowResize(t *testing.T) {
	m := createTestModel()

	newWidth, newHeight := 120, 40
	m2, _ := m.Update(tea.WindowSizeMsg{Width: newWidth, Height: newHeight})
	newModel := m2.(model)

	if newModel.width != newWidth {
		t.Errorf("Expected width %d, got %d", newWidth, newModel.width)
	}
	if newModel.height != newHeight {
		t.Errorf("Expected height %d, got %d", newHeight, newModel.height)
	}
}

func TestTUI_TickUpdateStats(t *testing.T) {
	m := createTestModel()
	m.width = 100
	m.height = 100

	// Send a tick message
	m2, _ := m.Update(tickMsg(time.Now()))
	newModel := m2.(model)

	// tickMsg triggers updateStats(). It should populate stats.
	if len(newModel.totalMemHistory) == 0 {
		t.Errorf("Expected totalMemHistory to be updated")
	}

	// The view should render without panicking after a tick
	view := newModel.View()
	if !strings.Contains(view, "MKILL") {
		t.Errorf("Expected MKILL in view after tick")
	}
}

func TestTUI_KillMessage(t *testing.T) {
	m := createTestModel()
	m.width = 100
	m.height = 100

	pidToKill := int32(1234)
	m.killing[pidToKill] = true

	killEvent := KillEvent{
		Time:   time.Now(),
		PID:    pidToKill,
		Name:   "test-proc",
		Reason: "testing",
	}

	m2, _ := m.Update(killMsg{event: killEvent})
	newModel := m2.(model)

	if len(newModel.killHistory) != 1 {
		t.Fatalf("Expected kill history length 1, got %d", len(newModel.killHistory))
	}
	if newModel.killHistory[0].PID != pidToKill {
		t.Errorf("Expected PID %d in kill history, got %d", pidToKill, newModel.killHistory[0].PID)
	}
	if newModel.killing[pidToKill] {
		t.Errorf("Expected PID %d to be removed from killing map", pidToKill)
	}
}

func TestTUI_CandidatesRendering(t *testing.T) {
	m := createTestModel()
	m.width = 100
	m.height = 100

	m.candidates = []*ProcStats{
		{
			PID:      9999,
			Name:     "memory-hog",
			RiseRate: 5.5,
		},
	}

	view := m.View()
	if !strings.Contains(view, "memory-hog") {
		t.Errorf("Expected candidate name in view")
	}
	if !strings.Contains(view, "5.50 MB/s") {
		t.Errorf("Expected rise rate in view")
	}
}

func TestTUI_ThresholdControls(t *testing.T) {
	m := createTestModel()
	initialThreshold := m.killThreshold

	// Test increase (+)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'='}})
	newModel, ok := m2.(model)
	if !ok {
		t.Fatalf("Update did not return a main.model")
	}

	if newModel.killThreshold <= initialThreshold {
		t.Errorf("Expected killThreshold to increase, got %f (initial %f)", newModel.killThreshold, initialThreshold)
	}

	// Test decrease (-)
	m3, _ := newModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	newModel3, _ := m3.(model)

	if newModel3.killThreshold != initialThreshold {
		t.Errorf("Expected killThreshold to return to initial, got %f", newModel3.killThreshold)
	}

	// Test min bound (0)
	mTestMin := createTestModel()
	mTestMin.killThreshold = 0.5
	mTestMin2, _ := mTestMin.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	if mTestMin2.(model).killThreshold < 0.0 {
		t.Errorf("Expected killThreshold to be bounded at 0.0, got %f", mTestMin2.(model).killThreshold)
	}

	// Test max bound (100)
	mTestMax := createTestModel()
	mTestMax.killThreshold = 99.5
	mTestMax2, _ := mTestMax.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'='}})
	if mTestMax2.(model).killThreshold > 100.0 {
		t.Errorf("Expected killThreshold to be bounded at 100.0, got %f", mTestMax2.(model).killThreshold)
	}
}

func TestTUI_ProtectProcesses(t *testing.T) {
	m := createTestModel()
	m.width = 100
	m.height = 100

	// Ensure chrome-remote-desktop is protected by default
	if !m.protected["chrome-remote-desktop"] {
		t.Errorf("Expected chrome-remote-desktop to be protected by default")
	}

	// Ensure chrome is protected by default
	if !m.protected["chrome"] {
		t.Errorf("Expected chrome to be protected by default")
	}
	// Add a mock process
	m.stats[1234] = &ProcStats{
		PID:        1234,
		Name:       "test-process",
		MemoryMB:   100.0,
		MemoryPct:  10.0,
		RiseRate:   10.0, // High enough to be a candidate
		LastUpdate: time.Now(),
	}

	// Set rows for selection
	rows := []table.Row{
		{"1234", "", "test-process", "100.0 MB", "10.0%", "10.00 MB/s"},
	}
	m.table.SetRows(rows)
	m.table.SetCursor(0)

	// Verify "test-process" is not protected yet
	if m.protected["test-process"] {
		t.Errorf("Expected test-process to not be protected initially")
	}

	// Press 'p'
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	newModel := m2.(model)

	if !newModel.protected["test-process"] {
		t.Errorf("Expected test-process to be protected after pressing 'p'")
	}

	// View should contain "Protected Processes" and "test-process"
	view := newModel.View()
	if !strings.Contains(view, "Protected Processes") {
		t.Errorf("Expected view to contain 'Protected Processes' header")
	}
	if !strings.Contains(view, "test-process") {
		t.Errorf("Expected view to contain 'test-process' in the protected list")
	}

	// updateStats should exclude test-process from candidates
	newModel.updateStats()
	for _, c := range newModel.candidates {
		if c.Name == "test-process" {
			t.Errorf("Expected test-process to be excluded from candidates due to being protected")
		}
	}
}

func TestTUI_ConfigLoadAndSave(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// Ensure default loads correctly when file doesn't exist
	loaded := loadConfig()
	if !loaded["chrome-remote-desktop-host"] {
		t.Errorf("Expected chrome-remote-desktop-host to be loaded from default config")
	}

	// Make changes and save via keypress
	m := createTestModel()
	m.protected["test-json-custom"] = true

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	_ = m2.(model)

	// Load config again from disk
	loaded2 := loadConfig()
	if !loaded2["test-json-custom"] {
		t.Errorf("Expected test-json-custom to be saved and loaded from JSON config")
	}

	// chrome-remote-desktop-host should also still be there if we started from createTestModel defaults
	if !loaded2["chrome-remote-desktop-host"] {
		t.Errorf("Expected chrome-remote-desktop-host to be saved and loaded from JSON config")
	}
}

func TestTUI_ProtectChromeRemoteDesktopHost(t *testing.T) {
	m := createTestModel()
	if !m.protected["chrome-remote-desktop-host"] {
		t.Errorf("Expected chrome-remote-desktop-host to be protected by default")
	}
}

func TestTUI_ManualKillConfirmation(t *testing.T) {
	m := createTestModel()
	m.width = 100
	m.height = 100

	m.stats[1234] = &ProcStats{
		PID:        1234,
		Name:       "test-process",
		MemoryMB:   100.0,
		MemoryPct:  10.0,
		RiseRate:   10.0,
		LastUpdate: time.Now(),
	}

	// Set rows for selection
	rows := []table.Row{
		{"1234", "", "test-process", "100.0 MB", "10.0%", "10.00 MB/s"},
	}
	m.table.SetRows(rows)
	m.table.SetCursor(0)

	// Press 'k' to initiate confirmation
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	newModel := m2.(model)

	if newModel.confirmKillPID != 1234 {
		t.Errorf("Expected confirmKillPID to be 1234, got %d", newModel.confirmKillPID)
	}

	// View should contain confirmation text
	view := newModel.View()
	if !strings.Contains(view, "Are you sure you want to kill process 1234 (test-process)?") {
		t.Errorf("Expected confirmation view to contain process info")
	}

	// Press 'esc' to cancel
	m3, _ := newModel.Update(tea.KeyMsg{Type: tea.KeyEsc})
	newModel3 := m3.(model)

	if newModel3.confirmKillPID != 0 {
		t.Errorf("Expected confirmKillPID to be cleared after cancellation, got %d", newModel3.confirmKillPID)
	}

	// Press 'k' again to initiate confirmation
	m4, _ := newModel3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	newModel4 := m4.(model)

	// Press 'k' to confirm kill
	m5, cmd := newModel4.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	newModel5 := m5.(model)

	if newModel5.confirmKillPID != 0 {
		t.Errorf("Expected confirmKillPID to be cleared after confirmation, got %d", newModel5.confirmKillPID)
	}
	if !newModel5.killing[1234] {
		t.Errorf("Expected process 1234 to be in killing state")
	}
	if cmd == nil {
		t.Errorf("Expected a kill command to be returned")
	}
}
