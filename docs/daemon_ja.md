[English version / 英語版](./daemon.md)

# Daemon コア

apogee はログインのたびに起動するバックグラウンドサービスとして自分自身をインストールできます。同じサブコマンドツリーが macOS（launchd）と Linux（systemd `--user`）の両方で動くので、オペレーターの操作感は両プラットフォームで共通です。

```sh
apogee daemon install     # ユニットを登録
apogee daemon start       # いま起動
apogee status             # daemon + コレクターの概況
apogee daemon status      # daemon の詳細レポート
apogee logs -f            # daemon ログを追従
apogee daemon stop        # 停止
apogee daemon uninstall   # ユニットファイルを削除
```

## ユニットファイルの配置場所

| プラットフォーム | パス |
|----------|------------------------------------------------------------------|
| macOS    | `~/Library/LaunchAgents/dev.biwashi.apogee.plist`                |
| Linux    | `$XDG_CONFIG_HOME/systemd/user/apogee.service`（既定 `~/.config/systemd/user/apogee.service`） |

どちらのユニットも、実行中の `apogee` バイナリを `serve --addr 127.0.0.1:4100 --db ~/.apogee/apogee.duckdb` で呼び出すのが既定です。install 時に addr や db パスを上書きできます。

```sh
apogee daemon install --addr 127.0.0.1:9100 --db ~/.apogee/local.duckdb
```

## スーパーバイザの基本操作

### macOS / launchd

apogee は `launchctl` に `gui/$(id -u)/` ドメインでシェルアウトします。daemon はあなたのユーザーで動き、ログイン環境へアクセスできます。

| 操作  | コマンド |
|------------|------------|
| Install    | `launchctl bootstrap gui/<uid> <plist>` |
| Start      | `launchctl kickstart gui/<uid>/dev.biwashi.apogee` |
| Restart    | `launchctl kickstart -k gui/<uid>/dev.biwashi.apogee` |
| Stop       | `launchctl bootout gui/<uid>/dev.biwashi.apogee` |
| Uninstall  | `launchctl bootout …` + `rm <plist>` |
| Status     | `launchctl print gui/<uid>/dev.biwashi.apogee` |

plist は既定で `KeepAlive=true` と `RunAtLoad=true` を設定するので、偶発的なクラッシュは launchd が再起動し、フレッシュログインでコレクターが自動起動します。

### Linux / systemd --user

apogee は `systemctl --user` にシェルアウトします。daemon はあなたのユーザーで動き、root は不要です。

| 操作  | コマンド |
|------------|------------|
| Install    | ユニットを書き出し、`systemctl --user daemon-reload`、`systemctl --user enable apogee.service` |
| Start      | `systemctl --user start apogee.service` |
| Restart    | `systemctl --user restart apogee.service` |
| Stop       | `systemctl --user stop apogee.service` |
| Uninstall  | `systemctl --user disable --now apogee.service` + `rm <unit>` |
| Status     | `systemctl --user show apogee.service --property=…` |

ユニットは既定で `Restart=on-failure` と 3 秒の backoff を設定します。

注意: `systemctl --user` にはユーザーセッションバスが必要です。ヘッドレスな Linux ボックスではログアウト後もユーザーインスタンスを生かすために `loginctl enable-linger $USER` を有効にしておく必要があるかもしれません。

## 詰まった daemon のデバッグ

```sh
# プロセスは本当に上がっている？
apogee daemon status

# ライブログを追従（Ctrl-C で抜ける）
apogee logs -f

# HTTP サーフェスを直接叩く
curl http://127.0.0.1:4100/v1/healthz

# フルリロード
apogee daemon restart
```

`apogee daemon status` が `Installed: yes` なのに `Running: no` で、ヘルスプローブも失敗しているなら、コレクターは設定ミスかポート衝突でクラッシュループしている可能性が高いです。`apogee.err.log` のスタックを確認してください。

```sh
tail -n 200 ~/.apogee/logs/apogee.err.log
```

よくある落とし穴:

- **ポート 4100 がすでに占有されている。** 以前の dev セッションの `apogee serve` が残っています。`pkill -f "apogee serve"` で片付けます。
- **DuckDB のロックファイル。** ハードキルすると `.duckdb.wal` のロックがディスクに残り、再 open を邪魔します。`.wal` を削除して再起動します。
- **壊れた plist / unit ファイル。** install の途中でクラッシュすると不完全なユニットが残ります。`apogee daemon install --force` で上書きします。
- **summarizer が `claude` を見つけられない。** ログに `summarizer: runner error … executable file not found in $PATH` が出ていたら、古いバージョンで install されたユニットファイルに PATH が設定されておらず、launchd / systemd が `/usr/bin:/bin:/usr/sbin:/sbin` の最小 PATH しか渡していないのが原因です。`claude` は通常 `~/.local/bin` や `/opt/homebrew/bin` にあるため、見つかりません。v0.1.8 以降で `apogee daemon install --force` を再実行すると、インストール時の `PATH` をスナップショットしてユニットに焼き付けるので、対話シェルと同じディレクトリ列が daemon にも継承されます。その後 `apogee daemon restart` で再読み込みしてください。

