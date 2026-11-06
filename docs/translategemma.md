# TranslateGemma

[TranslateGemma](https://blog.google/innovation-and-ai/technology/developers-tools/translategemma/)
is Google's translation-only model family, fine-tuned from Gemma 3. `translate`
runs it locally through [Ollama](https://ollama.com/library/translategemma), so
your text stays on your machine.

Use it for offline translation of short text. Stay with a general LLM provider
for documents, `--learn`, custom instructions, or text that must reliably come
out in Traditional Chinese.

> [`google-gemma/gemma-translator`](https://github.com/google-gemma/gemma-translator)
> is a different project: a voice translator built on Gemma 4 and LiteRT-LM.
> `translate` does not use it.

## Quick start

1. Download a model. `translate init` suggests a size for your hardware.

   ```sh
   ollama pull translategemma:4b
   ```

2. Try it:

   ```sh
   translate --engine ollama --model translategemma:4b --to ja "Hello, how are you?"
   ```

3. To make it the default for the command line, add this to your config
   (`translate config path` shows where it is):

   ```toml
   [cli]
   engine = "ollama"
   model = "translategemma:4b"
   ```

Use `--engine ollama`, not `--provider ollama`. With the default `engine = "auto"`,
`--provider` still tries the rest of `chain.order` first, so your text goes to
Copilot if that is running.

## Choosing a size

| Tag | Download | Intended for (Google) | MetricX, lower is better | COMET, higher is better |
|---|---|---|---|---|
| `translategemma:4b` (`latest`) | 3.3 GB | Mobile and edge devices | 5.32 | 81.6 |
| `translategemma:12b` | 8.1 GB | Consumer laptops | 3.60 | 83.5 |
| `translategemma:27b` | 17 GB | A single high-end GPU or cloud TPU | 3.09 | 84.4 |

Scores are Google's [model card](https://huggingface.co/google/translategemma-4b-it)
results on WMT24++ across 55 languages.

`translate init` prints a suggestion when Ollama is reachable or selected. It never
downloads a model or changes your config. It uses these thresholds:

| Hardware | Suggestion |
|---|---|
| NVIDIA GPU with 20 GB or more | `27b` |
| NVIDIA GPU with 10 GB or more | `12b` |
| NVIDIA GPU with less than 10 GB | `4b` |
| Apple Silicon with 32 GB or more | `27b` |
| Apple Silicon with 16 GB or more | `12b` |
| Apple Silicon with less than 16 GB | `4b` |
| No GPU detected | `4b` |

These thresholds are this project's own conservative estimate, not a Google
specification. AMD and Intel GPUs are not detected, so those hosts get the
`No GPU detected` suggestion. Check your free disk space before downloading.

## How `translate` builds the prompt

There is nothing to configure. Any model whose name starts with `translategemma`
gets the [format Ollama documents](https://ollama.com/library/translategemma): one
user message, no system message.

```text
You are a professional {SOURCE_LANG} ({SOURCE_CODE}) to {TARGET_LANG} ({TARGET_CODE}) translator. Your goal is to accurately convey the meaning and nuances of the original {SOURCE_LANG} text while adhering to {TARGET_LANG} grammar, vocabulary, and cultural sensitivities.
Produce only the {TARGET_LANG} translation, without any additional explanations or commentary. Please translate the following {SOURCE_LANG} text into {TARGET_LANG}:


{TEXT}
```

- **Source language:** `--from` if you set it, otherwise the other side of a
  `--pair`, otherwise offline detection. If detection isn't confident, the prompt
  says English. In testing, a wrong label did not change the translation.
- **Chinese codes:** `zh-TW` and `zh-Hant` are both sent as `zh-TW`, because
  `zh-Hant` produced almost only Simplified output. `zh-CN` is sent as `zh-Hans`.
- **Temperature:** `translate` sends `0`, so the same input gives the same result.

## What works differently

| Option | With TranslateGemma |
|---|---|
| `contextual` or `concise` preset | Returns one plain translation |
| `dictionary` preset | Returns one plain translation, with a warning |
| `instructions` | Ignored, with a warning |
| `--learn` | Error: the model can't produce structured output |
| `--preserve-format`, and multiline stdin that turns it on | Error: the model can translate code blocks |
| `--bilingual` | Falls back to per-block translation |
| Input over about 2,000 tokens | Error: split the text or use another engine |

For multiline text that isn't a document, pass `--preserve-format=false`.

With `engine = "auto"`, a rejected request moves on to the next engine in
`chain.order`, and the reason appears as a warning. With `--engine ollama`, you get
the error.

## Known limitations

These come from testing `translategemma:4b` only. See [Test results](#test-results).

- **Traditional Chinese is unreliable.** With a `zh-TW` target, 5 of 20 sentences
  came back entirely in Simplified. `translate` warns when the output contains
  Simplified-only characters, but doesn't convert it. Traditional output can also
  use mainland vocabulary such as `信息技術`.
- **Long input is limited.** The model card gives a 2K-token input limit. Over
  Ollama's context size, the start of the prompt is dropped without an error, and
  the reply can be unrelated to your text, so `translate` refuses long input.
- **Long output needs checking.** Text made of one sentence repeated 60 times came
  back as a one-sentence summary.
- **Translation can drop details.** One sentence lost "the printer" in the `zh-TW`
  target, and another left the word "afternoon" in English.
- **Idioms may be literal.** `猫の手も借りたい` became "borrow even a cat's paws".
- **Images aren't supported in `translate`.** The model accepts them, but `translate`
  only sends text.

The models are licensed under the
[Gemma Terms of Use](https://ai.google.dev/gemma/terms), not an open-source license.

## Test results

Tested on 2026-11-06: `translategemma:4b`, Apple M4 with 24 GB, Ollama 0.40.10, and
Ollama's default context of 4,096 tokens. The samples are small, so treat these as
indications rather than benchmarks. `12b` and `27b` are untested here.

| Test | Result |
|---|---|
| Speed, model already loaded | 0.4 s to the first token, about 33 streamed chunks per second |
| First request, model not loaded | 10.4 s |
| Repeat runs (3 each) | 1 distinct output at temperature 0, 2 with the server default |
| `zh-TW` target (20 sentences) | 15 Traditional, 5 Simplified |
| `zh-Hant` target (20 sentences) | 0 Traditional, 19 Simplified, 1 mixed |
| `zh-Hant-HK` target (20 sentences) | 6 Traditional, 13 Simplified, 1 undetermined |
| Wrong source label (16 short texts) | Same translation, apart from a final period |
| About 2,600 input tokens | Output cut off, reported as `finish_reason: length` |
| About 8,000 input tokens | Prompt dropped to 2,051 tokens, unrelated English reply, reported as `finish_reason: stop` |
| About 1,600 input tokens, just under the limit | Complete translation |
| Markdown sample | Structure and links kept; text inside a code block was translated |

The earlier community report of
[Simplified output for Traditional targets](https://reddit.sentinel-team.org/posts/1sl5k6d/snapshots/2026-04-17T23%3A20%3A42.645991Z)
measured 76% Simplified with `zh-Hant`. These results agree for `zh-Hant`, and
`zh-TW` did noticeably better here.
