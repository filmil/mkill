# mkill Specification

## Overview
`mkill` is a Terminal User Interface (TUI) application written in Go. Its primary function is to act as a memory watchcat, continuously monitoring the memory utilization of processes owned by the current user and safely terminating rogue processes that exhibit sharp, uncontrolled memory spikes when system memory is critically low.

## Core Logic & Thresholds
- **Monitoring Scope:** Only processes owned by the current user.
- **Refresh Interval:** The system state and UI refresh every 2 seconds.
- **Critical Threshold:** Kill evaluations only occur when the total system memory utilization exceeds **90%**.
- **Spike Detection:** 
  - The program maintains a rolling history of the last 10 memory samples for each process.
  - A process is considered a "sharp riser" (candidate) if its memory growth rate exceeds 0.5 MB/s.
- **Kill Criteria:** To be terminated, a process must:
  1. Be the top sharpest riser (highest growth rate).
  2. The system total memory must exceed the **90%** threshold.
  3. The process must NOT be in the "Protected" list.

## Process Protection and Manual Kill
Users can protect critical processes from being automatically terminated or manually trigger a termination.
- **Toggle Protection:** Press `p` on a selected process in the list to toggle its protected status.
- **Manual Kill:** Press `k` on a selected process to initiate a manual kill. A confirmation dialog will appear. Press `k` again to confirm the kill, or any other key to cancel.
- **Visual Feedback:** Protected processes are marked with a `p` in the second column of the process table.
- **Persistence:** Press `P` to save the current list of protected processes to the configuration file.
- **Configuration:** Settings are stored in `~/.config/mkill/config.json`.
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
- **Header:** Displays the application title and current total system memory percentage (color-coded: Green < 80%, Yellow > 80%, Red > 90%).
- **Top Pane (Process List):** A table displaying PID, Protection status (P), Name, Memory (MB), Memory (%), and Rise Rate (MB/s). Sorted descending by total memory.
- **Bottom-Left Pane (Candidates & Protected):** Split into two sections:
  - **Kill Candidates:** A numbered list of the top 10 processes currently exhibiting sharp memory growth.
  - **Protected Processes:** A list of processes currently marked as protected.
- **Bottom-Right Pane (Kill History & Memory Graph):** The top half logs recently killed processes (timestamp, PID, name, rate). The bottom half displays a real-time ASCII graph of the system's total memory occupancy (%).
- **Help Overlay:** A full-screen, centered overlay displaying keyboard shortcuts, toggled via the `?` key and closed via `esc`. Also mentions `=` and `-` for adjusting the kill threshold, and `p`/`P` for protection.

## Build System
- Written in Go (1.23+).
- Built and tested via **Bazel** using `rules_go` and `gazelle`.
- Automated CI/CD pipelines via GitHub Actions for testing, cross-compilation (Linux/macOS, amd64/arm64), and release generation.