/*
Copyright 2025 The Scion Authors.
*/

package supervisor

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/GoogleCloudPlatform/scion/pkg/sciontool/log"
)

// snapshotProcessNames reads process names from /proc for all current
// child processes. This must be called before reaping, since /proc/<pid>
// entries are removed once wait() completes.
func snapshotProcessNames() map[int]string {
	names := make(map[int]string)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return names
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err != nil {
			continue
		}
		if name := strings.TrimSpace(string(comm)); name != "" {
			names[pid] = name
		}
	}
	return names
}

// findZombieOrphans scans /proc for zombie processes whose parent is PID 1.
// These are true orphans that no one will wait on — safe to reap.
// Returns a set of PIDs to reap.
func findZombieOrphans() map[int]bool {
	orphans := make(map[int]bool)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return orphans
	}
	myPID := os.Getpid()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		// /proc/PID/stat: "pid (comm) state ppid ..."
		s := string(stat)
		closeParen := strings.LastIndex(s, ")")
		if closeParen < 0 || closeParen+2 >= len(s) {
			continue
		}
		fields := strings.Fields(s[closeParen+2:])
		if len(fields) < 2 {
			continue
		}
		state := fields[0]
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		// Only reap zombies (state "Z") whose parent is us (PID 1 / sciontool).
		if state == "Z" && ppid == myPID {
			orphans[pid] = true
		}
	}
	return orphans
}

// StartReaper starts a goroutine that reaps orphaned zombie processes and
// logs them. It scans /proc for zombies parented to PID 1 and only reaps
// those specific PIDs, avoiding races with Go's exec.Command.Wait() which
// waits on its own children.
func StartReaper() {
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGCHLD)

		for range sigs {
			// Snapshot process names before reaping.
			names := snapshotProcessNames()

			// Find zombie orphans (ppid == us, state == Z) before reaping.
			orphans := findZombieOrphans()

			// Reap only the identified orphan zombies by specific PID.
			for pid := range orphans {
				var ws syscall.WaitStatus
				reaped, err := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
				if err != nil || reaped <= 0 {
					continue
				}

				reason := "exited"
				if ws.Signaled() {
					reason = "killed by signal " + ws.Signal().String()
				}
				name := names[pid]
				if name == "" {
					name = "unknown"
				}
				log.Info("Reaped zombie process %d (%s) (reason: %s, exit code: %d)", pid, name, reason, ws.ExitStatus())
			}
		}
	}()
}
