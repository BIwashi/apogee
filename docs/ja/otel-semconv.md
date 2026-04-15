[English version / 英語版](../otel-semconv.md)

# apogee OpenTelemetry セマンティック規約

apogee は、Claude Code の実行を記述する `claude_code.*` という小さなセマンティック規約名前空間を所有しています。正式なソースは [`semconv/model/registry.yaml`](../../semconv/model/registry.yaml) にあり、[`semconv/attrs.go`](../../semconv/attrs.go) の Go 定数はそのファイルと 1 対 1 で対応し、drift を防ぐためのユニットテストを備えています。

本ドキュメントは人間向けのインデックスです。属性を追加・改名する際は、YAML を編集し、Go 定数を手で再生成し、`go test ./semconv/...` で整合性を確認してください。

## Span 名

| Name | Kind | Notes |
| --- | --- | --- |
| `claude_code.turn` | server | Claude Code のユーザーターンごとに 1 つ。トレースルート。 |
| `claude_code.tool.<name>` | internal | ツール呼び出しごとに 1 つ。`<name>` は Claude Code のツール名。 |
| `claude_code.tool.mcp.<server>.<tool>` | internal | MCP 経由で提供されるツール呼び出し。 |
| `claude_code.subagent.<type>` | internal | subagent 実行ごとに 1 つ。ターンルートの子。 |
| `claude_code.hitl.permission` | internal | HITL 権限要求ごとに 1 つ。 |
| `claude_code.turn.recap` | internal | サマライザが recap をランディングしたときに発行される事後補強 span。OTel span link でターンルートに紐づく。 |

## Resource 属性

apogee は標準の OTel resource キーに加え、`OTEL_RESOURCE_ATTRIBUTES` または TOML の `[telemetry.resource]` で上書きされた apogee 固有のキーを常に発行します。

| Key | Type | Notes |
| --- | --- | --- |
| `service.name` | string | 既定は `apogee`。 |
| `service.version` | string | `internal/version` からのビルドバージョン。 |
| `service.instance.id` | string | プロセスごとに `<hostname>-<pid>`。 |

## 属性グループ

### `claude_code.session.*`

| Attribute | Type | Required | Notes |
| --- | --- | --- | --- |
| `claude_code.session.id` | string | required | 安定した Claude Code セッション UUID。 |
| `claude_code.session.source_app` | string | recommended | セッションを所有する論理アプリ。 |
| `claude_code.session.machine_id` | string | opt-in | セッション発生元のマシン識別子。 |
| `claude_code.session.model` | string | recommended | イベント発生時にアクティブだったモデルエイリアス。 |

### `claude_code.turn.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.turn.id` | string | UUIDv7 のターン id。 |
| `claude_code.turn.status` | enum | `running` \| `completed` \| `errored` \| `stopped` \| `compacted`。 |
| `claude_code.turn.prompt_chars` | int | ユーザープロンプトの長さ。 |
| `claude_code.turn.output_chars` | int | アシスタント出力の長さ。 |
| `claude_code.turn.tool_call_count` | int | ターン内のツール呼び出し件数。 |
| `claude_code.turn.subagent_count` | int | 生成された subagent 件数。 |
| `claude_code.turn.error_count` | int | 失敗したツール呼び出し件数。 |

### `claude_code.agent.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.agent.id` | string | 安定したエージェント識別子。 |
| `claude_code.agent.kind` | enum | `main` \| `subagent`。 |
| `claude_code.agent.parent_id` | string | subagent の親エージェント。 |
| `claude_code.agent.type` | string | subagent の種別（`Explore`、`Plan` など）。 |

### `claude_code.tool.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.tool.name` | string | Claude Code 側でのツール名。 |
| `claude_code.tool.use_id` | string | アシスタントメッセージ上の tool use id。 |
| `claude_code.tool.mcp.server` | string | MCP サーバー名。 |
| `claude_code.tool.mcp.name` | string | MCP ツール名。 |
| `claude_code.tool.input_summary` | string | 入力引数の圧縮表現。 |
| `claude_code.tool.output_summary` | string | 出力ペイロードの圧縮表現。 |
| `claude_code.tool.blocked_reason` | string | ポリシー / HITL ゲートによるブロック理由。 |

### `claude_code.phase.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.phase.name` | enum (open) | `plan`、`explore`、`edit`、`test`、`run`、`commit`、`debug`、`delegate`、`verify`、`idle`。 |
| `claude_code.phase.inferred_by` | enum | `heuristic` \| `llm` \| `agent_declared`。 |

