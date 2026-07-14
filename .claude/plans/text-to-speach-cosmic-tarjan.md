# Plan: Free cross-platform text-to-speech (讀音 / pronunciation)

## Context

The user asked (paraphrased): *"Is there a free text-to-speech option? When looking up
words I sometimes want to hear the pronunciation (讀音)."* Today the tool shows phonetic
**text** (`DictEntry.Phonetic` — IPA for English, numbered pinyin like `ce4 shi4` for
Chinese) but can never **speak** anything: there is zero audio, zero `os/exec`, and no TTS
code in the repo.

Goal: add a **free, cross-platform TTS** capability so a lookup/translation can be heard,
triggered on demand from both the CLI and the TUI.

Decisions locked with the user:
- **Backend:** cross-platform, each OS with its own fallback chain — native offline first
  (macOS `say`, Linux `espeak-ng`/`spd-say`, Windows PowerShell SAPI), then a shared online
  fallback (**Google Translate TTS** MP3 → discovered player). Free; no API key.
- **What to speak:** the **secondary/foreign ("副") language** — for this user usually `en`
  — with the **TUI tab-focus** as an explicit override (focus the result pane → speak the
  result). Native-language input is not the target unless focused.
- **No phonetic-text changes** (pinyin stays `ce4 shi4`). Audio only.
- **Verified on this machine:** `say`, `afplay`, `ffplay` present; `say` has zh_CN
  (Tingting), zh_TW (Meijia), zh_HK (Sinji) + English voices.

This introduces the repo's **first `os/exec` usage** — it must stay small, injectable, and
never build a shell string (pass text as a discrete arg or on stdin).

## New package: `internal/tts/` (UI-agnostic, like `internal/engine`)

```
tts.go     Speaker interface, Options, New() fallback orchestrator, runtime.GOOS detection
runner.go  runner interface (exec injection) + execRunner{} — keeps exec testable
native.go  native offline backend; per-OS argv from a GOOS param (NO build tags)
google.go  Google translate_tts: download MP3 (net/http) → play
player.go  audio-player discovery (afplay/ffplay/mpv/mpg123) + play
voices.go  lang→say/espeak voice map; lang→google `tl` normalization
select.go  PURE side/lang selection ("副語言" + focus override)
cache.go   MP3 cache path + sha256 key
errors.go  ErrNoBackend, ErrNoPlayer, ErrEmptyText (mirror internal/engine/errors.go)
```

Core shapes:

```go
type Speaker interface {
    Name() string
    Available() bool                                     // binary/player present; no network
    Speak(ctx context.Context, text, lang string) error  // blocks until playback ends / ctx done
}
type Options struct {
    Order []string; Rate int; Voices map[string]string
    GoogleURL, UserAgent, CacheDir, Player string; Timeout time.Duration
    goos string; runner runner  // injected in tests; default runtime.GOOS + execRunner
}
func New(opt Options) Speaker    // *fallback wrapping ordered backends
```

- **runner injection** (`runner.go`): `look(name)`→`exec.LookPath`; `run(ctx,name,stdin,args...)`
  →`exec.CommandContext` (ctx cancellation kills the child; **stdin** carries long/untrusted
  text). Tests inject a `fakeRunner` — **no process is ever spawned in tests**.
- **native.go** references `"say"`/`"espeak-ng"`/`"powershell"` as strings so it compiles on
  every OS and all three argv shapes are unit-testable from one `go test`. macOS:
  `say [-v VOICE] [-r RATE] TEXT` (long text → `say -f -` on stdin). Windows: text on stdin
  into a fixed `System.Speech` script (no quoting/injection).
- **google.go**: own `*http.Client{Timeout}`, `http.NewRequestWithContext`, `url.Values.Encode()`,
  browser-ish `User-Agent` — copy `internal/engine/google.go:29-78` exactly. Chunk `q` to
  ≤200 chars, concat MP3 parts, cache, then `player.play`.

## Reuse (do not reinvent)

