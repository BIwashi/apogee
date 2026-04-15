[English version / 英語版](./menubar.md)

# apogee menubar (macOS)

ローカルの apogee コレクターをポーリングしてメニューバーにライブカウントを表示する、小さな macOS のステータスアイテムです。実装は [`internal/cli/menubar_darwin.go`](../internal/cli/menubar_darwin.go) にあり、[`caseymrm/menuet`](https://github.com/caseymrm/menuet) を使っています。非 darwin ビルドでは「macOS only」と明示して非 0 で終わるスタブにコンパイルされます。

手動で起動する場合:

    apogee menubar &

メニューバーアプリはコレクターの**クライアント**であって、サーバーではありません。`apogee daemon start` かフォアグラウンドの `apogee serve` がどこかで動いている必要があります。コレクターが到達不能なときはグリフが `offline` に切り替わり、HTTP サーフェスを叩く系のメニュー項目は隠れます。

グリフの意味:

- `apogee · ●`         — コレクターは動作中、緊急の attention なし
- `apogee · ● 3`       — 実行中のターンが 3 件
- `apogee · ▲ 1`       — `intervene_now` 判定のターンが 1 件
- `apogee · offline`   — コレクター不到達

グリフをクリックするとドロップダウンが出ます。

- daemon のステータス（running / installed / stopped / missing）
- `N sessions / M active / K intervene_now` のスナップショット
- Open dashboard（ブラウザ。内部で `open http://127.0.0.1:4100` を呼ぶ）
- Open logs（Finder で `~/.apogee/logs/` を開く）
- Restart daemon（`apogee daemon restart` にシェルアウト）
- Quit menubar

## フラグ

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | ポーリング対象のコレクター |
| `--interval` | `5s` | ポーリング間隔 |

## なぜドットが赤い？

グリフは `/v1/attention/counts` の結果を小さな状態機械に流して決まります。

| 見た目 | 意味 |
| --- | --- |
| 緑の丸 `●` | 実行中ターンがあり、すべて `healthy` |
| 数字付き緑丸 `● 3` | 実行中ターンが複数、すべて `healthy` |
| 黄色い山形 `▲` | 少なくとも 1 件が `watch` または `watchlist` |
| 数字付き赤三角 `▲ 1` | 少なくとも 1 件が `intervene_now` |
| グレーの `offline` | コレクター HTTP プローブ失敗 |

赤い三角は、attention エンジンが少なくとも 1 つのライブターンを `intervene_now` に分類したことを意味します。グリフをクリックして **Open dashboard** から Live ページへ飛べば、該当ターンが triage レールの先頭に並んでいます。

健全なドットを期待していたのに `offline` が出ている場合:

1. `apogee status` — daemon は動いている？
2. `apogee logs --err -n 200` — エラーログにクラッシュは？
3. `curl http://127.0.0.1:4100/v1/healthz` — HTTP サーフェスは上がっている？

より広いトラブルシューティング手順は [`daemon.md`](daemon.md#詰まった-daemon-のデバッグ) を参照してください。

## ログイン項目として登録する

オンボーディングウィザードは、メニューバーを **2 つ目の** launchd ユニットとして登録するので、ユーザーがシェルをいじらなくても毎回のログインで自動起動します。

    apogee menubar install     # macOS 専用
    apogee menubar status      # ユニットの状態を確認
    apogee menubar uninstall   # ログイン項目の登録を解除

このユニットはコレクターデーモンのユニットとは独立しています。メニューバーのインストール／アンインストールが `dev.biwashi.apogee` を触ることはありませんし、その逆も同じです。`install` サブコマンドは `~/Library/LaunchAgents/dev.biwashi.apogee.menubar.plist` に plist をアトミックに書き出し、launchd に bootstrap を依頼します。

| キー | 値 | 理由 |
| --- | --- | --- |
| `Label` | `dev.biwashi.apogee.menubar` | コレクターデーモンのラベルとは別にしておき、それぞれを独立して install / uninstall / inspect できるようにする。 |
| `ProgramArguments` | `[ <apogee の絶対パス>, "menubar" ]` | install 時に `os.Executable()` でバイナリパスを解決するので、apogee を更新したら `menubar install` を打ち直すだけで追随する。 |
| `RunAtLoad` | `true` | ユニットがロードされた瞬間（毎回のログイン時）に起動する。 |
| `KeepAlive` | `false` | メニューバーはインタラクティブ。ドロップダウンから **Quit menubar** を選んだときに launchd が勝手に復活させない。 |
| `LSUIElement` | `true` | Cocoa のメニューバー専用アプリ扱い。Dock アイコンもメインウィンドウもなし。 |
| `LimitLoadToSessionType` | `Aqua` | 本物の GUI ログインセッションでのみロードする。SSH やヘッドレスのセッションでは plist が見えないので、ユーザーが目の前にいないのにメニューバーが立ち上がることはない。 |
| `StandardOutPath` / `StandardErrorPath` | `~/.apogee/logs/menubar.{out,err}.log` | コレクターデーモンとログファイルを分けて、`apogee logs` と診断情報をノイズなく追える。 |

冪等: plist の中身が一致していれば `menubar install` を 2 回打っても no-op です。内容が衝突する既存 plist があり `--force` を付けていない場合は、`menubar already installed (pass --force to overwrite)` を返します。

`apogee onboard` はウィザードの menubar グループでも同じインストール導線を提示します。まっさらな mac では既定が **Install**、既に plist が入っている場合は **Re-install** になります。非 darwin ではグループごと非表示です。コレクターデーモンのインストールが成功した後にメニューバーのインストールが失敗した場合、ウィザードは警告ログを出して処理を続行します — メニューバーはあくまで利便性のための機能であり、デーモンをロールバックするより部分的な成功を維持するほうが妥当だからです。

どちらにしても、メニューバーアプリはポーリング先となる **daemon**（またはフォアグラウンドの `apogee serve`）がどこかで動いている必要があります。コレクターなしにメニューバーだけ動かしても意味はありません。

## 手動でログイン項目登録する場合（従来手段）

`apogee menubar install` が存在する前は、ログイン時にメニューバーを動かす唯一の方法は macOS のログイン項目に自分で追加することでした。

    システム設定 → 一般 → ログイン項目 → 開いた時に起動 → +
    apogee バイナリを追加し、引数に `menubar` を指定する

この手段は今でも使えますし、launchd ユニットの挙動がおかしいときのフォールバックとしても妥当です。ただし、その場合は先に `apogee menubar uninstall` で launchd ユニットを解除しておかないと、メニューバーの 1 つしかないスロットを 2 つのプロセスが奪い合うことになります。
