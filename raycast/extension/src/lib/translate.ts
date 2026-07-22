import { getPreferenceValues } from "@raycast/api";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const pexecFile = promisify(execFile);

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

export async function runDefine(word: string): Promise<DictEntry> {
  const bin = resolveBinary();
  const { stdout } = await pexecFile(bin, ["define", word, "--json"], {
    timeout: 60_000,
    env: baseEnv(),
  });
  return JSON.parse(stdout) as DictEntry;
}

/** Fire-and-forget TTS via `translate <text> --to <lang> --speak`. */
export function speak(text: string, to: string): void {
  const bin = resolveBinary();
  execFile(bin, [text, "--to", to, "--speak"], { env: baseEnv() }, () => {
    /* ignore — best-effort audio */
  });
}
