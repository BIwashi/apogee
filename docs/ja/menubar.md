[English version / 英語版](../menubar.md)

# apogee menubar (macOS)

ローカルの apogee コレクターをポーリングしてメニューバーにライブカウントを表示する、小さな macOS のステータスアイテムです。実装は [`internal/cli/menubar_darwin.go`](../../internal/cli/menubar_darwin.go) にあり、[`caseymrm/menuet`](https://github.com/caseymrm/menuet) を使っています。非 darwin ビルドでは「macOS only」と明示して非 0 で終わるスタブにコンパイルされます。

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

## 常時起動しておくには

PR #22 のオンボーディングウィザードでは、メニューバーを 2 つ目の launchd ユニット（`dev.biwashi.apogee.menubar`）として登録する導線が入る予定です。それまでは、シェルプロファイルでバックグラウンドプロセスとして起動するか、ログイン項目に手動登録します。

    システム設定 → 一般 → ログイン項目 → 開いた時に起動 → +
    apogee バイナリを追加し、引数に `menubar` を指定する

どちらにしても、メニューバーアプリはポーリング先となる **daemon**（またはフォアグラウンドの `apogee serve`）がどこかで動いている必要があります。コレクターなしにメニューバーだけ動かしても意味はありません。
