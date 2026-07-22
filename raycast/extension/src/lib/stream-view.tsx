import { useEffect, useRef, useState } from "react";
import { ActionPanel, Action, Detail } from "@raycast/api";
import { spawnTranslateStream } from "./translate";

/** A pushed Detail that streams `translate … --stream` output token-by-token.
 *  Opt-in from the Translate view (⌘↵) — useful for long text and streaming
 *  engines; on a buffering provider it simply appears after first-token latency. */
export function StreamView({
  text,
  to,
  engine,
}: {
  text: string;
  to: string;
  engine?: string;
}) {
  const [md, setMd] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const acc = useRef("");

  useEffect(() => {
    acc.current = "";
    setMd("");
    setIsLoading(true);
    const cancel = spawnTranslateStream(
      text,
      { to, engine: engine || undefined },
      {
        onData: (chunk) => {
          acc.current += chunk;
          setMd(acc.current);
        },
        onDone: () => setIsLoading(false),
        onError: (err) => {
          acc.current += `\n\n> error: ${err.message}`;
          setMd(acc.current);
          setIsLoading(false);
        },
      },
    );
    return cancel;
  }, [text, to, engine]);

  return (
    <Detail
      isLoading={isLoading}
      markdown={md || "…"}
      navigationTitle={`Translate → ${to}`}
      actions={
        <ActionPanel>
          <Action.CopyToClipboard title="Copy Translation" content={md} />
          <Action.Paste title="Paste Translation" content={md} />
        </ActionPanel>
      }
    />
  );
}
