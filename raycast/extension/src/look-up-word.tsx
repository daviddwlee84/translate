import { useEffect, useRef, useState } from "react";
import {
  ActionPanel,
  Action,
  Icon,
  LaunchProps,
  List,
  getPreferenceValues,
  showHUD,
} from "@raycast/api";
import { usePromise } from "@raycast/utils";
import {
  Candidate,
  DictSearchResult,
  HistoryEntry,
  isBinaryMissing,
  readConfig,
  runDictSearch,
  runHistory,
  speakText,
} from "./lib/translate";
import { useDebouncedValue } from "./lib/hooks";
import { BinaryNotFound } from "./lib/binary-not-found";
import { DefineDetail } from "./lib/define-detail";
import { HistoryDetail, oneline } from "./lib/history-detail";
import { LanguageDropdown } from "./lib/language-dropdown";
import { SHORTCUTS } from "./lib/shortcuts";

/** How many candidates to ask the CLI for. */
const SEARCH_LIMIT = 12;
/** How many history rows to load for the empty state. */
const HISTORY_LIMIT = 200;
/** How many history rows to keep alongside search results. */
const HISTORY_WITH_RESULTS = 5;

/**
 * Below this the prefix scan is both slowest (a one-letter range covers ~60k
 * ECDICT rows) and least useful. One character keeps showing history instead.
 */
const MIN_QUERY = 2;

/**
 * Dictionary search is local (single-digit ms on an indexed dictionary), so the
 * debounce only has to absorb a fast typist, not pay for a network call. CJK
 * input gets a little longer because an IME emits intermediate candidates.
 */
const DEBOUNCE_MS = 180;
const DEBOUNCE_MS_CJK = 300;

const CJK =
  /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/u;

