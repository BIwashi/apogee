[English version / 英語版](../architecture.md)

# apogee アーキテクチャ

このドキュメントは v0.1.3 時点の apogee のエンドツーエンドのアーキテクチャを説明します。個別サブシステムはそれぞれ独立したドキュメント（[`daemon.md`](daemon.md)、[`interventions.md`](interventions.md)、[`hooks.md`](hooks.md)、[`data-model.md`](data-model.md)、[`otel-semconv.md`](otel-semconv.md)、[`cli.md`](cli.md)）を持ち、最終的な真実のソースはソースコードそのものです。本ドキュメントとコードが食い違っていたらコードが正です。PR を送ってください。

---

## 30 秒で読む apogee

apogee は単一の Go バイナリで、以下のことを行います。

1. マシン上のすべての Claude Code セッションから、`.claude/settings.json` に登録された `apogee hook` サブコマンド経由で hook イベントを受け取る。
2. 各 hook ストリームを OpenTelemetry 形式のトレースに組み直し（1 ユーザーターン = 1 トレース）、埋め込み DuckDB に永続化し、span を任意の OTLP エクスポータにミラーする。
3. 埋め込まれた Next.js ダッシュボードへライブ更新を SSE で配信する。
4. 2 層の LLM サマライザ（ターンごとの recap + セッションごとの rollup）をローカルの `claude` CLI 経由で駆動する。
5. operator intervention（オペレーターからの自由文メッセージ）を受け付け、次の hook 発火時にそれを Claude Code へフック decision として返す。
6. バックグラウンドサービスとして動作する（macOS は launchd、Linux は systemd `--user`）。ログインのたびに自動起動し、管理の手間はかかりません。

すべては 1 つのバイナリに収まっています。Web UI は `embed.FS` で埋め込まれているため、`brew install BIwashi/tap/apogee` またはリリース tarball を展開するだけで、Node や Python に依存せず完全な製品が手に入ります。

---

## パイプライン

```
Claude Code ──► apogee hook --event X ──► POST /v1/events ──► ingest ──► reconstructor ──► duckdb
                        │                                         │                            │
                        │                                         ├── attention engine ────────┤
                        │                                         ├── summarizer (recap/rollup)┤
                        │                                         ├── interventions.Service ───┤
                        │                                         ├── hitl.Service ────────────┤
                        │                                         └── sse.Hub ─────────────────┤
                        │                                                                      │
                        │                                                metrics collector ────┤
                        │                                                otel exporter ────────┘
                        │
                        └─── POST /v1/sessions/{id}/interventions/claim ◄── operator composer (web UI)

collector が公開するルート:
  /v1/events                             hook の受信（ここへの書き込みが上記のパイプラインをトリガ）
  /v1/events/stream                      SSE 配信（すべての状態遷移がここへ流れる）
  /v1/turns/active                       ライブ triage 一覧
  /v1/turns/recent                       直近で閉じたターン一覧
  /v1/turns/{turn_id}                    単一ターン詳細
  /v1/turns/{turn_id}/spans              ターン内の span ツリー
  /v1/turns/{turn_id}/logs               ターン範囲の raw hook ログ
  /v1/turns/{turn_id}/attention          ターンの attention 状態 + 根拠
  /v1/turns/{turn_id}/recap              ターン単位の recap の取得 / 再生成
  /v1/sessions/recent                    直近のセッション一覧
  /v1/sessions/search                    セッションのファジー検索
  /v1/sessions/{id}                      単一セッション詳細
  /v1/sessions/{id}/summary              ヘッダー用の非正規化サマリ
  /v1/sessions/{id}/turns                セッション内のターン一覧
  /v1/sessions/{id}/logs                 セッション範囲の raw hook ログ
  /v1/sessions/{id}/rollup               セッション単位の rollup の取得 / 再生成
  /v1/attention/counts                   スコープ付き attention バケットのヒストグラム
  /v1/metrics/series                     KPI スパークライン用の時系列
  /v1/filter-options                     サイドバーのスコープ（source_app、model など）
  /v1/hitl                               HITL イベント一覧
  /v1/hitl/{hitl_id}                     単一 HITL
  /v1/hitl/{hitl_id}/respond             HITL への応答
  /v1/sessions/{id}/hitl/pending         セッション内の pending HITL
  /v1/turns/{turn_id}/hitl               ターン内の HITL
  /v1/interventions                      POST: オペレーターが intervention を投入
  /v1/interventions/{id}                 単一 intervention
  /v1/interventions/{id}/cancel          キャンセル
  /v1/interventions/{id}/delivered       hook 側の delivered コールバック
  /v1/interventions/{id}/consumed        reconstructor 側の consumed コールバック
  /v1/sessions/{id}/interventions        セッション内の intervention 一覧
  /v1/sessions/{id}/interventions/pending pending な intervention
  /v1/sessions/{id}/interventions/claim  hook 側の atomic claim
  /v1/turns/{turn_id}/interventions      ターン内の pending intervention
  /v1/agents/recent                      直近のエージェント（main + subagent）と呼び出し回数
  /v1/insights/overview                  集計分析のランディング
  /v1/info                               コレクターのビルド情報
  /v1/telemetry/status                   OTel エクスポータ設定 + カウンタ
  /v1/healthz                            liveness プローブ（JSON 本文は任意）

apogee サブコマンド:
  apogee serve                           コレクター + 埋め込みダッシュボードを起動
  apogee init                            .claude/settings.json に apogee hook を書き込む
  apogee hook --event <X>                Claude Code の hook エントリポイント（= バイナリ自身）
  apogee daemon {install,uninstall,start,stop,restart,status}
                                         launchd / systemd --user スーパーバイザ
  apogee status                          daemon + HTTP の一括 liveness 判定
  apogee logs                             daemon ログファイルを tail
  apogee open                            http://127.0.0.1:4100 をブラウザで開く
  apogee uninstall                       daemon を停止、hook を剥がし、任意でデータも削除
  apogee menubar                         macOS のステータスバーアプリ（daemon が必要）
  apogee doctor                          PATH / config / 依存関係のヘルスチェック
  apogee version                         ビルドバージョン + コミット + ビルド時刻を表示
```

