package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/build"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/update"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// releases is where release checks and downloads go; a.transport replaces the
// network in tests.
func (a *app) releases() update.Source { return update.GitHub(a.transport) }

// notifierEnabled reports whether this run may check for a new release. The
// check is for people at a terminal; scripts, CI, and agents never see it.
func (a *app) notifierEnabled(cmd *cobra.Command) bool {
	switch name := cmd.Name(); {
	case name == "update", name == "version", name == "completion", name == "help", strings.HasPrefix(name, "__complete"):
		return false
	}
	if cmd.HasSubCommands() { // a bare group only prints help or a usage error
		return false
	}
	if a.quiet || !update.IsRelease(build.Version) {
		return false
	}
	for _, name := range []string{"CI", "GSC_NO_UPDATE_NOTIFIER", "NO_UPDATE_NOTIFIER"} {
		if os.Getenv(name) != "" {
			return false
		}
	}
	if a.stderrTTY != nil {
		return a.stderrTTY()
	}
	f, ok := a.errOut.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// startUpdateCheck runs in PersistentPreRun. A fresh cache answers at once;
// otherwise GitHub is queried in the background (at most once a day) while the
// command runs.
func (a *app) startUpdateCheck(cmd *cobra.Command) {
	if !a.notifierEnabled(cmd) {
		return
	}
	dir, err := config.Dir()
	if err != nil {
		return
	}
	now := time.Now()
	c := update.ReadCache(dir)
	if !c.Due(now) {
		a.updateCheck = make(chan update.Cache, 1)
		a.updateCheck <- c
		return
	}
	c.AttemptedAt = now
	if update.WriteCache(dir, c) != nil {
		// Without a saved attempt every run would check (and wait) again.
		return
	}
	a.updateCheck = make(chan update.Cache, 1)
	src := a.releases()
	a.checkStarted = true
	a.checks.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), update.CheckTimeout)
		defer cancel()
		r, err := src.Latest(ctx)
		a.updateCheck <- update.Record(dir, r, err, time.Now())
	})
}

// printUpdateNotice runs after the command's output: in PersistentPostRun, or
// in run after an error message (Cobra skips PersistentPostRun on errors). A
// cached answer is used at once. If this run started the day's check, it waits
// up to NoticeWait for the answer: the check is already recorded as attempted,
// so an answer lost to a fast command would hide the notice for a day.
func (a *app) printUpdateNotice() {
	var c update.Cache
	select {
	case c = <-a.updateCheck: // a nil channel (no check) never receives
	default:
		if !a.checkStarted {
			return
		}
		select {
		case c = <-a.updateCheck:
		case <-time.After(update.NoticeWait):
			return
		}
	}
	now := time.Now()
	if !c.ShouldNotify(build.Version, now) {
		return
	}
	method := update.MethodSelf
	if exe, err := a.executable(); err == nil {
		method = update.Method(exe, os.Getenv, homeDir())
	}
	fmt.Fprint(a.errOut, "\n"+update.Notice(build.Version, c.LatestVersion, method))
	c.NotifiedVersion, c.NotifiedAt = c.LatestVersion, now
	if dir, err := config.Dir(); err == nil {
		_ = update.WriteCache(dir, c)
	}
}