- HTTP pattern: `internal/engine/google.go:29-78` (client, `NewRequestWithContext`, User-Agent, `url.Values`).
- TUI "act on current result" precedent: Copy case `internal/tui/update.go:217-226`, `copyText()` at `update.go:441`.
- TUI async/flash plumbing: `internal/tui/msgs.go` (`flashCmd`), `m.flash` (`model.go:112`), flash handling `update.go:64-66`.
- Atomic file write: `config.Save` temp+rename, `internal/config/config.go:248-261`.
- Language helpers: `lang.IsChinese`/`lang.Detect` (`internal/lang/detect.go:23/13`). **Add** exported
  `lang.Base(code)` by exporting the existing `baseCode` region-strip at `detect.go:106` (reused by voices + select).

## Side/language selection — `select.go` (pure, table-tested)

```go
type Side int; const ( SideAuto Side = iota; SideSource; SideResult )
type SelectInput struct { SourceText, SourceLang, ResultText, ResultLang, Foreign string; Forced Side }
type Choice struct{ Text, Lang string }
func Select(in SelectInput) (Choice, bool)
```

Logic: `Forced==SideSource/SideResult` → that pane (fall through if empty). `SideAuto` →
speak the side whose language matches `Foreign`; if `Foreign==""` derive it as the
**non-`zh` side** (user is zh-native), defaulting to the result when ambiguous. Region
(zh-TW vs zh-CN) comes only from the passed `*Lang` hint, never from detection.
Runtime inputs already exist: CLI `src`/`effTgt`/`r.Translation`/`pairWith`
(`cmd/root.go:141-146,169-174`); TUI `m.source`/`m.target`/`m.result`/`m.ta.Value()`/`m.pairWith`
(`internal/tui/model.go:90-94,110`).

## Config — `internal/config/config.go`

Add `TTS TTS` to `Config` (after `SmartDict`, ~`config.go:25`), struct modeled on `Google`
(`config.go:84-90`), and seed it in `Default()` (~`config.go:196`):

```go
type TTS struct {
    Enabled, AutoSpeak, PreferForeign bool
    Order   []string          // ["native","google"]
    Foreign string            // 副 language; "" => derive
    Rate    int; Voices map[string]string
    GoogleTTSURL, UserAgent, CacheDir, Player string; TimeoutMs int
}
// Default: Enabled:true, AutoSpeak:false, Order:["native","google"], PreferForeign:true,
//          GoogleTTSURL:"https://translate.google.com/translate_tts", UserAgent:"Mozilla/5.0 translate-cli", TimeoutMs:5000
```

`--speak` is a per-invocation trigger (like `--json`), **not** a `Resolved` field, so
`Resolved`/`applyOverlay`/env plumbing stay untouched; read `cfg.TTS` directly the way
`define.go:37` reads `cfg.Dict`. (`auto_speak` per-front-end overlay = documented follow-up.)

## CLI wiring — `cmd/root.go`, `cmd/define.go`, new `cmd/speak.go`

- Persistent flags after `root.go:77`: `--speak/-s` (bool) and `--speak-lang` (string). Leave `overrides()` untouched.
- In `runRoot`, after each `oneShot` result renders + `recordAndRemember` (args branch
  `root.go:170-174`, stdin branch `root.go:186-192`), guard `(flagSpeak || cfg.TTS.AutoSpeak) && cfg.TTS.Enabled`
  → call a `speakResult(...)` helper (new `cmd/speak.go`) that builds `tts.New`, runs
  `tts.Select`, and `Speak`s. Speak **after** stdout so audio never interleaves streamed tokens; errors go to stderr, never fatal.
- `define.go`: after `renderDict` (`define.go:62`), `--speak` pronounces the entry itself
  (`res.Dictionary.Word`, lang from detection) regardless of foreign preference.
- New subcommand `translate speak <text...> [--lang] [--backend] [--voice]` registered at
  `root.go:80` — fastest end-to-end smoke path.

## TUI wiring — `keys.go`, `update.go`, `model.go`, `msgs.go`, `view.go`, `cmd/root.go`

- `keys.go`: add `Speak = ctrl+s` (`^s speak`). Comment: Bubble Tea raw mode disables IXON,
  so `ctrl+s` is delivered normally (fallback `ctrl+b`/`ctrl+j` if ever needed).
