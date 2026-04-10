# mkill

[![Build and Test](https://github.com/filmil/mkill/actions/workflows/build-test.yml/badge.svg)](https://github.com/filmil/mkill/actions/workflows/build-test.yml)
[![Release](https://github.com/filmil/mkill/actions/workflows/release.yml/badge.svg)](https://github.com/filmil/mkill/actions/workflows/release.yml)

`mkill` is a Go program that continuously monitors memory usage of the programs
owned by the current user. It kills the program with the sharpest rise in
memory utilization when the overall system memory occupancy goes beyond the
user-configured kill threshold.

![mkill demo](demo.gif)

The program keeps a running history of memory use to ensure that it does not
kill long-running stable programs. It uses the `bubbletea` library to create a
top-like TUI (Terminal User Interface) widget which displays processes by
memory use, taking the entire size of the terminal and refreshing every few
seconds. It also features a pane which shows kill candidates, another which
shows which processes have been killed and why, and a pane listing explicitly
protected processes.

## Features

- **Continuous Monitoring**: Scans all processes owned by the current user
  every 2 seconds acting as a memory watchcat.
- **Smart Killing**: Triggers only when system memory exceeds the threshold.
  Identifies "sharp risers" by calculating the rate of memory growth (MB/s) and
  comparing current usage against a history of the last 10 samples.
- **Graceful Termination**: Sends `SIGTERM` first, waits up to 3 seconds, and
  if the process hasn't exited, sends `SIGKILL`.
- **Interactive TUI**: Top pane for process list, bottom-left pane split
  between a numbered list of kill candidates and protected processes. The
  bottom-right pane is split between kill history and a real-time ASCII graph
  of system memory occupancy. The memory occupancy percentage is color-coded
  green, orange (near limit), and red (over limit).
- **Adjustable Threshold**: Press `+` or `=` to increase, or `-` to decrease
  the kill threshold interactively.
- **Process Protection**: Press `p` while selecting a process to add its name
  to the global protect list, preventing it from ever being killed. Press `P`
  to persist this config across sessions (saved to
  `~/.config/mkill/config.json`). Protected processes are marked with a `p` in
  the table.
- **Manual Kill**: Press `k` to kill the currently highlighted process. Requires
  pressing `k` a second time to confirm.
- **Help Panel**: Press `?` to toggle a help screen overlay.
- **Configuration Persistence**: Press `P` to save your protected processes
  list to `~/.config/mkill/config.json`. These settings are automatically
  loaded on startup.

## Installation

### From Release
Download the latest binary for your architecture from the [Releases](https://github.com/filmil/mkill/releases) page.

### From Source (using Bazel)
```bash
bazel build //:mkill
./bazel-bin/mkill_/mkill
```

### From Source (using Go)
```bash
go build -o mkill main.go
./mkill
```

## Usage

Simply run the binary:
```bash
./mkill
```
