import { getPreferenceValues } from "@raycast/api";
import { execFile, spawn } from "node:child_process";
import { promisify } from "node:util";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { platform } from "./platform";

const pexecFile = promisify(execFile);

/** The target languages offered in the *static* manifest dropdowns (command
 *  arguments can't read anything at runtime, so this list is hand-synced with
 *  package.json). In-view dropdowns should prefer runLangs(), which returns the
 *  CLI's full language table. */
export const LANGS = [
  { title: "English", value: "en" },
  { title: "Chinese (Traditional)", value: "zh-TW" },
  { title: "Chinese (Simplified)", value: "zh-CN" },
  { title: "Japanese", value: "ja" },
  { title: "Korean", value: "ko" },
  { title: "Spanish", value: "es" },
  { title: "French", value: "fr" },
  { title: "German", value: "de" },
  { title: "Italian", value: "it" },
  { title: "Portuguese", value: "pt" },
];

/** Mirrors internal/engine/engine.go TranslateResult (JSON tags). */
export interface Definition {
  definition: string;
  example?: string;
}
export interface Meaning {
  part_of_speech: string;
  definitions: Definition[];
  synonyms?: string[];
  antonyms?: string[];
}
export interface DictEntry {
  word: string;
  phonetic?: string;
  meanings?: Meaning[];
  source_url?: string;
}
export interface TranslateResult {
  translation: string;
  detected_source?: string;
  target: string;
  alternatives?: string[];
  notes?: string;
  confidence?: number;
  warnings?: string[];
  engine?: string;
  model?: string;
  dictionary?: DictEntry;
  suggestions?: string[];
}

/** One row of `translate history --json`. */
export interface HistoryEntry {
  id: string;
  ts: string;
  source_lang: string;
  target_lang: string;
  engine?: string;
  model?: string;
  input: string;
  output: string;
}

/** Mirrors internal/engine/dictsearch.go Candidate. */
export interface Candidate {
  word: string;
  phonetic?: string;
  preview?: string;
  pos?: string;
  /** ECDICT frequency RANK: 1 is the most common word, absent means unranked. */
  rank?: number;
  match: "exact" | "prefix" | "fuzzy";
  distance?: number;
}

/** Mirrors internal/engine/dictsearch.go SearchResult. */
export interface DictSearchResult {
  query: string;
  script: string;
  /** "ecdict" | "cedict" | "wordlist" | "none" */
  source: string;
  candidates: Candidate[];
  notes?: string;
}

/** One entry of `translate lang list --json`. */
export interface LangInfo {
  code: string;
  name: string;
  aliases?: string[];
}

/** One entry of `translate models --json`. */
export interface ModelInfo {
  provider: string;
  /** "fast" | "default" | "max" */
  tier: string;
  model: string;
  /** True for the model this config uses with no override. */
  default?: boolean;
}

/**
 * Sentinel target meaning "let pair mode pick the direction" — the dropdown
 * equivalent of the TUI's ^g. It is not a language code, so it never reaches
 * --to; runTranslate turns it into --pair instead.
 */
export const AUTO_TARGET = "__auto__";

interface Prefs {
  binaryPath?: string;
  defaultTarget?: string;
  engine?: string;
  tier?: string;
}

let cachedBin: string | undefined;

/**
 * Resolve an ABSOLUTE path to the translate binary. Raycast does not source a
 * shell rc on EITHER platform — under launchd on macOS, PATH is roughly
 * /usr/bin:/bin — so a bare `translate` throws ENOENT. We probe known install
 * dirs (preference first). The dirs and the filename come from the platform
 * seam; only the reason differs by OS, not the rule.
 */
export function resolveBinary(): string {
  const prefs = getPreferenceValues<Prefs>();
  if (prefs.binaryPath && existsSync(prefs.binaryPath)) return prefs.binaryPath;
  if (cachedBin) return cachedBin;
  for (const dir of platform.probeDirs) {
    const candidate = join(dir, platform.binaryName);
    if (existsSync(candidate)) {
      cachedBin = candidate;
      return candidate;
    }
  }
  throw new Error(
    `translate CLI not found. Set the binary path in extension preferences, or install it (${platform.installCommands[0].command}).`,
  );
}

/** True when an error came from resolveBinary failing to locate the CLI. */
export function isBinaryMissing(e: unknown): boolean {
  return e instanceof Error && e.message.startsWith("translate CLI not found");
}