バイナリ自身が Claude Code の hook なので、別途 hook スクリプトを走らせるランタイムは不要です。Python 依存も、hook 用の埋め込み FS も、インストールする `hooks/` ディレクトリもありません。`apogee init` は実行中バイナリの絶対パスに `hook --event X --server-url ...` を付けたものを `.claude/settings.json` に書き込むだけで、インストールはこれだけで終わります。

---

## 「1 ユーザーターン = 1 OTel トレース」モデル

apogee は、Claude Code の 1 ユーザーターンを観測の単位として扱います。ターンとは `UserPromptSubmit`（ユーザーがプロンプトを送信した瞬間）から `Stop`（エージェントがターンを終えた瞬間）までの範囲です。その間にあるすべてのツール呼び出し・subagent 実行・HITL リクエストは、ルートの子 span としてモデル化されます。

```
trace = claude_code.turn                 (root。UserPromptSubmit で開き、Stop で閉じる)
├── span  claude_code.tool.Bash
├── span  claude_code.tool.Read
├── span  claude_code.subagent.Explore   (subagent の子)
│   ├── span  claude_code.tool.Grep
│   └── span  claude_code.tool.Read
├── span  claude_code.hitl.permission    (人間が応答するまで開きっぱなし)
├── span  claude_code.turn.recap         (事後の補強 span。root とリンクされる)
└── event claude_code.notification
```

これにより、トレース形式の任意のバックエンド（Jaeger、Tempo、Honeycomb、Datadog APM など）で意味のある描画ができます。ユーザープロンプトをルート、各ツール呼び出しをラベル付きの子としたフレームグラフが得られます。古典的なトレーシングバックエンドと apogee が違うのは、ダッシュボードが高速なカタログ表示のために非正規化した `turns` / `sessions` テーブルも読むことです（生 span から毎回再構築するのは現実的ではないため）。

---

## サブシステム

### `cmd/apogee` — エントリポイント

[`cmd/apogee/main.go`](../../cmd/apogee/main.go) が CLI エントリポイントです。そのまま [`internal/cli/root.go`](../../internal/cli/root.go) に委譲し、cobra のコマンドツリー全体を組み立てます。バイナリは同時に Claude Code の hook（[`hooks.md`](hooks.md) 参照）とスーパーバイザのクライアント（[`daemon.md`](daemon.md) 参照）でもあります。

