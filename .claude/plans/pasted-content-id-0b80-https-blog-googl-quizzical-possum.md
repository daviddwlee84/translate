# TranslateGemma：評估、prompt 相容與 host 建議

## Context

使用者想評估 TranslateGemma，確認是否值得支援，至少補一份 docs，並回顧 config 能否正確初始化它的 prompt。

**結論：值得以「Ollama 上的專用翻譯模型」支援，但不能只換 model 名稱。**

### 已查證的事實

| 項目 | 內容 | 來源 |
|---|---|---|
| 模型 | Gemma 3 微調，2026-01 發布；Gemma Terms（非 Apache-2.0，HF 需同意授權） | Google blog、registry license |
| Ollama tags | `4b` 3.3 GB（`latest`）/ `12b` 8.1 GB / `27b` 17 GB | registry manifest |
| WMT24++ | MetricX 5.32 / 3.60 / 3.09；COMET 81.6 / 83.5 / 84.4（4b / 12b / 27b） | HF model card |
| Roles | 只支援 user / assistant | HF model card |
| Ollama template | `system` 會被渲染成獨立的 user turn | registry template blob |
| Prompt | 單一 user message，必須有明確來源語言，文字前有兩個空行 | Ollama library |
| Context | Ollama 標 128K，HF 寫輸入 2K tokens，兩者不一致 | 兩邊頁面，待實測 |
| 繁中 | zh-Hant 目標 76% 片段輸出簡體 | 單篇社群 benchmark，未驗證 |

`google-gemma/gemma-translator` 是 Gemma 4 + LiteRT 的語音翻譯機，與 TranslateGemma 無關，docs 會註明避免混淆。

官方 prompt（Ollama library，空行與文字需逐字一致）：

```
You are a professional {SOURCE_LANG} ({SOURCE_CODE}) to {TARGET_LANG} ({TARGET_CODE}) translator. Your goal is to accurately convey the meaning and nuances of the original {SOURCE_LANG} text while adhering to {TARGET_LANG} grammar, vocabulary, and cultural sensitivities.
Produce only the {TARGET_LANG} translation, without any additional explanations or commentary. Please translate the following {SOURCE_LANG} text into {TARGET_LANG}:


{TEXT}
```

### repo 現況與缺口

- **Ollama 已支援**：走通用 OpenAI 相容路徑（`internal/config/config.go:262`）。`Provider.Type` 不影響行為。
- **目前的 config 無法正確初始化它**：沒有 prompt template 欄位；`preset` / `instructions` 只會接在 system prompt 後面。
- **`buildTranslatePrompt` 的輸出不符格式**（`internal/engine/prompt.go:136`）：
  - 預設 contextual preset 要求 2–4 行的語意清單。
  - 來源語言會送字面值 `auto`。
  - 語言名稱是 `chinese (traditional)`，官方格式是 `Chinese`。
  - `translateOpenAI` 一律送 system + user（`internal/engine/llm.go:778`），也不送 temperature。
- **`zh-CN` 不在 Ollama 的語言清單**，官方範例用 `zh-Hans`。
- **host 資源偵測完全沒有**。`init` 只探測 `/models` 是否回 200（`cmd/init.go:172`）。

## 使用者已決定

- 範圍：docs + prompt 相容 + `init` 的 host 建議提示。不自動下載、不改 config。
- 實測：只下載 4b；12b / 27b 之後再測。
- 繁中：確認有問題就警告並記 backlog，不加 OpenCC 依賴。

本機檢查：M4 / 24 GB、Ollama 0.40.10、模型目錄空的；磁碟剩 20 GiB（使用率 96%），下載 4b 後約剩 17 GiB。

## 實作步驟

### 1. 實測 4b（計畫批准後第一步）

執行前再跑一次 `df -h`，然後 `ollama pull translategemma:4b`。用小型語料直接打 `/v1/chat/completions`，記錄結果：

- **Prompt 比較**：現行 system + user 與官方單一 user message，用 en→es、en→ja、ja→en、zh→en 比較。
- **繁中**：目標 `zh-TW` 與 `zh-Hant`，統計簡繁比例，決定送哪個 code。
- **Temperature**：不設定與 `temperature: 0` 比較，看輸出穩定度。
- **長度**：約 1K / 3K / 8K tokens 輸入，確認是否會靜默截斷（用 Ollama log 與輸出尾端判斷）。
- **Markdown**：標題、清單、code fence、連結能否保留。
- **效能**：TTFT 與 tokens/s，以及 `ollama ps` 的記憶體用量。
- **現有 CLI 路由**：`--engine ollama --model translategemma:4b` 可用；確認 `--provider ollama` 在 `engine=auto` 下是否仍先走 copilot。

結果會決定 document 模式與長文要 warn 還是 error（見步驟 2）。

### 2. Prompt 相容（`internal/engine/`）

新增 `translategemma.go`，改 `llm.go`。不新增 config 欄位，沿用 `claude*` 按 model 名稱分派的既有做法（`llm.go:268`）：

