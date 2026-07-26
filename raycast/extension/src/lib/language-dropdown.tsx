import { useEffect, useState } from "react";
import { List } from "@raycast/api";
import {
  AUTO_TARGET,
  LANGS,
  LangInfo,
  readConfig,
  runLangs,
} from "./translate";

/**
 * The CLI's full language table, fetched once per command run. Starts from the
 * static LANGS subset so the dropdown is never empty on first paint, then swaps
 * in all 35 entries from `translate lang list --json`.
 */
export function useLanguages(): LangInfo[] {
  const [langs, setLangs] = useState<LangInfo[]>(() =>
    LANGS.map((l) => ({ code: l.value, name: l.title })),
  );
  useEffect(() => {
    runLangs()
      .then(setLangs)
      .catch(() => {
        /* keep the static list */
      });
  }, []);
  return langs;
}

/** The configured pair, or null when [general] pair_with isn't set. */
export interface PairInfo {
  home: string;
  away: string;
  /** Whether [general] pair is on, i.e. whether this is already the default. */
  enabled: boolean;
}

/**
 * Reads the configured bidirectional pair, so a target picker can offer "Auto"
 * with real language codes in its label instead of a vague word.
 */
export function usePair(): PairInfo | null {
  const [pair, setPair] = useState<PairInfo | null>(null);
  useEffect(() => {
    readConfig()
      .then((cfg) => {
        const g = cfg.general;
        if (g?.pair_with && g.default_target) {
          setPair({
            home: g.default_target,
            away: g.pair_with,
            enabled: !!g.pair,
          });
        }
      })
      .catch(() => {
        /* no pair offered */
      });
  }, []);
  return pair;
}

/** The label for the Auto entry, e.g. "Auto — zh-TW ⇄ en". */
export function autoLabel(pair: PairInfo | null): string {
  return pair
    ? `Auto — ${pair.home} ⇄ ${pair.away}`
    : "Auto (detect direction)";
}

/**
 * Target-language picker with an "Auto" entry — the dropdown equivalent of the
 * TUI's ^g.
 *
 * Auto is not cosmetic. With [general] pair = true the CLI already reroutes
 * home-language input to pair_with, so picking "Chinese" and pasting Chinese
 * gives you English. Making Auto explicit means picking a language can mean what
 * it says: a concrete choice sends --no-pair and always translates *into* it.
 */
export function LanguageDropdown(props: {
  value: string;
  onChange: (code: string) => void;
  /** Show the Auto entry (target pickers) or not (definition-language pickers). */
  auto?: PairInfo | null;
}) {
  const langs = useLanguages();
  // A value that isn't in the list would silently reset the dropdown, so keep
  // whatever the config resolved to (e.g. a code the CLI knows and we don't).
  const known =
    props.value === AUTO_TARGET || langs.some((l) => l.code === props.value);
  return (
    <List.Dropdown
      tooltip="Target language — Auto follows the configured pair, like ^g in the TUI"
      value={props.value}
      onChange={props.onChange}
    >
      {props.auto !== undefined ? (
        <List.Dropdown.Item
          title={autoLabel(props.auto)}
          value={AUTO_TARGET}
          keywords={["auto", "pair", "bidirectional"]}
        />
      ) : null}
      {!known && props.value ? (
        <List.Dropdown.Item title={props.value} value={props.value} />
      ) : null}
      {langs.map((l) => (
        <List.Dropdown.Item
          key={l.code}
          title={`${l.name} (${l.code})`}
          value={l.code}
        />
      ))}
    </List.Dropdown>
  );
}
