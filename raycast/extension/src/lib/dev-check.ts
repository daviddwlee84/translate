/**
 * The verification harness.
 *
 * A Raycast extension has no test runner, and adding one is store-review noise —
 * so the pure modules are asserted here instead, compiled by the already-present
 * tsc and run under node. The entire precondition is import discipline: nothing
 * reachable from this file may import `@raycast/api` at RUNTIME (type-only
 * imports are fine, since they are erased). That is why shortcuts.ts uses
 * `import type` and why markdown.ts imports nothing at all.
 *
 *   npx tsc --outDir .build/verify --module commonjs --target ES2022 \
 *     --lib ES2023 --esModuleInterop --strict src/lib/dev-check.ts
 *   node .build/verify/dev-check.js
 */
import { SHORTCUTS, formatBranch } from "./shortcuts";
import {
  normalizeTables,
  fencePreformatted,
  renderModelOutput,
  looksTabular,
} from "./markdown";

let failures = 0;

function check(name: string, actual: unknown, expected: unknown) {
  const a = JSON.stringify(actual);
  const e = JSON.stringify(expected);
  if (a === e) {
    console.log(`  ok   ${name}`);
    return;
  }
  failures++;
  console.log(`  FAIL ${name}\n       expected ${e}\n       actual   ${a}`);
}

function checkTrue(name: string, actual: boolean) {
  check(name, actual, true);
}

console.log("shortcuts");
{
  // Every entry must carry BOTH branches. A one-branch shortcut typechecks and
  // is then dropped in silence on the platform it omits.
  for (const [name, s] of Object.entries(SHORTCUTS)) {
    checkTrue(
      `${name} has both branches`,
      Boolean(s.macOS?.key) && Boolean(s.Windows?.key),
    );
    checkTrue(
      `${name} is cmd on macOS and ctrl on Windows`,
      s.macOS.modifiers.includes("cmd") && s.Windows.modifiers.includes("ctrl"),
    );
    checkTrue(
      `${name} does not hardcode cmd on Windows`,
      !s.Windows.modifiers.includes("cmd"),
    );
  }
  check(
    "label ⇧⌘S on macOS",
    formatBranch(SHORTCUTS.loadSelection.macOS, false),
    "⇧⌘S",
  );
  check(
    "label Ctrl+Shift+S on Windows",
    formatBranch(SHORTCUTS.loadSelection.Windows, true),
    "Ctrl+Shift+S",
  );
  check(
    "label ⌘↵ on macOS",
    formatBranch(SHORTCUTS.streamTranslate.macOS, false),
    "⌘↵",
  );
  check(
    "label Ctrl+↵ on Windows",
    formatBranch(SHORTCUTS.streamTranslate.Windows, true),
    "Ctrl+↵",
  );
}

console.log("markdown: tables");
{
  // The reported failure: pipe rows with no separator, wrapped in box rules.
  const broken = [
    "────────────────────────",
    "| Model | V1 | V2 |",
    "────────────────────────",
    "| dense-95 | 0.1210 | 0.0679 |",
    "| dense-250 | 0.1208 | 0.0826 |",
  ].join("\n");
  check(
    "inserts a missing separator and drops box rules",
    normalizeTables(broken),
    [
      "| Model | V1 | V2 |",
      "| --- | --- | --- |",
      "| dense-95 | 0.1210 | 0.0679 |",
      "| dense-250 | 0.1208 | 0.0826 |",
    ].join("\n"),
  );

  check(
    "strips alignment padding",
    normalizeTables("| a   | b     |\n| --- | ----- |\n| 1   | 2     |"),
    "| a | b |\n| --- | --- |\n| 1 | 2 |",
  );

  check(
    "pads a short row instead of dropping it",
    normalizeTables("| a | b | c |\n| 1 | 2 |"),
    "| a | b | c |\n| --- | --- | --- |\n| 1 | 2 |  |",
  );

  check(
    "keeps an over-long row's overflow in the last cell",
    normalizeTables("| a | b |\n| 1 | 2 | 3 |"),
    "| a | b |\n| --- | --- |\n| 1 | 2 3 |",
  );

  check(
    "leaves a single pipe row alone rather than inventing a header",
    normalizeTables("a | b"),
    "a | b",
  );

  check(
    "leaves prose untouched",
    normalizeTables("Hello world.\n\nSecond paragraph."),
    "Hello world.\n\nSecond paragraph.",
  );

  check(
    "does not reinterpret fenced content",
    normalizeTables("```\n| a | b |\n| c | d |\n```"),
    "```\n| a | b |\n| c | d |\n```",
  );
}

console.log("markdown: preformatted");
{
  const aligned = "NAME    SIZE\nfoo     12\nbar     34";
  check(
    "fences a 3-line aligned block",
    fencePreformatted(aligned),
    "```text\nNAME    SIZE\nfoo     12\nbar     34\n```",
  );
  check(
    "leaves a 2-line coincidence alone",
    fencePreformatted("NAME    SIZE\nfoo     12"),
    "NAME    SIZE\nfoo     12",
  );
  checkTrue(
    "neutralises backticks so the fence cannot close early",
    fencePreformatted("a    `x`\nb    `y`\nc    `z`").includes("`​"),
  );
}

console.log("markdown: pipeline");
{
  checkTrue(
    "looksTabular is true for a broken table",
    looksTabular("| a | b |\n| 1 | 2 |"),
  );
  checkTrue(
    "looksTabular is false for prose",
    !looksTabular("Just a sentence."),
  );
  check(
    "a repaired table is not then swallowed by the fencing pass",
    renderModelOutput("| a | b |\n| 1 | 2 |"),
    "| a | b |\n| --- | --- |\n| 1 | 2 |",
  );
}

console.log(
  failures === 0 ? "\nall checks passed" : `\n${failures} check(s) FAILED`,
);
process.exit(failures === 0 ? 0 : 1);