function baseEnv(): NodeJS.ProcessEnv {
  // Ensure the CLI can locate its config.toml (providers/API keys) under launchd.
  // Windows resolves the home dir from %USERPROFILE% (internal/xdgpath calls
  // os.UserHomeDir), so forcing HOME there would achieve nothing.
  if (!platform.forcesHome) return { ...process.env };
  return { ...process.env, HOME: process.env.HOME ?? homedir() };
}

/**
 * Above this many UTF-8 bytes, text goes to the CLI on stdin instead of argv.
 *
 * macOS caps a whole argument list at ARG_MAX (1 MiB) and fails the spawn with
 * E2BIG — measured: 900 KB via argv works, 1.1 MB is "argument list too long".
 * The CLI reads stdin when it gets no text argument, and the pipe has no such
 * limit, so long documents take that route. The threshold leaves plenty of room
 * for multi-byte text (a CJK character is 3 bytes).
 */
const ARGV_SAFE_BYTES = 128 * 1024;

function needsStdin(text: string): boolean {
  return Buffer.byteLength(text, "utf8") > ARGV_SAFE_BYTES;
}

/**
 * How long to let a translation run, scaled to the input.
 *
 * A document-sized translation legitimately takes minutes — measured against
 * copilot-proxy: 10 KB of prose took 61 s, 20 KB took 96 s. A flat 60 s killed
 * exactly the long inputs Translate Text exists for. ~10 ms per character with a
 * 1-minute floor and a 10-minute ceiling (matching the CLI's own backstop).
 */
function requestTimeout(text: string): number {
  return Math.min(600_000, Math.max(60_000, text.length * 10));
}

/**
 * Run the CLI with `input` written to stdin and no text argument, resolving with
 * stdout. Mirrors pexecFile's rejection shape (message + `stderr`) so callers
 * can keep inspecting stderr the same way.
 */
function execWithStdin(
  bin: string,
  args: string[],
  input: string,
  opts: { timeout?: number; signal?: AbortSignal } = {},
): Promise<string> {
  return new Promise((resolve, reject) => {
    const child = spawn(bin, args, { env: baseEnv() });
    let out = "";
    let err = "";
    let settled = false;

    const finish = (fn: () => void) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      opts.signal?.removeEventListener("abort", onAbort);
      fn();
    };
    const kill = (reason: string) =>
      finish(() => {
        // Windows has no POSIX signals — Node terminates the child outright and
        // ignores the name. That is still the semantics we want here (cancel a
        // superseded call); only the graceful-shutdown nuance is lost.
        if (!child.killed) child.kill("SIGTERM");
        reject(Object.assign(new Error(reason), { stderr: err }));
      });
    const onAbort = () => kill("aborted");
    const timer = opts.timeout
      ? setTimeout(
          () => kill(`timed out after ${opts.timeout}ms`),
          opts.timeout,
        )
      : (undefined as unknown as NodeJS.Timeout);

    opts.signal?.addEventListener("abort", onAbort, { once: true });

    child.stdout?.setEncoding("utf8");
    child.stderr?.setEncoding("utf8");
    child.stdout?.on("data", (d: string) => (out += d));
    child.stderr?.on("data", (d: string) => (err += d));
    child.on("error", (e) =>
      finish(() => reject(Object.assign(e, { stderr: err }))),
    );
    child.on("close", (code) =>
      finish(() =>
        code === 0
          ? resolve(out)
          : reject(
              Object.assign(
                new Error(`translate exited ${code}: ${err.trim()}`),
                {
                  stderr: err,
                },
              ),
            ),
      ),
    );

    child.stdin?.on("error", () => {
      /* the child may exit before we finish writing — the close handler reports it */
    });
    child.stdin?.end(input, "utf8");
  });
}

