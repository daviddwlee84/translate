import { ActionPanel, Action, Detail, Icon, showHUD } from "@raycast/api";
import { useState } from "react";
import { usePromise } from "@raycast/utils";
import {
  isBinaryMissing,
  runTranslate,
  speak,
  TranslateResult,
} from "./translate";
import { binaryMissingMarkdown } from "./binary-not-found";
import { SHORTCUTS } from "./shortcuts";
import { renderModelOutput, looksTabular } from "./markdown";

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
  // Every dependency is an ARGUMENT, not a closure capture: the argument array
  // is what usePromise keys on and re-runs from. A captured variable changes
  // silently and keeps serving the previous target's translation.
  const { data, isLoading, error } = usePromise(
    async (
      t: string,
      target?: string,
      eng?: string,
      mdl?: string,
      p?: boolean,
    ): Promise<TranslateResult> =>
      runTranslate(t, { to: target, engine: eng, model: mdl, pair: p }),
    [text, to, engine, model, pair],
  );

  // Model output goes through the markdown repair pass before it reaches
  // Detail — see lib/markdown.ts for why alignment can't survive the trip.
  // `raw` is a per-result escape hatch rather than a preference: a stored
  // preference value outlives a manifest default change, which is the wrong
  // behaviour for something you want to flip and flip back.
  const [raw, setRaw] = useState(false);

  const markdown = error
    ? errorMarkdown(error)
    : data
      ? renderTranslation(data, raw)
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
                shortcut={SHORTCUTS.copySource}
              />
              {looksTabular(data.translation) ? (
                <Action
                  title={raw ? "Show Formatted" : "Show Raw Output"}
                  icon={Icon.Code}
                  shortcut={SHORTCUTS.toggleRaw}
                  onAction={() => setRaw((v) => !v)}
                />
              ) : null}
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
  if (isBinaryMissing(error)) return binaryMissingMarkdown();
  return `# Translation failed\n\n\`\`\`\n${String(error.message ?? error)}\n\`\`\``;
}

/**
 * Plain paragraph (not an H1) so long translations read comfortably.
 *
 * Only the translation itself is repaired: alternatives, notes and warnings are
 * our own markup and are already well-formed.
 */
export function renderTranslation(r: TranslateResult, raw = false): string {
  const body = raw ? r.translation : renderModelOutput(r.translation);
  const lines = [body, ""];
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