### `internal/ingest` — hook → OTel span

[`internal/ingest/reconstructor.go`](../../internal/ingest/reconstructor.go) がステートフルな hook → OTel reconstructor です。セッション別の agent スタックと、`tool_use_id` ごとの未解決マップを保持することで、各 `PostToolUse` を正しい subagent の下にぶら下げ、ツール span を正しく open / close できます。各ハンドラはストアに書き込み、[`internal/ingest/otelmirror.go`](../../internal/ingest/otelmirror.go) 経由で OTel 側にもミラーし、hub に SSE エンベロープを publish します。

### `internal/store/duckdb` — 永続化

DuckDB は `github.com/marcboeker/go-duckdb/v2` 経由でプロセス内に同居します。スキーマは [`internal/store/duckdb/schema.sql`](../../internal/store/duckdb/schema.sql) に宣言されています。

| テーブル | 役割 |
|---|---|
| `sessions` | Claude Code セッションごとに 1 行 |
| `turns` | ユーザーターン（= トレースのルート）ごとに 1 行。`attention_*` / `phase_*` / `recap_json` の導出列も持つ |
| `spans` | OTel 形式の span。`attributes_json`、`events_json` がその他のフィールドを運ぶ |
| `logs` | hook イベントごとに 1 行（生 hook ログ、可逆） |
| `metric_points` | OTel メトリック、書き込み最適化の列志向 |
| `hitl_events` | HITL ライフサイクル |
| `session_rollups` | セッション単位のナラティブダイジェスト（Sonnet） |
| `interventions` | operator 由来のメッセージ。queued → claimed → delivered → consumed |
| `task_type_history` | ツール組合せ単位のローリング成功 / 失敗カウント（watchlist に利用） |

各列の詳細とどのサブシステムが書き / 読むかは [`data-model.md`](data-model.md) を参照してください。

### `internal/attention` — attention エンジン

ルールベースの attention エンジンは、reconstructor への書き込みのたびに、実行中のターンを次の 4 バケットのいずれかへ分類します。

```
healthy ──► watchlist ──► watch ──► intervene_now
```

- **healthy** — 異常なし。ツールエラーなし、pending な HITL なし、停滞なし。
- **watchlist** — このターンのツール組合せが歴史的に遅い、またはエラーになりやすい。`task_type_history` から読むので、現在のターンで何かが壊れる前に早期警告できます。
- **watch** — 実際のシグナル: リトライ、数秒経っても返事のない HITL、長時間の bash コマンドなど。一度見ておくべき状態です。
- **intervene_now** — ツールエラー、ツールブロック、長時間待ちの HITL、interventions サービスからの `intervention_pending` シグナル。ダッシュボードのライブ triage リストの先頭に並びます。

状態遷移は `turns` の `attention_state` / `attention_reason` / `attention_score` / `attention_tone` に書き戻され、`turn.updated` SSE イベントとして配信されるので、ダッシュボードは再フェッチなしに並べ替えを行います。

### `internal/summarizer` — 2 層の LLM サマライザ

apogee は Anthropic API に直接話しかけません。すべての LLM 呼び出しはローカルの `claude` CLI を経由します。オペレーターの既存の認証・レート制限・コンテキストがそのまま反映されます。

- **recap ワーカー** — ターンごと、Haiku 層。`Stop` で発火し、`headline` / `key_steps` / `outcome` / `failure_cause` などを持つ構造化 recap JSON を生成し、`turns.recap_json` に書き戻して、ルートにリンクする事後補強 span `claude_code.turn.recap` を発行します。
- **rollup ワーカー** — セッションごと、Sonnet 層。`SessionEnd` とスケジュール実行で発火し、ナラティブダイジェストを `session_rollups` に書き込みます。ダッシュボードのセッション詳細ページはこの行を読みます。

`claude` が `PATH` に無い場合、どちらのワーカーもログを一度残して黙ってスキップします。ダッシュボードは壊れるのではなく、recap パネルが空で描画されます。

### `internal/hitl` — Human-In-The-Loop サービス

