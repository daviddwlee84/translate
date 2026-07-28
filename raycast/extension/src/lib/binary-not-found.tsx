import {
  ActionPanel,
  Action,
  Icon,
  List,
  openExtensionPreferences,
} from "@raycast/api";
import { platform } from "./platform";

/**
 * Actionable empty state shown when the `translate` binary can't be found — instead
 * of a bare error. Offers copyable install commands and a jump to preferences (to
 * set a custom binary path). Raycast runs under a restricted PATH on both macOS
 * and Windows, so a binary at ~/.local/bin etc. isn't on the extension's PATH —
 * hence the probe/preference. The commands offered come from the platform seam:
 * Homebrew is macOS/Linux-only, so Windows sees `go install` alone.
 */
export function BinaryNotFound() {
  return (
    <List.EmptyView
      icon={Icon.Download}
      title="translate CLI not found"
      description="Install the translate binary, then reopen. If it lives in a non-standard location, set its path in preferences."
      actions={
        <ActionPanel>
          {platform.installCommands.map((c) => (
            <Action.CopyToClipboard
              key={c.command}
              title={c.title}
              content={c.command}
            />
          ))}
          <Action
            title="Open Extension Preferences"
            icon={Icon.Gear}
            onAction={openExtensionPreferences}
          />
          <Action.OpenInBrowser
            title="Open the Project Readme"
            url="https://github.com/daviddwlee84/translate#install"
          />
        </ActionPanel>
      }
    />
  );
}

/**
 * The Detail-surface twin of BinaryNotFound: same failure, same install
 * commands, rendered as markdown instead of an empty view. Kept here so the two
 * renderings of one failure cannot drift apart.
 */
export function binaryMissingMarkdown(): string {
  return [
    "# translate CLI not found",
    "",
    "Set the binary path in the extension preferences, or install it:",
    "",
    "```sh",
    platform.installHint,
    "```",
  ].join("\n");
}
