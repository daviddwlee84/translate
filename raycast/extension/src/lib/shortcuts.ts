/**
 * Every keyboard shortcut in the extension, in one table.
 *
 * WHY THIS FILE EXISTS: `Keyboard.Shortcut` is a union, and the single-binding
 * form takes a flat modifier list with no platform in the type — so
 * `{ modifiers: ["cmd"], key: "i" }` compiles everywhere and is then **dropped
 * in silence on Windows**. No error, no lint failure, no runtime warning: the
 * action simply renders with no key beside it. The two-branch form below is the
 * only shape the compiler forces you to finish.
 *
 * Deliberately NOT using `Keyboard.Shortcut.Common.*`, which maps per platform
 * for free but carries surprising bindings (`Common.Remove` is ⌃X, not ⌘⌫;
 * `Common.Duplicate` is ⌘D) that would silently re-key the existing actions.
 *
 * Note the capital W in `Windows` — lowercase `windows` is deprecated and still
 * typechecks.
 */
import type { Keyboard } from "@raycast/api";
import { isWindows } from "./platform";

/**
 * The per-platform arm of `Keyboard.Shortcut`, named so `label()` can read it.
 * `Keyboard.Shortcut` itself is a union, so it cannot be indexed by platform —
 * and the other arm of that union is exactly the one-binding shape this file
 * exists to avoid.
 */
type Branch = {
  modifiers: Keyboard.KeyModifier[];
  key: Keyboard.KeyEquivalent;
};
type PlatformShortcut = { macOS: Branch; Windows: Branch };

const S = (
  key: Keyboard.KeyEquivalent,
  ...extra: Keyboard.KeyModifier[]
): PlatformShortcut => ({
  macOS: { modifiers: ["cmd", ...extra], key },
  Windows: { modifiers: ["ctrl", ...extra], key },
});

export const SHORTCUTS = {
  /** ⌘↵ — the alternate primary action (stream, or accept the highlighted word). */
  streamTranslate: S("return"),
  useThisWord: S("return"),
  /** ⌘I — the secondary copy (source text / input / headword). */
  copySource: S("i"),
  /** ⌘E — cycle the engine override. */
  engine: S("e"),
  /** ⌘L — look the current word up again. */
  lookUpAgain: S("l"),
  /** ⌘Y — swap between the formatted and the raw model output. */
  toggleRaw: S("y"),

  // ⚠️ The three below collide with the Debug section Raycast injects into every
  // action panel while an extension is IN DEVELOPMENT (⌘R, ⇧⌘S, ⇧⌘D, ⇧⌘X, ⌘⌥D).
  // Raycast's own entry wins, so these do nothing under `ray develop` — but work
  // correctly in an installed build. They pass lint and typecheck either way.
  /** ⌘R — reload. Shadowed by Debug → Reload during development. */
  reload: S("r"),
  /** ⇧⌘S — load the current selection. Shadowed during development. */
  loadSelection: S("s", "shift"),
  /** ⇧⌘X — clear the input. Shadowed during development. */
  clear: S("x", "shift"),

  /** ⇧⌘V — load the clipboard. */
  loadClipboard: S("v", "shift"),
  /** ⇧⌘A — toggle the advanced section. */
  toggleAdvanced: S("a", "shift"),
  /** ⇧⌘T — open the current word in Translate. */
  openInTranslate: S("t", "shift"),
} as const;

const MAC_GLYPH: Record<string, string> = {
  cmd: "⌘",
  shift: "⇧",
  opt: "⌥",
  ctrl: "⌃",
  alt: "⌥",
  windows: "⊞",
};

const KEY_GLYPH: Record<string, string> = { return: "↵", enter: "↵" };

/**
 * Render one branch for inline UI copy. Split out from `label()` and given an
 * explicit `windows` flag so dev-check can assert BOTH platforms from one host —
 * `isWindows` is fixed at import time and would otherwise pin the test to macOS.
 */
export function formatBranch(branch: Branch, windows: boolean): string {
  const key = KEY_GLYPH[branch.key] ?? branch.key.toUpperCase();
  if (windows) {
    const mods = branch.modifiers.map(
      (m) => m.charAt(0).toUpperCase() + m.slice(1),
    );
    return [...mods, key].join("+");
  }
  // macOS orders modifiers ⌃⌥⇧⌘ and glues them to the key with no separator.
  const order = ["ctrl", "alt", "opt", "shift", "cmd"];
  const mods = [...branch.modifiers]
    .sort((a, b) => order.indexOf(a) - order.indexOf(b))
    .map((m) => MAC_GLYPH[m] ?? m);
  return mods.join("") + key;
}

/**
 * Render a shortcut for inline UI copy ("⇧⌘S loads the selection").
 *
 * Derived from the same table the actions bind, so the hint text cannot drift
 * from the key that is actually registered — and it reads natively on both
 * platforms instead of showing ⌘ to a Windows user.
 */
export function label(name: keyof typeof SHORTCUTS): string {
  return formatBranch(
    SHORTCUTS[name][isWindows ? "Windows" : "macOS"],
    isWindows,
  );
}
