[English version / 英語版](../data-model.md)

# apogee データモデル

apogee が書き込むすべての DuckDB テーブルの列レベルリファレンスです。正式なスキーマは [`internal/store/duckdb/schema.sql`](../../internal/store/duckdb/schema.sql) にあり、マイグレーションは最初の open 時に自動適用されます。

apogee は DuckDB をプロセス内 append-only の列指向ストアとして使います。書き込みは追加（rollup / intervention テーブルでは行置換）で、読み出しは reconstructor が書いているのと同じファイルから直接サーブされます。ORM はなく、すべてのクエリは [`internal/store/duckdb/`](../../internal/store/duckdb/) にあります。

---

## エンティティ概観

```
sessions         Claude Code セッションごとに 1 行
  └── turns      ユーザーターンごとに 1 行（= 1 OTel トレース）
        ├── spans            hook 由来の OTel span。ターンを親とする
        ├── hitl_events      ターンにスコープされた HITL ライフサイクル
        ├── interventions    ターンまたはセッションにスコープされた operator メッセージ
        └── logs             ターンにスコープされた raw hook ログ

session_rollups  セッションごとに 1 行。Sonnet 層のナラティブダイジェスト
task_type_history ツール組合せ単位のローリング成功 / 失敗カウント
metric_points    OTel メトリックポイント（時系列）
```

---

## `sessions`

コレクターが観測した Claude Code `session_id` ごとに 1 行。新しいセッション id が現れたときに reconstructor が書き込み、`apogee uninstall --purge` 以外では削除されません。

| Column | Type | Purpose |
| --- | --- | --- |
| `session_id` | VARCHAR PK | Claude Code セッション UUID |
| `source_app` | VARCHAR | 導出ラベル — リポ名または固定値 |
| `started_at` | TIMESTAMP | 最初のイベント時刻 |
| `ended_at` | TIMESTAMP NULL | `SessionEnd` hook で設定 |
| `last_seen_at` | TIMESTAMP | 各イベントで更新 |
| `turn_count` | INTEGER | このセッション配下のターン数（非正規化） |
| `model` | VARCHAR NULL | 直近の Claude Code モデルエイリアス |
| `machine_id` | VARCHAR NULL | 任意のマシン識別子 |

**Writers:** reconstructor（初回 insert、各イベントで update）。
**Readers:** Sessions カタログ、セッション詳細、interventions サービス（スコープ判定）、rollup ワーカー、コマンドパレット。

---

## `turns`

ユーザーターンごとに 1 行、1 つの OTel トレースに相当します。ターンは `UserPromptSubmit` で open し `Stop` で close します。この行には生メタデータに加え、attention エンジンと summarizer recap ワーカーが書き戻す導出列も含まれます。

| Column | Type | Purpose |
| --- | --- | --- |
| `turn_id` | VARCHAR PK | ターン id |
| `trace_id` | VARCHAR UNIQUE | OTel trace id（`turn_id` と 1:1） |
| `session_id` | VARCHAR | 親セッション |
| `source_app` | VARCHAR | セッションからコピーされる導出ラベル |
| `started_at` | TIMESTAMP | `UserPromptSubmit` の時刻 |
| `ended_at` | TIMESTAMP NULL | `Stop` の時刻 |
| `duration_ms` | BIGINT NULL | end - start から導出 |
| `status` | VARCHAR | `running` / `completed` / `errored` / `stopped` / `compacted` |
| `model` | VARCHAR NULL | Claude Code モデルエイリアス |
| `prompt_text` | VARCHAR | ユーザープロンプト（切り詰め） |
| `prompt_chars` | INTEGER | プロンプト長 |
| `output_chars` | INTEGER | アシスタント出力長 |
| `tool_call_count` | INTEGER | tool span 数 |
| `subagent_count` | INTEGER | subagent span 数 |
| `error_count` | INTEGER | 失敗したツール呼び出し数 |
| `input_tokens` | BIGINT NULL | 上流モデルの入力トークン数 |
| `output_tokens` | BIGINT NULL | 上流モデルの出力トークン数 |
| `headline` | VARCHAR NULL | 1 行の結果ラベル（recap） |
| `outcome_summary` | VARCHAR NULL | 短い結果サマリ（recap） |
| `attention_state` | VARCHAR | `healthy` / `watchlist` / `watch` / `intervene_now` |
| `attention_reason` | VARCHAR | 判定理由 |
| `attention_score` | DOUBLE | `[0, 1]` の数値スコア |
| `attention_tone` | VARCHAR | UI トーンキー |
| `phase` | VARCHAR | `plan` / `explore` / `edit` / `test` / `run` / `commit` / `debug` / `delegate` / `verify` / `idle` |
| `phase_confidence` | DOUBLE | phase ヒューリスティックの確信度 |
| `phase_since` | TIMESTAMP | 現 phase に入った時刻 |
| `attention_signals_json` | VARCHAR | attention シグナルの JSON |
| `recap_json` | VARCHAR NULL | summarizer の構造化 recap JSON |
| `recap_generated_at` | TIMESTAMP NULL | recap 書き込み時刻 |
| `recap_model` | VARCHAR NULL | recap を生成したモデル |

