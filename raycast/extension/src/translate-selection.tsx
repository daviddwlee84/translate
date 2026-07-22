import {
  Clipboard,
  getPreferenceValues,
  getSelectedText,
  LaunchProps,
  showHUD,
} from "@raycast/api";
import { runTranslate } from "./lib/translate";

interface Args {
  to?: string;
}

export default async function Command(props: LaunchProps<{ arguments: Args }>) {
  const prefs = getPreferenceValues<{ defaultTarget?: string }>();
  const to = props.arguments?.to || prefs.defaultTarget || "en";

  let text = "";
  try {
    text = (await getSelectedText()).trim();
  } catch {
    // No selection / app doesn't expose it — fall back to the clipboard.
    text = ((await Clipboard.readText()) ?? "").trim();
  }

  if (!text) {
    await showHUD("No text selected");
    return;
  }

  try {
    const res = await runTranslate(text, { to });
    await Clipboard.paste(res.translation);
    await showHUD(
      res.warnings?.length
        ? `Translated (⚠ ${res.warnings[0]})`
        : `Translated → ${res.target}`,
    );
  } catch (e) {
    await showHUD(`Translation failed: ${String((e as Error).message ?? e)}`);
  }
}
