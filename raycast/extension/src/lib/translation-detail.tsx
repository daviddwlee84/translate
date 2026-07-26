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
 */
export function TranslationDetail(props: {
  text: string;
  to: string;
  engine?: string;
}) {
  const { text, to, engine } = props;
  const { data, isLoading, error } = usePromise(
    async (t: string, target: string, eng?: string): Promise<TranslateResult> =>
      runTranslate(t, { to: target, engine: eng || undefined }),
    [text, to, engine],
  );

  const markdown = error
    ? errorMarkdown(error)
    : data
      ? renderTranslation(data)
      : "Translating…";

  return (
    <Detail
      isLoading={isLoading}
      navigationTitle={`Translate → ${to}`}
      markdown={markdown}
      metadata={data ? <Metadata result={data} /> : undefined}
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
                  speak(text, data.target || to);
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
  return lines.join("\n");
}

function Metadata(props: { result: TranslateResult }) {
  const r = props.result;
  return (
    <Detail.Metadata>
      <Detail.Metadata.Label title="Engine" text={r.engine ?? "—"} />
      {r.model ? <Detail.Metadata.Label title="Model" text={r.model} /> : null}
      <Detail.Metadata.Label
        title="Source"
        text={r.detected_source ?? "auto"}
      />
      <Detail.Metadata.Label title="Target" text={r.target} />
      {typeof r.confidence === "number" ? (
        <Detail.Metadata.Label
          title="Confidence"
          text={r.confidence.toFixed(2)}
        />
      ) : null}
    </Detail.Metadata>
  );
}
