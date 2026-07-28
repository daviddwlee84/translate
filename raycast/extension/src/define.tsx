import { useRef, useState } from "react";
import { ActionPanel, Action, Icon, List, showHUD } from "@raycast/api";
import { usePromise } from "@raycast/utils";
import {
  runDefine,
  speak,
  isBinaryMissing,
  TranslateResult,
} from "./lib/translate";
import { useDebouncedValue } from "./lib/hooks";
import { BinaryNotFound } from "./lib/binary-not-found";
import { DidYouMean } from "./lib/did-you-mean";
import { SHORTCUTS } from "./lib/shortcuts";

export default function Command() {
  const [word, setWord] = useState("");
  const debounced = useDebouncedValue(word, 500);
  const abortable = useRef<AbortController | undefined>(undefined);

  const { data, isLoading, error } = usePromise(
    async (w: string): Promise<TranslateResult | undefined> => {
      const trimmed = w.trim();
      if (!trimmed) return undefined;
      // Type-to-lookup fires on every debounced keystroke, so it must not record:
      // history belongs to Look Up Word, where you explicitly open a word.
      return runDefine(trimmed, {
        noHistory: true,
        signal: abortable.current?.signal,
      });
    },
    [debounced],
    { abortable },
  );

  const pending = word.trim().length > 0 && word.trim() !== debounced.trim();
  const showSuggestions =
    !!data && !data.dictionary && !!data.suggestions?.length;

  return (
    <List
      isLoading={isLoading || pending}
      searchText={word}
      onSearchTextChange={setWord}
      searchBarPlaceholder="Look up a word…"
      isShowingDetail={!!data && !showSuggestions}
    >
      {error ? (
        isBinaryMissing(error) ? (
          <BinaryNotFound />
        ) : (
          <List.EmptyView
            icon={Icon.Warning}
            title="Lookup failed"
            description={String(error.message ?? error)}
          />
        )
      ) : !data ? (
        <List.EmptyView
          icon={Icon.Book}
          title="Define"
          description="Type a word to look it up."
        />
      ) : showSuggestions ? (
        <DidYouMean suggestions={data.suggestions!} onPick={setWord} />
      ) : (
        <List.Item
          title={data.dictionary?.word ?? word}
          subtitle={data.engine}
          detail={
            <List.Item.Detail
              markdown={renderDefine(data, word)}
              metadata={renderMetadata(data)}
            />
          }
          actions={
            <ActionPanel>
              <Action.CopyToClipboard
                title="Copy Definition"
                content={data.translation}
              />
              <Action.CopyToClipboard
                title="Copy Word"
                content={data.dictionary?.word ?? word}
                shortcut={SHORTCUTS.copySource}
              />
              <Action
                title="Speak"
                icon={Icon.SpeakerHigh}
                onAction={async () => {
                  speak(
                    data.dictionary?.word ?? word.trim(),
                    data.target || "en",
                  );
                  await showHUD("Speaking…");
                }}
              />
            </ActionPanel>
          }
        />
      )}
    </List>
  );
}

function renderDefine(r: TranslateResult, word: string): string {
  const d = r.dictionary;
  if (d) {
    const lines = [`# ${d.word}`];
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
  // No dictionary entry — LLM fallback lives in `translation`.
  const lines = [`# ${word}`, "", r.translation, ""];
  if (r.warnings?.length) lines.push("---", ...r.warnings.map((w) => `> ${w}`));
  return lines.join("\n");
}

function renderMetadata(r: TranslateResult) {
  return (
    <List.Item.Detail.Metadata>
      <List.Item.Detail.Metadata.Label title="Engine" text={r.engine ?? "—"} />
      {r.dictionary?.phonetic ? (
        <List.Item.Detail.Metadata.Label
          title="Phonetic"
          text={r.dictionary.phonetic}
        />
      ) : null}
      {r.model ? (
        <List.Item.Detail.Metadata.Label title="Model" text={r.model} />
      ) : null}
      {r.dictionary?.source_url ? (
        <List.Item.Detail.Metadata.Link
          title="Source"
          target={r.dictionary.source_url}
          text="link"
        />
      ) : null}
    </List.Item.Detail.Metadata>
  );
}
