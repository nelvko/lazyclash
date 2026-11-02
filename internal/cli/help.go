package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Help is presentation owned by the application. Cobra still owns command
// discovery, argument parsing and completion generation.
func renderHelp(cmd *cobra.Command, locale i18n.Localizer) {
	localizeCommandTree(cmd.Root(), locale)
	w := cmd.OutOrStdout()
	if description := strings.TrimSpace(cmd.Long); description != "" {
		fmt.Fprintln(w, description)
	} else if cmd.Short != "" {
		fmt.Fprintln(w, cmd.Short)
	}
	fmt.Fprintf(w, "\n%s\n  %s\n", locale.T("cli.help.usage"), cmd.UseLine())
	if cmd.Example != "" {
		fmt.Fprintf(w, "\n%s\n%s\n", locale.T("cli.help.examples"), cmd.Example)
	}
	type row struct{ name, description string }
	printRows := func(title string, rows []row) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s\n", title)
		width := 0
		for _, item := range rows {
			width = max(width, ansi.StringWidth(item.name))
		}
		for _, item := range rows {
			fmt.Fprintf(w, "  %s%s  %s\n", item.name, strings.Repeat(" ", width-ansi.StringWidth(item.name)), item.description)
		}
	}
	var commands []row
	for _, child := range cmd.Commands() {
		if child.IsAvailableCommand() || child.Name() == "help" {
			commands = append(commands, row{child.Name(), child.Short})
		}
	}
	printRows(locale.T("cli.help.commands"), commands)
	flagRows := func(flags *pflag.FlagSet) []row {
		var rows []row
		flags.VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden {
				return
			}
			name := "--" + flag.Name
			if flag.Shorthand != "" && flag.ShorthandDeprecated == "" {
				name = "-" + flag.Shorthand + ", " + name
			}
			kind, usage := pflag.UnquoteUsage(flag)
			if kind != "" {
				name += " " + kind
			}
			if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "0" {
				usage += locale.T("cli.help.default", flag.DefValue)
			}
			rows = append(rows, row{name, usage})
		})
		return rows
	}
	printRows(locale.T("cli.help.flags"), flagRows(cmd.LocalFlags()))
	printRows(locale.T("cli.help.global_flags"), flagRows(cmd.InheritedFlags()))
	if len(commands) > 0 {
		fmt.Fprintf(w, "\n%s\n", locale.T("cli.help.more", cmd.CommandPath()))
	}
}
