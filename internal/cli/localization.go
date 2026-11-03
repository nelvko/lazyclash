package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	mango "github.com/muesli/mango-cobra"
	"github.com/muesli/roff"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const cliVersion = "0.1.0-dev"

// Execute owns presentation so language also applies to help and parse failures.
// JSON, shell code and completion scripts continue to use their original writers.
func Execute(ctx context.Context, args []string) error {
	root := New(args...)
	root.SetArgs(args)
	if language := root.Annotations["i18n.explicit"]; language != "" {
		if _, valid := i18n.Normalize(language); !valid {
			err := diagnostic.New("diagnostic.cli.language", "Language must be en, zh-Hans or auto")
			fmt.Fprintln(root.ErrOrStderr(), commandLocale(root).Error(err))
			return err
		}
	}
	err := root.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintln(root.ErrOrStderr(), invocationError(commandLocale(root), err))
	}
	return err
}

// A read-only scan selects presentation before Cobra handles --help or rejects
// arguments. Known value flags consume their next token, and -- ends scanning.
func bootstrapPresentation(root *cobra.Command, args []string) (home, language, serviceName string) {
	flags := map[string]*pflag.Flag{}
	var collect func(*cobra.Command)
	collect = func(cmd *cobra.Command) {
		cmd.Flags().VisitAll(func(flag *pflag.Flag) { flags[flag.Name] = flag })
		cmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) { flags[flag.Name] = flag })
		for _, child := range cmd.Commands() {
			collect(child)
		}
	}
	collect(root)
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		flag := flags[name]
		if flag == nil {
			continue
		}
		if !hasValue && flag.NoOptDefVal == "" {
			if index+1 >= len(args) {
				continue
			}
			index++
			value = args[index]
		}
		switch name {
		case "home":
			home = value
		case "lang":
			language = value
		case "service-name":
			serviceName = value
		}
	}
	return home, language, serviceName
}

// Reading a language preference never creates directories, locks or services.
// A missing/damaged preference must not prevent help or env --unset from working.
func preferredLanguage(home, name string) string {
	if name == "" && !(runtime.GOOS == "linux" && os.Geteuid() == 0) {
		name = os.Getenv("LAZYCLASH_SERVICE")
	}
	if name == "" {
		name = app.DefaultService
	}
	path, err := store.HomeForService(home, name)
	if err != nil {
		return ""
	}
	raw, err := store.Read(filepath.Join(path, "preferences.json"))
	if err != nil {
		return ""
	}
	var preferences struct{ Language string }
	if json.Unmarshal(raw, &preferences) != nil {
		return ""
	}
	return preferences.Language
}

func commandLocale(cmd *cobra.Command) i18n.Localizer {
	root := cmd.Root()
	language, _ := root.PersistentFlags().GetString("lang")
	home, _ := root.PersistentFlags().GetString("home")
	name, _ := root.PersistentFlags().GetString("service-name")
	if language == "" {
		language = root.Annotations["i18n.explicit"]
	}
	if home == "" {
		home = root.Annotations["i18n.home"]
	}
	if name == "" {
		name = root.Annotations["i18n.service-name"]
	}
	return i18n.New(i18n.Resolve(language, preferredLanguage(home, name), os.Getenv))
}

func invocationError(locale i18n.Localizer, err error) string {
	if locale.Language() == i18n.English {
		return err.Error()
	}
	var missing *pflag.NotExistError
	if errors.As(err, &missing) {
		return locale.T("cli.error.unknown_flag", missing.GetSpecifiedName())
	}
	var required *pflag.ValueRequiredError
	if errors.As(err, &required) {
		return locale.T("cli.error.flag_value_required", required.GetSpecifiedName())
	}
	var invalid *pflag.InvalidValueError
	if errors.As(err, &invalid) {
		return locale.T("cli.error.invalid_flag_value", invalid.GetFlag().Name, invalid.GetValue())
	}
	var syntax *pflag.InvalidSyntaxError
	if errors.As(err, &syntax) {
		return locale.T("cli.error.flag_syntax", syntax.GetSpecifiedFlag())
	}
	// Cobra exposes no unknown-command error type. Adapt its two quoted names
	// at this framework boundary; retain unknown formats as diagnostic details.
	if rest, ok := strings.CutPrefix(err.Error(), "unknown command "); ok {
		if quoted, parseErr := strconv.QuotedPrefix(rest); parseErr == nil {
			name, _ := strconv.Unquote(quoted)
			if rest, ok := strings.CutPrefix(rest[len(quoted):], " for "); ok {
				if quoted, parseErr := strconv.QuotedPrefix(rest); parseErr == nil {
					parent, _ := strconv.Unquote(quoted)
					return locale.T("diagnostic.cli.unknown_command", name, parent) + "\n" + locale.T("cli.error.help_hint")
				}
			}
		}
		return locale.T("cli.error.unknown_command", err.Error())
	}
	return locale.Error(err)
}

