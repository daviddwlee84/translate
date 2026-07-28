/**
 * Markdown repair for model output rendered in a Raycast `Detail`.
 *
 * THE PROBLEM: a translated table arrives looking like a terminal rendering —
 * `─` rules, pipe rows padded with spaces so the columns line up. Raycast's
 * `Detail` renders markdown in a PROPORTIONAL font, so the rules reflow as
 * prose and the pipe rows never become a table (GFM needs a `|---|---|`
 * separator directly under the header, and every row to agree on cell count).
 *
 * THE RULE BEHIND THE FIX: character-level alignment cannot be portable. A CJK
 * glyph occupies two cells in a terminal and an arbitrary fraction of one in a
 * proportional font, so padding that lines up in a terminal is guaranteed not to
 * line up here. Only STRUCTURE — the pipes and the separator row — survives the
 * trip. So this module throws the padding away and rebuilds the structure.
 *
 * The CLI is asked to emit that structure directly (internal/engine/prompt.go),
 * but this pass still has to exist: the extension talks to whatever `translate`
 * the user has installed, which may predate that prompt by any amount of time.
 *
 * Deliberately free of `@raycast/api` imports so dev-check.ts can assert it.
 */

/** A line is "pipe-ish" if it could be a table row. */
function isPipeRow(line: string): boolean {
  const t = line.trim();
  return t.includes("|") && (t.match(/\|/g)?.length ?? 0) >= 2;
}

/** A GFM separator row: only pipes, dashes, colons and spaces. */
function isSeparatorRow(line: string): boolean {
  const t = line.trim();
  return /^\|?[\s:|-]+\|[\s:|-]*$/.test(t) && t.includes("-");
}

/**
 * A bare horizontal rule made of box-drawing or dash runs — what a terminal
 * table uses for its borders. Not valid GFM, and it reflows as prose.
 */
function isBoxRule(line: string): boolean {
  const t = line.trim();
  return t.length > 0 && /^[─━—–\-=+_|\s]+$/.test(t) && !isPipeRow(t);
}

function cells(line: string): string[] {
  let t = line.trim();
  if (t.startsWith("|")) t = t.slice(1);
  if (t.endsWith("|")) t = t.slice(0, -1);
  return t.split("|").map((c) => c.trim());
}

function row(values: string[]): string {
  return `| ${values.join(" | ")} |`;
}

/**
 * Rebuild every table-like run into a well-formed GFM table.
 *
 * Rows are normalised to the header's cell count (padded with empties, extras
 * joined back into the last cell so nothing is silently dropped), a separator is
 * inserted when missing, and terminal box rules are discarded.
 */
export function normalizeTables(md: string): string {
  const lines = md.split("\n");
  const out: string[] = [];
  let i = 0;

  while (i < lines.length) {
    // Leave fenced blocks completely alone — they are already monospace, and
    // their content is not ours to reinterpret.
    if (lines[i].trim().startsWith("```")) {
      out.push(lines[i++]);
      while (i < lines.length && !lines[i].trim().startsWith("```"))
        out.push(lines[i++]);
      if (i < lines.length) out.push(lines[i++]);
      continue;
    }

    // A box rule directly above a table is that table's top border. Hold it
    // back: drop it if a pipe row follows, otherwise it was ordinary content
    // (a markdown `---` divider, say) and has to be emitted unchanged.
    if (isBoxRule(lines[i])) {
      const held: string[] = [];
      while (i < lines.length && isBoxRule(lines[i])) held.push(lines[i++]);
      if (!(i < lines.length && isPipeRow(lines[i]))) out.push(...held);
      continue;
    }

    if (!isPipeRow(lines[i])) {
      out.push(lines[i++]);
      continue;
    }

    // Collect the run: pipe rows, plus box rules sitting between them.
    const run: string[] = [];
    while (i < lines.length && (isPipeRow(lines[i]) || isBoxRule(lines[i]))) {
      if (isPipeRow(lines[i])) run.push(lines[i]);
      i++;
    }

    const rows = run.filter((r) => !isSeparatorRow(r)).map(cells);
    if (rows.length < 2) {
      // One row is not a table; emit it unchanged rather than inventing a header.
      out.push(...run);
      continue;
    }

    const width = rows[0].length;
    const fit = (r: string[]) =>
      r.length === width
        ? r
        : r.length < width
          ? [...r, ...Array(width - r.length).fill("")]
          : [...r.slice(0, width - 1), r.slice(width - 1).join(" ")];

    out.push(row(fit(rows[0])));
    out.push(row(Array(width).fill("---")));
    for (const r of rows.slice(1)) out.push(row(fit(r)));
  }

  return out.join("\n");
}

/**
 * Fence a run of column-aligned lines that contains NO pipes — ASCII art, a
 * `--help` screen, aligned key/value output. There is no structure to recover,
 * so the best available outcome is monospace.
 *
 * Backticks inside the content are neutralised with a zero-width space so the
 * output cannot close the fence early.
 */
export function fencePreformatted(md: string): string {
  const lines = md.split("\n");
  const out: string[] = [];
  let i = 0;
  const aligned = (l: string) => /\S {2,}\S/.test(l) && !isPipeRow(l);

  while (i < lines.length) {
    if (lines[i].trim().startsWith("```")) {
      out.push(lines[i++]);
      while (i < lines.length && !lines[i].trim().startsWith("```"))
        out.push(lines[i++]);
      if (i < lines.length) out.push(lines[i++]);
      continue;
    }
    if (!aligned(lines[i])) {
      out.push(lines[i++]);
      continue;
    }
    const block: string[] = [];
    while (i < lines.length && aligned(lines[i])) block.push(lines[i++]);
    // Two lines is a coincidence; three is a layout.
    if (block.length < 3) {
      out.push(...block);
      continue;
    }
    out.push("```text", ...block.map((l) => l.replace(/`/g, "`​")), "```");
  }
  return out.join("\n");
}

/**
 * The full repair pass applied to model output before it is handed to `Detail`.
 * Tables first: once a run is a proper GFM table it is no longer "aligned text"
 * for the fencing pass to swallow.
 */
export function renderModelOutput(md: string): string {
  return fencePreformatted(normalizeTables(md));
}

/** True when the repair pass would change anything — drives the raw/formatted toggle. */
export function looksTabular(md: string): boolean {
  return renderModelOutput(md) !== md;
}
