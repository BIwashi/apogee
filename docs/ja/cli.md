[English version / 英語版](../cli.md)

# apogee CLI リファレンス

v0.1.3 に同梱されているすべての `apogee` サブコマンドの正式リファレンスです。各コマンドの実装は [`internal/cli/`](../../internal/cli/) 以下にあり、[`internal/cli/root.go`](../../internal/cli/root.go) の cobra ツリーで公開されています。ヘルプ出力のスタイリングは [`charmbracelet/fang`](https://github.com/charmbracelet/fang) が担当し、TTY なら色付きのセクション見出しに、パイプ経由なら素のテキストに自動でフォールバックします。

```
Usage:
  apogee [command]

Available Commands:
  serve       コレクターと埋め込みダッシュボードを起動
  init        apogee hook を .claude/settings.json にインストール
  hook        Claude Code の hook ペイロードを apogee コレクターへ転送
  daemon      バックグラウンドサービスの install / start / stop / inspect
  status      daemon と HTTP の liveness を一括確認
  logs        daemon のログファイルを tail
  open        既定ブラウザでダッシュボードを開く
  uninstall   daemon 停止、hook 剥がし、任意でデータも削除
  menubar     macOS メニューバーアプリを実行（macOS 専用）
  doctor      ローカルインストールのヘルスチェック
  version     ビルドバージョン情報を表示

Flags:
  -h, --help      ヘルプを表示
  -v, --version   バージョンを表示
```

---

## apogee serve

コレクターと埋め込みダッシュボードを起動します。

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--addr` | `:4100` | HTTP リッスンアドレス |
| `--db` | `~/.apogee/apogee.duckdb` | DuckDB データベースファイル |
| `--config` | `~/.apogee/config.toml` | TOML 設定ファイル（任意） |

### 例

```sh
apogee serve --addr 127.0.0.1:4100 --db ~/.apogee/apogee.duckdb
```

### 備考

- 初回起動時にコレクターがデータベースファイルを作成し、マイグレーションを自動で適用します。
- `apogee daemon` スーパーバイザが launchd / systemd ユニットに書き込むのはまさにこのコマンドです。foreground 実行との違いはログが stdout / stderr に出るか、daemon ログファイルに出るかだけです。

---

## apogee init

`.claude/settings.json` に apogee hook エントリを書き込みます。

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--scope` | `user` | インストールスコープ。`user`（`~/.claude/settings.json`）か `project`（`./.claude/settings.json`） |
| `--target` | `` | ターゲットディレクトリを上書き。通常は不要 |
| `--server-url` | `http://localhost:4100/v1/events` | hook コマンドに書き込むコレクターのエンドポイント |
| `--source-app` | `` | `source_app` ラベルを固定。空（既定）なら hook が実行時に導出 |
| `--dry-run` | `false` | 書き込まずにプランだけ表示 |
| `--force` | `false` | 既存の apogee hook を確認なしに上書き。v0.1.x の `python3 send_event.py` 行もまとめて除去 |

### 例

```sh
# マシン上の全 Claude Code プロジェクトが 1 回のインストールで済む。
apogee init

# 書き込みなしで変更内容を確認。
apogee init --dry-run

# プロジェクトごとの自動導出ではなく、固定 source_app を使う。
apogee init --source-app my-team --force
```

### 備考

- 既定のスコープは `user`。マシン上のすべての Claude Code セッションが同じコレクターへ送るようになり、`source_app` は hook 側が実行時に `$APOGEE_SOURCE_APP`、git toplevel basename、`$PWD` の順で導出します（[`hooks.md`](hooks.md) 参照）。
- `settings.json` に書き込まれるコマンドは、実行中の `apogee` バイナリの絶対パスに `hook --event <X> --server-url ...` を付けたものです。Python は一切関与しません。
- 古い v0.1.x インストールで `python3 send_event.py` 行が残っていた場合、プラン出力が警告を出し、`--force` で置き換えます。

---

## apogee hook

Claude Code の hook ペイロードを apogee コレクターへ転送します。これが `.claude/settings.json` から呼び出されるコマンドです。バイナリ自身が hook なので、別スクリプトはありません。

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--event` | 必須 | hook イベント名（`PreToolUse`、`PostToolUse`、`UserPromptSubmit` など） |
| `--server-url` | `http://localhost:4100/v1/events` | コレクターエンドポイント |
| `--source-app` | `` | source_app ラベルを固定。空なら実行時に導出 |
| `--timeout` | `2s` | events POST の HTTP タイムアウト |

### 例

```sh
echo '{"session_id":"s-1","hook_event_type":"UserPromptSubmit"}' \
  | apogee hook --event UserPromptSubmit --server-url http://127.0.0.1:4100/v1/events
```

### 備考

- stdin から JSON の hook ペイロードを読み、コレクターへ POST し、Claude Code の hook パイプラインに影響を与えないよう stdin を stdout にエコーバックします。
- `PreToolUse` と `UserPromptSubmit` では、まず `POST /v1/sessions/{session_id}/interventions/claim` で operator intervention の claim を試みます。成功すれば Claude Code decision JSON が stdout にそのまま書かれ、stdin エコーは置き換えられます。
- 終了コードは常に 0 です。失敗する hook は Claude Code を壊すからです。
- ワイヤー契約の詳細は [`hooks.md`](hooks.md) を参照してください。

---

## apogee daemon

apogee をバックグラウンドサービスとして install / start / stop / inspect します。macOS は launchd、Linux は systemd `--user` を使います。

### サブコマンド

| コマンド | 説明 |
| --- | --- |
| `apogee daemon install` | ユニットファイルを書き込み、enable |
| `apogee daemon uninstall` | disable してユニットファイルを削除 |
| `apogee daemon start` | 今すぐ起動 |
| `apogee daemon stop` | 停止 |
| `apogee daemon restart` | stop + start |
| `apogee daemon status` | install / running / PID / 直近ログを詳細表示 |

### フラグ（`install` 時）

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | ユニットに焼き込むリッスンアドレス |
| `--db` | `~/.apogee/apogee.duckdb` | ユニットに焼き込む DuckDB パス |
| `--force` | `false` | 既存ユニットを上書き |

### 例

```sh
apogee daemon install
apogee daemon start
apogee daemon status
apogee daemon restart
apogee daemon stop
apogee daemon uninstall
```

`apogee daemon status` は lipgloss スタイルの 2 ボックス（Daemon + Collector）を表示します（`NO_COLOR=1` でキャプチャした素のサンプル）:

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

### 備考

- ユニットファイルは macOS では `~/Library/LaunchAgents/dev.biwashi.apogee.plist`、Linux では `~/.config/systemd/user/apogee.service` に置かれます。
- Collector ボックスは `/v1/healthz` プローブが失敗したときボーダーが赤に変わります。Daemon ボックスは未インストール時にボーダーが黄色になります。
- スーパーバイザの挙動、デバッグ、設定は [`daemon.md`](daemon.md) を参照してください。

---

## apogee status

daemon と HTTP の liveness を一括確認します。シェルプロンプトや CI チェックに組み込みやすいコンパクト形式で出力します。

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | プローブ対象のコレクター |
| `--json` | `false` | 2 行テキストの代わりに JSON で出す |

### 例

```
$ apogee status
APOGEE STATUS

Daemon:    running (pid 42317, uptime 1h 12m 4s)
╭─────────────────────────────────────────────────────────────────────────╮
│ Status:      running                                                    │
│ Installed:   yes                                                        │
│ ...                                                                     │
╰─────────────────────────────────────────────────────────────────────────╯

Collector: http://127.0.0.1:4100 (ok (HTTP 200, 3 ms))
╭───────────────────────────────────────────────╮
│ Endpoint:  http://127.0.0.1:4100              │
│ Health:    ok                                 │
│ Detail:    ok (HTTP 200, 3 ms)                │
│ Latency:   3ms                                │
╰───────────────────────────────────────────────╯
```

### 備考

- daemon が install されているのに動いていない、または HTTP プローブが失敗した場合は終了コードが非 0 になります。ログインシェルのヘルスチェックとして便利です。

---

## apogee logs

`~/.apogee/logs/` 以下の daemon ログファイルを tail します。

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `-f`, `--follow` | `false` | 新しい行を追従 |
| `-n`, `--lines` | `100` | 表示する末尾行数 |
| `--err` | `false` | stdout ログではなくエラーログ（`apogee.err.log`）を表示 |

### 例

```sh
apogee logs -f
apogee logs --err -n 200
```

### 備考

- `apogee serve` をフォアグラウンドで走らせている場合、ログは端末に直接出るため、このコマンドに tail 対象はありません。

---

## apogee open

既定ブラウザでダッシュボードを開きます。

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | 開くコレクター |

### 例

```sh
apogee open
```

### 備考

- macOS では `open`、Linux では `xdg-open` にフォールバックし、最終的に `open` を試します。

---

## apogee uninstall

daemon を停止し、`.claude/settings.json` から apogee の hook エントリを剥がし、任意でデータディレクトリも削除します。破壊的操作の前に確認を求めます。

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--purge` | `false` | `~/.apogee/`（DB + ログ + 設定）も削除 |
| `--yes` | `false` | 確認プロンプトを省略 |

### 例

```sh
apogee uninstall            # データは残す
apogee uninstall --purge    # ~/.apogee も丸ごと削除
```

### 備考

- daemon が未 install または既に停止している場合も正常に終了します。
- hook 除去は apogee バイナリパス、または v0.1.x の `python3 send_event.py` で始まるコマンド行をマッチ対象にします。

---

## apogee menubar

macOS のメニューバーアプリを起動します。macOS 専用で、他のプラットフォームでは "macOS only" メッセージを表示して非 0 で終了します。

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | ポーリング対象のコレクター |
| `--interval` | `5s` | ポーリング間隔 |

### 例

```sh
apogee menubar &
```

### 備考

- コレクター（または `apogee daemon start`）が動いている必要があります。メニューバーはクライアント側であってサーバーではありません。
- グリフは実行中ターンがあれば緑のドット、`intervene_now` が出たら赤い三角、コレクター不可達なら `offline` を表示します。
- フルメニュー内容とトラブルシューティングは [`menubar.md`](menubar.md) を参照してください。

---

## apogee doctor

ローカルインストールをヘルスチェックします。7 つのチェックを実行し、グリフ + メッセージ行とサマリフッタを表示します。

### チェック一覧

| 名前 | 説明 |
| --- | --- |
| `home` | `~/.apogee` が存在し書き込み可能か |
| `claude_cli` | `claude` が PATH にあるか（summarizer が利用） |
| `db_path` | 既定 DuckDB パスが書き込み可能か |
| `config` | `~/.apogee/config.toml` の有無（情報のみ） |
| `db_lock` | DuckDB のサイドカーロックが空、もしくはインストール済み daemon が保持しているか |
| `collector` | `127.0.0.1:4100` の `/v1/healthz` が 200 を返すか（500ms タイムアウト） |
| `hook_install` | `internal/cli/init.go::HookEvents` の各イベントが `~/.claude/settings.json` で apogee バイナリを指しているか |

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--json` | `false` | JSON 配列で出力（CI / scripts / `apogee menubar` から消費する用途） |

### 例

```
$ apogee doctor

  ✓ /Users/me/.apogee writable
  ✓ claude CLI on PATH (/Users/me/.local/bin/claude)
  ✓ default db path /Users/me/.apogee/apogee.duckdb
  ✓ no config file (defaults in use) (/Users/me/.apogee/config.toml)
  ✓ DuckDB file is unlocked
  ⚠ collector not running (http://127.0.0.1:4100/v1/healthz)
  ✓ apogee hook installed for 12/12 events

5 ok · 1 warning · 0 errors
```

```
$ apogee doctor --json
[
  {"name": "home",         "severity": "ok",   "message": "/Users/me/.apogee writable"},
  ...
  {"name": "hook_install", "severity": "ok",   "message": "apogee hook installed for 12/12 events"}
]
```

### 備考

- `doctor` は何も書き換えません。レポートを出すだけです。
- グリフは Unicode の `✓` / `⚠` / `✗`（U+2713, U+26A0, U+2717）です。`NO_COLOR=1` または stdout が TTY でないときは色がない素のテキストにフォールバックします。

---

## よくあるエラー

### DuckDB ロックの競合

同じ DuckDB ファイルを開こうとする 2 つ目の apogee プロセスは、生の driver エラーではなくスタイル付きエラーボックスを表示して exit 1 で終了します:

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

Holder の PID は `lsof -nP <db>` が利用できれば lsof から、なければサイドカー pid ファイルから取得します。詳しくは [`daemon.md`](daemon.md) のサイドカーファイル節を参照してください。

### daemon が起動しない

- `apogee daemon status` でインストール / ロード / 実行状態を確認。
- `apogee logs -f` で daemon の stdout / stderr を tail。
- launchd: `launchctl print gui/$(id -u)/dev.biwashi.apogee`。
- systemd: `journalctl --user -u apogee.service -f`。

### Hook が発火しない

`apogee doctor` を実行して `hook_install` 行を確認します。`partial` または `missing` であれば `apogee init --force` でエントリを書き直してください。

---

## apogee version

ビルドバージョン、コミット SHA、ビルド日時を表示します。

### フラグ

なし。

### 例

```sh
$ apogee version
apogee 0.1.3 (commit abcdef1, built 2026-04-15)
```

### 備考

- `apogee --version` は短い文字列だけを表示します。上のフルブロックが欲しいときは `apogee version` を使ってください。

---

## summarizer 設定

LLM recap (Haiku) と rollup (Sonnet) の各ワーカーは、`user_preferences` DuckDB テーブルに永続化されたいくつかのオペレーター制御の設定を反映します。これらはジョブ開始時に毎回読み直されるので、再起動なしで変更が反映されます。管理方法は 2 つあります:

1. **設定ページ** (`/settings`) には「Summarizer」セクションがあり、言語トグル、recap / rollup モデルオーバーライド、2 つのシステムプロンプトテキストエリアが表示されます。Save で `PATCH /v1/preferences` 経由で永続化します。
2. トップリボンの**コンパクトな言語ピッカー**で、`summarizer.language` を `EN` / `JA` の間でワンクリック切替できます。

同じ設定はスクリプト用に HTTP でも公開されています:

```sh
# 現在の状態を取得。
curl -s http://localhost:4100/v1/preferences

# recap と rollup を日本語出力に切替。
curl -s -X PATCH http://localhost:4100/v1/preferences \
  -H 'Content-Type: application/json' \
  -d '{"summarizer.language":"ja"}'

# recap システムプロンプトと recap モデルオーバーライドを設定。
curl -s -X PATCH http://localhost:4100/v1/preferences \
  -H 'Content-Type: application/json' \
  -d '{"summarizer.recap_system_prompt":"必ずファイルパスに言及してください。","summarizer.recap_model":"claude-haiku-4-5"}'

# すべての summarizer.* 設定をデフォルトにリセット。
curl -s -X DELETE http://localhost:4100/v1/preferences
```

バリデーション: `summarizer.language` は `"en"` または `"ja"`、2 つのシステムプロンプトはそれぞれ最大 2048 文字、モデルオーバーライドは `claude-{haiku,sonnet,opus}-…` 形式の alias である必要があります。空文字列でオーバーライドをクリアすると `~/.apogee/config.toml` にフォールバックします。

---

## グローバルフラグ

すべてのサブコマンドで使えます。

| フラグ | 説明 |
| --- | --- |
| `-h`, `--help` | コマンドのヘルプ |
| `-v`, `--version` | `apogee --version` の短い文字列 |

グローバルな `--verbose` フラグはありません。ネットワーク I/O を持つサブコマンドは、INFO レベルでそれぞれ stderr に進捗を出力し、エラーは常にログに残します。
