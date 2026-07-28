/**
 * Windows half of the platform seam.
 *
 * Raycast for Windows does not run your shell profile either, so the same rule
 * applies as on macOS: never invoke a bare binary name, always resolve an
 * absolute path, and let the `binaryPath` preference override the probe.
 *
 * There is no Windows release artifact yet (see backlog/release-binaries.md),
 * so `go install` is the only documented install path — which lands the binary
 * in %GOBIN% or, by default, ~\go\bin.
 */
import { homedir } from "node:os";
import { join } from "node:path";

export const binaryName = "translate.exe";

/**
 * ~/.local/bin comes first for parity with macOS: the repo's own install recipe
 * pins GOBIN there, and `join(homedir(), …)` resolves correctly on Windows
 * (C:\Users\you\.local\bin). Then the default GOBIN, then the common
 * third-party package managers.
 */
export const probeDirs = [
  join(homedir(), ".local", "bin"),
  join(homedir(), "go", "bin"),
  join(
    process.env.LOCALAPPDATA ?? join(homedir(), "AppData", "Local"),
    "Programs",
    "translate",
  ),
  join(homedir(), "scoop", "shims"),
  join(
    process.env.ChocolateyInstall ?? join("C:", "ProgramData", "chocolatey"),
    "bin",
  ),
  join(process.env.ProgramFiles ?? join("C:", "Program Files"), "translate"),
];

export const installCommands = [
  {
    title: "Copy Go Install Command",
    command: "go install github.com/daviddwlee84/translate@latest",
  },
];

export const installHint = installCommands.map((c) => c.command).join("\n");

/**
 * internal/xdgpath resolves via os.UserHomeDir(), which is %USERPROFILE% on
 * Windows — so the config lands under the home directory rather than %APPDATA%.
 * Un-idiomatic for Windows, deliberately consistent across hosts.
 */
export const configPathHint = "%USERPROFILE%\\.config\\translate\\config.toml";

/** Go reads %USERPROFILE% here, not HOME — forcing HOME would be a no-op. */
export const forcesHome = false;
