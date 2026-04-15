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
- **さっきのセッション全体では、何が起きた？** 三層構造の LLM サマライザが、ターン単位の recap（Haiku）、セッション単位のナラティブ rollup（Sonnet）、そして tier-3 の **フェーズナラティブ** — ターンを意味的なチャンク (implement / review / debug / plan / test / commit / delegate / explore) にグルーピングし、フェーズごとに headline、1〜3 文の narrative、key_steps を生成 — をすべてローカルの `claude` CLI で生成します。Anthropic API キーは不要です。

### サマライザのカスタマイズ

サマライザは出力言語、任意のオペレーター用システムプロンプト、任意のモデルオーバーライドを `user_preferences` DuckDB テーブルからジョブごとに読みます。管理方法は 2 つあります:

- **設定ページ** (`/settings`) には専用の **Summarizer** セクションがあり、言語トグル、recap / rollup のモデルオーバーライド入力、2 つのシステムプロンプトテキストエリア（各 2048 文字まで）が並びます。
- トップリボンのコンパクトな **EN / JA 言語ピッカー**で、recap + rollup の出力言語をワンクリックで切り替えられます。`EN ▸ JA` にすればスキーマを変えずに次の recap から日本語になります。

どちらの操作も同じ `PATCH /v1/preferences` エンドポイントに書き込むので、スクリプトでの一括設定にも対応しています。HTTP の完全な仕様と検証ルールは [`docs/cli_ja.md`](docs/cli_ja.md#summarizer-設定) を参照してください。

**フェーズナラティブ** (tier 3) は専用の設定キー — `summarizer.narrative_system_prompt` と `summarizer.narrative_model`（既定値 `claude-sonnet-4-6`）— と、`POST /v1/sessions/:id/narrative` の手動再生成ルートを備えています。スキーマ、チェイニング、陳腐化ガード、コスト見積もりの詳細は [`docs/narrative_ja.md`](docs/narrative_ja.md) を参照してください。

---

## 主な機能

| 画面 / サーフェス | 内容 |
|---|---|
| Live ページ | フォーカスカード駆動のランディング画面。実行中ターンをヒーロー表示し、フレームグラフ・recap 見出し・現在の phase と tool・ターン詳細へ飛ぶ CTA を並べます。縦の triage レールには実行中ターンを持つセッションが attention 順に並びます。 |
| Sessions カタログ | 収集済みセッションの検索・フィルタ可能な一覧（Datadog の Service Catalog に相当）。 |
| Agents | エージェントごとのビュー。main / subagent の分割、呼び出し回数、滑走平均時間、親 → 子のツリー表示。 |
| Insights | 集計分析。エラー率、継続時間パーセンタイル、上位ツール、上位 phase、直近 24 時間の watchlist セッション。 |
| イベントブラウザ | `/events/` — 保存されたフックイベントをページネーション付きで一覧（1 ページ 50 件、Prev / Next、URL バックのページ番号、サイドドロワーで JSON インスペクタ）。Live ダッシュボードのイベントティッカーは 180 px の固定高さ＋内部スクロールに変更され、新しいイベントが届いてもページが押し下がらなくなりました。 |
| Settings | コレクターのビルド情報と OTel エクスポータの状態。config パスや daemon / hook のインストール導線もこの画面から辿れます。 |
| Session 詳細 | Timeline タブ（フェーズナラティブ）を既定表示。セッション単位の rollup、スコープ付き KPI、attention 順に並ぶ全ターンも引き続き利用可能。 |
| Turn 詳細 | スイムレーン、span ツリー、recap パネル、attention の根拠、HITL キュー。 |
| コマンドパレット | セッション・スコープ・最近のプロンプトを横断するファジー検索（⌘K）。 |
| Recap ワーカー | ターンごとの構造化 recap をローカル `claude` CLI（Haiku）で生成。 |
| Rollup ワーカー | セッションごとのナラティブダイジェストをローカル `claude` CLI（Sonnet）で生成。 |
| フェーズナラティブ | すべての closed ターンを意味的なフェーズにグルーピングする tier-3 ワーカー。headline、1〜3 文の narrative、key_steps、kind チップ、duration、tool summary を備え、`/session` の Timeline タブにクリック可能なカードとサイドドロワー詳細で表示されます。 |
| HITL キュー | 権限要求をファーストクラスのレコードとして扱い、オペレーターの判断を構造化して保持。 |
| Operator Interventions | 実行中の Claude Code セッションへ自由文のメッセージを投入。次の `PreToolUse` / `UserPromptSubmit` フックが、それを `{"decision":"block","reason":...}` または追加コンテキストとして Claude Code に返します。 |
| OpenTelemetry | OTLP gRPC / HTTP エクスポート、完全な `claude_code.*` semconv レジストリ。 |
| フックエントリポイント | `apogee hook --event X` — バイナリそのものが hook です。Python 依存は一切ありません。 |
| バックグラウンドサービス | `apogee daemon {install,uninstall,start,stop,restart,status}` — launchd（macOS）/ systemd `--user`（Linux）。lipgloss のスタイリングで色分けされた出力。 |
| macOS メニューバー | `apogee menubar` — ローカルコレクターをポーリングするネイティブのステータスアイテム。 |
| Doctor | `apogee doctor` — 7 つの環境チェック（home / claude CLI / db path / config / DB lock / collector / hook install）。`--json` で機械可読出力。 |
| CLI | `serve`, `init`, `hook`, `daemon`, `status`, `logs`, `open`, `uninstall`, `menubar`, `doctor`, `version`。単一バイナリ、Node / Python ランタイムなし。 |
| デザイン | ライト / ダーク両テーマを搭載。`prefers-color-scheme` を自動検出し、トップリボンのトグルで切り替えできます。詳細は [`docs/design-tokens_ja.md`](docs/design-tokens_ja.md) を参照。 |

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
                                │  │ /events/     ページネーション付きブラウザ │ │
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

永続化は DuckDB で、`spans` / `logs` / `metric_points` など OTel 互換のテーブルに加え、ダッシュボードの高速読み出し用に非正規化した `sessions` / `turns` / `hitl_events` / `session_rollups` / `interventions` / `task_type_history` を持ちます。`turns` の行には導出カラム `attention_state` / `attention_reason` / `phase` / `recap_json` も書き戻されます。詳しくは [`docs/architecture_ja.md`](docs/architecture_ja.md) と [`internal/store/duckdb/schema.sql`](internal/store/duckdb/schema.sql) を参照してください。

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

ハッピーパスはコマンド 2 つです。このセクションの残りはそれぞれのコマンドが **実際に何をするか**、**うまく動くと何が見えるか**、**うまく動かなかったときの復旧手順** を説明しています。

### 1. バイナリをインストール

```sh
brew install BIwashi/tap/apogee
```

他のインストール手段:

| 配布元 | コマンド | 備考 |
|---|---|---|
| Homebrew tap | `brew install BIwashi/tap/apogee` | 推奨。ユニバーサルバイナリで埋め込みダッシュボードも含まれます。アップデートは `brew upgrade apogee`。 |
| `go install` | `go install github.com/BIwashi/apogee/cmd/apogee@latest` | Go モジュールプロキシは Next.js バンドルを配布できないので、UI は `make web-build` を促すプレースホルダーになります。API は完全に動作します。CLI だけ欲しい場合に使ってください。 |
| リリース tarball | [Releases](https://github.com/BIwashi/apogee/releases) からダウンロード | darwin amd64 / arm64 対応。Linux は v0.2.0 で対応予定。 |
| ソースビルド | `git clone ... && make build` | `./bin/apogee` にコミットハッシュとビルド時刻入りのバイナリが出ます。 |

確認:

```sh
$ apogee version
apogee v0.1.6 (commit 7345124, built 2026-04-15T05:39:33Z, go1.25.0)
```

### 2. オンボーディングウィザードを走らせる

```sh
apogee onboard
```

`apogee onboard` はセットアップのすべてのステップを 1 回のシェル実行でまとめて対話的に処理するコマンドです。**再実行前提**で設計されていて、各プロンプトのデフォルトはディスク上の現在の状態（`~/.apogee/config.toml` + DuckDB preferences + `~/.claude/settings.json` + 実行中の daemon 状態）から読み込みます。バージョンアップ後や設定変更後に再実行しても安全です。

ウィザードは 5 つのことをやります:

1. **Claude Code hooks** — `~/.claude/settings.json` に各 hook イベントごとのエントリを書き込み、現在の `apogee` バイナリの `apogee hook --event X` サブコマンドを指させます。既定で user スコープなので、このマシン上のすべてのプロジェクトが同じコレクターに送信されます。`source_app` ラベルは hook 発火時に `$APOGEE_SOURCE_APP` → `git rev-parse --show-toplevel` → `$PWD` の順で自動決定されるので、プロジェクトごとの設定は不要です。
2. **バックグラウンドサービス (daemon)** — macOS では `~/Library/LaunchAgents/dev.biwashi.apogee.plist` として `launchd` user agent に、Linux では `~/.config/systemd/user/apogee.service` として `systemd --user` ユニットに登録します。ログは `~/.apogee/logs/apogee.{out,err}.log` に出ます。ログイン時に自動起動し、クラッシュしても自動復旧します。
3. **LLM サマライザー** — 出力言語 (`en` / `ja`) と任意のシステムプロンプトを聞きます。per-turn recap (Haiku)、per-session rollup (Sonnet)、phase narrative (Sonnet) のそれぞれに別々のシステムプロンプトを設定できます。設定は DuckDB の `user_preferences` に保存され、後でダッシュボードの Settings ページから変更できます。
4. **OpenTelemetry エクスポート** (任意) — Tempo / Honeycomb / Datadog などの外部コレクターへトレースを転送したい場合は、ここで OTLP エンドポイントとプロトコルを設定します。
5. **ダッシュボードを開く** — デーモンを起動し、既定のブラウザで `http://127.0.0.1:4100/` を開きます。

クリーンなマシンで実行した例（すべてのデフォルトを受け入れる場合）:

```
APOGEE ONBOARD

  This will:
    1. Install hooks into ~/.claude/settings.json
    2. Install apogee as a user-scope background service
    3. Configure the LLM summarizer
    4. Optionally wire an OTLP endpoint
    5. Start the daemon and open the dashboard

? Install Claude Code hooks at user scope?          › Yes
? Install apogee as a background service?           › Yes
? Start the daemon immediately after install?       › Yes
? Summarizer output language                        › ja
? Recap system prompt (optional, leave empty)       › (空)
? Rollup system prompt (optional, leave empty)      › (空)
? Narrative system prompt (optional, leave empty)   › (空)
? Forward traces to an external OTLP endpoint?      › No
? Open the dashboard after everything is wired?     › Yes

? Apply these changes?                              › Yes

Applying...
  ✓ wrote ~/.apogee/config.toml
  ✓ installed 12 hook events into ~/.claude/settings.json
  ✓ installed launchd unit dev.biwashi.apogee
  ✓ wrote summarizer preferences (language=ja)
  ✓ daemon started (pid 62341)
  ✓ opened http://127.0.0.1:4100/ in your browser

apogee is ready.
  Run `apogee status` to check the daemon.
  Run `apogee doctor` to verify the environment.
```

非対話モード / スクリプト用のフラグ:

| フラグ | 動作 |
|---|---|
| `--yes` / `--non-interactive` | すべてのプロンプトをスキップし、現在の状態をそのままデフォルトとして適用。プロビジョニング / CI 用。 |
| `--dry-run` | プランを表示するだけで何も書き込まない。 |
| `--skip-hooks` | `~/.claude/settings.json` を触らない。 |
| `--skip-daemon` | デーモンをインストール・起動しない。 |
| `--skip-summarizer` | サマライザー設定を書き換えない。 |
| `--skip-telemetry` | `[telemetry]` ブロックを触らない。 |

完全なウォークスルーとフラグリファレンスは [`docs/onboard_ja.md`](docs/onboard_ja.md) にあります。

### 3. Claude Code セッションを起動する

デーモンはこの時点で `http://127.0.0.1:4100` で待機していて、このマシン上のすべての Claude Code セッションがそこへ送信されます。任意のプロジェクトでセッションを開始してください。

```sh
cd ~/work/my-project
claude
```

数秒以内にダッシュボードが反応します:

- **Live ページ** (`/`): 左のトリアージレールに実行中のターンが attention 状態順で並び、右の **Focus Card** が選択中ターンのフレームグラフ、現在のフェーズ、現在のツール、per-turn recap の見出しをリアルタイムに表示します。上部のイベントティッカーは高さ固定なので、新しいイベントが流れてきてもページがシフトしません。
- **トップリボン**: **LIVE** のドットはアプリレベルの SSE プロバイダに持ち上がっているので、ルート遷移をしても緑のままです。4 つのセレクター (source_app / session / time range / language) と、テーマトグル (System / Light / Dark) がここに並びます。
- **セッション詳細** (`/session/?id=<id>`): 既定で **Timeline タブ** が開きます。タイムラインは LLM が分類したフェーズ (implement / review / debug / plan / test / commit / delegate / explore / other) をクリッカブルカードでレンダリングします。カードに 350ms ホバーするとプレビューが表示され、クリックすると右側ドロワーが開いてフェーズ全文・キーステップ・ツール使用量のバーチャート・フェーズ内のターン一覧が見えます。
- **ターン詳細** (`/turn/?sess=<sess>&turn=<turn>`): swim lane + span tree + recap 3 パネル + HITL キュー + オペレーター介入コンポーザー。
- サイドバーから **Sessions / Agents / Insights / Events / Settings** ページへ。

### 4. 環境を検証する

セットアップがおかしいと感じたら、いつでも実行できます:

```sh
$ apogee doctor

apogee doctor

  ✓ /Users/shota/.apogee writable
  ✓ claude CLI on PATH (/Users/shota/.local/bin/claude)
  ✓ default db path /Users/shota/.apogee/apogee.duckdb
  ✓ no config file (defaults in use)
  ✓ DuckDB file is unlocked
  ✓ collector ok (http://127.0.0.1:4100/v1/healthz, 1 ms)
  ✓ apogee hook installed for 12/12 events

7 ok · 0 warnings · 0 errors
```

スクリプトや `apogee menubar` から読みたい場合は `--json` を付けてください。

### 5. 日常の操作

```sh
apogee status          # デーモン + コレクター + 最近のアクティビティ一発確認
apogee logs -f         # デーモンの stdout + stderr を tail
apogee open            # 既定ブラウザで http://127.0.0.1:4100/ を開く
apogee daemon restart  # バージョンアップ後や設定変更後

apogee daemon stop     # サービス停止（アンインストールはしない）
apogee uninstall       # デーモン + hooks を消す、任意で ~/.apogee も削除
```

macOS なら、ライブステータスアイコンとクイックアクションのためにメニューバーアプリも起動できます:

```sh
apogee menubar &
```

### トラブルシューティング

**起動時に `DuckDB file is locked` が出る。**

他の apogee プロセスが DB を掴んでいます。だいたいは daemon インストール前の `apogee serve` が残っているケースです。kill して再起動:

```sh
pkill -f "apogee serve"
lsof /Users/shota/.apogee/apogee.duckdb   # 何も出ないことを確認
apogee daemon restart
```

2 回目以降の起動では apogee が `<db>.apogee.lock` + `<db>.apogee.pid` のサイドカーファイルを書き、コンフリクトが起きたら PID・パス・3 ステップの修復手順が入った整形済みのエラーボックスを表示します。DuckDB の生エラーがオペレーターに届くことはありません。

**`LIVE` インジケーターが赤い。**

コレクターに到達できていません。`apogee daemon status` を実行し、デーモンが動いていなければ `apogee daemon start`。動いているのにコレクターが応答しない場合は `apogee logs -f` で `apogee.err.log` の出力を確認してください。

**hooks は入っているのにダッシュボードに何も流れてこない。**

`apogee doctor` の `apogee hook installed for 12/12 events` 行が緑になっているか確認してください。`apogee init` / `apogee onboard` を走らせる前から Claude Code が起動していた場合は、`~/.claude/settings.json` の変更を取り込むために Claude Code を再起動してください。

**`go install` ユーザがプレースホルダーダッシュボードを見る。**

これは仕様です。Next.js の静的エクスポートは Go モジュールプロキシを通せません。ローカルチェックアウトで `make web-build` を実行するか、`brew` / リリース tarball 経由で再インストールしてください。いずれも完全なダッシュボードを含みます。

**デーモンが再起動を繰り返す。**

`apogee logs -f` で終了理由が出ます。もっとも多いのは DuckDB のロック競合（上記）。次に多いのは `brew uninstall` 後に残った plist が削除済みバイナリを指しているケースで、`apogee daemon uninstall && apogee daemon install` で plist を書き直せば直ります。

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

reconstructor の書き込みは、SDK 経由で本物の OTel span にもミラーされます。これにより apogee は任意のバックエンド（Tempo、Honeycomb、Datadog など）に対する OTLP ソースとしても使えます。`claude_code.*` 属性は [`semconv/`](semconv/) に同梱されたバージョン付きの semconv レジストリに従い、[`docs/otel-semconv_ja.md`](docs/otel-semconv_ja.md) に詳細がまとまっています。`OTEL_EXPORTER_OTLP_ENDPOINT`（または TOML の同等項目）を設定すれば、コレクターは自動でエクスポートします。

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
                    otel-semconv。日本語ミラーは docs/*_ja.md として併置
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
apogee daemon status
```

`apogee daemon install` はスタイル付きの成功ボックスを表示します（NO_COLOR=1 で取得した素のサンプル。TTY ではボーダー / 文字色がボールドで色付けされます）:

```
╭───────────────────────────────────────────────────────────────────────╮
│ ✓ daemon installed                                                    │
│                                                                       │
│ Label:      dev.biwashi.apogee                                        │
│ Unit path:  /Users/me/Library/LaunchAgents/dev.biwashi.apogee.plist   │
│ Collector:  http://127.0.0.1:4100                                     │
│ Logs:       /Users/me/.apogee/logs/apogee.{out,err}.log               │
│                                                                       │
│ The daemon will start automatically on next login. To start it now:   │
│   apogee daemon start                                                 │
╰───────────────────────────────────────────────────────────────────────╯
```

`apogee daemon status` は Daemon ボックス（info ボーダー）と Collector ボックス（到達可能なら success、失敗なら error ボーダー）の 2 セクション構成です:

```
Daemon: dev.biwashi.apogee
╭─────────────────────────────────────────────────────────────────────────╮
│ Status:      running                                                    │
│ Installed:   yes                                                        │
│ Loaded:      yes                                                        │
│ Running:     yes                                                        │
│ PID:         12345                                                      │
│ Started at:  2026-04-15 13:01:20                                        │
│ Uptime:      1h 12m 4s                                                  │
│ Last exit:   0                                                          │
│ Unit path:   /Users/me/Library/LaunchAgents/dev.biwashi.apogee.plist    │
│ Logs:        ~/.apogee/logs/apogee.{out,err}.log                        │
╰─────────────────────────────────────────────────────────────────────────╯

Collector: http://127.0.0.1:4100
╭───────────────────────────────────────────────╮
│ Endpoint:  http://127.0.0.1:4100              │
│ Health:    ok                                 │
│ Detail:    ok (HTTP 200, 3 ms)                │
│ Latency:   3ms                                │
╰───────────────────────────────────────────────╯
```

停止・再起動・ログ追従:

```sh
apogee daemon stop      # ✓ daemon stopped (dev.biwashi.apogee)
apogee daemon restart   # ✓ daemon restarted (dev.biwashi.apogee)
apogee logs -f          # ~/.apogee/logs/apogee.{out,err}.log を tail
apogee open             # http://127.0.0.1:4100 をブラウザで開く
```

`apogee logs -f` は両方のストリームを最後 50 行から追従します:

```
==> /Users/me/.apogee/logs/apogee.out.log <==
{"time":"2026-04-15T13:01:38+09:00","level":"INFO","msg":"collector listening","addr":"127.0.0.1:4100"}
```

apogee を完全に取り除くには:

```sh
apogee uninstall            # daemon を停止、hook を剥がし、データ削除前に確認
apogee uninstall --purge    # さらに ~/.apogee も丸ごと削除
```

`apogee daemon uninstall`（`apogee uninstall` から呼ばれます）は info ボックスを表示します:

```
╭─────────────────────────────╮
│ daemon uninstalled          │
│                             │
│ Label:  dev.biwashi.apogee  │
╰─────────────────────────────╯
```

ユニットファイルは macOS では `~/Library/LaunchAgents/dev.biwashi.apogee.plist`、Linux では `~/.config/systemd/user/apogee.service` に置かれます。完全な運用チートシートは [`docs/daemon_ja.md`](docs/daemon_ja.md)、`apogee doctor` の全チェックは [`docs/doctor.md`](docs/doctor.md) を参照してください。

`assets/screenshots/` 配下のスクリーンショットを再生成するには:

```sh
bash scripts/capture-screenshots.sh
```

このスクリプトはインメモリ DB でコレクターを立ち上げ、フィクスチャバッチを POST し、playwright 経由で Chromium を駆動します。

---

## トラブルシューティング

### DuckDB ロックの競合

apogee は DuckDB ファイルの隣にサイドカーロック（`<db>.apogee.lock`）と pid ファイル（`<db>.apogee.pid`）を書き込みます。同じ DB を指すコレクターを誤って 2 つ起動すると、2 つ目はスタイル付きエラーボックスを表示して exit 1 で終了します（生の driver エラーは出ません）:

```
╭──────────────────────────────────────────────────────────╮
│ Another apogee process is already using the DuckDB file. │
│                                                          │
│ Path:    /Users/me/.apogee/apogee.duckdb                 │
│ Holder:  apogee (pid 12345)                              │
│                                                          │
│ To fix:                                                  │
│   1. apogee daemon stop                                  │
│   2. or: kill 12345                                      │
│   3. or: apogee serve --db <alt path>                    │
╰──────────────────────────────────────────────────────────╯
```

`apogee daemon stop`（マネージドでない場合は `kill <pid>`）を実行してから再度コマンドを叩いてください。Holder の PID は可能な場合 `lsof -nP <db>` で検出され、フォールバックとして pid ファイルを参照します。

### daemon が起動しない

- `apogee daemon status` でインストール / ロード状態と Collector ボックスを確認。
- `apogee logs -f` で `~/.apogee/logs/apogee.{out,err}.log` を tail。
- launchd: `launchctl print gui/$(id -u)/dev.biwashi.apogee` でスーパーバイザの観測値を確認。
- systemd: `journalctl --user -u apogee.service -f` でユニットログを確認。

### Hook が発火しない

`apogee doctor` を実行してください。`hook_install` チェックが `~/.claude/settings.json` を読み、`internal/cli/init.go::HookEvents` の各イベントが apogee バイナリを指しているかを確認します:

```
apogee doctor

  ✓ /Users/me/.apogee writable
  ✓ claude CLI on PATH (/Users/me/.local/bin/claude)
  ✓ default db path /Users/me/.apogee/apogee.duckdb
  ✓ no config file (defaults in use) (/Users/me/.apogee/config.toml)
  ✓ DuckDB file is unlocked
  ⚠ collector not running (http://127.0.0.1:4100/v1/healthz)
  ✓ apogee hook installed for 12/12 events

5 ok · 1 warning · 0 errors
```

`apogee doctor --json` は同じチェックを CI / scripts / `apogee menubar` 向けの JSON 配列で出力します。`hook_install` が partial / missing と報告されたら `apogee init --force` で再書き込みしてください。

---

## ライセンス

Apache License 2.0。詳細は [LICENSE](LICENSE) を参照してください。
