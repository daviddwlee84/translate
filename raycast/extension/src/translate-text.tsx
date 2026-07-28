import { useEffect, useState } from "react";
import {
  ActionPanel,
  Action,
  Clipboard,
  Form,
  Icon,
  LaunchProps,
  getPreferenceValues,
  getSelectedText,
  showToast,
  Toast,
  useNavigation,
} from "@raycast/api";
import { readConfig, runModels, AUTO_TARGET, ModelInfo } from "./lib/translate";
import { autoLabel, usePair, useLanguages } from "./lib/language-dropdown";
import { TranslationDetail } from "./lib/translation-detail";
import { StreamView } from "./lib/stream-view";
import { SHORTCUTS, label } from "./lib/shortcuts";

const ENGINES = [
  { title: "Auto (fallback chain)", value: "" },
  { title: "Google", value: "google" },
  { title: "Dictionary (offline)", value: "dict" },
  { title: "Copilot", value: "copilot" },
  { title: "Ollama", value: "ollama" },
];

/** "Use the engine's tier default" — no --model flag. */
const MODEL_DEFAULT = "";

/**
 * Translate a long passage.
 *
 * The other commands take their input in the search bar, which Raycast caps —
 * pasting a long passage there is refused with "The text you are trying to paste
 * is too long". That guard lives in the Raycast app, not in the extension API,
 * so it can't be raised or caught; the only way around it is to not use the
 * search bar. A Form.TextArea takes as much text as you like, and the shell-out
 * layer switches to stdin past 128 KB so ARG_MAX isn't the next wall.
 *
 * Nothing is prefilled automatically — loading the selection or the clipboard is
 * an explicit action, so opening the command is never surprising.
 */