export interface TranslateOptions {
  to?: string;
  from?: string;
  engine?: string;
  tier?: string;
  /** Exact model id, overriding engine/tier selection. */
  model?: string;
  /**
   * Direction. Tri-state on purpose:
   *   undefined → leave it to the config ([general] pair)
   *   true      → --pair: detect and route between the pair languages
   *   false     → --no-pair: always translate into `to`
   *
   * This matters because [general] pair = true silently overrides --to for
   * home-language input, so "translate into X" is only true with --no-pair.
   */
  pair?: boolean;
  /**
   * LLM prompt style (`--preset`). Only the LLM engines honour it; google and
   * dict ignore it. "contextual" is the one worth reaching for interactively —
   * it lists 2-4 senses instead of committing to one.
   */
  preset?: "concise" | "contextual" | "dictionary";
  /**
   * Extra system-prompt guidance (`--instructions`) — domain terminology, the
   * context a term appears in. Appended to whichever preset is active.
   */
  instructions?: string;
  noHistory?: boolean;
  signal?: AbortSignal;
}

/**
 * Turns a target + pair choice into CLI flags.
 *
 * AUTO_TARGET means "no --to at all": the CLI's own home target and pair_with
 * decide the direction. Anything else falls back to the Raycast preference, and
 * finally to nothing (letting the CLI config's default_target apply).
 */
function directionArgs(
  to: string | undefined,
  pair: boolean | undefined,
  prefTarget: string | undefined,
): { to?: string; pair?: boolean } {
  if (to === AUTO_TARGET) return { to: undefined, pair: true };
  return { to: to || prefTarget || undefined, pair };
}

export async function runTranslate(
  text: string,
  opts: TranslateOptions = {},
): Promise<TranslateResult> {
  const prefs = getPreferenceValues<Prefs>();
  const bin = resolveBinary();
  const dir = directionArgs(opts.to, opts.pair, prefs.defaultTarget);
  const viaStdin = needsStdin(text);
  // With no text argument the CLI reads stdin, which sidesteps ARG_MAX.
  const args = viaStdin ? [] : [text];
  // In auto/pair mode we deliberately send no --to: the CLI's own home target
  // plus [general] pair_with decide the direction.
  if (dir.to) args.push("--to", dir.to);
  args.push("--json");
  if (dir.pair === true) args.push("--pair");
  if (dir.pair === false) args.push("--no-pair");
  if (opts.from) args.push("--from", opts.from);
  const engine = opts.engine ?? prefs.engine;
  if (engine) args.push("--engine", engine);
  const tier = opts.tier ?? prefs.tier;
  if (tier) args.push("--tier", tier);
  if (opts.model) args.push("--model", opts.model);
  if (opts.preset) args.push("--preset", opts.preset);
  if (opts.instructions) args.push("--instructions", opts.instructions);
  if (opts.noHistory) args.push("--no-history");

  const timeout = requestTimeout(text);
  if (viaStdin) {
    const stdout = await execWithStdin(bin, args, text, {
      timeout,
      signal: opts.signal,
    });
    return JSON.parse(stdout) as TranslateResult;
  }
  const { stdout } = await pexecFile(bin, args, {
    timeout, // LLM engines routinely exceed useExec's 10s default
    maxBuffer: 16 * 1024 * 1024,
    env: baseEnv(),
    signal: opts.signal, // cancel a superseded call when the user keeps typing
  });
  return JSON.parse(stdout) as TranslateResult;
}

let cachedModels: ModelInfo[] | undefined;

/**
 * `translate models --json` — the models the configured providers declare, one
 * per tier, cached per command run. Deliberately not a live /v1/models probe:
 * copilot-proxy advertises ids it then refuses to serve, so a probed list would
 * offer choices that fail at request time. Empty on an older CLI.
 */
export async function runModels(): Promise<ModelInfo[]> {
  if (cachedModels) return cachedModels;
  try {
    const bin = resolveBinary();
    const { stdout } = await pexecFile(bin, ["models", "--json"], {
      timeout: 10_000,
      maxBuffer: 1024 * 1024,
      env: baseEnv(),
    });
    const parsed = JSON.parse(stdout) as ModelInfo[];
    cachedModels = Array.isArray(parsed) ? parsed : [];
  } catch {
    cachedModels = []; // older CLI without `models`, or no binary
  }
  return cachedModels;
}

export interface DefineOptions {
  /** Definition language for the LLM fallback. A dictionary hit ignores it — the
   *  offline tiers are script-fixed (CC-CEDICT zh→en, ECDICT en→zh). */
  to?: string;
  /** Force smart-dict, i.e. fall back to the LLM when the dictionary misses. */
  smart?: boolean;
  /** Suppress the history row. Browsing must not record; opening a word should. */
  noHistory?: boolean;
  signal?: AbortSignal;
}

