import {
  ActionPanel,
  Action,
  Detail,
  Icon,
  launchCommand,
  LaunchType,
  showHUD,
} from "@raycast/api";
import { usePromise } from "@raycast/utils";
import {
  isBinaryMissing,
  isNoDictEntry,
  runDefine,
  speakText,
  TranslateResult,
} from "./translate";

/**
 * The full-page definition for one word — the payoff of pressing ⏎ on a
 * candidate. A whole Detail with markdown beats a List detail pane once the
 * definition (or the LLM fallback's prose) runs long.
 *
 * This is also the ONLY place the extension writes history: `runDefine` is called
 * without --no-history, so a word is remembered when you open it, never while you
 * type. onRecorded lets the pushing list refresh its recent-lookups section.
 */
export function DefineDetail(props: {
  word: string;
  to: string;
  onRecorded?: () => void;
}) {
  const { word, to } = props;
  const { data, isLoading, error } = usePromise(
    async (w: string, target: string): Promise<TranslateResult> =>
      // smart: fall back to the LLM when the offline dictionary has no entry.
      runDefine(w, { to: target, smart: true }),
    [word, to],
    { onData: () => props.onRecorded?.() },
  );

  const markdown = error
    ? errorMarkdown(word, error)
    : data
      ? renderDefinition(data, word)
      : `# ${word}\n\nLooking up…`;

  return (
    <Detail
      isLoading={isLoading}
      navigationTitle={word}
      markdown={markdown}
      metadata={data ? <Metadata result={data} to={to} /> : undefined}
      actions={
        <ActionPanel>
          {data ? (
            <Action.CopyToClipboard
              title="Copy Definition"
              content={definitionText(data, word)}
            />
          ) : null}
          <Action.CopyToClipboard
            title="Copy Word"
            content={word}
            shortcut={{ modifiers: ["cmd"], key: "i" }}
          />
          {data ? (
            <Action.Paste
              title="Paste Definition"
              content={definitionText(data, word)}
            />
          ) : null}
          <Action
            title="Speak"
            icon={Icon.SpeakerHigh}
            onAction={async () => {
              // Pronounce the headword itself — `translate speak` neither
              // translates nor records, unlike the Translate command's Speak.
              speakText(word);
              await showHUD("Speaking…");
            }}
          />
          <Action
            title="Open in Translate"
            icon={Icon.Text}
            shortcut={{ modifiers: ["cmd", "shift"], key: "t" }}
            onAction={async () => {
              await launchCommand({
                name: "translate",
                type: LaunchType.UserInitiated,
                context: { seed: word, to },
              });
            }}
          />
        </ActionPanel>
      }
    />
  );
}

function errorMarkdown(word: string, error: Error): string {
  if (isNoDictEntry(error)) {
    return [
      `# ${word}`,
      "",
      "No dictionary entry, and no LLM fallback is configured.",
      "",
      "Configure a provider in `~/.config/translate/config.toml` (or run `translate init`) to define unknown words with an LLM.",
    ].join("\n");
  }
  if (isBinaryMissing(error)) {
    return [
      "# translate CLI not found",
      "",
      "Set the binary path in the extension preferences, or install it:",
      "",
      "```sh",
      "brew install daviddwlee84/tap/translate",
      "```",
    ].join("\n");
  }
  return `# ${word}\n\nLookup failed.\n\n\`\`\`\n${String(error.message ?? error)}\n\`\`\``;
}

/** The plain text a Copy/Paste action should produce. */
function definitionText(r: TranslateResult, word: string): string {
  const d = r.dictionary;
  if (!d) return r.translation;
  const lines: string[] = [];
  for (const m of d.meanings ?? []) {
    for (const def of m.definitions ?? []) lines.push(def.definition);
  }
  return lines.length > 0 ? lines.join("\n") : r.translation || word;
}

/** Renders a dictionary entry, or the LLM fallback's prose, as markdown. */
export function renderDefinition(r: TranslateResult, word: string): string {
  const d = r.dictionary;
  if (d) {
    const lines = [`# ${d.word || word}`];
    if (d.phonetic) lines.push(`*/${d.phonetic}/*`);
    lines.push("");
    for (const m of d.meanings ?? []) {
      lines.push(`## ${m.part_of_speech}`);
      for (const def of m.definitions ?? []) {
        lines.push(
          `- ${def.definition}${def.example ? `\n  - _${def.example}_` : ""}`,
        );
      }
      lines.push("");
    }
    return lines.join("\n");
  }
  // No dictionary entry — the LLM fallback's definition lives in `translation`.
  const lines = [`# ${word}`, "", r.translation, ""];
  if (r.warnings?.length) lines.push("---", ...r.warnings.map((w) => `> ${w}`));
  return lines.join("\n");
}

function Metadata(props: { result: TranslateResult; to: string }) {
  const { result: r, to } = props;
  // A dictionary hit reports the language its glosses are actually written in,
  // which is fixed by the data (ECDICT is en→zh, CC-CEDICT is zh→en). Say so
  // rather than letting the picked target look ignored.
  const scriptFixed =
    r.engine === "dictionary" && !!r.target && r.target !== to;
  return (
    <Detail.Metadata>
      <Detail.Metadata.Label title="Engine" text={r.engine ?? "—"} />
      {r.dictionary?.phonetic ? (
        <Detail.Metadata.Label title="Phonetic" text={r.dictionary.phonetic} />
      ) : null}
      {r.model ? <Detail.Metadata.Label title="Model" text={r.model} /> : null}
      <Detail.Metadata.Label
        title="Definition language"
        text={
          scriptFixed
            ? `${r.target} — the offline dictionary is zh↔en; “${to}” applies to the LLM fallback`
            : r.target || to
        }
      />
      {r.dictionary?.source_url ? (
        <Detail.Metadata.Link
          title="Source"
          target={r.dictionary.source_url}
          text="link"
        />
      ) : null}
    </Detail.Metadata>
  );
}
