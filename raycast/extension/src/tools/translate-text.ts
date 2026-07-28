import {
  runTranslate,
  isBinaryMissing,
  TranslateResult,
} from "../lib/translate";

type Input = {
  /**
   * The text to translate, verbatim. Pass exactly what the user wrote —
   * do not pre-translate, summarise, or rephrase it first.
   */
  text: string;
  /**
   * Target language code, e.g. "en", "zh-TW", "zh-CN", "ja", "ko", "es", "fr",
   * "de". Omit it to use the target the user configured, which is usually what
   * they want; only set it when they name a language.
   */
  to?: string;
  /**
   * Source language code, or "auto" to let the CLI detect it. Omit unless the
   * user states the source language and detection is likely to get it wrong.
   */
  from?: string;
  /**
   * Prompt style. "concise" returns only the translation. "contextual" returns
   * 2-4 numbered senses with a short context label each — use it when the user
   * asks what a word means, how it changes by context, or is comparing terms.
   * "dictionary" adds one or two example sentences.
   */
  preset?: "concise" | "contextual" | "dictionary";
  /**
   * Domain context for the translation, e.g. "API rate limiting", "React
   * hooks", "legal contract". Pass this WHENEVER the user names a field or
   * situation — it is what stops a technical term being translated as its
   * everyday sense.
   */
  instructions?: string;
};

/** What the tool hands back when the CLI could not run at all. */
type ToolError = { error: string; hint?: string };

/**
 * Translate text with the user's own translate CLI — their configured engine,
 * fallback chain, preset and API keys.
 *
 * Present the returned `translation` verbatim as the answer; do not re-translate
 * it or reword it. If `warnings` is non-empty, tell the user what it says: the
 * CLI exits successfully even when one engine failed and a later one in the
 * chain answered instead, so a warning is the only sign the result did not come
 * from where they expected. `engine` and `model` say which one did answer.
 */
export default async function tool(
  input: Input,
): Promise<TranslateResult | ToolError> {
  try {
    return await runTranslate(input.text, {
      to: input.to,
      from: input.from,
      preset: input.preset,
      instructions: input.instructions,
    });
  } catch (e) {
    // Expected failures are DATA, not exceptions. An unhandled throw is caught
    // by Raycast, which shows an error screen and ends the run — returning the
    // failure lets the model tell the user something useful instead.
    if (isBinaryMissing(e)) {
      return {
        error:
          "The translate CLI is not installed, or is not where the extension looked.",
        hint: "Install it with `go install github.com/daviddwlee84/translate@latest`, or set the binary path in the extension preferences.",
      };
    }
    return { error: String((e as Error)?.message ?? e) };
  }
}
