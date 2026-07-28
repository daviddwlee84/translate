import { ActionPanel, Action, Detail, Icon, showHUD } from "@raycast/api";
import { HistoryEntry, speakText } from "./translate";
import { DefineDetail } from "./define-detail";
import { SHORTCUTS } from "./shortcuts";
import { renderModelOutput } from "./markdown";

/**
 * A stored history row as a full page.
 *
 * Nothing is re-run: the record already holds the answer, so this renders
 * instantly and adds no new history row. That matters most for long past
 * translations, which are unreadable in a narrow list detail pane but fine as a
 * markdown page.
 */
export function HistoryDetail(props: { entry: HistoryEntry; to: string }) {
  const { entry, to } = props;
  return (
    <Detail
      navigationTitle={oneline(entry.input, 48)}
      markdown={renderEntry(entry)}
      metadata={
        <Detail.Metadata>
          <Detail.Metadata.Label
            title="Direction"
            text={`${entry.source_lang} → ${entry.target_lang}`}
          />
          <Detail.Metadata.Label title="Engine" text={entry.engine ?? "—"} />
          {entry.model ? (
            <Detail.Metadata.Label title="Model" text={entry.model} />
          ) : null}
          <Detail.Metadata.Label title="When" text={formatTS(entry.ts)} />
        </Detail.Metadata>
      }
      actions={
        <ActionPanel>
          <Action.CopyToClipboard title="Copy Output" content={entry.output} />
          <Action.Paste title="Paste Output" content={entry.output} />
          <Action.CopyToClipboard
            title="Copy Input"
            content={entry.input}
            shortcut={SHORTCUTS.copySource}
          />
          <Action.Push
            title="Look up Again"
            icon={Icon.Book}
            shortcut={SHORTCUTS.lookUpAgain}
            target={<DefineDetail word={entry.input} to={to} />}
          />
          <Action
            title="Speak"
            icon={Icon.SpeakerHigh}
            onAction={async () => {
              speakText(entry.output, entry.target_lang);
              await showHUD("Speaking…");
            }}
          />
        </ActionPanel>
      }
    />
  );
}

export function renderEntry(h: HistoryEntry): string {
  // The stored output is model text, so it gets the same repair pass as a live
  // translation — a table recorded before the CLI learned to emit GFM structure
  // would otherwise render as reflowed prose forever.
  return [`# ${h.input}`, "", renderModelOutput(h.output)].join("\n");
}

export function formatTS(ts: string): string {
  const d = new Date(ts);
  return isNaN(d.getTime()) ? ts : d.toLocaleString();
}

export function oneline(s: string, n: number): string {
  const flat = s.replace(/\s+/g, " ").trim();
  return flat.length <= n ? flat : `${flat.slice(0, n - 1)}…`;
}
