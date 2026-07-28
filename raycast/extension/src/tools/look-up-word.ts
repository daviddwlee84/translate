import {
  runDefine,
  isBinaryMissing,
  isNoDictEntry,
  DictEntry,
  TranslateResult,
} from "../lib/translate";

type Input = {
  /**
   * The word or short term to look up. Single words and fixed phrases only —
   * this is a dictionary, not a translator. For a sentence, use
   * `translate-text`.
   */
  word: string;
  /**
   * Language for the definitions, e.g. "zh-TW" or "en". Note the offline
   * dictionary is fixed at English↔Chinese, so this only takes effect when the
   * lookup falls through to the LLM.
   */
  to?: string;
};

type LookupResult = {
  word: string;
  phonetic?: string;
  /** Present when the answer came from the offline dictionary. */
  dictionary?: DictEntry;
  /** The gloss line, or the LLM fallback's definition. */
  definition: string;
  /** "dictionary" for an offline hit, otherwise the provider that answered. */
  source: string;
  warnings?: string[];
};

type LookupMiss = {
  error: string;
  /** Near-miss headwords to offer as "did you mean". */
  suggestions: string[];
};

/**
 * Look a word up in the local offline dictionary (ECDICT for English to Chinese,
 * CC-CEDICT for Chinese to English).
 *
 * The `phonetic` field is real dictionary data — use it verbatim and never
 * invent or guess a KK/IPA transcription. The glosses are terse and
 * sense-ordered, not context-aware: for what a term means inside a particular
 * field, call the translate-text tool with `preset` set to "contextual" and an
 * `instructions` value naming that field.
 *
 * A miss returns `suggestions` rather than failing, so offer those as
 * "did you mean" instead of reporting an error.
 */
export default async function tool(
  input: Input,
): Promise<LookupResult | LookupMiss | { error: string; hint?: string }> {
  try {
    const r: TranslateResult = await runDefine(input.word, {
      to: input.to,
      // A tool call is not a user lookup — keep it out of the history the
      // Look up Word command shows, so the model cannot pollute it.
      noHistory: true,
    });
    if (r.suggestions?.length && !r.dictionary) {
      return {
        error: `No dictionary entry for "${input.word}".`,
        suggestions: r.suggestions,
      };
    }
    return {
      word: r.dictionary?.word ?? input.word,
      phonetic: r.dictionary?.phonetic,
      dictionary: r.dictionary,
      definition: r.translation,
      source: r.engine ?? "unknown",
      warnings: r.warnings,
    };
  } catch (e) {
    if (isNoDictEntry(e)) {
      return {
        error: `No dictionary entry for "${input.word}", and no LLM fallback is configured.`,
        suggestions: [],
      };
    }
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