## DuckDB ロック

すべてのコレクターは DuckDB ファイルを開く前に、隣に置いたサイドカーファイルへ排他ロックを取得します。これにより「同じファイルを 2 つのコレクターが開く」という地雷を踏み抜き、生の DuckDB driver エラーを実用的なスタイル付きエラーボックスに変換します。ロックは 2 つのファイルからなります:

| ファイル | 目的 |
| --- | --- |
| `<db>.apogee.lock` | サイドカーロックファイル。コレクターはプロセスの生存期間中、このファイルに対して `flock(LOCK_EX|LOCK_NB)` を保持します。`internal/store/duckdb/lock.go` のプローブも同じファイルを `LOCK_NB` で開き、`EWOULDBLOCK` / `EAGAIN` を「ロック保持中」と扱います。 |
| `<db>.apogee.pid` | 保持者の PID を 10 進テキストで書き込んだサイドカー pid ファイル。ロック獲得時に書き込まれ、リリース時に削除されます。`lsof` が無い環境での保持者 PID フォールバックに使われます。 |

プローブが衝突を検出すると、2 つ目のコレクターは以下のスタイル付きボックスを表示して exit 1 で終了します（保持者の PID は `lsof -nP <db>` で取得し、なければ pid ファイルにフォールバック）:

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

`apogee doctor` は同じプローブを `db_lock` チェックで公開しています。ロックが空なら OK、実行中の daemon が保持していれば（PID 一致）OK、それ以外は error です。

サイドカーファイルは正常リリース時に削除されます。コレクターが kill -9（あるいはホストクラッシュ）された場合、`<db>.apogee.lock` がディスクに残りますが、プロセス終了時にカーネルが flock を解放するため、次回起動は手動クリーンアップなしで成功します。pid ファイルは古いまま残る場合があるので、無視して再実行してください。

`:memory:` センチネルではロックプリフライトはスキップされます。

## daemon なしで走らせる

ローカル開発では通常 daemon は不要です。

```sh
apogee serve --addr :4100 --db .local/apogee.duckdb
```

これはユニットが実行するのとまったく同じコマンドですが、foreground 実行です。停止は Ctrl-C。このモードでは stdout が直接端末に出るので `apogee logs` は使いません。

## 設定

`~/.apogee/config.toml` の `[daemon]` ブロックがノブを持ちます。

```toml
[daemon]
label         = "dev.biwashi.apogee"
addr          = "127.0.0.1:4100"
db_path       = "~/.apogee/apogee.duckdb"
log_dir       = "~/.apogee/logs"
keep_alive    = true
run_at_load   = true
```

すべての値にデフォルトがあるので、このブロックは純粋に追加設定用です。

## FAQ

### apogee はログインで自動起動する？

`apogee daemon install` を一度実行していれば、はい。macOS の plist は `RunAtLoad=true` と `KeepAlive=true` を設定するので、launchd がログインのたびにコレクターを起動し、クラッシュ時も再起動します。Linux の systemd user ユニットは `Restart=on-failure` を設定し、`apogee daemon install` が裏で走らせる `systemctl --user enable apogee.service` によって次回ログイン時に起動するようになります。ヘッドレスな Linux ボックスではログアウト後もユーザーバスを生かすために `loginctl enable-linger $USER` を併用したいはずです。

作業中だけコレクターを動かしたいなら、`apogee daemon install` をスキップして `apogee serve` を端末や `make dev` から直接走らせるだけで十分です。

### アンインストールの仕方は？

```sh
apogee uninstall
```

次の順で完全に片付けます。

1. daemon を停止（`apogee daemon stop`）。
2. ユニットファイルを削除（`apogee daemon uninstall`）。
3. `~/.claude/settings.json` から `apogee hook` エントリを剥がす。v0.1.x の `python3 send_event.py` 行もまとめて除去する。
4. `~/.apogee/` を触る前に確認プロンプトを出す。

`~/.apogee/`（DB + ログ + 設定）も丸ごと削除したい場合は `--purge` を付けます。

```sh
apogee uninstall --purge
```

スクリプト用途で「本当に消していい？」の確認を省くなら `--yes` も加えます。

```sh
apogee uninstall --purge --yes
```

完全なフラグリファレンスは [`cli.md`](cli.md) を参照してください。
