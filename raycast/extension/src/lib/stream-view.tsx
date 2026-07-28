import { useEffect, useRef, useState } from "react";
import { ActionPanel, Action, Detail } from "@raycast/api";
import { spawnTranslateStream } from "./translate";
import { renderModelOutput } from "./markdown";

/** A pushed Detail that streams `translate … --stream` output token-by-token.
 *  Opt-in from the Translate view (⌘↵) — useful for long text and streaming
 *  engines; on a buffering provider it simply appears after first-token latency. */
export function StreamView({
  text,
  to,
  engine,
  model,
  pair,
}: {
  text: string;
  to?: string;
  engine?: string;
  model?: string;
  pair?: boolean;
}) {
  const [md, setMd] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const acc = useRef("");
  // Repair runs ONCE, on completion. A half-arrived table has no full header row
  // to normalise the other rows against, and re-repairing every chunk would make
  // the view flicker between shapes as columns appear.
  const [done, setDone] = useState(false);

  useEffect(() => {
    acc.current = "";
    setMd("");
    setIsLoading(true);
    setDone(false);
    const cancel = spawnTranslateStream(
      text,
      { to, engine: engine || undefined, model, pair },
      {
        onData: (chunk) => {
          acc.current += chunk;
          setMd(acc.current);
        },
        onDone: () => {
          setIsLoading(false);
          setDone(true);
        },
        onError: (err) => {
          acc.current += `\n\n> error: ${err.message}`;
          setMd(acc.current);
          setIsLoading(false);
        },
      },
    );
    return cancel;
  }, [text, to, engine, model, pair]);

  return (
    <Detail
      isLoading={isLoading}
      markdown={(done ? renderModelOutput(md) : md) || "…"}
      navigationTitle={to ? `Translate → ${to}` : "Translate"}
      actions={
        <ActionPanel>
          <Action.CopyToClipboard title="Copy Translation" content={md} />
          <Action.Paste title="Paste Translation" content={md} />
        </ActionPanel>
      }
    />
  );
}
