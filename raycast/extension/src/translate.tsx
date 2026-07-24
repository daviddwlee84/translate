import { useEffect, useRef, useState } from "react";
import {
  ActionPanel,
  Action,
  Clipboard,
  getSelectedText,
  Icon,
  LaunchProps,
  List,
  getPreferenceValues,
  showHUD,
} from "@raycast/api";
import { usePromise } from "@raycast/utils";
import {
  runTranslate,
  speak,
  readConfig,
  LANGS,
  isBinaryMissing,
  TranslateResult,
} from "./lib/translate";
import { useDebouncedValue } from "./lib/hooks";
import { StreamView } from "./lib/stream-view";
import { BinaryNotFound } from "./lib/binary-not-found";
import { DidYouMean } from "./lib/did-you-mean";

const ENGINES = [
  { title: "Auto (fallback chain)", value: "" },
  { title: "Google", value: "google" },
  { title: "Dictionary (offline)", value: "dict" },
  { title: "Copilot", value: "copilot" },
  { title: "Ollama", value: "ollama" },
];

interface Prefs {
  defaultTarget?: string;
  liveDebounceMs?: string;
  prefill?: string;
}

export default function Command(
  props: LaunchProps<{ launchContext?: { seed?: string; to?: string } }>,
) {
  const prefs = getPreferenceValues<Prefs>();
  const ctx = props.launchContext;
  const prefDebounce = Number(prefs.liveDebounceMs);
  const [debounceMs, setDebounceMs] = useState(
    prefDebounce > 0 ? Math.max(250, prefDebounce) : 700,
  );
  const [text, setText] = useState(ctx?.seed ?? "");
  const [to, setTo] = useState(ctx?.to || prefs.defaultTarget || "");
  const [engine, setEngine] = useState("");

  // Inherit the default target + debounce from the CLI config (~/.config/translate)
  // when the matching Raycast preference is empty. Falls back to "en"/700ms.
  useEffect(() => {
    if (to && prefs.liveDebounceMs) return; // nothing to inherit
    readConfig()
      .then((cfg) => {
        if (!to && cfg.general?.default_target)
          setTo(cfg.general.default_target);
        if (!prefs.liveDebounceMs && cfg.general?.debounce_ms)
          setDebounceMs(Math.max(250, cfg.general.debounce_ms));
      })
      .catch(() => {
        /* fall back to defaults */
      })
      .finally(() => setTo((t) => t || "en"));
  }, []);

  // On first open, seed the input from the current selection (or clipboard), per the
  // "Prefill input from" preference — unless we were launched with a seed (from
  // Translate Selection). Empty when there's nothing to grab, so you can also type.
  useEffect(() => {
    if (ctx?.seed) return;
    const mode = prefs.prefill ?? "selection";
    if (mode === "none") return;
    (async () => {
      let seed = "";
      if (mode === "selection") {
        try {
          seed = (await getSelectedText()).trim();
        } catch {
          seed = "";
        }
      } else if (mode === "clipboard") {
        seed = ((await Clipboard.readText()) ?? "").trim();
      }
      if (seed) setText(seed);
    })();
  }, []);

  // Only translate once the user pauses (debounce) and cancel superseded in-flight
  // calls (abortable) — so typing a phrase no longer fires an LLM call per keystroke.
  const debouncedText = useDebouncedValue(text, debounceMs);
  const abortable = useRef<AbortController>();

  const { data, isLoading, error } = usePromise(
    async (
      q: string,
      target: string,
      eng: string,
    ): Promise<TranslateResult | undefined> => {
      const trimmed = q.trim();
      if (!trimmed) return undefined;
      return runTranslate(trimmed, {
        to: target,
        engine: eng || undefined,
        signal: abortable.current?.signal,
      });
    },
    [debouncedText, to, engine],
    { abortable },
  );

  // Keep the loading bar up while the typed text is still ahead of the debounced value.
  const pending =
    text.trim().length > 0 && text.trim() !== debouncedText.trim();

  // A single-word typo routes to the dictionary and comes back with an empty
  // translation + fuzzy suggestions — surface them as "did you mean?".
  const showSuggestions =
    !!data && !data.translation && !!data.suggestions?.length;

  return (
    <List
      isLoading={isLoading || pending}
      searchText={text}
      onSearchTextChange={setText}
      searchBarPlaceholder="Type text to translate…"
      isShowingDetail={!!data?.translation}
      searchBarAccessory={
        <List.Dropdown tooltip="Target language" value={to} onChange={setTo}>
          {LANGS.map((l) => (
            <List.Dropdown.Item key={l.value} title={l.title} value={l.value} />
          ))}
        </List.Dropdown>
      }
    >
      {error ? (
        isBinaryMissing(error) ? (
          <BinaryNotFound />
        ) : (
          <List.EmptyView
            icon={Icon.Warning}
            title="Translation failed"
            description={String(error.message ?? error)}
          />
        )
      ) : !data ? (
        <List.EmptyView
          icon={Icon.Globe}
          title="Translate"
          description="Enter text above and pick a language."
        />
      ) : showSuggestions ? (
        <DidYouMean suggestions={data.suggestions!} onPick={setText} />
      ) : data.translation ? (
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
              <Action.Push
                title="Translate (streaming)"
                icon={Icon.Text}
                shortcut={{ modifiers: ["cmd"], key: "return" }}
                target={
                  <StreamView
                    text={debouncedText.trim()}
                    to={to}
                    engine={engine}
                  />
                }
              />
              <Action.CopyToClipboard
                title="Copy Source Text"
                content={debouncedText.trim()}
                shortcut={{ modifiers: ["cmd"], key: "i" }}
              />
              <Action
                title="Speak"
                icon={Icon.SpeakerHigh}
                onAction={async () => {
                  speak(debouncedText.trim(), to);
                  await showHUD("Speaking…");
                }}
              />
              <ActionPanel.Submenu
                title="Engine"
                icon={Icon.Gear}
                shortcut={{ modifiers: ["cmd"], key: "e" }}
              >
                {ENGINES.map((e) => (
                  <Action
                    key={e.value || "auto"}
                    title={e.title}
                    icon={engine === e.value ? Icon.CheckCircle : Icon.Circle}
                    onAction={() => setEngine(e.value)}
                  />
                ))}
              </ActionPanel.Submenu>
            </ActionPanel>
          }
        />
      ) : (
        <List.EmptyView
          icon={Icon.Text}
          title="No translation"
          description="No result for this input — try rephrasing or another engine (⌘E)."
        />
      )}
    </List>
  );
}

function renderMarkdown(r: TranslateResult): string {
  // Plain paragraph (not an H1) so long translations read comfortably.
  const lines = [r.translation, ""];
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
