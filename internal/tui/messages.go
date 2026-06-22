package tui

import (
	"time"

	"github.com/ander0code/pgflow/internal/backups"
	"github.com/ander0code/pgflow/internal/pg"
)

// timers
type (
	tickMsg        time.Time
	spinnerTickMsg time.Time
	splashTickMsg  time.Time
	statusClearMsg struct{}
)

// scanDoneMsg carries the result of scanning the backup directory.
type scanDoneMsg struct {
	folders []backups.Folder
	err     error
}

// statusDoneMsg carries the connection / tunnel health check.
type statusDoneMsg struct {
	localOK  bool
	tunnelOK bool
	prodOK   bool
}

// prodDBsMsg / localDBsMsg carry a database list for a wizard step.
type prodDBsMsg struct {
	dbs []string
	err error
}

type localDBsMsg struct {
	dbs []string
	err error
}

// logEvent is one item streamed from a running pg_dump / pg_restore: either a
// progress line, or the terminal result (done=true).
type logEvent struct {
	line       string
	done       bool
	kind       string // "dump" | "restore"
	target     string
	dumpRes    pg.DumpResult
	restoreRes pg.RestoreResult
	err        error
}

// logEventMsg delivers a logEvent into the Bubble Tea update loop.
type logEventMsg logEvent

// streamStartedMsg hands the model the channel to pull log events from.
type streamStartedMsg struct {
	ch chan logEvent
}

type verifyDoneMsg struct {
	path string
	res  pg.VerifyResult
}

type deleteDoneMsg struct {
	name string
	err  error
}

type tunnelDoneMsg struct {
	up       bool
	external bool // true if the port was already up but pgflow didn't open it
	err      error
}

type connTestMsg struct {
	label   string
	version string
	err     error
}