**Writers:** reconstructor（生列）、attention エンジン（`attention_*`、`phase_*`）、summarizer recap ワーカー（`recap_json`、`recap_generated_at`、`recap_model`、`headline`、`outcome_summary`）。

**Readers:** Live ページのフレームグラフ + triage レール、ターン詳細ページ、Sessions カタログ、Insights オーバービュー、SSE ブロードキャスタ。

---

## `spans`

OTel 形式の span 行。1 span = 1 行で、`trace_id` + `parent_span_id` が親子関係を表します。インデックスされない OTel ペイロードは `attributes_json` と `events_json` に畳み込まれます。

| Column | Type | Purpose |
| --- | --- | --- |
| `trace_id` | VARCHAR | PK の一部 — トレース（= ターン） |
| `span_id` | VARCHAR | PK の一部 |
| `parent_span_id` | VARCHAR NULL | 親 span。ターンルートは NULL |
| `name` | VARCHAR | `claude_code.turn` / `claude_code.tool.*` / `claude_code.subagent.*` / `claude_code.hitl.permission` / `claude_code.turn.recap` |
| `kind` | VARCHAR | OTel span kind（`INTERNAL` / `SERVER` / ...） |
| `start_time` | TIMESTAMP | span open 時刻 |
| `end_time` | TIMESTAMP NULL | span close 時刻 |
| `duration_ns` | BIGINT NULL | end - start から導出 |
| `status_code` | VARCHAR | `UNSET` / `OK` / `ERROR` |
| `status_message` | VARCHAR NULL | エラー説明 |
| `service_name` | VARCHAR | 既定 `claude-code` |
| `session_id` | VARCHAR NULL | 非正規化されたセッション id |
| `turn_id` | VARCHAR NULL | 非正規化されたターン id |
| `agent_id` | VARCHAR NULL | エージェント id（main / subagent） |
| `agent_kind` | VARCHAR NULL | `main` / `subagent` |
| `tool_name` | VARCHAR NULL | Claude Code のツール名 |
| `tool_use_id` | VARCHAR NULL | アシスタントメッセージの `tool_use_id` |
| `mcp_server` | VARCHAR NULL | 該当する場合の MCP サーバー名 |
| `mcp_tool` | VARCHAR NULL | 該当する場合の MCP ツール名 |
| `hook_event` | VARCHAR NULL | span を open した hook イベント |
| `attributes_json` | VARCHAR | JSON オブジェクト — `claude_code.*` + `gen_ai.*` 属性 |
| `events_json` | VARCHAR | JSON 配列 — span イベント（`claude_code.notification` など） |

### `attributes_json` の形

厳密な JSON オブジェクトです。キーは [`semconv/model/registry.yaml`](../../semconv/model/registry.yaml) の `claude_code.*` 属性 id に加え、上流の `gen_ai.*` キーもいくつか含みます。

```json
{
  "claude_code.tool.name": "Bash",
  "claude_code.tool.use_id": "tool_01HXYZ...",
  "claude_code.tool.input_summary": "git status",
  "claude_code.phase.name": "test",
  "claude_code.phase.inferred_by": "heuristic",
  "gen_ai.system": "anthropic",
  "gen_ai.request.model": "claude-sonnet-4-6"
}
```

### `events_json` の形

