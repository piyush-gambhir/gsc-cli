package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/coverage"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Offline discovery for agents: `gsc commands` describes the command tree and
// `gsc api methods` the API registry. Neither reads config or calls the network.

type flagInfo struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

type commandInfo struct {
	Command     string     `json:"command"`
	Summary     string     `json:"summary"`
	Usage       string     `json:"usage"`
	Aliases     []string   `json:"aliases"`
	Effect      string     `json:"effect"`        // read, remote_write, or local_write
	Conditional bool       `json:"conditional"`   // the effect depends on arguments (gsc api, gsc update)
	Interactive bool       `json:"interactive"`   // may prompt or open a browser
	Secret      bool       `json:"secret_output"` // prints a credential; agents must not run it
	APIMethods  []string   `json:"api_methods"`
	Flags       []flagInfo `json:"flags"`
	Examples    []string   `json:"examples"`
}

type commandList struct {
	GlobalFlags []flagInfo    `json:"global_flags"`
	Commands    []commandInfo `json:"commands"`
}

func (l commandList) Table() output.Table {
	t := output.Table{Columns: []string{"command", "effect", "summary"}}
	for _, c := range l.Commands {
		t.Rows = append(t.Rows, []any{c.Command, c.Effect, c.Summary})
	}
	return t
}

func flagList(fs *pflag.FlagSet) []flagInfo {
	out := []flagInfo{}
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		out = append(out, flagInfo{Name: f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: f.DefValue, Description: f.Usage})
	})
	return out
}

// resolution is what one set of arguments would run, decided by the real parser.
type resolution struct {
	Command     string            `json:"command"`
	Effect      string            `json:"effect"`
	Conditional bool              `json:"conditional"`
	Interactive bool              `json:"interactive"`
	Secret      bool              `json:"secret_output"`
	Flags       map[string]string `json:"flags"`
	Args        []string          `json:"args"`
}

// resolveCmd answers "what would these arguments run?" offline, so a runner can
// authorise by effect instead of guessing from argument strings.
func (a *app) resolveCmd() *cobra.Command {
	return &cobra.Command{Use: "resolve -- <arguments>", Short: "Show the command, effect and flags that arguments would run, without running them",
		DisableFlagParsing: true,
		Example:            "  gsc commands resolve -- top queries -s sc-domain:example.com --last 7d\n  gsc commands resolve -- auth -o json token",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Everything after "--" is the command being resolved; the answer is always JSON.
			for i, arg := range args {
				if arg == "--" {
					args = args[i+1:]
					break
				}
			}
			a.format = "json"
			probe := NewRoot(strings.NewReader(""), io.Discard, io.Discard)
			target, rest, err := probe.Find(args)
			if err != nil || target == probe || !target.Runnable() || isGroup(target) {
				return withKind(kindUsage, fmt.Errorf("arguments do not name a runnable gsc command (%v)", err))
			}
			if err := target.ParseFlags(rest); err != nil {
				return withKind(kindUsage, err)
			}
			info := describe(target, nil)
			res := resolution{Command: info.Command, Effect: info.Effect, Conditional: info.Conditional, Interactive: info.Interactive,
				Secret: info.Secret, Flags: map[string]string{}, Args: target.Flags().Args()}
			target.Flags().Visit(func(f *pflag.Flag) { res.Flags[f.Name] = f.Value.String() })
			return a.print(res)
		}}
}

func (a *app) commandsCmd() *cobra.Command {
	c := a.commandsListCmd()
	c.AddCommand(a.resolveCmd())
	return c
}

