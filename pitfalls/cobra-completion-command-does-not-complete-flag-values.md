# Cobra's free `completion` command completes nothing useful — flag values fall back to filenames

**Symptoms** (grep this section): `translate --to <TAB>` lists files in the
current directory instead of language codes; `--provider`/`--model`/`--engine`
tab-completion offers filenames; `tool __complete --to ""` prints only `:0` and
`ShellCompDirectiveDefault`; a package manager installed the tool but TAB does
nothing; completions work on one machine and not another.
**First seen**: 2026-08-27.
**Affects**: any cobra CLI that never calls `RegisterFlagCompletionFunc`, and any
tool distributed without a completion-install step.
**Status**: fixed — `cmd/completion.go` (2026-08-27).

## Symptom

Cobra adds a `completion bash|zsh|fish|powershell` subcommand automatically, so
`translate completion zsh > ~/.zfunc/_translate` works and TAB *appears*
supported. But the generated script only knows subcommands and flag *names*.
Ask it for a flag **value** and you get filename completion:

```console
$ ./translate __complete --to ""
:0
Completion ended with directive: ShellCompDirectiveDefault
```

`:0` is `ShellCompDirectiveDefault`, i.e. "shell, do your normal filename thing".

## Two independent gaps

**1. No value completion.** Fixed by registering a function per flag. The data
usually already exists — here `lang.List()` and the config's providers are the
same sources behind `translate lang list --json` and `translate models --json`:

```go
_ = root.RegisterFlagCompletionFunc("to",
    func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
        return langValues(false), cobra.ShellCompDirectiveNoFileComp
    })
```

Return `"value\tdescription"` — cobra splits on the tab and shells that support
descriptions render the right-hand half. Always end with
`ShellCompDirectiveNoFileComp`, or the shell adds filenames to *your* candidates.

`RegisterFlagCompletionFunc` returns an error only when the flag name doesn't
exist, so a typo degrades **silently** back to filename completion. Cover every
registered flag with a test that drives the hidden `__complete` command.

**2. Nothing installs the generated script.** `go install` never will, and a
package manager won't either unless the packaging says so. Until 2026-08-27 the
Homebrew formula shipped no completions at all — TAB only worked on machines
whose dotfiles happened to run `translate completion zsh > ~/.zfunc/_translate`.
In a formula, one line fixes it:

```ruby
generate_completions_from_executable(bin/"translate", shell_parameter_format: :cobra)
```

`shell_parameter_format: :cobra` means "invoke `<binary> completion <shell>`".

## Gotcha: completion must have no side effects

`config.Load()` writes a default config file when none exists. Calling it from a
completion function means **pressing TAB creates files**. Use a read-only path —
here `config.LoadForRead()`, which never writes and falls back to `Default()`.
`TestCompletionDoesNotCreateConfig` guards it.

## Note on positional args

Setting `root.ValidArgsFunction = cobra.NoFileCompletions` does *not* stop the
first word from completing — cobra still offers subcommand names there, which is
what you want. It suppresses filename noise from the second word on.