厳密な JSON 配列です。各要素は `name`、`time_ms`（unix millis）、`attributes`（文字列 / 数値 / 真偽値のフラットオブジェクト）を持ちます。

```json
[
  { "name": "claude_code.prompt", "time_ms": 1713138123456, "attributes": { "claude_code.prompt.chars": 412 } },
  { "name": "claude_code.notification", "time_ms": 1713138125123, "attributes": { "claude_code.notification.type": "idle" } }
]
```

**Writers:** reconstructor + OTel ミラー + summarizer recap span エミッター。
**Readers:** ターン詳細のスイムレーン、span ツリー取得エンドポイント、attention エンジンのシグナル収集。

---

## `logs`

生 hook ログ行 — hook イベントごとに 1 行。ターン詳細ページの「Raw logs」パネルが描画し、reconstructor がフルターン再構築時に読み返します。

| Column | Type | Purpose |
| --- | --- | --- |
| `id` | BIGINT PK | オート id |
| `timestamp` | TIMESTAMP | hook イベント時刻 |
| `trace_id` | VARCHAR NULL | 判明している場合の trace id |
| `span_id` | VARCHAR NULL | 判明している場合の span id |
| `severity_text` | VARCHAR | OTel ログ重大度の文字列 |
| `severity_number` | INTEGER | OTel ログ重大度の数値 |
| `body` | VARCHAR | ログ本文（通常は生 hook ペイロードの抜粋） |
| `session_id` | VARCHAR NULL | セッション id |
| `turn_id` | VARCHAR NULL | ターン id |
| `hook_event` | VARCHAR | hook イベント名 |
| `source_app` | VARCHAR | 導出ラベル |
| `attributes_json` | VARCHAR | 追加の構造化フィールド |

**Writers:** reconstructor（hook イベントごとに 1 行）。
**Readers:** ターン詳細の raw ログパネル、セッション詳細の raw ログパネル。

---

## `metric_points`

OTel メトリックポイント。書き込み最適化の列指向で、バックグラウンドメトリックサンプラと reconstructor（span close 時のヒストグラム書き込み）が書き込みます。

| Column | Type | Purpose |
| --- | --- | --- |
| `id` | BIGINT PK | オート id |
| `timestamp` | TIMESTAMP | サンプル時刻 |
| `name` | VARCHAR | メトリック名（`claude_code.turn.duration_ms` など） |
| `kind` | VARCHAR | `gauge` / `counter` / `histogram` |
| `value` | DOUBLE NULL | counter / gauge のスカラー |
| `histogram_json` | VARCHAR NULL | histogram の場合のバケット本体 |
| `unit` | VARCHAR NULL | 単位 |
| `labels_json` | VARCHAR | ラベルの JSON オブジェクト |

**Writers:** `internal/metrics/collector.go`、reconstructor。
**Readers:** `/v1/metrics/series`（KPI スパークライン）、Insights オーバービュー。

---

## `hitl_events`

HITL ライフサイクル行。`permission` / `tool_approval` / `prompt` / `choice` 要求ごとに 1 行。ライフサイクルは `pending → responded | timeout | expired | error`。

| Column | Type | Purpose |
| --- | --- | --- |
| `id` | BIGINT PK | オート id |
| `hitl_id` | VARCHAR UNIQUE | 安定した HITL id（`hitl-<8 hex>`） |
| `span_id` | VARCHAR | HITL span id |
| `trace_id` | VARCHAR | trace / turn id |
| `session_id` | VARCHAR | セッション id |
| `turn_id` | VARCHAR | ターン id |
| `kind` | VARCHAR | `permission` / `tool_approval` / `prompt` / `choice` |
| `status` | VARCHAR | `pending` / `responded` / `timeout` / `expired` / `error` |
| `requested_at` | TIMESTAMP | HITL が開いた時刻 |
| `responded_at` | TIMESTAMP NULL | 人間が応答した時刻 |
| `question` | VARCHAR | 質問本文 |
| `suggestions_json` | VARCHAR | 推奨応答の JSON 配列 |
| `context_json` | VARCHAR | 追加コンテキストの JSON |
| `decision` | VARCHAR NULL | `allow` / `deny` / `custom` / `timeout` |
| `reason_category` | VARCHAR NULL | 大まかな理由カテゴリ |
| `operator_note` | VARCHAR NULL | 自由記述メモ |
| `resume_mode` | VARCHAR NULL | `continue` / `retry` / `abort` / `alternative` |
| `operator_id` | VARCHAR NULL | 応答したオペレーター |

