package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/build"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/update"
	"github.com/spf13/cobra"
)

func (a *app) update() *cobra.Command {
	var check bool
	c := &cobra.Command{
		Use:   "update",
		Short: "Update gsc to the latest release (SHA-256 verified)",
		Long: "Downloads the latest GitHub release for this OS and architecture, verifies it against the release's\n" +
			"checksums.txt, and replaces the running gsc executable. Works on macOS, Linux, and Windows; on Windows\n" +
			"the old executable is renamed to gsc.exe.old and deleted on a later run.\n\n" +
			"Asks \"Update now? [Y/n]\" when stdin is a terminal; --yes skips the question, and --no-input requires\n" +
			"--yes. --check only reports the current and latest versions. A gsc in a Go bin directory ($GOBIN,\n" +
			"$GOPATH/bin, or ~/go/bin) is not replaced: rebuild it from your checkout instead. --read-only blocks\n" +
			"installing but allows --check.\n\n" +
			"Update notice: in an interactive terminal, gsc checks the github.com releases page (not the GitHub API,\n" +
			"so its rate limit never applies) for a new release at most once a day and, after a command's output,\n" +
			"prints a notice on stderr. The command that runs the day's check waits up to 1 second after its output\n" +
			"for the answer; other commands never wait. gsc update and update --check store their result in the same\n" +
			"cache. It never checks when stderr is not a terminal, when CI is set, with --quiet, or when\n" +
			"GSC_NO_UPDATE_NOTIFIER or NO_UPDATE_NOTIFIER is set (to anything).",
		Example:     "  gsc update --check\n  gsc update\n  gsc update --yes --no-input",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{annWritesLocal: "conditional", annInteractive: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.readOnly && !check {
				return withKind(kindReadOnly, fmt.Errorf("self-update is blocked by --read-only; use update --check"))
			}
			current := build.Version
			exe, err := a.executable()
			if err != nil {
				return err
			}
			method := update.Method(exe, os.Getenv, homeDir())
			r, err := a.releases().Latest(cmd.Context())
			dir, dirErr := config.Dir()
			if dirErr == nil {
				update.Record(dir, r, err, time.Now())
			}
			if err != nil {
				return err
			}
			// A local build ("dev") can always install the release.
			available := update.Newer(r.Version(), current) || !update.IsRelease(current)
			result := map[string]any{"current_version": current, "latest_version": r.Version(),
				"update_available": available, "release_url": r.URL, "install_method": method}
			if check {
				return a.print(result)
			}
			result["updated"] = false
			from, to := "v"+strings.TrimPrefix(current, "v"), r.Tag
			if !update.IsRelease(current) {
				from = current
			}
			if !available {
				return a.updateResult(result, fmt.Sprintf("gsc %s is already the latest version.", from))
			}
			if method == update.MethodGo {
				result["update_command"] = update.SourceUpdate
				return a.updateResult(result, fmt.Sprintf("gsc %s -> %s is available. This gsc is in a Go bin directory (%s), so it is not replaced.\nUpdate with: %s\nRelease notes: %s",
					from, to, filepath.Dir(exe), update.SourceUpdate, r.URL))
			}
			if a.noInput && !a.yes && !a.dryRun {
				return withKind(kindConfirmation, fmt.Errorf("gsc update needs confirmation; pass --yes to update to %s without a prompt", to))
			}
			goos := runtime.GOOS
			if err := update.CheckWritable(exe, goos); err != nil {
				return err
			}
			if a.dryRun {
				return a.updateResult(result, fmt.Sprintf("Would update gsc %s -> %s at %s (dry run; nothing downloaded).", from, to, exe))
			}
			if !a.yes && a.isTerminal() {
				fmt.Fprintf(a.errOut, "gsc %s -> %s\nUpdate now? [Y/n] ", from, to)
				line, err := a.reader().ReadString('\n')
				// Enter means yes; Ctrl-D (EOF) or a read error is not an answer.
				if answer := strings.ToLower(strings.TrimSpace(line)); err != nil || (answer != "" && answer != "y" && answer != "yes") {
					return errors.New("update cancelled")
				}
			}
			a.info("Downloading gsc %s...", to)
			bin, err := a.releases().Fetch(cmd.Context(), r, goos, runtime.GOARCH)
			if err != nil {
				return err
			}
			if err := update.Replace(exe, bin, goos); err != nil {
				return err
			}
			result["updated"] = true
			return a.updateResult(result, fmt.Sprintf("Updated gsc %s -> %s\nRelease notes: %s", from, to, r.URL))
		}}
	c.Flags().BoolVar(&check, "check", false, "Only report the current and latest versions (always checks the github.com releases page)")
	return c
}

// updateResult prints the outcome as text in table mode and as data otherwise.
func (a *app) updateResult(data map[string]any, text string) error {
	if a.format == "table" {
		_, err := fmt.Fprintln(a.out, text)
		return err
	}
	return a.print(data)
}

// executable is the resolved path of the running gsc.
func (a *app) executable() (string, error) {
	if a.exePath != "" {
		return a.exePath, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
