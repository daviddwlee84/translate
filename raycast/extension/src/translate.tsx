import { useEffect, useRef, useState } from "react";
import {
  ActionPanel,
  Action,
  Icon,
  List,
  getPreferenceValues,
  showHUD,
} from "@raycast/api";
import { usePromise } from "@raycast/utils";
import { runTranslate, speak, TranslateResult } from "./lib/translate";

const LANGS = [
  { title: "English", value: "en" },
  { title: "Chinese (Traditional)", value: "zh-TW" },
  { title: "Chinese (Simplified)", value: "zh-CN" },
  { title: "Japanese", value: "ja" },
  { title: "Korean", value: "ko" },
  { title: "Spanish", value: "es" },
  { title: "French", value: "fr" },
  { title: "German", value: "de" },
  { title: "Italian", value: "it" },
  { title: "Portuguese", value: "pt" },
];

interface Prefs {
  defaultTarget?: string;
  liveDebounceMs?: string;
}

/** Debounce a value so the expensive translate call only fires after the user pauses typing. */
function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(id);
  }, [value, delayMs]);
  return debounced;
}

export default function Command() {
  const prefs = getPreferenceValues<Prefs>();
  const debounceMs = Math.max(250, Number(prefs.liveDebounceMs) || 700);
  const [text, setText] = useState("");
  const [to, setTo] = useState(prefs.defaultTarget ?? "en");

  // Only translate once the user pauses (debounce) and cancel superseded in-flight
  // calls (abortable) — so typing a phrase no longer fires an LLM call per keystroke.
  const debouncedText = useDebouncedValue(text, debounceMs);
  const abortable = useRef<AbortController>();

  const { data, isLoading, error } = usePromise(
    async (q: string, target: string): Promise<TranslateResult | undefined> => {
      const trimmed = q.trim();
      if (!trimmed) return undefined;
      return runTranslate(trimmed, {
        to: target,
        signal: abortable.current?.signal,
      });
    },
    [debouncedText, to],
    { abortable },
  );

  // Keep the loading bar up while the typed text is still ahead of the debounced value.
  const pending =
    text.trim().length > 0 && text.trim() !== debouncedText.trim();

  return (
    <List
      isLoading={isLoading || pending}
      searchText={text}
      onSearchTextChange={setText}
      searchBarPlaceholder="Type text to translate…"
      isShowingDetail={!!data}
      searchBarAccessory={
        <List.Dropdown
          tooltip="Target language"
          storeValue
          value={to}
          onChange={setTo}
        >
          {LANGS.map((l) => (
            <List.Dropdown.Item key={l.value} title={l.title} value={l.value} />
          ))}
        </List.Dropdown>
      }
    >
      {error ? (
        <List.EmptyView
          icon={Icon.Warning}
          title="Translation failed"
          description={String(error.message ?? error)}
        />
      ) : !data ? (
        <List.EmptyView
          icon={Icon.Globe}
          title="Translate"
          description="Enter text above and pick a language."
        />
      ) : (
        <List.Item
          title={data.translation}
          subtitle={data.engine}
          detail={
            <List.Item.Detail
              markdown={renderMarkdown(data)}
              metadata={renderMetadata(data)}
            />
          }
          actions={
            <ActionPanel>
              <Action.CopyToClipboard
                title="Copy Translation"
                content={data.translation}
              />
              <Action.Paste
                title="Paste Translation"
                content={data.translation}
              />
              <Action
                title="Speak"
                icon={Icon.SpeakerHigh}
                onAction={async () => {
                  speak(debouncedText.trim(), to);
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

function renderMarkdown(r: TranslateResult): string {
  const lines = [`# ${r.translation}`, ""];
  if (r.alternatives?.length)
    lines.push("## Alternatives", ...r.alternatives.map((a) => `- ${a}`), "");
  if (r.notes) lines.push("## Notes", r.notes, "");
  if (r.warnings?.length)
    lines.push("## Warnings", ...r.warnings.map((w) => `> ${w}`), "");
  return lines.join("\n");
}

function renderMetadata(r: TranslateResult) {
  return (
    <List.Item.Detail.Metadata>
      <List.Item.Detail.Metadata.Label title="Engine" text={r.engine ?? "—"} />
      {r.model ? (
        <List.Item.Detail.Metadata.Label title="Model" text={r.model} />
      ) : null}
      <List.Item.Detail.Metadata.Label
        title="Source"
        text={r.detected_source ?? "auto"}
      />
      <List.Item.Detail.Metadata.Label title="Target" text={r.target} />
      {typeof r.confidence === "number" ? (
        <List.Item.Detail.Metadata.Label
          title="Confidence"
          text={r.confidence.toFixed(2)}
        />
      ) : null}
    </List.Item.Detail.Metadata>
  );
}