**Writers:** `internal/hitl/service.go`。
**Readers:** ターン詳細の HITL キューパネル、セッション HITL エンドポイント、attention エンジンのシグナル収集。

---

## `session_rollups`

Sonnet 層が生成するセッションごとのナラティブダイジェスト。セッションあたり 1 行で、新しい rollup が来るたびに `INSERT OR REPLACE` で丸ごと置き換えられます。

| Column | Type | Purpose |
| --- | --- | --- |
| `session_id` | VARCHAR PK | セッション id |
| `generated_at` | TIMESTAMP | 書き込み時刻 |
| `model` | VARCHAR | rollup を生成したモデル |
| `from_turn_id` | VARCHAR NULL | 最初にカバーしたターン |
| `to_turn_id` | VARCHAR NULL | 最後にカバーしたターン |
| `turn_count` | INTEGER | カバーしたターン数 |
| `rollup_json` | VARCHAR | ナラティブダイジェスト JSON |

**Writers:** `internal/summarizer/rollup.go`。
**Readers:** セッション詳細ページの rollup パネル、Sessions カタログのツールチップ。

---

## `interventions`

オペレーターがライブの Claude Code セッションへ投入するメッセージ。ライフサイクルは `queued → claimed → delivered → consumed` で、`cancelled` / `expired` が終端の off-ramp です。claim は `apogee hook` が `PreToolUse` / `UserPromptSubmit` のタイミングで実行する atomic なフリップです。

| Column | Type | Purpose |
| --- | --- | --- |
| `intervention_id` | VARCHAR PK | 安定した id |
| `session_id` | VARCHAR | 対象セッション |
| `turn_id` | VARCHAR NULL | 対象ターン（`scope=this_turn` 時） |
| `operator_id` | VARCHAR NULL | 投入者 |
| `created_at` | TIMESTAMP | 投入時刻 |
| `claimed_at` | TIMESTAMP NULL | claim された時刻 |
| `delivered_at` | TIMESTAMP NULL | hook が配達を報告した時刻 |
| `consumed_at` | TIMESTAMP NULL | 下流 hook が後続活動を観測した時刻 |
| `expired_at` | TIMESTAMP NULL | スイーパーが `expired` に倒した時刻 |
| `cancelled_at` | TIMESTAMP NULL | オペレーターがキャンセルした時刻 |
| `auto_expire_at` | TIMESTAMP | スイーパーの期限 |
| `message` | VARCHAR | 操作者メッセージ本文 |
| `delivery_mode` | VARCHAR | `interrupt` / `context` / `both` |
| `scope` | VARCHAR | `this_turn` / `this_session` |
| `urgency` | VARCHAR | `high` / `normal` / `low` |
| `status` | VARCHAR | 現在のライフサイクル状態 |
| `delivered_via` | VARCHAR NULL | 配達に使われた hook イベント |
| `consumed_event_id` | BIGINT NULL | consume したログ行 id |
| `notes` | VARCHAR NULL | 自由記述メモ |

**Writers:** `internal/interventions/service.go`（submit / claim / delivered / consumed / expired / cancelled）、`apogee hook`（claim と delivered コールバック）。

**Readers:** Operator Queue UI、SSE ブロードキャスタ、attention エンジン（`intervention_pending` シグナル）。

---

## `task_type_history`

ターンのツール組合せ（canonical tool signature）ごとのローリング成功 / 失敗カウント。ターンが閉じるときに attention エンジンが書き込み、`watchlist` バケットのために読み返します。これにより apogee は現在のターンで何かが壊れる *前* に、過去遅かったり失敗率が高いツール組合せを警告できます。

| Column | Type | Purpose |
| --- | --- | --- |
| `pattern` | VARCHAR PK | ツール組合せの正規化シグネチャ（ソートされたツール名集合） |
| `turn_count` | BIGINT | そのシグネチャで観測したターン総数 |
| `success_count` | BIGINT | `completed` で閉じたターン数 |
| `failure_count` | BIGINT | `errored` で閉じたターン数 |
| `last_updated` | TIMESTAMP | 最後の行更新時刻 |