- `isTranslateGemma(model)`：小寫，取最後一個 `/` 之後的部分，判斷前綴 `translategemma`。
- `buildTranslateGemmaPrompt(req) (string, error)`：產生官方 prompt，golden test 鎖定逐字內容與兩個空行。
- **語言對應**：`lang.Name` 轉 Title Case；`zh-CN → Chinese (zh-Hans)`；`zh-TW` 依實測決定 code。
- **來源語言解析順序**：
  1. 明確指定的 source。
  2. Pair 模式下，target 以外的另一個語言。
  3. `lang.Detect`。
  4. 都無法判定就回 error（提示用 `--from`），在第一個 token 之前失敗，chain 可 failover。
- **`translateOpenAI`**：此模型只送一則 user message，加 `temperature: 0`。`chatRequest` 加 `Temperature *float64 \`json:",omitempty"\``，其他模型的 request body 不變。
- **與模型不相容的功能**：

| 情況 | 行為 |
|---|---|
| `contextual`（預設）/ `concise` preset | 靜默輸出純翻譯 |
| `dictionary` preset、非空 `instructions` | 忽略，加入 `res.Warnings` |
| Tabular 輸入 | 不加 table directive，輸入原樣送出 |
| `Learn`、`Bilingual` | 不打網路，回明確 error |
| `PreserveFormat` | 預設回 error；只有實測 Markdown 全部保留才開放 |
| 目標為 `zh-TW` | 實測確認問題才加警告 |
| 輸入超過實測安全長度 | 會靜默截斷則 error，否則 warn；chunking 進 backlog |

### 3. Host 建議（新增 `internal/hostinfo/`，改 `cmd/init.go`）

- **`TotalMemory()`**：維持 `CGO_ENABLED=0`，用 build tags 分平台：
  - darwin：`hw.memsize`
  - linux：`/proc/meminfo`
  - windows：`GlobalMemoryStatusEx`
  - `golang.org/x/sys` 從 indirect 改 direct。
- **`NvidiaVRAM()`**：best-effort 呼叫 `nvidia-smi`，有 timeout，失敗回 0。
- **`RecommendTranslateGemma(ram, vram, unifiedMemory)`**：純函式，表格測試。閾值是本專案的保守啟發式，不是官方規格，docs 會明講：

| 硬體 | 建議 |
|---|---|
| Apple 統一記憶體 ≥ 32 GB | `27b` |
| Apple 統一記憶體 ≥ 16 GB | `12b` |
| Apple 統一記憶體 < 16 GB | `4b` |
| NVIDIA VRAM ≥ 20 GB | `27b` |
| NVIDIA VRAM ≥ 10 GB | `12b` |
| NVIDIA VRAM < 10 GB，或無法偵測到 GPU | `4b` |

- **`cmd/init.go`**：只在 Ollama 可連線或選擇 ollama 時印一行提示（硬體、建議 tag、下載大小、`ollama pull` 指令）。文字由可測試的函式產生；偵測失敗就不印。

### 4. Docs 與 backlog

- **新增 `docs/translategemma.md`**：
  - 模型介紹、size 表、硬體建議，以及與 `gemma-translator` 的區別。
  - 授權：Gemma Terms。
  - 安裝與用法，以 `[cli] engine` / `model` overlay 為例，不改 `[[provider]]`。
  - translate 如何組 prompt、與 config 的關係。
  - 不支援的功能與上表行為。
  - 實測結果（標明日期、機型、版本）；未測的 12b / 27b 明確標為未驗證；第三方數字附來源。
- **更新 `README.md`**（`:153` 的 Ollama 項目）：加連結與語法。
- **`TODO.md` + `backlog/translategemma.md`**，用 `.agents/skills/project-knowledge-harness/scripts/add-todo.sh` 新增，並更新 `backlog/README.md` 索引：
  - 繁中 OpenCC 後處理（附實測數據、串流取捨）。
  - 長文 chunking。
  - 12b / 27b 實測。
  - 圖片翻譯（目前 `Message.Content` 只支援字串）。

## 不做的事

- 不新增 config 欄位，所以不需要 bump `SchemaVersion`，也避開 `[[provider]]` 預設值無法傳到既有 config 的疑慮。
- 不改預設 model 或 chain。
- 不寫原生 Ollama client，也不加 `num_ctx`（`/v1` 路徑不適用）。
- 不 commit、不打 tag，除非使用者另外要求。

## 驗證

1. **單元測試**：`go test ./cmd/... ./internal/...`
   - `internal/engine/llm_document_test.go` 的做法：用 `httptest` 解碼送出的 request，斷言恰好一則 user message、沒有 system、`temperature` 為 0，且 prompt 逐字相符。
   - 回歸：非 TranslateGemma 模型的 request body 不變。
   - 來源語言解析與不支援模式的 error；chain failover。
2. **跨平台 build**：`GOOS={darwin,linux,windows} GOARCH={amd64,arm64} CGO_ENABLED=0 go build ./...`
3. **端到端（真實 4b）**：
   - `translate --engine ollama --model translategemma:4b --to ja "Hello, how are you?"`
   - `--to zh-TW`、串流與 `--json`、`--learn`（應回明確 error）。
4. **`translate init`**：確認這台機器印出建議 `12b` 的提示。
5. **Backlog 格式**：`.agents/skills/project-knowledge-harness/scripts/todo-kanban.sh --validate-only TODO.md`

完成後回報實測結果。模型會留在本機供後續使用，並附上移除指令 `ollama rm translategemma:4b`。
