import {
  Clipboard,
  getSelectedText,
  launchCommand,
  LaunchProps,
  LaunchType,
  showHUD,
} from "@raycast/api";

interface Args {
  to?: string;
}

/**
 * Grab the current selection (or clipboard) and open the editable Translate view
 * prefilled with it — rather than translating and pasting blind. Falls back to the
 * clipboard, and to an empty Translate when there's nothing to grab. An optional
 * `to` argument seeds the target language.
 */
export default async function Command(props: LaunchProps<{ arguments: Args }>) {
  let seed = "";
  try {
    seed = (await getSelectedText()).trim();
  } catch {
    // No selection / app doesn't expose it — fall back to the clipboard.
    seed = ((await Clipboard.readText()) ?? "").trim();
  }

  try {
    await launchCommand({
      name: "translate",
      type: LaunchType.UserInitiated,
      context: { seed, to: props.arguments?.to || undefined },
    });
  } catch (e) {
    await showHUD(
      `Couldn't open Translate: ${String((e as Error).message ?? e)}`,
    );
  }
}