- `update.go`: new case mirroring Copy (`update.go:217-226`): if `m.focus==focusOutput`
  force `SideResult`, else `SideAuto`; run `tts.Select`, then `m.stopSpeak()` (debounce) and
  return `speakCmd(...)` as a `tea.Cmd` (non-blocking). Flash `speaking…` / `nothing to speak` / `no TTS backend`.
- `msgs.go`: `speakDoneMsg{err}` + `speakCmd` running `Speaker.Speak` in the cmd goroutine; handle it near `flashClearMsg` (`update.go:64`).
- `model.go`: add `speakCancel context.CancelFunc` + `stopSpeak()` (mirror `cancelInflight`,
  `update.go:420`); call it in Quit (`update.go:174-176`) and Clear (`update.go:246`).
- `tui.Params` (`model.go:28-42`): add `Speaker tts.Speaker` and `Foreign string`, built once
  in `runTUI` (`root.go:319-353`) from `cfg.TTS`.
- `view.go`: append `^s speak` to the two hardcoded footer strings (dict `view.go:132`, translate `view.go:160`); height auto-reserves via `footerContent(true)`.

## Caching & cancellation

- Add `xdgpath.CacheDir()` mirroring `DataDir` (`internal/xdgpath/paths.go:34`); tts uses
  `<CacheDir>/tts`, `os.MkdirAll(...,0o700)` lazily (NOT in `EnsureDirs`). `cfg.TTS.CacheDir` overrides.
- Cache key `sha256(tl + "\x00" + text)` → `<hash>.mp3`; hit ⇒ replay, miss ⇒ download →
  atomic temp+rename → play. Unbounded for v1 (LRU cap = follow-up).
- All exec via `exec.CommandContext`, all HTTP via `http.NewRequestWithContext`; CLI ctx is
  SIGINT-cancellable, TUI ctx cancelled by `stopSpeak`/Quit — cancel kills playback mid-word.

## Error / UX handling

No backend/player → `ErrNoBackend`, CLI stderr "speech unavailable (install espeak-ng or a
player like ffplay)", TUI flash `no TTS backend`. Missing voice → retry `say` without `-v`,
then default. Empty text → `Select` returns false / `ErrEmptyText`. `ffplay` must use
`-nodisp -autoexit -loglevel quiet`. Google needs `client=tw-ob` + User-Agent; 429/400 surface as errors.

## Verification

1. `just check` (fmt+vet+build) and `just test` — unit tests:
   - `select_test.go`: foreign=en with zh↔en source/result picks the en side; `Forced` overrides; zh-TW region preserved; empty-side fallback; both-empty ⇒ false.
   - `voices_test.go`: say/espeak/google maps for en, zh, zh-CN, zh-TW, zh-HK, ja; `lang.Base` fallback; config `Voices` override wins.
   - `tts_test.go` (fakeRunner): fallback ordering (native before google, skip unavailable); argv is `["say","-v","Meijia","測試"]`-shaped (no shell string); Windows uses stdin; cancelled ctx propagates. No real process spawned.
   - `config` test: `Default().TTS` seeds + TOML round-trip.
2. Manual smoke (audible):
   - `./translate speak "hello world"` → hears English (`say`/`afplay`).
   - `./translate speak "測試一下" --lang zh-TW` → Meijia voice.
   - `./translate "hola mundo" --to en --speak` → speaks the English (副) side.
   - `./translate define ephemeral --speak` → pronounces the headword.
   - `just tui`: type + Enter, `^s` (input focus → 副/foreign side); `Tab` → result pane, `^s` (speaks result). `--debug` confirms chosen side+lang.
   - Force Google path: `[tts].order = ["google"]` (or offline) and repeat `speak` to exercise MP3 fetch+cache+player.

## Critical files

- `internal/tts/*` — new package (all files above).
- `internal/config/config.go` — `TTS` struct + `Default()` seed.
- `internal/xdgpath/paths.go` — add `CacheDir()`.
- `internal/lang/detect.go` — export `lang.Base`.
- `cmd/root.go`, `cmd/define.go`, new `cmd/speak.go` — flags, speak-after-render, subcommand, TUI Params.
- `internal/tui/keys.go`, `update.go`, `model.go`, `msgs.go`, `view.go` — `^s` key, handler, state, async msg, footer.
