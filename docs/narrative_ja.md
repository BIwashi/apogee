# フェーズナラティブ (サマライザ tier 3)

apogee のサマライザは 3 階層で動作します:

| Tier | モデル                    | スコープ       | ワーカー                            |
| ---- | ------------------------- | -------------- | ----------------------------------- |
| 1    | Haiku                     | 1 ターン       | `internal/summarizer/worker.go`     |
| 2    | Sonnet                    | 1 セッション   | `internal/summarizer/rollup.go`     |
| 3    | Sonnet (デフォルト)       | 1 セッション   | `internal/summarizer/narrative.go`  |

Tier 3 は既存の tier-2 ロールアップとセッションの closed ターンを読み、
それらを少数の意味的「フェーズ」— 進行中の作業を大局的に記述する短い
人間向けの塊 — にグルーピングします。出力は `PhaseBlock` の配列で、
同じ `session_rollups.rollup_json` 行に書き戻されます。`phases[]` を
持たない古いロールアップもそのままパースできます。

`/session?id=<id>` の Timeline タブは、各フェーズをクリック可能な
カードとして、ホバープレビューとサイドドロワーでの詳細表示付きで
レンダリングします。フェーズをクリックするとドロワーが開き、350 ms
ホバーするとフル narrative + key_steps + tool summary を含む
浮動プレビューが表示されます。

## フェーズスキーマ

```go
type PhaseBlock struct {
    Index        int            `json:"index"`
    StartedAt    time.Time      `json:"started_at"`
    EndedAt      time.Time      `json:"ended_at"`
    Headline     string         `json:"headline"`     // コミットメッセージ風
    Narrative    string         `json:"narrative"`    // 1〜3 文
    KeySteps     []string       `json:"key_steps"`    // 2〜5 項目
    Kind         string         `json:"kind"`         // 下記 enum
    TurnIDs      []string       `json:"turn_ids"`
    TurnCount    int            `json:"turn_count"`
    DurationMs   int64          `json:"duration_ms"`
    ToolSummary  map[string]int `json:"tool_summary"` // 例 {"Edit":8,"Bash":3}
}
```

`Kind` は以下のいずれか:

```
implement | review | debug | plan | test | commit | delegate | explore | other
```

narrative ワーカーは外側の `Rollup` blob に 2 つのメタデータ
フィールドを追記します:

- `narrative_generated_at` — tier-3 ワーカーが最後にフェーズを書き込んだ時刻
- `narrative_model` — 使用されたモデル alias

## トリガー経路

1. **tier-2 ロールアップワーカーからの連鎖。** セッションロールアップが
   完了すると、サービスは理由 `session_rollup` でナラティブジョブを
   enqueue します。フェーズはロールアップ直後に自動生成されます。
2. **手動。** `POST /v1/sessions/:id/narrative` は理由 `manual` で
   ジョブを enqueue します。Timeline タブの regenerate ボタンと
   空状態の "Generate narrative" ボタンの両方がこのルートを叩きます。

## 陳腐化ガード

narrative ワーカーは以下の場合にジョブをスキップします:

- セッションの closed ターンが 2 未満
- 既存の `narrative_generated_at` が現在から 30 秒以内
- tier-2 ロールアップが前回のナラティブ実行から変わっておらず、
  ジョブ理由が `manual` ではない

この 3 つのガードにより、明示的なスケジューラなしでも、健全な
セッションに対してはワーカーがアイドルのままです。

## プロンプト構造

`BuildNarrativePrompt` は以下をシリアライズします:

- セッションメタデータ (id, source_app, turn count, started_at, last_ended_at)
- コンテキストとしての tier-2 ロールアップ (headline + narrative + highlights)
- ターン順リスト: 各ターンの headline, outcome, key_steps, スパン由来の tool summary

続いて指示ブロック (既定は英語、`summarizer.language` が `ja` のとき
日本語)。TypeScript スキーマブロックは英語のままで、モデルは常に
カノニカルな型定義を見ます。

`summarizer.narrative_system_prompt` が設定されている場合は、指示
ブロックの前に `# User system prompt` 見出しで追加されます。

## 設定 (Preferences)

`summarizer.` プレフィックスの新しいキー 3 つ:

| キー                                 | 型                 | 既定値                |
| ------------------------------------ | ------------------ | --------------------- |
| `summarizer.narrative_system_prompt` | 文字列 (≤ 2048 B)  | `""`                  |
| `summarizer.narrative_model`         | モデル alias       | `claude-sonnet-4-6`   |
| `summarizer.language`                | `"en"` \| `"ja"`   | tier 1/2 を継承       |

既存の recap / rollup 設定と同じ解決順: UI オーバーライド → TOML
設定 → 既定値。

## コスト見積もり

1 ナラティブ実行はセッションあたり 1 回の Sonnet 呼び出しで、tier-2
ロールアップが更新されたタイミングで発火します。典型的なセッション
(5〜15 closed ターン) のプロンプトは 4〜10 KB、出力は 1〜3 KB 程度
— つまり約 5k 入力 + 2k 出力トークン。Sonnet の料金では
セッションあたり 1 セント未満であり、陳腐化ガードにより変化が
ない限りワーカーはアイドルのままです。

## 手動再生成

```sh
curl -X POST http://127.0.0.1:4400/v1/sessions/<id>/narrative
```

レスポンスは `202 Accepted` と `{"enqueued": true}` です。フェーズが
書き込まれるとワーカーが `session.updated` SSE イベントをブロード
キャストするので、Timeline タブの SWR キャッシュは自動的に再検証
されます。