### `claude_code.hitl.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.hitl.id` | string | HITL 要求 id（`hitl-<8 hex>`）。 |
| `claude_code.hitl.kind` | enum | `permission` \| `tool_approval` \| `prompt` \| `choice`。 |
| `claude_code.hitl.status` | enum | `pending` \| `responded` \| `timeout` \| `error` \| `expired`。 |
| `claude_code.hitl.decision` | enum | `allow` \| `deny` \| `custom` \| `timeout`。 |
| `claude_code.hitl.reason_category` | string | 大まかな理由カテゴリ。 |
| `claude_code.hitl.operator_note` | string | 自由記述メモ。 |
| `claude_code.hitl.resume_mode` | enum | `continue` \| `retry` \| `abort` \| `alternative`。 |

### `claude_code.intervention.*`

operator intervention 属性です。intervention はオペレーターがダッシュボードの composer 経由でライブの Claude Code セッションへ投入する自由記述メッセージで、次の `PreToolUse` / `UserPromptSubmit` hook が Claude Code decision として中継します。ライフサイクル全体は [`interventions.md`](interventions.md) を参照してください。

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.intervention.id` | string | 安定した intervention 識別子。 |
| `claude_code.intervention.delivery_mode` | enum | `interrupt` \| `context` \| `both`。 |
| `claude_code.intervention.scope` | enum | `this_turn` \| `this_session`。 |
| `claude_code.intervention.urgency` | enum | `high` \| `normal` \| `low`。 |
| `claude_code.intervention.status` | enum | `queued` \| `claimed` \| `delivered` \| `consumed` \| `expired` \| `cancelled`。 |
| `claude_code.intervention.delivered_via` | string | 配達に使われた hook イベント名（`PreToolUse` \| `UserPromptSubmit`）。 |
| `claude_code.intervention.operator_id` | string | 付与されたオペレーター識別子。 |

### `claude_code.attention.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.attention.state` | enum | `healthy` \| `watchlist` \| `watch` \| `intervene_now`。 |
| `claude_code.attention.score` | double | `[0, 1]` 範囲の数値スコア。 |
| `claude_code.attention.reason` | string | 簡潔な判定理由。 |

### `claude_code.recap.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.recap.headline` | string | 1 行の見出し。 |
| `claude_code.recap.outcome` | enum | `success` \| `partial` \| `failure` \| `aborted`。 |
| `claude_code.recap.model` | string | recap を生成したモデルエイリアス。 |
| `claude_code.recap.key_steps` | string[] | 主なステップの順序付きリスト。 |
| `claude_code.recap.failure_cause` | string | 失敗時の自由記述の原因。 |

### `claude_code.compaction.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.compaction.trigger` | string | compaction を起こしたトリガ。 |

## Events

| Event | Notes |
| --- | --- |
| `claude_code.prompt` | ターンルート span 上のユーザープロンプトテキスト。`claude_code.prompt.text` と `claude_code.prompt.chars` を持つ。 |
| `claude_code.assistant_message` | ターン中のアシスタントメッセージ。`claude_code.assistant_message.text` と `claude_code.assistant_message.chars` を持つ。 |
| `claude_code.notification` | ターン中に出た Claude Code の通知。`claude_code.notification.type` と `claude_code.notification.message` を持つ。 |
| `claude_code.tool.blocked` | ツール呼び出しがブロックされた。`claude_code.tool.blocked_reason` を持つ。 |
| `claude_code.intervention.delivered` | operator intervention が hook 経由で Claude Code に中継された。`claude_code.intervention.id`、`claude_code.intervention.delivery_mode`、`claude_code.intervention.delivered_via` を持つ。 |

## GenAI semconv の再利用

apogee はモデル情報やトークン使用量を持つすべての span に、上流の `gen_ai.*` 属性も書き込みます。

- `gen_ai.system` — `anthropic` に設定。
- `gen_ai.request.model`
- `gen_ai.response.model`
- `gen_ai.operation.name`
- `gen_ai.usage.input_tokens`
- `gen_ai.usage.output_tokens`

これらのキーは OpenTelemetry GenAI semconv SIG が定義しており、apogee が所有するものではありません。再利用することで、汎用的な OTel バックエンド（Tempo、Jaeger、Datadog、Honeycomb など）が既存の GenAI ダッシュボードで Claude Code トレースを描画できます。
