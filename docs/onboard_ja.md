# apogee onboard — 対話式セットアップウィザード

`apogee onboard` は新規マシンでのセットアップを 1 コマンドにまとめた
フローです。通常なら手動で順番に実行する 4 つのインストール手順を
ひとつにつなげて実行します。

1. Claude Code のフックを `~/.claude/settings.json` に登録
2. apogee をユーザースコープのバックグラウンドサービスとしてインストール
   （macOS は launchd、Linux は systemd `--user`）
3. LLM サマライザー設定（言語と任意のシステムプロンプト）を
   DuckDB の `user_preferences` テーブルに書き込み
4. 任意で外部 OTLP コレクターを `~/.apogee/config.toml` の
   `[telemetry]` ブロックに設定

適用ステップの後、デーモンを起動してブラウザでダッシュボードを開きます。
各プロンプトのデフォルト値はディスク上の現在の状態（`config.toml`、
DuckDB preferences、`settings.json`、デーモンマネージャーのステータス）
から読み込まれるため、再実行は安全で、実際に変更したい差分だけを
提案してくれます。

## クイックスタート

```sh
# 対話モード — ウィザードを順に進める
apogee onboard

# すべてのデフォルトを受け入れて、プロンプトもブラウザも開かない。
# プロビジョニング / CI パスはこちら。
apogee onboard --yes

# 書き込みせずプランだけ確認する
apogee onboard --dry-run

# 1 ステップだけ実行して、他はスキップする
apogee onboard --skip-daemon --skip-telemetry
```

## フラグ

| フラグ | デフォルト | 説明 |
| --- | --- | --- |
| `--yes`, `-y` | `false` | プロンプトを出さずにすべてのデフォルトを受け入れる |
| `--non-interactive` | `false` | `--yes` のエイリアス（スクリプト向け） |
| `--config` | `~/.apogee/config.toml` | 書き込む設定ファイル |
| `--db` | `~/.apogee/apogee.duckdb` | preferences 用の DuckDB ファイル |
| `--addr` | `127.0.0.1:4100` | コレクターの listen アドレス |
| `--skip-daemon` | `false` | デーモンのインストール/起動をスキップ |
| `--skip-hooks` | `false` | `settings.json` へのフック書き込みをスキップ |
| `--skip-summarizer` | `false` | サマライザー preferences の書き込みをスキップ |
| `--skip-telemetry` | `false` | OTLP エクスポート設定をスキップ |
| `--dry-run` | `false` | プランを出力して終了 |

環境変数 `APOGEE_ONBOARD_NONINTERACTIVE=1` は `--yes` と同等で、
Docker の `RUN` ステップや CI スモークテストで便利です。

## プランボックス

各実行は適用前にプランボックスを出力します。対話モードでは各セクションを
タブで行き来してから確定できますし、`--yes` モードでは現在の状態から
そのままプランが導出されます。

```
╭───────────────────────────────────────────────────────────────────────────────╮
│ apogee onboard — plan                                                         │
│                                                                               │
│ Config:      /Users/you/.apogee/config.toml                                   │
│ DB:          /Users/you/.apogee/apogee.duckdb                                 │
│ Hooks:       install /Users/you/.claude/settings.json (dynamic source_app)    │
│ Daemon:      install dev.biwashi.apogee @ 127.0.0.1:4100 · start              │
│ Summarizer:  language=en                                                      │
│ OTel:        disabled                                                         │
│ Open:        open http://127.0.0.1:4100/                                      │
╰───────────────────────────────────────────────────────────────────────────────╯
```

プランの各フィールドは 1 つの適用ステップに対応します。

| プランフィールド | 情報源 | 適用ステップ |
| --- | --- | --- |
| `Config` | `--config` | TOML 書き換え |
| `DB` | `--db` | DuckDB オープン |
| `Hooks` | `~/.claude/settings.json` の存在判定 | `init.Init(cfg)` |
| `Daemon` | `daemon.Manager.Status(ctx)` | `Manager.Install` + `Start` |
| `Summarizer` | `summarizer.*` preference 行 | `Store.UpsertPreference` |
| `OTel` | `config.toml` の `[telemetry]` ブロック | TOML 書き換え |
| `Open` | 対話確認（`--yes` では常に無効） | `apogee open` ヘルパー |

## 適用時の出力

```
Applying...

✓ wrote ~/.apogee/config.toml
✓ installed 12 hook events into ~/.claude/settings.json
✓ installed dev.biwashi.apogee unit at ~/Library/LaunchAgents/dev.biwashi.apogee.plist
✓ wrote summarizer preferences (language=en)
✓ daemon started (dev.biwashi.apogee)

apogee is ready.
  Run `apogee status` to check the daemon.
  Run `apogee doctor` to verify the environment.
```

ステップごとに `✓` / `⚠` / `✗` のグリフで状態を出力します。最初の失敗
ステップでアボートして整形済みのエラーボックスを出しますが、それ以前に
成功したステップはロールバック**しません** — 完了済みの作業を取り消す
よりは、部分的にインストール済みの状態で終わる方がユーザーにとって
ましだからです。

## 非対話モード（CI / Docker）

次のいずれかが真の場合、ウィザードは非対話モードとして動作し、確認を
挟まずに適用まで進みます。

- `--yes` または `--non-interactive` が指定されている
- 環境変数 `APOGEE_ONBOARD_NONINTERACTIVE=1` が設定されている
- `stdin` が TTY ではない（`docker run` へのパイプなど）

非対話モードでは：

- 各プロンプトのデフォルトがそのまま適用される
- 新規マシンの状態（未登録）ではすべてのステップがインストールパスを通る
- 空ではない既存のサマライザーシステムプロンプトは、空のデフォルトで
  上書きされない
- ブラウザは開かない

## 冪等性と再実行

`apogee onboard` は何度実行しても安全です。4 つの適用ステップはそれぞれ
`apogee init` と `apogee daemon install` が使っているパッケージレベルの
ヘルパーを呼び出しているだけで、どれもすでに冪等です。状態が変わっていない
マシンで再実行すると次のような出力になります。

```
✓ wrote ~/.apogee/config.toml
✓ installed 0 hook events into ~/.claude/settings.json
✓ installed dev.biwashi.apogee unit at ~/Library/LaunchAgents/dev.biwashi.apogee.plist
✓ wrote summarizer preferences (language=en)
```

対話モードでデーモンの選択肢に `reinstall` を選ぶ（あるいは
既にインストール済みの `--yes` 実行で plan の `reinstall` が選ばれる）
と、unit ファイルが上書き再生成されます。

## 失敗時の扱い

各ステップはエラーをグリフ付き 1 行で表示します。

```
✗ daemon install: signalling launchd bootstrap: ...
```

いずれかのステップが失敗した場合、終了コードは `1` です。まずは
`apogee doctor` で環境全体を点検してください。同じ 7 項目のヘルス
チェックをエンドツーエンドで確認できます。
