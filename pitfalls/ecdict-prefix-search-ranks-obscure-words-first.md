# ECDICT prefix search returns obscure words first, or takes ~400 ms per keystroke

**Symptoms** (grep this section): typing `test` in a dictionary picker suggests `testbed`, `testamentary`, `testiness` instead of `testing`/`tested`; the most common word never appears near the top; `ORDER BY frq DESC` produces a plausible-looking but useless ranking; a prefix query over `ecdict.db` takes 300–500 ms; `EXPLAIN QUERY PLAN` shows `SCAN entries` where you expected `SEARCH entries USING INDEX idx_word_lc`; `LIKE 'x%'` ignores an index that clearly covers the column.
**First seen**: 2026-07-26
**Affects**: any query against the locally-built ECDICT SQLite (`~/.local/share/translate/dict/ecdict.db`, ~770k rows, built by `translate dict update ecdict`).

## Symptom

Two independent traps, both of which produce *working* code that is quietly wrong.

**1. Ranking is inverted.** The obvious reading of a column named `frq` is "how
often the word occurs", so you sort descending. The result looks like a real
ranking, which is why it survives review:

```console
$ sqlite3 ecdict.db "select word,frq from entries where word_lc>='test' and word_lc<'tesu' order by frq desc limit 5;"
test|575
testbed|45406
testamentary|44575
testiness|44119
testis|38446
```

**2. The prefix query scans the whole table.**

```console
$ sqlite3 ecdict.db "explain query plan select word from entries where word_lc like 'test%';"
QUERY PLAN
`--SCAN entries

$ time sqlite3 ecdict.db "select count(*) from entries where word_lc like 'test%';"
402
sqlite3   0.11s user 0.06s system 37% cpu 0.435 total
```

435 ms for 402 rows out of 770,611.

## Root cause

**1. `frq` is a frequency *rank*, not a count.** 1 is the most common word; a
larger number is rarer; **0 means unknown/unranked** (only 42,231 of 770,611 rows
carry a rank at all).

```console
$ sqlite3 ecdict.db "select word,frq from entries where word_lc in ('the','and','test','testosterone','zymurgy') order by frq;"
zymurgy|0
the|1
and|3
test|575
testosterone|10400
```

So the correct order is **ascending**, with 0 pushed to the *end* rather than the
front — `ORDER BY (frq = 0) ASC, frq ASC`. A plain `frq ASC` is also wrong: every
unranked word would sort ahead of `the`.

**2. SQLite's LIKE optimization does not apply here.** SQLite can only turn
`col LIKE 'prefix%'` into an index range scan when the column has a `COLLATE NOCASE`
index (or `PRAGMA case_sensitive_like=ON`). `idx_word_lc` is a plain BINARY index
on `word_lc`, so `LIKE` falls back to evaluating the pattern on every row.

## Workaround

Use an explicit **range** predicate and the full ranking clause
(`internal/engine/ecdict.go`, `ecdictDB.prefixSearch`):

```sql
SELECT word, phonetic, substr(translation,1,240), substr(definition,1,240), pos, frq
  FROM entries
 WHERE word_lc >= :q AND word_lc < :qUpper      -- range, NOT LIKE
 ORDER BY (word_lc = :q) DESC,                  -- exact headword first
          (instr(word_lc,' ') > 0) ASC,         -- single words before phrases
          (frq = 0) ASC,                        -- ranked words before unranked
          frq ASC,                              -- lower rank = more common
          length(word_lc) ASC,
          word_lc ASC
 LIMIT :n;
```

`:qUpper` is `q + "\U0010FFFF"` (`engine.prefixUpper`): every string starting with
`q` sorts below it under BINARY collation, and nothing else does.

```console
$ sqlite3 ecdict.db "explain query plan select word from entries where word_lc >= 'test' and word_lc < 'test' || char(1114111);"
QUERY PLAN
`--SEARCH entries USING INDEX idx_word_lc (word_lc>? AND word_lc<?)
```

Measured after the fix: `tes` 4 ms · `te` 20 ms · `a` (worst case, ~60k rows in
range) 33 ms warm / 275 ms with a cold page cache. Ranked output for `test`:
`test, testing, testimony, testify, tester, testament, testosterone, …`.

## Prevention

- `internal/engine/dictsearch_test.go` (`TestEcdictPrefixSearchRanking`) fixes the
  expected order against a fixture containing both `frq = 0` rows and rows whose
  rank order is the opposite of their alphabetical order, so a `DESC` flip fails
  loudly.
- The comment on `prefixSearch` states both traps at the query site.
- `bnc` (the other ECDICT frequency column) is **not imported at all** —
  `internal/engine/dictdata.go` reads only `rec[9]` (`frq`). Don't plan a ranking
  around a column that isn't there.
- Inflected forms (`tests`, `testifying`) have `frq = 0` and therefore sort late.
  Their lemma is in the `exchange` column (`test` → `s:tests/d:tested/i:testing`),
  which is the raw material for a future lemma-frequency boost — tracked in
  `TODO.md`.