HITL は `hitl_events` テーブルに裏打ちされた構造化レコードです。サービスがライフサイクル（pending → responded / timeout / expired / error）と応答 API（`POST /v1/hitl/{id}/respond`）、ターン詳細ページが引くセッション別 pending クエリを持ちます。属性グループは [`semconv/model/registry.yaml`](../../semconv/model/registry.yaml) を参照してください。

### `internal/interventions` — Operator Interventions

HITL の逆方向です。オペレーターはダッシュボードの composer から自由文メッセージを実行中の Claude Code セッションへ投入します。次の `PreToolUse` または `UserPromptSubmit` フックがそれを atomic に claim（`POST /v1/sessions/{id}/interventions/claim`）し、Claude Code decision JSON を stdout に書いて報告します。詳細なライフサイクルは [`interventions.md`](interventions.md) を参照してください。

### `internal/sse` — SSE fan-out hub

[`internal/sse/event.go`](../../internal/sse/event.go) が hub が配信するすべてのイベントを定義します。hub はプロセス内で、クライアントごとに上限つきチャネルを持ち、遅いコンシューマは穏やかにドロップします。イベント例:

- `turn.started`、`turn.updated`、`turn.ended`
- `span.inserted`、`span.updated`
- `session.updated`
- `hitl.*` ライフサイクル遷移
- `intervention.submitted`、`intervention.claimed`、`intervention.delivered`、`intervention.consumed`、`intervention.expired`、`intervention.cancelled`

すべて同じエンベロープ（`{type, at, data}`）を共有するので、web クライアントは 1 フィールドで dispatch できます。

### `internal/metrics` — バックグラウンドメトリックサンプラ

低レートのバックグラウンドサンプラが、数秒ごとに OTel 形式のメトリックポイントを `metric_points` テーブルへ書き込みます。Live ページの KPI スパークラインと Insights オーバービューで使われます。

### `internal/otel` + `internal/telemetry` — OTLP エクスポート

reconstructor の各 span は Go の OTel SDK で本物の span にミラーされ、OTLP エンドポイントが設定されていれば OTLP/gRPC または OTLP/HTTP で外部に送られます。`claude_code.*` 名前空間は [`semconv/model/registry.yaml`](../../semconv/model/registry.yaml) に記述され、[`semconv/attrs.go`](../../semconv/attrs.go) に型付き Go 定数として出てきます。属性一覧は [`otel-semconv.md`](otel-semconv.md) を参照してください。

telemetry 設定の解決順は **env > TOML > デフォルト** です。エンドポイントが未設定なら noop tracer provider を入れ、reconstructor は `Tracer.Start` を呼び続けますが何もエクスポートされません。

### `internal/daemon` — launchd / systemd スーパーバイザ

`apogee daemon {install, uninstall, start, stop, restart, status}` がプラットフォーム固有のユニットファイルを書き、`launchctl`（macOS）または `systemctl --user`（Linux）にシェルアウトしてプロセスを管理します。実際に走るのは常に `apogee serve --addr 127.0.0.1:4100 --db ~/.apogee/apogee.duckdb` で、foreground の `apogee serve` とは実質同じです。詳しいチートシートは [`daemon.md`](daemon.md) を参照してください。

### `internal/cli/menubar_darwin.go` — macOS メニューバーアプリ

`apogee menubar` は `caseymrm/menuet` ベースのステータスバーアプリで、ローカルコレクターを 5 秒間隔でポーリングし、daemon とセッションのカウントをメニューバーに表示します。daemon（または foreground の `apogee serve`）が起動している必要があります。詳細は [`menubar.md`](menubar.md) を参照。

### `internal/webassets` + `web/` — 埋め込みダッシュボード

Next.js ダッシュボードは静的エクスポートされ、`embed.FS` で Go バイナリに取り込まれます。chi ルーターの SPA フォールバックハンドラが、マッチしない GET をすべて `index.html` に書き換えます。公開しているルートは次のとおりです。

```
/              Live フォーカスダッシュボード（フレームグラフ + triage レール）
/sessions      サービスカタログ（検索 + フィルタ）
/session?id=   単一セッション詳細（rollup + ターン一覧）
/turn?sess=&turn= 単一ターン詳細（スイムレーン + recap + HITL + operator queue）
/agents        main / subagent ビュー
/insights      集計分析（エラー率、パーセンタイルレイテンシ、上位ツール / phase）
/settings      コレクター情報、OTel エクスポータ状態、インストール導線
/styleguide    デザイントークンリファレンス（dev 専用）
```