export default function Command(
  props: LaunchProps<{ arguments: Arguments.TranslateText }>,
) {
  const prefs = getPreferenceValues<Preferences.TranslateText>();
  const { push } = useNavigation();
  const langs = useLanguages();
  const pair = usePair();
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [showAdvanced, setShowAdvanced] = useState(false);

  const [text, setText] = useState("");
  // Explicitly <string>: the manifest dropdown's union is only the ten static
  // options, but this also holds AUTO_TARGET and codes from runLangs().
  const [to, setTo] = useState<string>(
    props.arguments?.to || prefs.defaultTarget || "",
  );
  const [engine, setEngine] = useState(prefs.engine ?? "");
  const [model, setModel] = useState(MODEL_DEFAULT);

  useEffect(() => {
    runModels()
      .then(setModels)
      .catch(() => setModels([]));
  }, []);

  // Same inheritance as the other commands: argument → Raycast preference →
  // the CLI config's default_target → "en".
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

  const submit = (streaming: boolean) => {
    const trimmed = text.trim();
    if (!trimmed) {
      showToast({ style: Toast.Style.Failure, title: "Nothing to translate" });
      return;
    }
    // Picking a concrete language means "translate into this", so pair routing
    // is turned off explicitly — otherwise [general] pair would reroute
    // home-language input and the chosen target would be quietly ignored.
    //
    // A picked model also implies its provider: "llama3.2:3b" belongs to ollama,
    // not to whatever the chain would have reached first.
    const picked = models.find((m) => m.model === model);
    const opts = {
      to,
      pair: to === AUTO_TARGET ? undefined : false,
      engine: engine || picked?.provider || undefined,
      model: model || undefined,
    };
    push(
      streaming ? (
        <StreamView {...opts} text={trimmed} />
      ) : (
        <TranslationDetail {...opts} text={trimmed} />
      ),
    );
  };

  const load = async (from: "selection" | "clipboard") => {
    try {
      const got =
        from === "selection"
          ? await getSelectedText()
          : ((await Clipboard.readText()) ?? "");
      if (!got.trim()) {
        showToast({
          style: Toast.Style.Failure,
          title: `Nothing in the ${from}`,
        });
        return;
      }
      setText(got);
    } catch {
      showToast({
        style: Toast.Style.Failure,
        title: `Couldn't read the ${from}`,
        message:
          from === "selection"
            ? "Grant Raycast Accessibility permission, or copy the text instead."
            : undefined,
      });
    }
  };

  return (
    <Form
      actions={
        <ActionPanel>
          <Action
            title="Translate"
            icon={Icon.Text}
            onAction={() => submit(false)}
          />
          <Action
            title="Translate (streaming)"
            icon={Icon.Bolt}
            shortcut={SHORTCUTS.streamTranslate}
            onAction={() => submit(true)}
          />
          <Action
            title="Load Selection"
            icon={Icon.TextCursor}
            shortcut={SHORTCUTS.loadSelection}
            onAction={() => load("selection")}
          />
          <Action
            title="Load Clipboard"
            icon={Icon.Clipboard}
            shortcut={SHORTCUTS.loadClipboard}
            onAction={() => load("clipboard")}
          />
          <Action
            title="Clear"
            icon={Icon.Trash}
            shortcut={SHORTCUTS.clear}
            onAction={() => setText("")}
          />
          <Action
            title={showAdvanced ? "Hide Advanced" : "Show Advanced"}
            icon={Icon.Cog}
            shortcut={SHORTCUTS.toggleAdvanced}
            onAction={() => setShowAdvanced((v) => !v)}
          />
        </ActionPanel>
      }
    >
      <Form.TextArea
        id="text"
        title="Text"
        placeholder="Paste or type as much as you like — this box has no search-bar limit."
        value={text}
        onChange={setText}
      />
      <Form.Description
        title=""
        text={
          text
            ? `${text.length.toLocaleString()} characters`
            : `${label("loadSelection")} loads the selection · ${label("loadClipboard")} loads the clipboard · ${label("toggleAdvanced")} advanced`
        }
      />
      <Form.Separator />
      <Form.Dropdown
        id="to"
        title="Target"
        value={to}
        onChange={setTo}
        storeValue={false}
        info={
          pair
            ? `Auto follows the configured pair (${pair.home} ⇄ ${pair.away}), like ^g in the TUI. Picking a language always translates into it.`
            : "Picking a language always translates into it."
        }
      >
        <Form.Dropdown.Item value={AUTO_TARGET} title={autoLabel(pair)} />
        {langs.map((l) => (
          <Form.Dropdown.Item
            key={l.code}
            value={l.code}
            title={`${l.name} (${l.code})`}
          />
        ))}
      </Form.Dropdown>
      {showAdvanced ? (
        <>
          <Form.Dropdown
            id="engine"
            title="Engine"
            value={engine}
            onChange={setEngine}
          >
            {ENGINES.map((e) => (
              <Form.Dropdown.Item
                key={e.value}
                value={e.value}
                title={e.title}
              />
            ))}
          </Form.Dropdown>
          {models.length === 0 ? (
            // An empty list almost always means the *installed* CLI predates
            // `translate models`, not that no providers are configured — the
            // extension resolves a binary from ~/.local/bin, which is easy to
            // leave behind while iterating on the repo.
            <Form.Description
              title="Model"
              text="No models listed — the installed translate CLI is probably older than `translate models`. Run `just install` (or brew upgrade) and reopen."
            />
          ) : (
            <Form.Dropdown
              id="model"
              title="Model"
              value={model}
              onChange={setModel}
              info="Models declared by the configured providers. Leave on the default to let the engine and tier decide."
            >
              <Form.Dropdown.Item
                value={MODEL_DEFAULT}
                title="Default (engine + tier)"
              />
              {models.map((m) => (
                <Form.Dropdown.Item
                  key={`${m.provider}:${m.model}`}
                  value={m.model}
                  title={`${m.model} — ${m.provider} ${m.tier}${m.default ? " ★" : ""}`}
                />
              ))}
            </Form.Dropdown>
          )}
        </>
      ) : null}
    </Form>
  );
}