const NO_DICT_ENTRY = "no dictionary entry";

/**
 * `translate define <word> --json`. The top-level payload is a TranslateResult
 * whose `.dictionary` holds the entry (on a dict hit); on a miss it falls back
 * to an LLM definition in `.translation` with a `warnings[]` note.
 *
 * A *hard* miss (the plain dictionary with no LLM fallback and nothing close
 * enough to suggest) exits 1 with a bare stderr line and no JSON at all — that
 * is re-thrown as a tagged error so callers can render it as "no entry" rather
 * than as a crash. See isNoDictEntry.
 *
 * Unless noHistory is set this writes a history row, which is what makes
 * "opening a word remembers it" true.
 */
export async function runDefine(
  word: string,
  opts: DefineOptions = {},
): Promise<TranslateResult> {
  const bin = resolveBinary();
  const args = ["define", word, "--json"];
  if (opts.smart) args.push("--smart");
  if (opts.to) args.push("--to", opts.to);
  if (opts.noHistory) args.push("--no-history");
  try {
    const { stdout } = await pexecFile(bin, args, {
      timeout: 60_000,
      maxBuffer: 16 * 1024 * 1024,
      env: baseEnv(),
      signal: opts.signal,
    });
    return JSON.parse(stdout) as TranslateResult;
  } catch (e) {
    const stderr = String((e as { stderr?: string }).stderr ?? "");
    if (stderr.includes(NO_DICT_ENTRY)) {
      throw new Error(`${NO_DICT_ENTRY}: ${word}`);
    }
    throw e;
  }
}

/** True when `define` exited on a hard dictionary miss (no JSON was emitted). */
export function isNoDictEntry(e: unknown): boolean {
  return e instanceof Error && e.message.startsWith(NO_DICT_ENTRY);
}

/**
 * `translate dict search <q> --limit N --json` — ranked headword candidates with
 * one-line definition previews.
 *
 * Local data only: no network, no LLM, single-digit milliseconds on an indexed
 * dictionary. That is the point — it is safe to run on every keystroke, and the
 * expensive LLM fallback is deferred to the moment a word is actually opened.
 * It also opens no history store, so typing can never pollute history.
 *
 * Finding nothing is not an error: `candidates` comes back empty and exit is 0.
 */
export async function runDictSearch(
  q: string,
  limit = 12,
  signal?: AbortSignal,
): Promise<DictSearchResult> {
  const bin = resolveBinary();
  const { stdout } = await pexecFile(
    bin,
    ["dict", "search", q, "--limit", String(limit), "--json"],
    {
      timeout: 10_000,
      maxBuffer: 8 * 1024 * 1024,
      env: baseEnv(),
      signal,
    },
  );
  const parsed = JSON.parse(stdout) as DictSearchResult;
  return { ...parsed, candidates: parsed.candidates ?? [] };
}

let cachedLangs: LangInfo[] | undefined;

/**
 * `translate lang list --json` — the CLI's full language table (35 entries),
 * cached per command run. Falls back to the hardcoded LANGS subset when the
 * installed binary predates the subcommand, so an older CLI still works.
 */
export async function runLangs(): Promise<LangInfo[]> {
  if (cachedLangs) return cachedLangs;
  try {
    const bin = resolveBinary();
    const { stdout } = await pexecFile(bin, ["lang", "list", "--json"], {
      timeout: 10_000,
      maxBuffer: 1024 * 1024,
      env: baseEnv(),
    });
    const parsed = JSON.parse(stdout) as LangInfo[];
    if (Array.isArray(parsed) && parsed.length > 0) {
      cachedLangs = parsed;
      return cachedLangs;
    }
  } catch {
    /* older CLI or no binary — fall through to the static list */
  }
  cachedLangs = LANGS.map((l) => ({ code: l.value, name: l.title }));
  return cachedLangs;
}

/**
 * `translate history --json` (recent) or `translate history search <q> --json`.
 * Both return an array of HistoryEntry. Local + fast (no network).
 */