**Writers:** attention エンジン（ターン close 時）。
**Readers:** attention エンジン（ターン open 時、watchlist 照会）。

---

## `user_preferences`

オペレーターが調整できるランタイム設定の汎用 K/V テーブルです。値は JSON エンコードされた文字列として保存しているため、将来より複雑な型（リスト、オブジェクト）が必要になってもスキーママイグレーションなしに追加できます。PR #29 で追加されました。型付きアクセサは [`internal/store/duckdb/preferences.go`](../../internal/store/duckdb/preferences.go)、HTTP ルートは [`internal/collector/preferences.go`](../../internal/collector/preferences.go) にあります。

| Column | Type | Purpose |
| --- | --- | --- |
| `key` | VARCHAR PK | ドット区切りの設定 ID（`summarizer.language` など） |
| `value_json` | VARCHAR | JSON エンコードされた値 |
| `updated_at` | TIMESTAMP | 最終書込時刻（UTC） |

ドキュメント済みの summarizer キー（マイグレーションなしで今後追加可能）:

| Key | Default | Meaning |
| --- | --- | --- |
| `summarizer.language` | `"en"` | recap + rollup ワーカーの出力言語。`"en"` または `"ja"`。 |
| `summarizer.recap_system_prompt` | `""` | Haiku recap 命令ブロックに追記される自由テキスト。最大 2048 文字。 |
| `summarizer.rollup_system_prompt` | `""` | Sonnet rollup 命令ブロックに追記される自由テキスト。最大 2048 文字。 |
| `summarizer.recap_model` | `""` | recap モデルエイリアスのオーバーライド。空なら `~/.apogee/config.toml` の `[summarizer] recap_model` にフォールバック。 |
| `summarizer.rollup_model` | `""` | rollup モデルエイリアスのオーバーライド。空なら同じく config ファイルにフォールバック。 |

**Writers:** `PATCH /v1/preferences`、`DELETE /v1/preferences`。
**Readers:** summarizer ワーカープールがジョブ開始時に毎回再読込するため、プロンプト言語やモデルオーバーライドは再起動なしで反映されます。

---

## インデックスチートシート

スキーマはダッシュボードのホットパスに合わせた小さなインデックスセットを作ります。

- `sessions(last_seen_at DESC)` — 直近セッション一覧。
- `sessions(source_app)` — スコープフィルター。
- `turns(session_id, started_at DESC)` — セッション内ターン一覧。
- `turns(started_at DESC)` — Live / recent ターン一覧。
- `turns(status)` — 実行中ターン。
- `turns(attention_state)` — attention でのソート。
- `spans(trace_id)` — ターンの span ツリー。
- `spans(session_id, start_time)` — セッションスコープのクエリ。
- `spans(turn_id, start_time)` — ターンスコープのクエリ。
- `spans(tool_name)`、`spans(name)`、`spans(start_time DESC)` — ターン横断フィルター。
- `logs(session_id, timestamp)`、`logs(trace_id)`、`logs(hook_event)`、`logs(timestamp DESC)` — raw ログ fetch。
- `hitl_events(session_id, requested_at DESC)`、`hitl_events(turn_id)`、`hitl_events(status)`、`hitl_events(kind)`。
- `session_rollups(generated_at DESC)` — 直近生成 rollup。
- `interventions(session_id, created_at DESC)`、`interventions(session_id, status)`（pending 高速パス）、`interventions(status)`、`interventions(auto_expire_at)`（スイーパー）。
- `task_type_history(last_updated DESC)` — 直近更新。
- `metric_points(name, timestamp DESC)` — 時系列 fetch。

---

## マイグレーション

`internal/store/duckdb/migrate.go` は open 時に毎回、`PRAGMA table_info` を目標スキーマと差分してカラム追加を適用します。これにより古い DB も新しいバイナリと互換のまま、`apogee migrate` のような明示手順なしで動きます。マイグレーターは `apogee serve` 起動の一部として走ります。

**DuckDB ファイルは絶対に手で編集しないでください。** クリーンな状態が欲しければ `apogee uninstall --purge` でローテート / 削除してください。
