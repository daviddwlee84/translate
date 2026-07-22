import {
  ActionPanel,
  Action,
  Icon,
  List,
  openExtensionPreferences,
} from "@raycast/api";

const INSTALL_BREW = "brew install daviddwlee84/tap/translate";
const INSTALL_GO = "go install github.com/daviddwlee84/translate@latest";

/**
 * Actionable empty state shown when the `translate` binary can't be found — instead
 * of a bare error. Offers copyable install commands and a jump to preferences (to
 * set a custom binary path). Raycast runs under a restricted PATH, so the binary
 * living at ~/.local/bin etc. isn't on the extension's PATH — hence the probe/preference.
 */
export function BinaryNotFound() {
  return (
    <List.EmptyView
      icon={Icon.Download}
      title="translate CLI not found"
      description="Install the translate binary, then reopen. If it lives in a non-standard location, set its path in preferences."
      actions={
        <ActionPanel>
          <Action.CopyToClipboard
            title="Copy Homebrew Install Command"
            content={INSTALL_BREW}
          />
          <Action.CopyToClipboard
            title="Copy Go Install Command"
            content={INSTALL_GO}
          />
          <Action
            title="Open Extension Preferences"
            icon={Icon.Gear}
            onAction={openExtensionPreferences}
          />
          <Action.OpenInBrowser
            title="Open the Homebrew Tap"
            url="https://github.com/daviddwlee84/homebrew-tap"
          />
        </ActionPanel>
      }
    />
  );
}