コマンドパレット（`⌘K`）はルートではなくグローバルオーバーレイです。古い `/timeline` エイリアスは PR #24 で削除されました。

---

## データフロー — 1 ツール呼び出しの経路

```
Claude Code PreToolUse
        │
        ▼
  apogee hook --event PreToolUse
        │   ├── POST /v1/sessions/{id}/interventions/claim
        │   │     204: stdin → stdout を素通り
        │   │     200: Claude Code decision JSON を stdout に書き、POST /delivered
        │   └── POST /v1/events（常に）
        ▼
  ingest.Reconstructor.Apply
        │   ├── 新しいターンならルート span を open
        │   ├── claude_code.tool.<name> span を open
        │   ├── span + log 行を DuckDB に書く
        │   ├── OTel SDK にもミラー
        │   ├── そのターンで attention エンジンを再評価
        │   └── SSE hub に span.inserted + turn.updated を配信
        ▼
  web/app (Live ページ)
        │   ├── /v1/events/stream でエンベロープを受け取る
        │   └── フレームグラフ・phase ヘッダー・triage レールを再描画
        ▼
  PostToolUse が到着 → span が閉じ、継続時間が埋まり、attention が再採点されて
  span.updated エンベロープが同じ経路で配信されます。
```

書き込みはすべて append-only で、DuckDB はバースト中もライターをロックせずに並行読み出しできます。各サイドチャネル（OTel、SSE、summarizer、attention エンジン）は「1 hook イベント in、1 状態遷移 out」という同じ契約に沿って動くため、ひとつのサイドチャネルの失敗が他を止めることはありません。

---

## 制約

- **単一バイナリ。** サイドカー DB なし、JRE 埋め込みなし、デプロイ時の Node ランタイムなし。DuckDB はプロセス内、Next.js バンドルは埋め込みです。
- **ローカルファースト。** ネットワークなしでも動きます。OTLP エクスポートは任意です。
- **Claude Code を壊さない。** hook は常に exit 0 です。転送失敗は stderr にログを出して飲み込みます。失敗する hook は Claude Code を壊しますが、それは観測ツールのやることではありません。
- **ストレージは短命。** DuckDB DB は開発ループ観測向けで、長期アーカイブではありません。ファイルが数 GB を超えたらローテートか Parquet 書き出しを想定してください。
- **ダークファースト UI。** [`design-tokens.md`](design-tokens.md) を参照してください。

---

## バックグラウンドサービス運用

まっさらな環境でのフルセットアップは次のとおりです。

```sh
brew install BIwashi/tap/apogee
apogee init                  # hook を ~/.claude/settings.json に書き込む
apogee daemon install        # launchd / systemd --user ユニットを登録
apogee daemon start          # いま起動。以降はログインのたびに自動起動
apogee open                  # http://127.0.0.1:4100 をブラウザで開く
```

macOS ならメニューバーアプリも同居で走らせられます。

```sh
apogee menubar &
```

メニューバーは daemon の HTTP サーフェスをポーリングし、コンパクトな状態表示をステータスバーに描画します。詳細は [`menubar.md`](menubar.md) を参照してください。

apogee を完全に取り除くには:

```sh
apogee uninstall             # daemon を停止、hook を剥がし、データ削除前に確認
apogee uninstall --purge     # さらに ~/.apogee を丸ごと削除
```

---

## 次に読むべきドキュメント

- [`cli.md`](cli.md) — すべてのサブコマンドとフラグ
- [`hooks.md`](hooks.md) — hook の契約とワイヤー形式
- [`interventions.md`](interventions.md) — オペレーター発のメッセージ
- [`daemon.md`](daemon.md) — launchd / systemd スーパーバイザ
- [`menubar.md`](menubar.md) — macOS ステータスバーアプリ
- [`data-model.md`](data-model.md) — DuckDB スキーマリファレンス
- [`otel-semconv.md`](otel-semconv.md) — `claude_code.*` 属性
- [`design-tokens.md`](design-tokens.md) — ビジュアルシステム仕様
