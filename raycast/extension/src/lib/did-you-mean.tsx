import { ActionPanel, Action, Icon, List } from "@raycast/api";

/**
 * A Google-style "did you mean?" section built from the engine's fuzzy dictionary
 * suggestions — returned (in `TranslateResult.suggestions`) when a single-word
 * lookup/translation has no exact match, e.g. "cononical" → ["canonical", …].
 * Picking one re-runs the search with the corrected term. Full sentences skip this
 * (they go to the LLM, which corrects typos implicitly).
 */
export function DidYouMean({
  suggestions,
  onPick,
}: {
  suggestions: string[];
  onPick: (s: string) => void;
}) {
  const [best, ...rest] = suggestions;
  return (
    <List.Section title="Did you mean?">
      {[best, ...rest].map((s, i) => (
        <List.Item
          key={s}
          title={s}
          icon={i === 0 ? Icon.Checkmark : Icon.MagnifyingGlass}
          accessories={i === 0 ? [{ text: "closest" }] : undefined}
          actions={
            <ActionPanel>
              <Action
                title="Use This"
                icon={Icon.ArrowRight}
                onAction={() => onPick(s)}
              />
            </ActionPanel>
          }
        />
      ))}
    </List.Section>
  );
}
