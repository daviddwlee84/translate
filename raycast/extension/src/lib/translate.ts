import { getPreferenceValues } from "@raycast/api";
import { execFile, spawn } from "node:child_process";
import { promisify } from "node:util";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const pexecFile = promisify(execFile);

/** The target languages offered in dropdowns. Keep in sync with the static
 *  dropdowns declared in package.json (command arguments can't read this at runtime). */
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

interface Prefs {
  binaryPath?: string;
  defaultTarget?: string;
  engine?: string;
  tier?: string;
}

const PROBE_DIRS = [
  join(homedir(), ".local", "bin"),
  "/opt/homebrew/bin",
  "/usr/local/bin",
  join(homedir(), "go", "bin"),
];

let cachedBin: string | undefined;

/**
 * Resolve an ABSOLUTE path to the translate binary. Raycast runs under launchd
 * with a restricted PATH that does not inherit the shell rc, so a bare
 * `translate` throws ENOENT — we probe known install dirs (preference first).
 */
export function resolveBinary(): string {
  const prefs = getPreferenceValues<Prefs>();
  if (prefs.binaryPath && existsSync(prefs.binaryPath)) return prefs.binaryPath;
  if (cachedBin) return cachedBin;
  for (const dir of PROBE_DIRS) {
    const candidate = join(dir, "translate");
    if (existsSync(candidate)) {
      cachedBin = candidate;
      return candidate;
    }
  }
  throw new Error(
    "translate CLI not found. Set the binary path in extension preferences, or install it (just install / brew install daviddwlee84/tap/translate).",
  );
}

function baseEnv(): NodeJS.ProcessEnv {
  // Ensure the CLI can locate its config.toml (providers/API keys) under launchd.
  return { ...process.env, HOME: process.env.HOME ?? homedir() };
}

export interface TranslateOptions {
  to?: string;
  from?: string;
  engine?: string;
  tier?: string;
  noHistory?: boolean;
  signal?: AbortSignal;
}

export async function runTranslate(
  text: string,
  opts: TranslateOptions = {},
): Promise<TranslateResult> {
  const prefs = getPreferenceValues<Prefs>();
  const bin = resolveBinary();
  const args = [text, "--to", opts.to ?? prefs.defaultTarget ?? "en", "--json"];
  if (opts.from) args.push("--from", opts.from);
  const engine = opts.engine ?? prefs.engine;
  if (engine) args.push("--engine", engine);
  const tier = opts.tier ?? prefs.tier;
  if (tier) args.push("--tier", tier);
  if (opts.noHistory) args.push("--no-history");

  const { stdout } = await pexecFile(bin, args, {
    timeout: 60_000, // LLM engines routinely exceed useExec's 10s default
    maxBuffer: 16 * 1024 * 1024,
    env: baseEnv(),
    signal: opts.signal, // cancel a superseded call when the user keeps typing
  });
  return JSON.parse(stdout) as TranslateResult;
}

/**
 * `translate define <word> --json`. The top-level payload is a TranslateResult
 * whose `.dictionary` holds the entry (on a dict hit); on a miss it falls back
 * to an LLM definition in `.translation` with a `warnings[]` note.
 */
export async function runDefine(
  word: string,
  signal?: AbortSignal,
): Promise<TranslateResult> {
  const bin = resolveBinary();
  const { stdout } = await pexecFile(bin, ["define", word, "--json"], {
    timeout: 60_000,
    maxBuffer: 16 * 1024 * 1024,
    env: baseEnv(),
    signal,
  });
  return JSON.parse(stdout) as TranslateResult;
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

/** Fire-and-forget TTS via `translate <text> --to <lang> --speak`. */
export function speak(text: string, to: string): void {
  const bin = resolveBinary();
  execFile(bin, [text, "--to", to, "--speak"], { env: baseEnv() }, () => {
    /* ignore — best-effort audio */
  });
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
  const args = [text, "--to", opts.to ?? "en", "--stream", "--no-history"];
  if (opts.engine) args.push("--engine", opts.engine);
  if (opts.from) args.push("--from", opts.from);
  if (opts.tier) args.push("--tier", opts.tier);

  const child = spawn(bin, args, { env: baseEnv() });
  child.stdout?.setEncoding("utf8");
  child.stdout?.on("data", (d: Buffer | string) => h.onData(d.toString()));
  child.on("close", (code) => h.onDone(code));
  child.on("error", (err) => h.onError(err));
  return () => {
    if (!child.killed) child.kill("SIGTERM");
  };
}