func (a *app) commandsListCmd() *cobra.Command {
	return &cobra.Command{Use: "commands", Short: "Describe every command, its flags, and whether it reads or writes (for agents)", Args: cobra.NoArgs,
		Long: "Offline: lists every runnable command with its usage, flags and defaults, effect (read, remote_write,\n" +
			"or local_write), whether it may prompt, the Search Console API methods it exists to call, and examples.\n" +
			"Global flags are listed once. Use -o json for the full description. Some calls are incidental and not\n" +
			"listed: a bare host given to -s, --verify, doctor --online, and login each list properties once\n" +
			"(webmasters.sites.list).",
		RunE: func(cmd *cobra.Command, args []string) error {
			methods := map[string][]string{}
			for _, e := range coverage.Entries {
				for _, c := range e.Commands {
					methods[c] = append(methods[c], e.ID)
				}
			}
			root := cmd.Root()
			list := commandList{GlobalFlags: flagList(root.PersistentFlags()), Commands: []commandInfo{}}
			var walk func(*cobra.Command)
			walk = func(c *cobra.Command) {
				for _, sub := range c.Commands() {
					if sub.Hidden || !sub.IsAvailableCommand() {
						continue
					}
					if sub.Runnable() && !isGroup(sub) {
						list.Commands = append(list.Commands, describe(sub, methods[sub.CommandPath()]))
					}
					walk(sub)
				}
			}
			walk(root)
			return a.print(list)
		}}
}

func describe(c *cobra.Command, methods []string) commandInfo {
	info := commandInfo{Command: c.CommandPath(), Summary: c.Short, Usage: c.UseLine(), Aliases: c.Aliases, Effect: "read",
		Interactive: c.Annotations[annInteractive] == "true", Secret: c.Annotations[annSecret] == "true", APIMethods: methods, Flags: flagList(c.LocalNonPersistentFlags()), Examples: []string{}}
	switch {
	case c.Annotations[annMutates] != "":
		info.Effect, info.Conditional = "remote_write", c.Annotations[annMutates] == "conditional"
	case c.Annotations[annWritesLocal] != "":
		info.Effect, info.Conditional = "local_write", c.Annotations[annWritesLocal] == "conditional"
	}
	if info.Aliases == nil {
		info.Aliases = []string{}
	}
	if info.APIMethods == nil {
		info.APIMethods = []string{}
	}
	for _, line := range strings.Split(c.Example, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			info.Examples = append(info.Examples, line)
		}
	}
	return info
}

// apiMethods is the registry behind `gsc api methods`, pinned to the vendored
// discovery snapshot (docs/api-coverage.md).
type apiMethods []map[string]any

func (m apiMethods) Table() output.Table {
	brief := func(v any) string { // the query method backs 16 commands
		cmds, _ := v.([]string)
		if len(cmds) > 3 {
			return fmt.Sprintf("%s and %d more", strings.Join(cmds[:2], ", "), len(cmds)-2)
		}
		return strings.Join(cmds, ", ")
	}
	t := output.Table{Columns: []string{"method", "http", "effect", "status", "commands"}, Human: map[string]func(any) string{"commands": brief}}
	for _, r := range m {
		t.Rows = append(t.Rows, []any{r["method"], r["http"], r["effect"], r["status"], r["commands"]})
	}
	return t
}

func (a *app) apiMethods() *cobra.Command {
	return &cobra.Command{Use: "methods", Short: "List every Search Console API method: HTTP verb, read or write effect, status, and the commands that call it",
		Args: cobra.NoArgs,
		Long: "Offline: reads the method registry pinned to the vendored discovery snapshot. path is what gsc api takes;\n" +
			"status is implemented, retired, or skipped (with a note saying why).",
		Example: "  gsc api methods\n  gsc api methods -o json",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := make(apiMethods, 0, len(coverage.Entries))
			for _, e := range coverage.Entries {
				host := "searchconsole.googleapis.com"
				if strings.HasPrefix(e.ID, "indexing.") {
					host = "indexing.googleapis.com"
				}
				effect := "write"
				if apiIsRead(e.HTTP, "/"+e.Path) {
					effect = "read"
				}
				cmds := e.Commands
				if cmds == nil {
					cmds = []string{}
				}
				rows = append(rows, map[string]any{"method": e.ID, "http": e.HTTP, "host": host, "path": "/" + e.Path, "effect": effect,
					"status": string(e.Status), "commands": cmds, "note": nullIfEmpty(e.Reason)})
			}
			return a.print(rows)
		}}
}
