# mkill Specification

## Overview
`mkill` is a Terminal User Interface (TUI) application written in Go. Its primary function is to act as a memory watchcat, continuously monitoring the memory utilization of processes owned by the current user and safely terminating rogue processes that exhibit sharp, uncontrolled memory spikes when system memory is critically low.

## Core Logic & Thresholds
- **Monitoring Scope:** Only processes owned by the current user.
- **Memory Occupancy (RAM + Swap):** The figure compared against the kill
  threshold is produced by `combinedMemory(*mem.VirtualMemoryStat,
  *mem.SwapMemoryStat)`, which returns a `memSample{Occupancy, RAMPct, SwapPct,
  HasSwap}`.
  - When swap is present (`SwapMemoryStat.Total > 0`), `Occupancy` spans the
    combined pool: `(ramUsed + swapUsed) / (ramTotal + swapTotal) * 100`. That
    pool is what the kernel must exhaust before the OOM killer runs, so it is
    the honest measure of how close the system is to an OOM event.
  - `ramUsed` is `Total - Available` (falling back to `Used` when `Available` is
    unavailable), keeping it consistent with gopsutil's `UsedPercent`, i.e.
    reclaimable page cache does not count as used. `swapUsed` is clamped to
    `swapTotal`.
  - Without swap (or when a reading is `nil`), `Occupancy` is the plain RAM
    occupancy and behavior is identical to the pre-swap implementation.
  - `RAMPct` and `SwapPct` are carried alongside for display only; they do not
    take part in the kill decision.
  - The rolling `totalMemHistory` that feeds the chart stores `Occupancy`.
- **Refresh Interval:** The system state and UI refresh every 2 seconds.
- **Critical Threshold:** Kill evaluations only occur when the total system memory utilization exceeds **90%**.
- **Spike Detection:** 
  - The program maintains a rolling history of the last 10 memory samples for each process.
  - A process is considered a "sharp riser" (candidate) if its memory growth rate exceeds 0.5 MB/s.
- **Swap Kill Threshold:** A second threshold, `swapThreshold`, applies to
  swap-only occupancy (`memSample.SwapPct`) and defaults to
  `swapKillThreshold` (**90%**). It exists because on a machine with a large
  swap area the combined pool figure stays low while the system thrashes.
  - `model.killTrigger()` returns the tripped trigger and reports over-threshold
    when `Occupancy >= killThreshold` **or** (swap is present and
    `SwapPct >= swapThreshold`). The mem threshold is checked first, so its
    trigger wins when both are over.
  - The trigger is recorded in the kill reason, e.g.
    `Spike: 10.00 MB/s, swap 92.1%`.
  - Adjusted interactively with `]` (increase) and `[` (decrease), clamped to
    0-100 in steps of 1.0, exactly like `+`/`-` for the memory threshold.
  - Persisted to `config.json` as `swap_threshold` when `P` is pressed. The
    field is decoded through a `*float64`, so a config predating the setting
    (or omitting the key) falls back to the default instead of being read as 0%.
  - Without swap the threshold never trips, whatever its value.
- **Kill Criteria:** To be terminated, a process must:
  1. Be the top sharpest riser (highest growth rate).
  2. The system total memory must exceed the **90%** threshold, or swap
     occupancy must reach the swap kill threshold.
  3. The process must NOT be in the "Protected" list.

## Process Protection and Manual Kill
Users can protect critical processes from being automatically terminated or manually trigger a termination.
- **Toggle Protection:** Press `p` on a selected process in the list to toggle its protected status.
- **Manual Kill:** Press `k` on a selected process to initiate a manual kill. A confirmation dialog will appear. Press `k` again to confirm the kill, or any other key to cancel.
- **Visual Feedback:** Protected processes are marked with a `p` in the second column of the process table.
- **Persistence:** Press `P` to save the current list of protected processes to the configuration file.
- **Configuration:** Settings are stored in `~/.config/mkill/config.json`, as
  `{"protected": [...], "swap_threshold": 90}`.
- **Default Protected:** By default, common critical applications like Chrome and Chrome Remote Desktop are protected.

