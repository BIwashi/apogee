// Package-level comment is in doc.go / root.go; this file implements
// the `apogee onboard` interactive setup wizard (PR #31).
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/BIwashi/apogee/internal/daemon"
	"github.com/BIwashi/apogee/internal/summarizer"
	"github.com/BIwashi/apogee/internal/telemetry"
)

// onboardOptions is the resolved flag set passed into runOnboard. Kept
// as a plain struct so tests can construct it directly without going
// through cobra. The manager seam is an interface so the wizard can
// drive a fake daemon.Manager without ever touching launchctl /
// systemctl.
type onboardOptions struct {
	Yes            bool
	ConfigPath     string
	DBPath         string
	Addr           string
	SkipDaemon     bool
	SkipHooks      bool
	SkipSummarizer bool
	SkipTelemetry  bool
	SkipMenubar    bool
	DryRun         bool
	NoOpenBrowser  bool

	// ManagerFactory is indirected so onboard_test.go can inject a
	// fake Manager without touching the package-level managerFactory
	// used by `apogee daemon`. Defaults to managerFactory when nil.
	ManagerFactory func() (daemon.Manager, error)

	// MenubarManagerFactory is the second-manager seam for the
	// menubar-login-item install path. Defaults to the package-level
	// menubarManagerFactory (which in turn wraps
	// daemon.NewManagerWithLabel(daemon.MenubarLabel)). Tests pass a
	// fake that records Install/Uninstall calls on the menubar label.
	MenubarManagerFactory func() (daemon.Manager, error)

	// LoadPrefs and WritePrefs are injectable seams so the tests can
	// capture summarizer-preference reads and writes without opening a
	// real DuckDB file. When nil, the production helpers that wrap
	// duckdb.Store are used.
	LoadPrefs  func(ctx context.Context, dbPath string) (summarizer.Preferences, error)
	WritePrefs func(ctx context.Context, dbPath string, prefs summarizer.Preferences) error

	// StartDaemon is a test seam for actually starting the installed
	// daemon. Defaults to calling Manager.Start. Tests pass a no-op.
	StartDaemon func(ctx context.Context, m daemon.Manager) error

	// OpenBrowser is a test seam mirroring `apogee open`. Defaults to
	// the OS-specific helper used by the production open command.
	OpenBrowser func(ctx context.Context, out io.Writer, url string) error

	// Stdin/Stdout/Stderr let tests inject buffers. cobra fills these
	// from the parent command but the wizard is also callable from
	// tests that construct an onboardOptions directly.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// onboardState is the live snapshot of everything the wizard needs
// to populate its defaults from. Every field is best-effort — a
// fresh machine with none of the pieces installed still returns a
// valid state, it just has every bool set to "absent".
type onboardState struct {
	ConfigPath      string
	HomeDir         string
	HooksInstalled  bool
	HooksPath       string
	DaemonStatus    daemon.Status
	DaemonOK        bool
	MenubarStatus   daemon.Status
	MenubarOK       bool
	MenubarPlatform bool
	Prefs           summarizer.Preferences
	Defaults        summarizer.Config // canonical defaults for placeholder hints
	// Models is the merged catalog + availability view the wizard
	// drives the model dropdowns from. Populated by loadOnboardState
	// best-effort: a fresh install with no DuckDB cache still yields a
	// usable slice (every row available=true, "unknown = assume
	// available"). Never nil after loadOnboardState.
	Models          []summarizer.ModelInfo
	ModelsAvailable map[string]bool
	// RecapDefault / RollupDefault / NarrativeDefault are the
	// cheapest-currently-available aliases per tier, precomputed so
	// the form doesn't re-run the resolver on every keystroke.
	RecapDefault     string
	RollupDefault    string
	NarrativeDefault string
	Telemetry        telemetry.Config
	HasTelemetry     bool
}

// onboardPlan is the committed, about-to-be-applied shape of the
// wizard's decisions. Once renderOnboardPlan has shown it and the
// user has confirmed, applyOnboardPlan walks each field and invokes
// the corresponding package-level helper.
type onboardPlan struct {
	// Hooks
	HooksAction     string // "install" | "reinstall" | "skip"
	SourceAppPinned string

	// Daemon
	DaemonAction string // "install" | "reinstall" | "skip"
	Addr         string
	DBPath       string
	StartDaemon  bool

	// Menubar — macOS-only login item (second launchd unit).
	// "install" | "reinstall" | "skip".  On non-darwin the onboard
	// wizard hides the group and forces "skip".
	MenubarAction string

	// Summarizer
	SkipSummarizer         bool
	SummarizerLanguage     string
	RecapSystemPrompt      string
	RollupSystemPrompt     string
	NarrativeSystemPrompt  string
	RecapModelOverride     string
	RollupModelOverride    string
	NarrativeModelOverride string

	// OTel
	SkipTelemetry     bool
	TelemetryEnabled  bool
	TelemetryEndpoint string
	TelemetryProtocol string

	// Browser
	OpenBrowser bool
}

// NewOnboardCmd builds the `apogee onboard` cobra subcommand. Every
// knob has a flag so scripting / CI can drive the whole flow without
// ever reading a prompt. The interactive path is entered only when
// every precondition is met: stdin is a TTY, --yes is not set, and
// APOGEE_ONBOARD_NONINTERACTIVE is unset.
func NewOnboardCmd(stdout, stderr io.Writer) *cobra.Command {
	opts := onboardOptions{}
	var nonInteractive bool
	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Interactive setup wizard: hooks + daemon + summarizer + dashboard",
		Long: `Walk through the full apogee install in one command:

  1. Install Claude Code hooks into ~/.claude/settings.json
  2. Install apogee as a user-scope background service
  3. Configure the LLM summarizer (language + optional system prompts)
  4. Optionally wire an external OTLP endpoint
  5. Start the daemon and open the dashboard

Every prompt's default is loaded from the current state on disk
(config.toml, DuckDB preferences, settings.json, daemon status), so
re-running ` + "`apogee onboard`" + ` is safe and each run only proposes
the deltas you actually want to apply.

Pass --yes to accept every default and skip the prompts — this is
the provisioning / CI path.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if nonInteractive {
				opts.Yes = true
			}
			if os.Getenv("APOGEE_ONBOARD_NONINTERACTIVE") != "" {
				opts.Yes = true
			}
			opts.Stdout = styledWriter(stdout)
			opts.Stderr = stderr
			opts.Stdin = os.Stdin
			return runOnboard(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Accept all defaults without prompting")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Alias for --yes (scripting friendly)")
	cmd.Flags().StringVar(&opts.ConfigPath, "config", "~/.apogee/config.toml", "Config file to write")
	cmd.Flags().StringVar(&opts.DBPath, "db", "~/.apogee/apogee.duckdb", "DuckDB file")
	cmd.Flags().StringVar(&opts.Addr, "addr", daemon.DefaultAddr, "Collector bind address")
	cmd.Flags().BoolVar(&opts.SkipDaemon, "skip-daemon", false, "Do not install / start the daemon")
	cmd.Flags().BoolVar(&opts.SkipHooks, "skip-hooks", false, "Do not install hooks into .claude/settings.json")
	cmd.Flags().BoolVar(&opts.SkipSummarizer, "skip-summarizer", false, "Do not write summarizer preferences")
	cmd.Flags().BoolVar(&opts.SkipTelemetry, "skip-telemetry", false, "Do not configure OTLP export")
	cmd.Flags().BoolVar(&opts.SkipMenubar, "skip-menubar", false, "Do not register the macOS menubar as a login item")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show the plan without writing anything")
	return cmd
}

// runOnboard is the cobra-agnostic entry point. Tests call this
// directly with a fully-populated onboardOptions.
func runOnboard(ctx context.Context, opts onboardOptions) error {
	fillDefaults(&opts)

	current, err := loadOnboardState(ctx, opts)
	if err != nil {
		return err
	}

	plan := toPlanDefaults(opts, current)

	// Interactive step — only when we have a TTY AND the user did not
	// opt out. huh.Form bails out on non-tty anyway but we short-
	// circuit to avoid surprising behaviour when piped.
	if !opts.Yes && !opts.DryRun && isInteractive(opts.Stdin) {
		if err := promptOnboardPlan(&plan, current, opts); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Fprintln(opts.Stdout, styleMuted.Render("cancelled"))
				return nil
			}
			return err
		}
	}

	// Dry-run: render the plan and exit.
	if opts.DryRun {
		renderOnboardPlan(opts.Stdout, plan, opts)
		fmt.Fprintln(opts.Stdout, styleMuted.Render("Run without --dry-run to apply."))
		return nil
	}

	// Final confirmation when interactive. --yes / --non-interactive
	// / non-TTY paths skip the prompt entirely.
	if !opts.Yes && isInteractive(opts.Stdin) {
		renderOnboardPlan(opts.Stdout, plan, opts)
		var confirm bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Apply these changes?").
					Affirmative("Yes").
					Negative("No").
					Value(&confirm),
			),
		)
		if err := confirmForm.Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Fprintln(opts.Stdout, styleMuted.Render("cancelled"))
				return nil
			}
			return err
		}
		if !confirm {
			fmt.Fprintln(opts.Stdout, styleMuted.Render("cancelled"))
			return nil
		}
	}

	return applyOnboardPlan(ctx, plan, opts, current)
}

// fillDefaults wires default values for every optional field on opts
// so the rest of the file can assume non-nil seams.
func fillDefaults(opts *onboardOptions) {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.ManagerFactory == nil {
		opts.ManagerFactory = managerFactory
	}
	if opts.MenubarManagerFactory == nil {
		opts.MenubarManagerFactory = menubarManagerFactory
	}
	if opts.LoadPrefs == nil {
		opts.LoadPrefs = loadSummarizerPreferencesFromDB
	}
	if opts.WritePrefs == nil {
		opts.WritePrefs = writeSummarizerPreferencesToDB
	}
	if opts.StartDaemon == nil {
		opts.StartDaemon = func(ctx context.Context, m daemon.Manager) error {
			return m.Start(ctx)
		}
	}
	if opts.OpenBrowser == nil {
		opts.OpenBrowser = func(ctx context.Context, out io.Writer, url string) error {
			return openURL(out, out, url)
		}
	}
	if opts.Addr == "" {
		opts.Addr = daemon.DefaultAddr
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "~/.apogee/config.toml"
	}
	if opts.DBPath == "" {
		opts.DBPath = "~/.apogee/apogee.duckdb"
	}
}

// isInteractive returns true when r looks like a real terminal. The
// check is conservative: anything other than *os.File with a TTY fd
// is treated as non-interactive so piped / captured / null stdin
// never strands the user at a prompt that cannot be answered.
func isInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok || f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// loadOnboardState inspects every side channel (config.toml, daemon
// manager, DuckDB preferences, settings.json) and returns the live
// snapshot the wizard prompts from.
func loadOnboardState(ctx context.Context, opts onboardOptions) (onboardState, error) {
	state := onboardState{}

	if home, err := os.UserHomeDir(); err == nil {
		state.HomeDir = home
	}

	// Resolve the config + db paths so everything downstream works on
	// absolute filesystem locations.
	if expanded, err := expandHome(opts.ConfigPath); err == nil {
		state.ConfigPath = expanded
	} else {
		state.ConfigPath = opts.ConfigPath
	}

	dbPath, err := expandHome(opts.DBPath)
	if err != nil {
		dbPath = opts.DBPath
	}

	// Hooks presence — read ~/.claude/settings.json if present and
	// check whether any apogee-binary hook command is already wired.
	settingsPath := userSettingsPath(state.HomeDir)
	state.HooksPath = settingsPath
	if raw, err := os.ReadFile(settingsPath); err == nil {
		var settings map[string]any
		if json.Unmarshal(raw, &settings) == nil {
			hooks, _ := hooksSectionOf(settings)
			for _, ev := range HookEvents {
				if hookEventCoveredByApogee(listOf(hooks[ev])) {
					state.HooksInstalled = true
					break
				}
			}
		}
	}

	// Daemon — ask the manager. Missing manager / unsupported platform
	// / error just leaves DaemonOK=false and the wizard treats the
	// daemon section as "not installed".
	if opts.ManagerFactory != nil {
		m, err := opts.ManagerFactory()
		if err == nil && m != nil {
			if s, serr := m.Status(ctx); serr == nil || errors.Is(serr, daemon.ErrNotSupported) {
				state.DaemonStatus = s
				state.DaemonOK = true
			}
		}
	}

	// Menubar — second launchd unit on darwin only. We probe the
	// menubar manager the same way we probe the collector daemon so
	// the wizard can default the action to "reinstall" when the
	// plist already exists and to "install" on a fresh machine.
	// Non-darwin always leaves MenubarPlatform=false which hides
	// the menubar group in the form and forces action=skip in the
	// plan defaults.
	state.MenubarPlatform = isMenubarPlatform()
	if state.MenubarPlatform && opts.MenubarManagerFactory != nil {
		mm, err := opts.MenubarManagerFactory()
		if err == nil && mm != nil {
			if s, serr := mm.Status(ctx); serr == nil || errors.Is(serr, daemon.ErrNotSupported) {
				state.MenubarStatus = s
				state.MenubarOK = true
			}
		}
	}

	// Summarizer preferences — best effort; a locked / missing DB
	// just yields summarizer.Defaults().
	if !opts.SkipSummarizer && opts.LoadPrefs != nil {
		prefs, err := opts.LoadPrefs(ctx, dbPath)
		if err == nil {
			state.Prefs = prefs
		} else {
			state.Prefs = summarizer.Defaults()
		}
	} else {
		state.Prefs = summarizer.Defaults()
	}

	// Canonical summarizer defaults — used as placeholder hints in
	// the wizard so every model override row shows the default model
	// alias next to the input, and every system prompt textarea
	// shows an example.
	state.Defaults = summarizer.Default()

	// Load the model catalog + availability cache. The cache is best
	// effort: a fresh install (empty DuckDB, locked DB, missing file)
	// still yields the full KnownModels slice with availability
	// defaulting to true on every row.
	state.ModelsAvailable = loadModelAvailability(ctx, dbPath)
	state.Models = mergeModelCatalog(summarizer.KnownModels, state.ModelsAvailable)
	state.RecapDefault = summarizer.ResolveDefaultModel(summarizer.UseCaseRecap, state.ModelsAvailable)
	state.RollupDefault = summarizer.ResolveDefaultModel(summarizer.UseCaseRollup, state.ModelsAvailable)
	state.NarrativeDefault = summarizer.ResolveDefaultModel(summarizer.UseCaseNarrative, state.ModelsAvailable)

	// Telemetry — read the TOML file directly rather than going
	// through telemetry.LoadConfig because the latter also overlays
	// env vars, which would let a locally-set OTEL_EXPORTER_OTLP_*
	// poison the wizard's defaults.
	if _, statErr := os.Stat(state.ConfigPath); statErr == nil {
		var tf struct {
			Telemetry struct {
				Enabled  *bool  `toml:"enabled"`
				Endpoint string `toml:"endpoint"`
				Protocol string `toml:"protocol"`
			} `toml:"telemetry"`
		}
		if _, err := toml.DecodeFile(state.ConfigPath, &tf); err == nil {
			state.HasTelemetry = true
			if tf.Telemetry.Enabled != nil {
				state.Telemetry.Enabled = *tf.Telemetry.Enabled
			}
			if tf.Telemetry.Endpoint != "" {
				state.Telemetry.Endpoint = tf.Telemetry.Endpoint
				if tf.Telemetry.Enabled == nil {
					state.Telemetry.Enabled = true
				}
			}
			if tf.Telemetry.Protocol != "" {
				state.Telemetry.Protocol = telemetry.Protocol(strings.ToLower(tf.Telemetry.Protocol))
			}
		}
	}
	if state.Telemetry.Protocol == "" {
		state.Telemetry.Protocol = telemetry.ProtocolGRPC
	}

	return state, nil
}

// userSettingsPath returns ~/.claude/settings.json given a resolved
// home directory. When home is empty we fall back to the literal
// path and let downstream reads fail gracefully.
func userSettingsPath(home string) string {
	if home == "" {
		return filepath.Join("~", ".claude", "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// toPlanDefaults seeds an onboardPlan from the current state +
// skip flags. The plan is what the wizard uses as the initial value
// for each prompt (or, in --yes mode, the final committed plan).
func toPlanDefaults(opts onboardOptions, state onboardState) onboardPlan {
	plan := onboardPlan{
		Addr:              opts.Addr,
		DBPath:            opts.DBPath,
		OpenBrowser:       !opts.Yes, // --yes / CI: leave the browser closed
		SkipSummarizer:    opts.SkipSummarizer,
		SkipTelemetry:     opts.SkipTelemetry,
		TelemetryEndpoint: "http://localhost:4317",
		TelemetryProtocol: string(telemetry.ProtocolGRPC),
	}

	// Hooks
	switch {
	case opts.SkipHooks:
		plan.HooksAction = "skip"
	case state.HooksInstalled:
		plan.HooksAction = "reinstall"
	default:
		plan.HooksAction = "install"
	}

	// Daemon
	switch {
	case opts.SkipDaemon:
		plan.DaemonAction = "skip"
	case state.DaemonStatus.Installed:
		plan.DaemonAction = "reinstall"
	default:
		plan.DaemonAction = "install"
	}
	// Start if we installed/reinstalled AND the daemon is not
	// already running.
	plan.StartDaemon = plan.DaemonAction != "skip" && !state.DaemonStatus.Running

	// Menubar — macOS-only login item. Default to install on a
	// fresh mac, reinstall when the plist already exists, and skip
	// on non-darwin (the group is hidden in the form too).
	switch {
	case opts.SkipMenubar:
		plan.MenubarAction = "skip"
	case !state.MenubarPlatform:
		plan.MenubarAction = "skip"
	case state.MenubarStatus.Installed:
		plan.MenubarAction = "reinstall"
	default:
		plan.MenubarAction = "install"
	}

	// Summarizer — language defaults to the persisted value or "en".
	if state.Prefs.Language != "" {
		plan.SummarizerLanguage = state.Prefs.Language
	} else {
		plan.SummarizerLanguage = summarizer.LanguageEN
	}
	plan.RecapSystemPrompt = state.Prefs.RecapSystemPrompt
	plan.RollupSystemPrompt = state.Prefs.RollupSystemPrompt
	plan.NarrativeSystemPrompt = state.Prefs.NarrativeSystemPrompt
	plan.RecapModelOverride = state.Prefs.RecapModelOverride
	plan.RollupModelOverride = state.Prefs.RollupModelOverride
	plan.NarrativeModelOverride = state.Prefs.NarrativeModelOverride

	// OTel
	if state.Telemetry.Endpoint != "" {
		plan.TelemetryEndpoint = state.Telemetry.Endpoint
	}
	if state.Telemetry.Protocol != "" {
		plan.TelemetryProtocol = string(state.Telemetry.Protocol)
	}
	if state.Telemetry.Enabled && state.Telemetry.Endpoint != "" {
		plan.TelemetryEnabled = true
	}

	return plan
}

// promptOnboardPlan drives the huh.Form for the interactive path.
// The form is built from the plan's current defaults and mutates
// them in place. Cancelled prompts surface ErrUserAborted which the
// caller translates into a clean "cancelled" exit.
func promptOnboardPlan(plan *onboardPlan, state onboardState, opts onboardOptions) error {
	// Hooks group
	hookChoices := []huh.Option[string]{
		huh.NewOption("Install", "install"),
		huh.NewOption("Skip", "skip"),
	}
	if state.HooksInstalled {
		hookChoices = []huh.Option[string]{
			huh.NewOption("Re-install (refresh entries)", "reinstall"),
			huh.NewOption("Skip (already installed)", "skip"),
		}
	}

	daemonChoices := []huh.Option[string]{
		huh.NewOption("Install", "install"),
		huh.NewOption("Skip", "skip"),
	}
	if state.DaemonStatus.Installed {
		daemonChoices = []huh.Option[string]{
			huh.NewOption("Re-install", "reinstall"),
			huh.NewOption("Skip (already installed)", "skip"),
		}
	}

	menubarChoices := []huh.Option[string]{
		huh.NewOption("Install", "install"),
		huh.NewOption("Skip", "skip"),
	}
	if state.MenubarStatus.Installed {
		menubarChoices = []huh.Option[string]{
			huh.NewOption("Re-install", "reinstall"),
			huh.NewOption("Skip (already installed)", "skip"),
		}
	}

	protoChoices := []huh.Option[string]{
		huh.NewOption("grpc", "grpc"),
		huh.NewOption("http/protobuf", "http"),
	}

	langChoices := []huh.Option[string]{
		huh.NewOption("English (en)", summarizer.LanguageEN),
		huh.NewOption("日本語 (ja)", summarizer.LanguageJA),
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("APOGEE ONBOARD").
				Description("Interactive setup. Every default is loaded from your current state, so re-runs only propose the deltas."),
			huh.NewSelect[string]().
				Title("Install Claude Code hooks at ~/.claude/settings.json?").
				Options(hookChoices...).
				Value(&plan.HooksAction),
			huh.NewInput().
				Title("Pin the source_app label? (leave empty for dynamic derivation)").
				Value(&plan.SourceAppPinned),
		).WithHideFunc(func() bool { return opts.SkipHooks }),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Install apogee as a background service (launchd / systemd)?").
				Options(daemonChoices...).
				Value(&plan.DaemonAction),
			huh.NewInput().
				Title("Collector bind address").
				Value(&plan.Addr),
			huh.NewConfirm().
				Title("Start the daemon immediately after install?").
				Value(&plan.StartDaemon),
		).WithHideFunc(func() bool { return opts.SkipDaemon }),
		huh.NewGroup(
			huh.NewNote().Title("Menubar").Description("macOS menu bar companion (dev.biwashi.apogee.menubar). Registered as a launchd login item so it starts at every login, independent from the collector daemon."),
			huh.NewSelect[string]().
				Title("Install menubar app as a login item?").
				Options(menubarChoices...).
				Value(&plan.MenubarAction),
		).WithHideFunc(func() bool { return opts.SkipMenubar || !state.MenubarPlatform }),
		huh.NewGroup(
			huh.NewNote().Title("Summarizer").Description("Three LLM tiers: per-turn recap, per-session rollup, phase narrative. Language + optional system prompts (2048 chars max)."),
			huh.NewSelect[string]().
				Title("Output language").
				Description("Applies to every tier. Default: "+summarizer.LanguageEN+" (English).").
				Options(langChoices...).
				Value(&plan.SummarizerLanguage),
			huh.NewText().
				Title("Recap system prompt — tier 1 (per turn, "+state.RecapDefault+")").
				Description("Prepended to the recap prompt. Leave empty to use the default instructions only.").
				Placeholder("e.g. Keep every recap under 3 sentences. Prefer active voice.").
				Value(&plan.RecapSystemPrompt).
				CharLimit(summarizer.SystemPromptMaxLen),
			huh.NewText().
				Title("Rollup system prompt — tier 2 (per session, "+state.RollupDefault+")").
				Description("Prepended to the rollup prompt. Leave empty to use the default instructions only.").
				Placeholder("e.g. Focus on user intent and outcomes, not implementation details.").
				Value(&plan.RollupSystemPrompt).
				CharLimit(summarizer.SystemPromptMaxLen),
			huh.NewText().
				Title("Narrative system prompt — tier 3 (phase timeline, "+state.NarrativeDefault+")").
				Description("Prepended to the narrative prompt. Leave empty to use the default instructions only.").
				Placeholder("e.g. Use 3-6 phases per session. Prefer verb-led headlines.").
				Value(&plan.NarrativeSystemPrompt).
				CharLimit(summarizer.SystemPromptMaxLen),
			huh.NewSelect[string]().
				Title("Recap model").
				Description("Tier 1 — runs once per turn. Default is the cheapest currently-available entry from the catalog.").
				Options(modelOptions(state.Models, summarizer.UseCaseRecap, state.RecapDefault, state.ModelsAvailable)...).
				Value(&plan.RecapModelOverride),
			huh.NewSelect[string]().
				Title("Rollup model").
				Description("Tier 2 — runs once per session. Default is the cheapest currently-available entry from the catalog.").
				Options(modelOptions(state.Models, summarizer.UseCaseRollup, state.RollupDefault, state.ModelsAvailable)...).
				Value(&plan.RollupModelOverride),
			huh.NewSelect[string]().
				Title("Narrative model").
				Description("Tier 3 — phase timeline. Default is the cheapest currently-available entry from the catalog.").
				Options(modelOptions(state.Models, summarizer.UseCaseNarrative, state.NarrativeDefault, state.ModelsAvailable)...).
				Value(&plan.NarrativeModelOverride),
		).WithHideFunc(func() bool { return opts.SkipSummarizer }),
		huh.NewGroup(
			huh.NewNote().Title("OpenTelemetry").Description("Forward apogee's OTel spans to an external collector (optional)."),
			huh.NewConfirm().
				Title("Enable OTLP export?").
				Value(&plan.TelemetryEnabled),
			huh.NewInput().
				Title("Endpoint").
				Value(&plan.TelemetryEndpoint),
			huh.NewSelect[string]().
				Title("Protocol").
				Options(protoChoices...).
				Value(&plan.TelemetryProtocol),
		).WithHideFunc(func() bool { return opts.SkipTelemetry }),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Open the dashboard in your browser after applying?").
				Value(&plan.OpenBrowser),
		),
	)

	return form.Run()
}

// renderOnboardPlan prints a styled plan box so the user can eyeball
// every decision before confirming. Reused by both the dry-run path
// and the interactive confirmation step.
func renderOnboardPlan(out io.Writer, plan onboardPlan, opts onboardOptions) {
	expandedDB, _ := expandHome(opts.DBPath)
	expandedCfg, _ := expandHome(opts.ConfigPath)
	hooksTarget := "~/.claude/settings.json"
	if home, err := os.UserHomeDir(); err == nil {
		hooksTarget = filepath.Join(home, ".claude", "settings.json")
	}

	rows := [][2]string{}
	rows = append(rows, [2]string{"Config", expandedCfg})
	rows = append(rows, [2]string{"DB", expandedDB})
	rows = append(rows, [2]string{"Hooks", formatHooksPlan(plan, hooksTarget)})
	rows = append(rows, [2]string{"Daemon", formatDaemonPlan(plan)})
	rows = append(rows, [2]string{"Menubar", formatMenubarPlan(plan, opts)})
	rows = append(rows, [2]string{"Summarizer", formatSummarizerPlan(plan)})
	rows = append(rows, [2]string{"OTel", formatTelemetryPlan(plan)})
	rows = append(rows, [2]string{"Open", formatOpenPlan(plan)})

	body := keyValueLines(rows)
	inner := styleHeading.Render("apogee onboard — plan") + "\n\n" + body
	fmt.Fprintln(out, boxInfo.Render(inner))
}

func formatHooksPlan(plan onboardPlan, target string) string {
	switch plan.HooksAction {
	case "skip":
		return "skip"
	case "reinstall":
		s := "re-install " + target
		if plan.SourceAppPinned != "" {
			s += " (source_app=" + plan.SourceAppPinned + ")"
		} else {
			s += " (dynamic source_app)"
		}
		return s
	default:
		s := "install " + target
		if plan.SourceAppPinned != "" {
			s += " (source_app=" + plan.SourceAppPinned + ")"
		} else {
			s += " (dynamic source_app)"
		}
		return s
	}
}

func formatDaemonPlan(plan onboardPlan) string {
	if plan.DaemonAction == "skip" {
		return "skip"
	}
	verb := "install"
	if plan.DaemonAction == "reinstall" {
		verb = "re-install"
	}
	s := verb + " " + daemon.DefaultLabel + " @ " + plan.Addr
	if plan.StartDaemon {
		s += " · start"
	}
	return s
}

// formatMenubarPlan renders the Menubar: row in the plan box. On
// non-darwin / --skip-menubar we render "skip" with a platform hint
// so the row stays informative instead of disappearing.
func formatMenubarPlan(plan onboardPlan, opts onboardOptions) string {
	if !isMenubarPlatform() {
		return "skip (macOS only)"
	}
	if opts.SkipMenubar || plan.MenubarAction == "skip" {
		return "skip"
	}
	verb := "install"
	if plan.MenubarAction == "reinstall" {
		verb = "re-install"
	}
	return verb + " " + daemon.MenubarLabel + " (macOS login item)"
}

func formatSummarizerPlan(plan onboardPlan) string {
	if plan.SkipSummarizer {
		return "skip"
	}
	parts := []string{"language=" + plan.SummarizerLanguage}
	if plan.RecapSystemPrompt != "" {
		parts = append(parts, fmt.Sprintf("recap_prompt=%d chars", len(plan.RecapSystemPrompt)))
	}
	if plan.RollupSystemPrompt != "" {
		parts = append(parts, fmt.Sprintf("rollup_prompt=%d chars", len(plan.RollupSystemPrompt)))
	}
	if plan.NarrativeSystemPrompt != "" {
		parts = append(parts, fmt.Sprintf("narrative_prompt=%d chars", len(plan.NarrativeSystemPrompt)))
	}
	if plan.RecapModelOverride != "" {
		parts = append(parts, "recap_model="+plan.RecapModelOverride)
	}
	if plan.RollupModelOverride != "" {
		parts = append(parts, "rollup_model="+plan.RollupModelOverride)
	}
	if plan.NarrativeModelOverride != "" {
		parts = append(parts, "narrative_model="+plan.NarrativeModelOverride)
	}
	return strings.Join(parts, ", ")
}

func formatTelemetryPlan(plan onboardPlan) string {
	if plan.SkipTelemetry || !plan.TelemetryEnabled {
		return "disabled"
	}
	return plan.TelemetryEndpoint + " (" + plan.TelemetryProtocol + ")"
}

func formatOpenPlan(plan onboardPlan) string {
	if plan.OpenBrowser {
		return "open http://" + plan.Addr + "/"
	}
	return "skip"
}

// applyOnboardPlan walks each planned action in order and invokes
// the corresponding package-level helper. Each step prints its own
// success / failure line. The first failing step aborts (returning a
// non-nil error so cobra exits with code 1); earlier successes are
// NOT rolled back — partial success is better than undoing work.
func applyOnboardPlan(ctx context.Context, plan onboardPlan, opts onboardOptions, state onboardState) error {
	out := opts.Stdout

	fmt.Fprintln(out, renderHeading("Applying..."))
	fmt.Fprintln(out)

	// 1. Config TOML (telemetry block + shell).
	if err := applyConfigTOML(opts, plan); err != nil {
		fmt.Fprintln(out, formatStatusLine("error", "write "+opts.ConfigPath+": "+err.Error()))
		return err
	}
	fmt.Fprintln(out, formatStatusLine("ok", "wrote "+shortenPath(opts.ConfigPath)))

	// 2. Hooks
	if plan.HooksAction != "skip" {
		res, err := applyHooksInstall(plan)
		if err != nil {
			fmt.Fprintln(out, formatStatusLine("error", "hook install: "+err.Error()))
			return err
		}
		msg := fmt.Sprintf("installed %d hook events into %s", len(res.Added), shortenPath(res.SettingsPath))
		if res.LegacyFound > 0 {
			msg += fmt.Sprintf(" (replaced %d legacy entries)", res.LegacyFound)
		}
		fmt.Fprintln(out, formatStatusLine("ok", msg))
	} else {
		fmt.Fprintln(out, formatStatusLine("info", "hooks: skipped"))
	}

	// 3. Daemon install
	var dm daemon.Manager
	if plan.DaemonAction != "skip" {
		m, err := opts.ManagerFactory()
		if err != nil {
			fmt.Fprintln(out, formatStatusLine("error", "daemon: "+err.Error()))
			return err
		}
		dm = m
		cfg, err := resolveDaemonConfig("", plan.Addr, opts.DBPath)
		if err != nil {
			fmt.Fprintln(out, formatStatusLine("error", "daemon config: "+err.Error()))
			return err
		}
		cfg.Force = plan.DaemonAction == "reinstall"
		if err := m.Install(ctx, cfg); err != nil {
			if errors.Is(err, daemon.ErrAlreadyInstalled) && plan.DaemonAction != "reinstall" {
				fmt.Fprintln(out, formatStatusLine("info", "daemon: already installed (pass reinstall to overwrite)"))
			} else {
				fmt.Fprintln(out, formatStatusLine("error", "daemon install: "+err.Error()))
				return err
			}
		} else {
			fmt.Fprintln(out, formatStatusLine("ok", "installed "+m.Label()+" unit at "+m.UnitPath()))
		}
	} else {
		fmt.Fprintln(out, formatStatusLine("info", "daemon: skipped"))
	}

	// 3b. Menubar login item (macOS only). Partial-success
	// semantics: if the menubar install fails we log the failure
	// and continue — we do NOT roll back the daemon install that
	// just succeeded. The collector is the load-bearing surface;
	// the menubar is a convenience.
	if !opts.SkipMenubar && plan.MenubarAction != "skip" {
		if !isMenubarPlatform() {
			fmt.Fprintln(out, formatStatusLine("info", "menubar: skipped (macOS only)"))
		} else if opts.MenubarManagerFactory != nil {
			mm, err := opts.MenubarManagerFactory()
			if err != nil {
				fmt.Fprintln(out, formatStatusLine("warn", "menubar: "+err.Error()))
			} else {
				mcfg, merr := resolveMenubarConfig()
				if merr != nil {
					fmt.Fprintln(out, formatStatusLine("warn", "menubar config: "+merr.Error()))
				} else {
					mcfg.Force = plan.MenubarAction == "reinstall"
					if err := mm.Install(ctx, mcfg); err != nil {
						if errors.Is(err, daemon.ErrAlreadyInstalled) && plan.MenubarAction != "reinstall" {
							fmt.Fprintln(out, formatStatusLine("info", "menubar: already installed (pass reinstall to overwrite)"))
						} else if errors.Is(err, daemon.ErrNotSupported) {
							fmt.Fprintln(out, formatStatusLine("info", "menubar: platform not supported"))
						} else {
							fmt.Fprintln(out, formatStatusLine("warn", "menubar install: "+err.Error()))
						}
					} else {
						fmt.Fprintln(out, formatStatusLine("ok", "installed "+mm.Label()+" unit at "+mm.UnitPath()))
					}
				}
			}
		}
	} else if opts.SkipMenubar || plan.MenubarAction == "skip" {
		fmt.Fprintln(out, formatStatusLine("info", "menubar: skipped"))
	}

	// 4. Summarizer preferences
	if !plan.SkipSummarizer {
		prefs := summarizer.Preferences{
			Language:               plan.SummarizerLanguage,
			RecapSystemPrompt:      plan.RecapSystemPrompt,
			RollupSystemPrompt:     plan.RollupSystemPrompt,
			NarrativeSystemPrompt:  plan.NarrativeSystemPrompt,
			RecapModelOverride:     plan.RecapModelOverride,
			RollupModelOverride:    plan.RollupModelOverride,
			NarrativeModelOverride: plan.NarrativeModelOverride,
		}
		// In --yes / non-interactive mode we must NOT overwrite a
		// non-empty existing prompt with an empty default. Guard
		// each field against the captured state.
		if opts.Yes {
			if state.Prefs.RecapSystemPrompt != "" && prefs.RecapSystemPrompt == "" {
				prefs.RecapSystemPrompt = state.Prefs.RecapSystemPrompt
			}
			if state.Prefs.RollupSystemPrompt != "" && prefs.RollupSystemPrompt == "" {
				prefs.RollupSystemPrompt = state.Prefs.RollupSystemPrompt
			}
			if state.Prefs.NarrativeSystemPrompt != "" && prefs.NarrativeSystemPrompt == "" {
				prefs.NarrativeSystemPrompt = state.Prefs.NarrativeSystemPrompt
			}
		}
		dbPath, _ := expandHome(opts.DBPath)
		if err := opts.WritePrefs(ctx, dbPath, prefs); err != nil {
			fmt.Fprintln(out, formatStatusLine("error", "summarizer prefs: "+err.Error()))
			return err
		}
		fmt.Fprintln(out, formatStatusLine("ok", "wrote summarizer preferences (language="+prefs.Language+")"))
	} else {
		fmt.Fprintln(out, formatStatusLine("info", "summarizer: skipped"))
	}

	// 5. Start daemon
	if !plan.SkipTelemetry {
		// Already handled inside applyConfigTOML above; no-op here.
		_ = plan.TelemetryEnabled
	}
	if plan.DaemonAction != "skip" && plan.StartDaemon && dm != nil {
		if err := opts.StartDaemon(ctx, dm); err != nil {
			if errors.Is(err, daemon.ErrNotSupported) {
				fmt.Fprintln(out, formatStatusLine("warn", "daemon start: platform not supported"))
			} else {
				fmt.Fprintln(out, formatStatusLine("error", "daemon start: "+err.Error()))
				return err
			}
		} else {
			fmt.Fprintln(out, formatStatusLine("ok", "daemon started ("+dm.Label()+")"))
		}
	}

	// 6. Open browser
	if plan.OpenBrowser && !opts.Yes && !opts.NoOpenBrowser {
		url := "http://" + plan.Addr + "/"
		if err := opts.OpenBrowser(ctx, out, url); err != nil {
			fmt.Fprintln(out, formatStatusLine("warn", "open: "+err.Error()))
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, styleSuccess.Render("apogee is ready."))
	fmt.Fprintln(out, styleMuted.Render("  Run `apogee status` to check the daemon."))
	fmt.Fprintln(out, styleMuted.Render("  Run `apogee doctor` to verify the environment."))
	return nil
}

// applyHooksInstall drives the existing Init() path with the fields
// the wizard collected. Force=true so a re-install refreshes stale
// entries in place.
func applyHooksInstall(plan onboardPlan) (*InitResult, error) {
	target, err := ResolveTarget("", ScopeUser)
	if err != nil {
		return nil, err
	}
	cfg := InitConfig{
		Target:      target,
		SourceApp:   plan.SourceAppPinned,
		ServerURL:   "http://" + plan.Addr + "/v1/events",
		Scope:       ScopeUser,
		HookCommand: DefaultHookCommand(),
		Force:       plan.HooksAction == "reinstall",
	}
	return Init(cfg)
}

// applyConfigTOML writes (or refreshes) ~/.apogee/config.toml with
// the telemetry block the user chose. Other blocks are preserved via
// a naive decode-then-re-marshal using BurntSushi/toml, which keeps
// unknown sections intact on the way through.
//
// Writing the config is always attempted so the file exists for
// downstream consumers (summarizer, telemetry loader) even when
// telemetry is disabled.
func applyConfigTOML(opts onboardOptions, plan onboardPlan) error {
	path, err := expandHome(opts.ConfigPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	existing := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = toml.Unmarshal(raw, &existing)
	} else if !os.IsNotExist(err) {
		return err
	}

	// [telemetry] section — only touched when the user did not skip.
	if !opts.SkipTelemetry && !plan.SkipTelemetry {
		tSec, _ := existing["telemetry"].(map[string]any)
		if tSec == nil {
			tSec = map[string]any{}
		}
		tSec["enabled"] = plan.TelemetryEnabled
		if plan.TelemetryEndpoint != "" {
			tSec["endpoint"] = plan.TelemetryEndpoint
		}
		if plan.TelemetryProtocol != "" {
			tSec["protocol"] = plan.TelemetryProtocol
		}
		existing["telemetry"] = tSec
	}

	// [daemon] section — always stamp the addr/db_path so downstream
	// `apogee serve` reads match the wizard's choice.
	if !opts.SkipDaemon && plan.DaemonAction != "skip" {
		dSec, _ := existing["daemon"].(map[string]any)
		if dSec == nil {
			dSec = map[string]any{}
		}
		dSec["addr"] = plan.Addr
		dSec["db_path"] = opts.DBPath
		existing["daemon"] = dSec
	}

	return writeTOMLFile(path, existing)
}

// writeTOMLFile serialises data and atomically writes it to path.
// The encoder sorts keys and omits empty sections so re-runs produce
// byte-identical output on unchanged input.
func writeTOMLFile(path string, data map[string]any) error {
	var buf strings.Builder
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(sortedMap(data)); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(buf.String()), 0o644)
}

// sortedMap returns a new map with every nested map/slice replaced
// by a sorted copy so the toml encoder emits deterministic output.
// BurntSushi/toml already iterates map keys in sorted order when
// encoding, so we mostly just walk through and normalise nested
// maps of any-typed values.
func sortedMap(in map[string]any) map[string]any {
	out := map[string]any{}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := in[k].(type) {
		case map[string]any:
			out[k] = sortedMap(v)
		default:
			out[k] = v
		}
	}
	return out
}

// shortenPath swaps the user home directory for ~ for prettier
// display in the confirmation lines.
func shortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
