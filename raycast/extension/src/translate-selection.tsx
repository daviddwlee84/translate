import {
  Clipboard,
  getPreferenceValues,
  getSelectedText,
  showHUD,
} from "@raycast/api";
import { runTranslate } from "./lib/translate";

export default async function Command() {
  const prefs = getPreferenceValues<{ defaultTarget?: string }>();

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
    const res = await runTranslate(text, { to: prefs.defaultTarget });
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
