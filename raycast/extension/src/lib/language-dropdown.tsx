import { useEffect, useState } from "react";
import { List } from "@raycast/api";
import { LANGS, LangInfo, runLangs } from "./translate";

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

/**
 * Definition-language picker for the search bar.
 *
 * The offline dictionary tiers are script-fixed (CC-CEDICT defines Chinese in
 * English, ECDICT defines English in Chinese), so this only steers the LLM
 * fallback — the tooltip says so rather than letting the choice look broken on a
 * dictionary hit.
 */
export function LanguageDropdown(props: {
  value: string;
  onChange: (code: string) => void;
}) {
  const langs = useLanguages();
  // A value that isn't in the list would silently reset the dropdown, so keep
  // whatever the config resolved to (e.g. a code the CLI knows and we don't).
  const known = langs.some((l) => l.code === props.value);
  return (
    <List.Dropdown
      tooltip="Definition language — used for the LLM fallback; dictionary hits are zh↔en"
      value={props.value}
      onChange={props.onChange}
    >
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
