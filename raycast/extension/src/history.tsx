import { useRef } from "react";
import { ActionPanel, Action, Icon, List } from "@raycast/api";
import { usePromise } from "@raycast/utils";
import { runHistory, HistoryEntry } from "./lib/translate";

export default function Command() {
  const abortable = useRef<AbortController>();
  const { data, isLoading, revalidate } = usePromise(
    async (): Promise<HistoryEntry[]> =>
      runHistory(undefined, 200, abortable.current?.signal),
    [],
    { abortable },
  );

  const entries = data ?? [];

  return (
    <List
      isLoading={isLoading}
      searchBarPlaceholder="Search history…"
      isShowingDetail={entries.length > 0}
    >
      {entries.length === 0 && !isLoading ? (
        <List.EmptyView
          icon={Icon.Clock}
          title="No history yet"
          description="Translations you run will show up here."
        />
      ) : (
        entries.map((h) => (
          <List.Item
            key={h.id}
            title={h.output || "(empty)"}
            subtitle={h.input}
            keywords={[
              h.input,
              h.output,
              h.source_lang,
              h.target_lang,
              h.engine ?? "",
            ]}
            accessories={[{ tag: h.target_lang }, { text: h.engine }]}
            detail={
              <List.Item.Detail
                markdown={renderEntry(h)}
                metadata={renderMetadata(h)}
              />
            }
            actions={
              <ActionPanel>
                <Action.CopyToClipboard
                  title="Copy Output"
                  content={h.output}
                />
                <Action.Paste title="Paste Output" content={h.output} />
                <Action.CopyToClipboard
                  title="Copy Input"
                  content={h.input}
                  shortcut={{ modifiers: ["cmd"], key: "i" }}
                />
                <Action
                  title="Reload"
                  icon={Icon.ArrowClockwise}
                  onAction={() => revalidate()}
                  shortcut={{ modifiers: ["cmd"], key: "r" }}
                />
              </ActionPanel>
            }
          />
        ))
      )}
    </List>
  );
}

function renderEntry(h: HistoryEntry): string {
  return [`**${h.input}**`, "", `→ ${h.output}`].join("\n");
}

function fmtTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}

function renderMetadata(h: HistoryEntry) {
  return (
    <List.Item.Detail.Metadata>
      <List.Item.Detail.Metadata.Label title="Source" text={h.source_lang} />
      <List.Item.Detail.Metadata.Label title="Target" text={h.target_lang} />
      <List.Item.Detail.Metadata.Label title="Engine" text={h.engine ?? "—"} />
      {h.model ? (
        <List.Item.Detail.Metadata.Label title="Model" text={h.model} />
      ) : null}
      <List.Item.Detail.Metadata.Label title="When" text={fmtTime(h.ts)} />
    </List.Item.Detail.Metadata>
  );
}