func configurePresentation(root *cobra.Command, args []string) {
	root.Version = cliVersion
	root.SilenceErrors = true
	root.SetVersionTemplate("{{.Version}}\n")
	root.Flags().BoolP("version", "v", false, "Print version information")
	root.SetHelpCommand(&cobra.Command{
		Use: "help [command]", Short: "Help about any command",
		RunE: func(cmd *cobra.Command, args []string) error {
			target, rest, err := cmd.Root().Find(args)
			if err != nil {
				return err
			}
			if len(rest) != 0 {
				return diagnostic.New("diagnostic.cli.help_topic", "Unknown help topic: %s", strings.Join(args, " "))
			}
			return target.Help()
		},
	})
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	// Cobra captures the output writer when adding its default commands. Resolve
	// it at execution instead so SetOut remains usable by callers and tests.
	for _, command := range root.Commands() {
		if command.Name() != "completion" {
			continue
		}
		command.Args = noArgs
		for _, shell := range command.Commands() {
			shell.Args = noArgs
			shell.RunE = func(cmd *cobra.Command, _ []string) error {
				noDescriptions, _ := cmd.Flags().GetBool("no-descriptions")
				noDescriptions = noDescriptions || cmd.Root().CompletionOptions.DisableDescriptions
				out := cmd.OutOrStdout()
				switch cmd.Name() {
				case "bash":
					return cmd.Root().GenBashCompletionV2(out, !noDescriptions)
				case "zsh":
					if noDescriptions {
						return cmd.Root().GenZshCompletionNoDesc(out)
					}
					return cmd.Root().GenZshCompletion(out)
				case "fish":
					return cmd.Root().GenFishCompletion(out, !noDescriptions)
				case "powershell":
					if noDescriptions {
						return cmd.Root().GenPowerShellCompletion(out)
					}
					return cmd.Root().GenPowerShellCompletionWithDesc(out)
				}
				return nil
			}
		}
	}
	root.AddCommand(&cobra.Command{
		Use: "man", Short: "Generates manpages", Hidden: true, Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			localizeCommandTree(cmd.Root(), commandLocale(cmd))
			page, err := mango.NewManPage(1, cmd.Root())
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), page.Build(roff.NewDocument()))
			return err
		},
	})
	home, language, name := bootstrapPresentation(root, args)
	root.Annotations = map[string]string{"i18n.home": home, "i18n.explicit": language, "i18n.service-name": name}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) { renderHelp(cmd, commandLocale(cmd)) })
	root.SetUsageFunc(func(cmd *cobra.Command) error { renderHelp(cmd, commandLocale(cmd)); return nil })
	localizeCommandTree(root, commandLocale(root))
}

func commandMessagePath(cmd *cobra.Command) string {
	path := strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()))
	if path == "" {
		return "root"
	}
	return strings.ReplaceAll(path, " ", ".")
}

func localizeCommandTree(root *cobra.Command, locale i18n.Localizer) {
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		path := commandMessagePath(cmd)
		for _, field := range []struct {
			suffix string
			value  *string
		}{{"short", &cmd.Short}, {"long", &cmd.Long}} {
			id := "cli.command." + path + "." + field.suffix
			if translated := locale.T(id); translated != id {
				*field.value = translated
			}
		}
		cmd.InitDefaultHelpFlag()
		cmd.InitDefaultVersionFlag()
		translateFlag := func(flag *pflag.Flag) {
			id := "cli.flag." + path + "." + flag.Name
			if translated := locale.T(id); translated != id {
				flag.Usage = translated
				return
			}
			id = "cli.flag." + flag.Name
			if translated := locale.T(id); translated != id {
				flag.Usage = translated
			}
		}
		cmd.Flags().VisitAll(translateFlag)
		cmd.PersistentFlags().VisitAll(translateFlag)
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return diagnostic.New("diagnostic.cli.unknown_command", "unknown command %q for %q", args[0], cmd.CommandPath())
	}
	return nil
}

func exactArgs(count int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != count {
			return diagnostic.New("diagnostic.cli.exact_args", "accepts %d arg(s), received %d", count, len(args))
		}
		return nil
	}
}

func maximumArgs(count int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) > count {
			return diagnostic.New("diagnostic.cli.maximum_args", "accepts at most %d arg(s), received %d", count, len(args))
		}
		return nil
	}
}

func onlyValidArgs(cmd *cobra.Command, args []string) error {
	for _, value := range args {
		valid := false
		for _, candidate := range cmd.ValidArgs {
			if value == strings.SplitN(candidate, "\t", 2)[0] {
				valid = true
				break
			}
		}
		if !valid {
			return diagnostic.New("diagnostic.cli.invalid_argument", "invalid argument %q for %q", value, cmd.CommandPath())
		}
	}
	return nil
}
