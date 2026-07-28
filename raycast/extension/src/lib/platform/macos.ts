/**
 * macOS half of the platform seam.
 *
 * Raycast runs extensions under launchd, which never sources a shell rc, so
 * PATH is roughly /usr/bin:/bin and a bare `translate` throws ENOENT. Every
 * value here exists so the rest of the extension never has to know that.
 */
import { homedir } from "node:os";
import { join } from "node:path";

export const binaryName = "translate";

/**
 * Install dirs probed in order. ~/.local/bin stays FIRST because it is the
 * blessed install location (`just install` / GOBIN-pinned `go install`) — a
 * stray copy elsewhere must not win. Both Homebrew prefixes are probed:
 * /opt/homebrew on Apple Silicon, /usr/local on Intel.
 */
export const probeDirs = [
  join(homedir(), ".local", "bin"),
  "/opt/homebrew/bin",
  "/usr/local/bin",
  join(homedir(), "go", "bin"),
];

export const installCommands = [
  {
    title: "Copy Homebrew Install Command",
    command: "brew install daviddwlee84/tap/translate",
  },
  {
    title: "Copy Go Install Command",
    command: "go install github.com/daviddwlee84/translate@latest",
  },
];

/** Shown in error/onboarding markdown. */
export const installHint = installCommands.map((c) => c.command).join("\n");

export const configPathHint = "~/.config/translate/config.toml";

/** Raycast forces HOME so the CLI can find its config under launchd. */
export const forcesHome = true;