export async function runHistory(
  query?: string,
  limit = 200,
  signal?: AbortSignal,
): Promise<HistoryEntry[]> {
  const bin = resolveBinary();
  const q = query?.trim();
  const args = q
    ? ["history", "search", q, "--json"]
    : ["history", "--json", "--limit", String(limit)];
  const { stdout } = await pexecFile(bin, args, {
    timeout: 30_000,
    maxBuffer: 32 * 1024 * 1024,
    env: baseEnv(),
    signal,
  });
  const parsed = JSON.parse(stdout);
  return Array.isArray(parsed) ? (parsed as HistoryEntry[]) : [];
}

/**
 * Fire-and-forget TTS via `translate <text> --to <lang> --speak`. This runs a
 * real translation (it speaks the translated side), so it passes --no-history:
 * pressing Speak is not a lookup and should not add a history row.
 */
export function speak(text: string, to: string): void {
  const bin = resolveBinary();
  execFile(
    bin,
    [text, "--to", to, "--speak", "--no-history"],
    { env: baseEnv() },
    () => {
      /* ignore — best-effort audio */
    },
  );
}

/**
 * Fire-and-forget TTS via `translate speak <text>` — pronounces the text as-is.
 * Unlike speak() this neither translates nor records, which is what you want for
 * "how is this word pronounced".
 */
export function speakText(text: string, lang?: string): void {
  const bin = resolveBinary();
  const args = ["speak", text];
  if (lang) args.push("--lang", lang);
  execFile(bin, args, { env: baseEnv() }, () => {
    /* ignore — best-effort audio */
  });
}

/** The effective CLI config (subset we care about) from `config show --json`. */
export interface CliConfig {
  general?: {
    default_target?: string;
    default_source?: string;
    debounce_ms?: number;
    engine?: string;
    tier?: string;
    live_translate?: boolean;
    /** Bidirectional mode: home-language input goes to pair_with instead. */
    pair?: boolean;
    pair_with?: string;
  };
}

let cachedConfig: CliConfig | undefined;

/**
 * Read the CLI's effective config via `translate config show --json` (cached per
 * command run). Lets the extension inherit the default target / debounce from
 * ~/.config/translate/config.toml when the Raycast preferences are left empty.
 */
export async function readConfig(): Promise<CliConfig> {
  if (cachedConfig) return cachedConfig;
  const bin = resolveBinary();
  const { stdout } = await pexecFile(bin, ["config", "show", "--json"], {
    timeout: 10_000,
    maxBuffer: 4 * 1024 * 1024,
    env: baseEnv(),
  });
  cachedConfig = JSON.parse(stdout) as CliConfig;
  return cachedConfig;
}

export interface StreamHandlers {
  onData: (chunk: string) => void;
  onDone: (code: number | null) => void;
  onError: (err: Error) => void;
}

/**
 * Spawn `translate <text> --to <lang> [--engine] --stream` and stream plain-text
 * stdout chunks (the `--stream` flag forces streaming over a pipe). Returns a
 * cancel function that kills the child (call it on unmount). Whether output arrives
 * progressively depends on the provider — ollama streams; copilot-proxy buffers its
 * claude responses. `--no-history` avoids duplicating a history row the live view
 * may already have recorded.
 */
export function spawnTranslateStream(
  text: string,
  opts: TranslateOptions,
  h: StreamHandlers,
): () => void {
  const bin = resolveBinary();
  const dir = directionArgs(opts.to, opts.pair, undefined);
  const viaStdin = needsStdin(text);
  const args = viaStdin ? [] : [text];
  if (dir.to) args.push("--to", dir.to);
  args.push("--stream", "--no-history");
  if (dir.pair === true) args.push("--pair");
  if (dir.pair === false) args.push("--no-pair");
  if (opts.engine) args.push("--engine", opts.engine);
  if (opts.from) args.push("--from", opts.from);
  if (opts.tier) args.push("--tier", opts.tier);
  if (opts.model) args.push("--model", opts.model);

  const child = spawn(bin, args, { env: baseEnv() });
  child.stdout?.setEncoding("utf8");
  child.stdout?.on("data", (d: Buffer | string) => h.onData(d.toString()));
  child.on("close", (code) => h.onDone(code));
  child.on("error", (err) => h.onError(err));
  if (viaStdin) {
    child.stdin?.on("error", () => {
      /* reported via the close/error handlers above */
    });
    child.stdin?.end(text, "utf8");
  }
  return () => {
    // See the note on the other kill site: on Windows this is a hard terminate.
    if (!child.killed) child.kill("SIGTERM");
  };
}
