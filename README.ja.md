<p align="right"><a href="README.md">English</a> / 日本語</p>

<p align="center">
  <img src="assets/branding/apogee-banner.png" alt="apogee" width="600">
</p>

<p align="center">
  <strong>Claude Code エージェント群を、もっとも高い視点から眺める。</strong>
</p>

<p align="center">
  <img src="assets/screenshots/dashboard-overview.png" alt="apogee live triage dashboard" width="100%">
  <br>
  <em>Live フォーカスダッシュボード。実行中のターンがヒーロー表示され、その横の triage レールには「実行中ターンのあるセッション」が attention 順に並びます。</em>
</p>

apogee は、マルチエージェントの [Claude Code](https://docs.claude.com/en/docs/claude-code) セッションを観測するための単一バイナリのダッシュボードです。あらゆるフックイベントを取り込み、OpenTelemetry 互換のトレースに組み立て、DuckDB に保存し、Go バイナリに埋め込まれた NASA 調の Next.js ダッシュボードへ SSE で配信します。

> [!WARNING]
> apogee は現在も活発に開発中です。最初のタグ付きリリースまで、API・スキーマ・ディスク上のフォーマットはコミット間で互換性なく変わり得ます。

---

## なぜ apogee なのか

マルチエージェントの Claude Code ワークフローを回していると、各エージェントが「いま何をやっているのか」がすぐに見えなくなります。どのツールが走っているのか、どの権限要求が出ているのか、どのコマンドがブロックされたのか、どの subagent が詰まっているのか。apogee はこの 3 つの問いに一目で答えます。

- **いま、どこを見ればいい？** ルールベースの attention エンジンが、実行中ターンを `healthy / watchlist / watch / intervene_now` の 4 つに振り分け、一番うるさいものを常にリストの先頭へ並べ替えます。
- **このターンは、この瞬間、何をしている？** phase ヒューリスティック（plan / explore / edit / test / commit / delegate）と、全ツール・subagent・HITL をひとつの時間軸に描くライブスイムレーンを備えています。
- **さっきのセッション全体では、何が起きた？** 二層構造の LLM サマライザ が、ターン単位の recap（Haiku）とセッション単位のナラティブ rollup（Sonnet）をローカルの `claude` CLI 経由で生成します。Anthropic API キーは不要です。

---

## 主な機能

| 画面 / サーフェス | 内容 |
|---|---|
| Live ページ | フォーカスカード駆動のランディング画面。実行中ターンをヒーロー表示し、フレームグラフ・recap 見出し・現在の phase と tool・ターン詳細へ飛ぶ CTA を並べます。縦の triage レールには実行中ターンを持つセッションが attention 順に並びます。 |
| Sessions カタログ | 収集済みセッションの検索・フィルタ可能な一覧（Datadog の Service Catalog に相当）。 |
| Agents | エージェントごとのビュー。main / subagent の分割、呼び出し回数、滑走平均時間、親 → 子のツリー表示。 |
| Insights | 集計分析。エラー率、継続時間パーセンタイル、上位ツール、上位 phase、直近 24 時間の watchlist セッション。 |
| Settings | コレクターのビルド情報と OTel エクスポータの状態。config パスや daemon / hook のインストール導線もこの画面から辿れます。 |
| Session 詳細 | セッション単位の rollup、スコープ付き KPI、attention 順に並ぶ全ターン。 |
| Turn 詳細 | スイムレーン、span ツリー、recap パネル、attention の根拠、HITL キュー。 |
| コマンドパレット | セッション・スコープ・最近のプロンプトを横断するファジー検索（⌘K）。 |
| Recap ワーカー | ターンごとの構造化 recap をローカル `claude` CLI（Haiku）で生成。 |
| Rollup ワーカー | セッションごとのナラティブダイジェストをローカル `claude` CLI（Sonnet）で生成。 |
| HITL キュー | 権限要求をファーストクラスのレコードとして扱い、オペレーターの判断を構造化して保持。 |
| Operator Interventions | 実行中の Claude Code セッションへ自由文のメッセージを投入。次の `PreToolUse` / `UserPromptSubmit` フックが、それを `{"decision":"block","reason":...}` または追加コンテキストとして Claude Code に返します。 |
| OpenTelemetry | OTLP gRPC / HTTP エクスポート、完全な `claude_code.*` semconv レジストリ。 |
| フックエントリポイント | `apogee hook --event X` — バイナリそのものが hook です。Python 依存は一切ありません。 |
| バックグラウンドサービス | `apogee daemon {install,start,stop,restart,status}` — launchd（macOS）/ systemd `--user`（Linux）。 |
| macOS メニューバー | `apogee menubar` — ローカルコレクターをポーリングするネイティブのステータスアイテム。 |
| CLI | `serve`, `init`, `hook`, `daemon`, `status`, `logs`, `open`, `uninstall`, `menubar`, `doctor`, `version`。単一バイナリ、Node / Python ランタイムなし。 |

<p align="center">
  <img src="assets/screenshots/session-detail.png" alt="session detail" width="49%">
  <img src="assets/screenshots/turn-detail.png" alt="turn detail" width="49%">
  <br>
  <em>セッション rollup とターン単位のスイムレーン。どちらもローカルの claude CLI で生成されています。</em>
</p>

---

## アーキテクチャ

```
┌────────────────────────┐      ┌──────────────────────────────────────────────┐
│  Claude Code hooks     │      │  apogee collector  (single Go binary)         │
│  `apogee hook --event` │─POST─│                                               │
│  12 hook events        │ JSON │  ┌─ ingest ──────────────────────────────┐   │
└────────────────────────┘      │  │ reconstructor: hook → OTel spans      │   │
                                │  │ セッション別 agent stack + 保留中      │   │
                                │  │ tool_use_id マップ                     │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ store/duckdb ─▼──────────────────────┐   │
                                │  │ sessions · turns · spans · logs ·      │   │
                                │  │ metric_points · hitl_events ·          │   │
                                │  │ session_rollups · interventions ·      │   │
                                │  │ task_type_history                      │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ attention ────▼──────────────────────┐   │
                                │  │ ルールエンジン + phase ヒューリスティック│  │
                                │  │ + 履歴ベースの watchlist                │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ summarizer ───▼──────────────────────┐   │
                                │  │ recap worker   (Haiku, ターン単位)     │   │
                                │  │ rollup worker  (Sonnet, セッション単位) │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ interventions ▼──────────────────────┐   │
                                │  │ queued → claimed → delivered → consumed│  │
                                │  │ atomic claim + 自動 expire スイーパ    │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ sse ──────────▼──────────────────────┐   │
                                │  │ hub + /v1/events/stream                │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ telemetry ────▼──────────────────────┐   │
                                │  │ OTel SDK + OTLP gRPC/HTTP exporter     │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ web (Next.js static, embed.FS) ──────▼─┐ │
                                │  │ /            Live フォーカス画面         │ │
                                │  │ /sessions/   サービスカタログ           │ │
                                │  │ /session?id= セッション詳細 + rollup    │ │
                                │  │ /turn?sess=  ターン詳細 + operator queue │ │
                                │  │ /agents      main / subagent ビュー     │ │
                                │  │ /insights    集計分析                   │ │
                                │  │ /settings    コレクター情報 + OTel      │ │
                                │  └─────────────────────────────────────────┘ │
                                └──────────────────────────────────────────────┘

                                   ┌────────────┐              ┌─────────────┐
                                   │ daemon     │──launchctl──▶│ launchd     │
                                   │ supervisor │──systemctl──▶│ systemd user│
                                   └────────────┘              └─────────────┘
                                   ┌────────────┐
                                   │ menubar    │ (macOS ステータスアイテム)
                                   └────────────┘
```

### データモデル

apogee は **Claude Code の 1 ユーザーターンを 1 OpenTelemetry トレース**として扱います。

```
trace = claude_code.turn  (root span。UserPromptSubmit で開き、Stop で閉じる)
├── span  claude_code.tool.Bash
├── span  claude_code.tool.Read
├── span  claude_code.subagent.Explore      (subagent の子)
│   ├── span  claude_code.tool.Grep
│   └── span  claude_code.tool.Read
├── span  claude_code.hitl.permission       (人間が応答するまで開きっぱなし)
└── span event  claude_code.notification
```

永続化は DuckDB で、`spans` / `logs` / `metric_points` など OTel 互換のテーブルに加え、ダッシュボードの高速読み出し用に非正規化した `sessions` / `turns` / `hitl_events` / `session_rollups` / `interventions` / `task_type_history` を持ちます。`turns` の行には導出カラム `attention_state` / `attention_reason` / `phase` / `recap_json` も書き戻されます。詳しくは [`docs/ja/architecture.md`](docs/ja/architecture.md) と [`internal/store/duckdb/schema.sql`](internal/store/duckdb/schema.sql) を参照してください。

---

## ステータス

| 領域 | 状態 |
|---|---|
| モノレポ雛形 + デザインシステム | shipped |
| コレクター中核: DuckDB + トレース reconstructor | shipped |
| SSE fan-out + Live ダッシュボード骨格 | shipped |
| attention エンジン + KPI ストリップ | shipped |
| ターン詳細 + スイムレーン + フィルターチップ | shipped |
| LLM サマライザ (ターンごと Haiku, セッションごと Sonnet) | shipped |
| 構造化レコードとしての HITL | shipped |
| OpenTelemetry semconv レジストリ + OTLP エクスポート | shipped |
| 埋め込みフロントエンド + CLI 配布 | shipped |
| README / スクリーンショット / セッション rollup 仕上げ | shipped |
| Operator Interventions (バックエンド + UI) | shipped |
| Go ネイティブの hook、Python ライブラリの削除 | shipped |
| Daemon (launchd / systemd `--user`) | shipped |
| macOS メニューバーアプリ | shipped |
| UI リデザイン — Live フォーカス、情報設計の見直し | shipped |

次に何が入るかは [open pull requests](https://github.com/BIwashi/apogee/pulls) を確認してください。

---

## クイックスタート

```sh
# 1. インストール（Homebrew tap、go install、ソースビルドのいずれか）。
brew install BIwashi/tap/apogee
# あるいは
go install github.com/BIwashi/apogee/cmd/apogee@latest

# 2. コレクターを起動し、このマシンの全プロジェクト向けに hook を一度だけ入れる。
apogee serve &
apogee init

# 3. ダッシュボードを開く。
open http://localhost:4100
```

これだけです。`apogee init` は既定で **user スコープ**（`~/.claude/settings.json`）にインストールするので、このマシン上の Claude Code セッションはすべて同じコレクターに送信されます。`source_app` ラベルは hook 発火時に、次の順序で動的に決まります。

1. `$APOGEE_SOURCE_APP` — 明示的な上書き。
2. `basename $(git rev-parse --show-toplevel)` — git リポジトリ内のセッションの場合。
3. `basename $PWD` — フォールバック。

つまり `~/work/newmo-backend` で `claude` を起動すれば自動的に `source_app=newmo-backend` として、`~/work/apogee` で起動すれば `source_app=apogee` としてラベル付けされます。再設定は不要です。

固定ラベルで上書きしたい場合は `apogee init --source-app my-project` を使ってください。プロジェクト単位のインストールが必要なら `apogee init --scope project` で今も可能です。

> [!NOTE]
> `go install` で作ったバイナリの埋め込みダッシュボードはプレースホルダーページです。API は完全に動作しますが、UI はローカルで `make web-build` を走らせるか、リリースバイナリを使うように案内するスタブになります。Next.js の静的エクスポートは Go モジュールプロキシで配布できないためです。`brew install` やリリース tarball には常に完全なダッシュボードが含まれています。

コレクターが動き出したら、任意のプロジェクトで Claude Code を再起動すれば、すべての hook イベントがダッシュボードへ流れ始めます。

---

## 設定

apogee は任意で `~/.apogee/config.toml` を読み取ります。すべての値にデフォルトがあるので、このファイルは純粋に追加設定用です。

```toml
[telemetry]
enabled       = true
endpoint      = "https://otlp.example.com"
protocol      = "grpc"           # "grpc" または "http"
service_name  = "apogee"
sample_ratio  = 1.0

[summarizer]
enabled       = true
recap_model   = "claude-haiku-4-5"
rollup_model  = "claude-sonnet-4-6"
concurrency   = 1
timeout_seconds = 120

[daemon]
label         = "dev.biwashi.apogee"
addr          = "127.0.0.1:4100"
db_path       = "~/.apogee/apogee.duckdb"
log_dir       = "~/.apogee/logs"
keep_alive    = true
run_at_load   = true
```

すべての値は環境変数でも上書きできます（例: `APOGEE_RECAP_MODEL`, `APOGEE_ROLLUP_MODEL`, `OTEL_EXPORTER_OTLP_ENDPOINT`）。完全な一覧は `internal/summarizer/config.go` と `internal/telemetry/config.go` を参照してください。

---

## OpenTelemetry 連携

reconstructor の書き込みは、SDK 経由で本物の OTel span にもミラーされます。これにより apogee は任意のバックエンド（Tempo、Honeycomb、Datadog など）に対する OTLP ソースとしても使えます。`claude_code.*` 属性は [`semconv/`](semconv/) に同梱されたバージョン付きの semconv レジストリに従い、[`docs/ja/otel-semconv.md`](docs/ja/otel-semconv.md) に詳細がまとまっています。`OTEL_EXPORTER_OTLP_ENDPOINT`（または TOML の同等項目）を設定すれば、コレクターは自動でエクスポートします。

---

## リポジトリ構成

```
cmd/apogee/         Go エントリポイント（CLI + 埋め込みサーバー）
internal/
  attention/        ルールエンジン、phase ヒューリスティック、履歴読み出し
  cli/              cobra コマンド（serve / init / hook / daemon /
                    status / logs / open / uninstall / menubar /
                    doctor / version）
  collector/        chi ルーター、サーバー配線、SSE エンドポイント
  daemon/           launchd / systemd --user スーパーバイザ
  hitl/             HITL サービス: ライフサイクル、expire、応答 API
  ingest/           hook ペイロード型、ステートフル trace reconstructor
  interventions/    operator interventions サービス（queued → consumed）
  metrics/          metric_points に書き込むバックグラウンドサンプラ
  otel/             OTel 形式の Go モデル
  sse/              fan-out hub + イベントエンベロープ
  store/duckdb/     DuckDB スキーマ + クエリ + マイグレーション
  summarizer/       recap ワーカー (Haiku) + rollup ワーカー (Sonnet)
  telemetry/        OTel SDK プロバイダ、OTLP エクスポータ
  webassets/        Next.js 静的エクスポートを載せる embed.FS
  version/          ビルドバージョン定数
web/                Next.js 16 ダッシュボード（App Router, Tailwind v4）
  app/              ルートと React コンポーネント
  app/lib/          型付き API クライアント、SWR フック、デザイントークン
  public/fonts/     ディスプレイフォントアセット
assets/branding/    apogee バナー、ロゴ、アイコン
assets/screenshots/ コミット済みダッシュボードスクリーンショット
scripts/            screenshot キャプチャ（playwright）とフィクスチャ
semconv/            `claude_code.*` 向け OpenTelemetry semconv
                    （`apogee hook` が hook エントリポイント。
                    `hooks/` ディレクトリも Python 依存もありません）
docs/               architecture / cli / hooks / data-model /
                    design-tokens / daemon / menubar / interventions /
                    otel-semconv。日本語ミラーは docs/ja/ 以下
.github/workflows/  CI（Go の vet/build/test、web の typecheck/lint/build）
```

---

## ローカル開発

必要なもの: Go 1.24+、Node 20+、C ツールチェーン（DuckDB は cgo バインディングの `github.com/marcboeker/go-duckdb/v2` 経由で使っているため）。

```sh
# Go
go build ./...
go vet ./...
go test ./... -race -count=1

# Web (web/ 配下で実行)
npm install
npm run dev       # Next.js 開発サーバー (http://localhost:3000)
npm run typecheck
npm run lint
npm run build

# コレクターを走らせる（リポジトリルートから）
go run ./cmd/apogee serve --addr :4100 --db .local/apogee.duckdb
```

コレクター単体ではただのサーバーで、Claude Code セッションからイベントが流れ込まない限りダッシュボードは空のままです。コレクターが立ち上がったら、**ローカルで** ビルドしたバイナリ（brew でインストールしたものではなく）で hook を user スコープに一度だけインストールし、このマシン上のすべての Claude Code セッションを開発用コレクターへ向けます。

```sh
# コレクターが走っている状態で、user スコープに hook をインストール。
make build                    # ./bin/apogee を生成
./bin/apogee init             # ~/.claude/settings.json を書き換え
```

これ以降、任意のプロジェクトで `claude` を起動するとローカルコレクターへ流れ込み、ダッシュボードが点灯します。

Makefile を使うこともできます。

```sh
make build            # ./bin/apogee をビルド
make run-collector    # .local/apogee.duckdb を使ってコレクターを起動
make test             # go vet + レーステスト
make dev              # コレクターと Next.js 開発サーバーを同時起動
```

`make dev` はすでにコレクターと Next.js 開発サーバーの両方を起動するので、`make dev` + `./bin/apogee init` が新規コントリビューター向けの最小セットアップです。

> `make dev` が `:4100` で *"address already in use"* を出した場合、古いコレクターがポートを掴んだままです。`lsof -nP -iTCP:4100 -sTCP:LISTEN` で発見し、`pkill -f "apogee serve"` で止めてください。

---

## apogee をバックグラウンドサービスとして動かす

インストールが済んでいれば、apogee を launchd（macOS）または systemd の user サービス（Linux）として登録し、ログインのたびに自動起動するようにできます。

```sh
apogee daemon install
apogee daemon start
apogee status
```

停止・再起動・ログ追従も同じ流れです。

```sh
apogee daemon stop
apogee daemon restart
apogee logs -f
apogee open       # http://127.0.0.1:4100 をブラウザで開く
```

apogee を完全に取り除くには:

```sh
apogee uninstall            # daemon を停止、hook を剥がし、データ削除前に確認
apogee uninstall --purge    # さらに ~/.apogee も丸ごと削除
```

ユニットファイルは macOS では `~/Library/LaunchAgents/dev.biwashi.apogee.plist`、Linux では `~/.config/systemd/user/apogee.service` に置かれます。完全な運用チートシートは [`docs/ja/daemon.md`](docs/ja/daemon.md) を参照してください。

`assets/screenshots/` 配下のスクリーンショットを再生成するには:

```sh
bash scripts/capture-screenshots.sh
```

このスクリプトはインメモリ DB でコレクターを立ち上げ、フィクスチャバッチを POST し、playwright 経由で Chromium を駆動します。

---

## ライセンス

Apache License 2.0。詳細は [LICENSE](LICENSE) を参照してください。