export default function Command(
  props: LaunchProps<{ arguments: Arguments.LookUpWord }>,
) {
  const prefs = getPreferenceValues<Preferences.LookUpWord>();
  const [query, setQuery] = useState("");
  // Command argument (picked in the Raycast root bar) → Raycast preference →
  // the CLI config's default_target → "en".
  // Explicitly <string>: the manifest dropdown's union is only the ten static
  // options, but this also holds codes that came from runLangs().
  const [to, setTo] = useState<string>(
    props.arguments?.to || prefs.defaultTarget || "",
  );

  useEffect(() => {
    if (to) return;
    readConfig()
      .then((cfg) => {
        if (cfg.general?.default_target) setTo(cfg.general.default_target);
      })
      .catch(() => {
        /* fall back below */
      })
      .finally(() => setTo((t) => t || "en"));
  }, []);

  const trimmed = query.trim();
  const debounced = useDebouncedValue(
    trimmed,
    CJK.test(trimmed) ? DEBOUNCE_MS_CJK : DEBOUNCE_MS,
  );
  const searchAbort = useRef<AbortController | undefined>(undefined);
  const historyAbort = useRef<AbortController | undefined>(undefined);

  const search = usePromise(
    async (q: string): Promise<DictSearchResult | undefined> => {
      if (q.length < MIN_QUERY) return undefined;
      return runDictSearch(q, SEARCH_LIMIT, searchAbort.current?.signal);
    },
    [debounced],
    { abortable: searchAbort },
  );

  const history = usePromise(
    async (): Promise<HistoryEntry[]> =>
      runHistory(undefined, HISTORY_LIMIT, historyAbort.current?.signal),
    [],
    { abortable: historyAbort },
  );

  const error = search.error ?? history.error;
  const pending = trimmed.length >= MIN_QUERY && trimmed !== debounced;
  const searching = trimmed.length >= MIN_QUERY;
  const candidates = searching ? (search.data?.candidates ?? []) : [];
  const entries = history.data ?? [];

  const dict = candidates.filter((c) => c.match !== "fuzzy");
  const fuzzy = candidates.filter((c) => c.match === "fuzzy");
  const recent = searching ? matchingHistory(entries, trimmed) : entries;

  // Opening a word writes a history row; refresh so it is at the top when the
  // pushed Detail is popped. Raycast keeps this list mounted underneath, so the
  // revalidate lands while the Detail is still open.
  const onRecorded = () => history.revalidate();

  const rowActions = (c: Candidate) => (
    <ActionPanel>
      <Action.Push
        title="Show Definition"
        icon={Icon.Book}
        target={<DefineDetail word={c.word} to={to} onRecorded={onRecorded} />}
      />
      <Action
        title="Use This Word"
        icon={Icon.MagnifyingGlass}
        shortcut={SHORTCUTS.useThisWord}
        onAction={() => setQuery(c.word)}
      />
      <Action.CopyToClipboard
        title="Copy Word"
        content={c.word}
        shortcut={SHORTCUTS.copySource}
      />
      <Action
        title="Speak"
        icon={Icon.SpeakerHigh}
        onAction={async () => {
          speakText(c.word);
          await showHUD("Speaking…");
        }}
      />
    </ActionPanel>
  );

  if (error && isBinaryMissing(error)) {
    return (
      <List>
        <BinaryNotFound />
      </List>
    );
  }

  return (
    <List
      isLoading={search.isLoading || history.isLoading || pending}
      searchText={query}
      onSearchTextChange={setQuery}
      searchBarPlaceholder="Look up a word…"
      searchBarAccessory={<LanguageDropdown value={to} onChange={setTo} />}
    >
      {error ? (
        <List.EmptyView
          icon={Icon.Warning}
          title="Lookup failed"
          description={String(error.message ?? error)}
        />
      ) : null}

      {dict.length > 0 ? (
        <List.Section title="Dictionary">
          {dict.map((c) => (
            <List.Item
              key={`d:${c.word}`}
              icon={Icon.Book}
              title={c.word}
              subtitle={c.preview}
              accessories={c.phonetic ? [{ text: c.phonetic }] : undefined}
              actions={rowActions(c)}
            />
          ))}
        </List.Section>
      ) : null}

      {fuzzy.length > 0 ? (
        <List.Section title="Did you mean?">
          {fuzzy.map((c) => (
            <List.Item
              key={`f:${c.word}`}
              icon={Icon.QuestionMark}
              title={c.word}
              subtitle={c.preview}
              accessories={[{ text: `~${c.distance ?? "?"}` }]}
              actions={rowActions(c)}
            />
          ))}
        </List.Section>
      ) : null}

      {/* No local headword matched — offer the LLM explicitly rather than firing
          a multi-second call on every keystroke. */}
      {searching && !search.isLoading && !pending && candidates.length === 0 ? (
        <List.Section title="No dictionary match">
          <List.Item
            icon={Icon.Stars}
            title={`Ask the LLM for “${trimmed}”`}
            subtitle="Define with the configured provider"
            actions={
              <ActionPanel>
                <Action.Push
                  title="Show Definition"
                  icon={Icon.Stars}
                  target={
                    <DefineDetail
                      word={trimmed}
                      to={to}
                      onRecorded={onRecorded}
                    />
                  }
                />
              </ActionPanel>
            }
          />
        </List.Section>
      ) : null}

      {search.data?.notes ? (
        <List.Section title="Dictionary data">
          <List.Item
            icon={Icon.Info}
            title={search.data.notes}
            actions={
              <ActionPanel>
                <Action.CopyToClipboard
                  title="Copy Suggested Command"
                  content={commandFromNotes(search.data.notes)}
                />
              </ActionPanel>
            }
          />
        </List.Section>
      ) : null}

      {recent.length > 0 ? (
        <List.Section title={searching ? "Recent" : "Recent Lookups"}>
          {recent.map((h) => (
            <List.Item
              key={h.id}
              icon={Icon.Clock}
              title={oneline(h.input, 60)}
              subtitle={oneline(h.output, 80)}
              accessories={[{ tag: h.target_lang }]}
              actions={
                <ActionPanel>
                  <Action.Push
                    title="Show Entry"
                    icon={Icon.Document}
                    target={<HistoryDetail entry={h} to={to} />}
                  />
                  <Action.Push
                    title="Look up Again"
                    icon={Icon.Book}
                    shortcut={SHORTCUTS.lookUpAgain}
                    target={
                      <DefineDetail
                        word={h.input}
                        to={to}
                        onRecorded={onRecorded}
                      />
                    }
                  />
                  <Action.CopyToClipboard
                    title="Copy Output"
                    content={h.output}
                  />
                  <Action
                    title="Reload History"
                    icon={Icon.ArrowClockwise}
                    shortcut={SHORTCUTS.reload}
                    onAction={onRecorded}
                  />
                </ActionPanel>
              }
            />
          ))}
        </List.Section>
      ) : null}

      {!searching && recent.length === 0 && !history.isLoading ? (
        <List.EmptyView
          icon={Icon.Book}
          title="Look Up Word"
          description="Type a word to search the dictionary. Words you open show up here."
        />
      ) : null}
    </List>
  );
}

/**
 * History rows worth showing next to live results: the ones whose input or output
 * contains what was typed. Already in memory, so this costs nothing.
 */
function matchingHistory(entries: HistoryEntry[], q: string): HistoryEntry[] {
  const needle = q.toLowerCase();
  return entries
    .filter(
      (h) =>
        h.input.toLowerCase().includes(needle) ||
        h.output.toLowerCase().includes(needle),
    )
    .slice(0, HISTORY_WITH_RESULTS);
}

/** Pull the `translate …` command out of a notes hint, for the copy action. */
function commandFromNotes(notes: string): string {
  const m = notes.match(/`([^`]+)`/);
  return m ? m[1] : notes;
}
