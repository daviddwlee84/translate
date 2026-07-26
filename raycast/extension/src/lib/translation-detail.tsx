import { ActionPanel, Action, Detail, Icon, showHUD } from "@raycast/api";
import { usePromise } from "@raycast/utils";
import {
  isBinaryMissing,
  runTranslate,
  speak,
  TranslateResult,
} from "./translate";

/**
 * A translation rendered as a full page.
 *
 * The List-based Translate command shows its result in a detail pane, which is
 * cramped once the input is a paragraph rather than a phrase — this is what
 * Translate Text pushes after submitting.
 *
 * Engine/model/direction go in a footer line at the end of the scroll rather
 * than a Detail.Metadata sidebar: the sidebar is a fixed third of the window,
 * which is a lot of width to spend permanently on five short labels when the
 * point of this view is reading room.
 */
export function TranslationDetail(props: {
  text: string;
  to?: string;
  engine?: string;
  model?: string;
  pair?: boolean;
}) {
  const { text, to, engine, model, pair } = props;
  const { data, isLoading, error } = usePromise(
    async (): Promise<TranslateResult> =>
      runTranslate(text, { to, engine, model, pair }),
    [text, to, engine, model, pair],
  );

  const markdown = error
    ? errorMarkdown(error)
    : data
      ? renderTranslation(data)
      : "Translating…";

  return (
    <Detail
      isLoading={isLoading}
      navigationTitle={
        data?.target ? `Translate → ${data.target}` : "Translate"
      }
      markdown={markdown}
      actions={
        <ActionPanel>
          {data ? (
            <>
              <Action.CopyToClipboard
                title="Copy Translation"
                content={data.translation}
              />
              <Action.Paste
                title="Paste Translation"
                content={data.translation}
              />
              <Action.CopyToClipboard
                title="Copy Source Text"
                content={text}
                shortcut={{ modifiers: ["cmd"], key: "i" }}
              />
              <Action
                title="Speak"
                icon={Icon.SpeakerHigh}
                onAction={async () => {
                  speak(text, data.target || to || "en");
                  await showHUD("Speaking…");
                }}
              />
            </>
          ) : null}
        </ActionPanel>
      }
    />
  );
}

function errorMarkdown(error: Error): string {
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
  return `# Translation failed\n\n\`\`\`\n${String(error.message ?? error)}\n\`\`\``;
}

/** Plain paragraph (not an H1) so long translations read comfortably. */
export function renderTranslation(r: TranslateResult): string {
  const lines = [r.translation, ""];
  if (r.alternatives?.length)
    lines.push("## Alternatives", ...r.alternatives.map((a) => `- ${a}`), "");
  if (r.notes) lines.push("## Notes", r.notes, "");
  if (r.warnings?.length)
    lines.push("## Warnings", ...r.warnings.map((w) => `> ${w}`), "");
  lines.push("---", footer(r));
  return lines.join("\n");
}

/** One dim line at the end of the scroll, in place of a metadata sidebar. */
function footer(r: TranslateResult): string {
  const bits = [
    `${r.detected_source ?? "auto"} → ${r.target}`,
    r.engine ?? "—",
    r.model,
    typeof r.confidence === "number"
      ? `confidence ${r.confidence.toFixed(2)}`
      : undefined,
  ].filter(Boolean);
  return `*${bits.join(" · ")}*`;
}
