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

### 備考

- ユニットファイルは macOS では `~/Library/LaunchAgents/dev.biwashi.apogee.plist`、Linux では `~/.config/systemd/user/apogee.service` に置かれます。
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

```sh
$ apogee status
daemon:    running (pid 42317)
collector: ok (http://127.0.0.1:4100)
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

ローカルインストールをヘルスチェックします。apogee バイナリが解決できるか、設定ファイルがパースできるか、DB ファイルが書けるか、`claude` が PATH にあるか、設定コレクターへの hook POST が通るかを確認します。

### フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--server-url` | `http://localhost:4100/v1/events` | プローブ対象のコレクター |

### 例

```sh
apogee doctor
```

### 備考

- `doctor` は何も書き換えません。レポートを出すだけです。

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

## グローバルフラグ

すべてのサブコマンドで使えます。

| フラグ | 説明 |
| --- | --- |
| `-h`, `--help` | コマンドのヘルプ |
| `-v`, `--version` | `apogee --version` の短い文字列 |

グローバルな `--verbose` フラグはありません。ネットワーク I/O を持つサブコマンドは、INFO レベルでそれぞれ stderr に進捗を出力し、エラーは常にログに残します。