## Graceful Termination Procedure
When a process meets the kill criteria, the application performs an asynchronous kill procedure:
1. Sends a `SIGTERM` signal.
2. Polls the process state every 500ms for up to 3 seconds.
3. If the process is still alive after 3 seconds, sends a `SIGKILL` signal.
4. Records the outcome (successful `SIGTERM` or required `SIGKILL`) in the history.
5. The procedure is non-blocking to the main UI thread.

## User Interface
Built using `charmbracelet/bubbletea` and `lipgloss`, taking up the full terminal window.
- **Header:** Displays the application title and current total system memory percentage (color-coded: Green < 80%, Yellow > 80%, Red > 90%). When swap is available, the combined percentage is followed by the RAM and swap breakdown, e.g. `Total Memory: 70.0% (RAM: 90.0% | Swap: 50.0%)`, and the threshold readout gains the swap threshold: `(Threshold: 90.0% | Swap: 85.0%)`. The readout turns red when either threshold is reached.
- **Top Pane (Process List):** A table displaying PID, Protection status (P), Name, Age (process lifetime), Memory (MB), Memory (%), and Rise Rate (MB/s). Sorted descending by total memory.
- **Bottom-Left Pane (Candidates & Protected):** Split into two sections:
  - **Kill Candidates:** A numbered list of the top 10 processes currently exhibiting sharp memory growth.
  - **Protected Processes:** A list of processes currently marked as protected.
- **Bottom-Right Pane (Kill History & Memory Graph):** The top half logs recently killed processes (timestamp, PID, name, rate). The list automatically hides older entries off the bottom to ensure the pane fits exactly within the terminal window. The bottom half displays a real-time ASCII graph of the system's total memory occupancy (%), titled `System Memory Occupancy, RAM+Swap (%)` when swap is present and `System Memory Occupancy (%)` otherwise.
- **Help Overlay:** A full-screen, centered overlay displaying keyboard shortcuts, toggled via the `?` key and closed via `esc`. Also mentions `=` and `-` for adjusting the kill threshold, `p`/`P` for protection, `]`/`[` for adjusting the swap kill threshold, and a note that the threshold is compared against combined RAM + swap occupancy when swap is present.

## Build System
- Written in Go (1.23+).
- Built and tested via **Bazel** using `rules_go` and `gazelle`.
- Automated CI/CD pipelines via GitHub Actions for testing, cross-compilation (Linux/macOS, amd64/arm64), and release generation.

## Subtask: Termui Braille Mode Memory Chart
The system memory occupancy graph in the Bottom-Right Pane is rendered using
the [`termui`](https://github.com/gizak/termui) library's `Plot` widget in
**braille mode** (`widgets.MarkerBraille`). This replaces the previous
`asciigraph` ASCII implementation and provides a higher-resolution chart by
exploiting Unicode braille glyphs, where each character cell encodes a 2x4
sub-grid of dots.

### Requirements
- **Library:** `github.com/gizak/termui/v3` is added as a Go module
  dependency. The Bazel build system (`MODULE.bazel` and `BUILD.bazel`) is
  updated via `gazelle` to wire up the new dependency.
- **Chart Style:** The data series is drawn in **green**
  (`ui.ColorGreen`). The braille marker yields a smooth line plot that
  honours sub-character resolution.
- **Integration with Bubbletea:** Because `termui` and `bubbletea` both
  manage the terminal directly, the chart is rendered offline. A
  `*ui.Buffer` of the desired width and height is allocated, the `Plot`
  widget is told to draw into it, and the resulting cell grid is
  serialised to a string with embedded ANSI escape sequences. That string
  is then composed into the bubbletea `View()` output exactly where the
  previous ASCII graph was placed.
- **Data Source:** The same rolling history of system memory occupancy
  percentages (`totalMemHistory`) feeds the chart.
- **No Terminal Init:** `termui.Init()` / `termui.Close()` are NOT
  called; only the widget's pure drawing API is used so it does not
  conflict with bubbletea's screen management.

### Testing
A unit test verifies that the chart renderer produces non-empty output and
contains the expected ANSI green foreground escape sequence when invoked
with a sample data slice.