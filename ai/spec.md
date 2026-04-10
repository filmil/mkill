# mkill Specification

## Overview
`mkill` is a Terminal User Interface (TUI) application written in Go. Its primary function is to act as a memory watchdog, continuously monitoring the memory utilization of processes owned by the current user and safely terminating rogue processes that exhibit sharp, uncontrolled memory spikes when system memory is critically low.

## Core Logic & Thresholds
- **Monitoring Scope:** Only processes owned by the current user.
- **Refresh Interval:** The system state and UI refresh every 2 seconds.
- **Critical Threshold:** Kill evaluations only occur when the total system memory utilization exceeds **90%**.
- **Spike Detection:** 
  - The program maintains a rolling history of the last 10 memory samples for each process.
  - A process is considered a "sharp riser" (candidate) if its memory growth rate exceeds 0.5 MB/s.
- **Kill Criteria:** To be terminated, a process must simply:
  1. Be the top sharpest riser (highest growth rate).
  2. The system total memory must exceed the **90%** threshold.

## Graceful Termination Procedure
When a process meets the kill criteria, the application performs an asynchronous kill procedure:
1. Sends a `SIGTERM` signal.
2. Polls the process state every 500ms for up to 3 seconds.
3. If the process is still alive after 3 seconds, sends a `SIGKILL` signal.
4. Records the outcome (successful `SIGTERM` or required `SIGKILL`) in the history.
5. The procedure is non-blocking to the main UI thread.

## User Interface
Built using `charmbracelet/bubbletea` and `lipgloss`, taking up the full terminal window.
- **Header:** Displays the application title and current total system memory percentage (color-coded: Green < 80%, Yellow > 80%, Red > 90%).
- **Top Pane (Process List):** A table displaying PID, Name, Memory (MB), Memory (%), and Rise Rate (MB/s). Sorted descending by total memory.
- **Bottom-Left Pane (Kill Candidates):** A numbered list of the top 10 processes currently exhibiting sharp memory growth.
- **Bottom-Right Pane (Kill History):** A log of recently killed processes, including timestamp, PID, name, and the specific reason/rate.
- **Help Overlay:** A full-screen, centered overlay displaying keyboard shortcuts, toggled via the `?` key and closed via `esc`. Also mentions `=` and `-` for adjusting the kill threshold.

## Build System
- Written in Go (1.23+).
- Built and tested via **Bazel** using `rules_go` and `gazelle`.
- Automated CI/CD pipelines via GitHub Actions for testing, cross-compilation (Linux/macOS, amd64/arm64), and release generation.