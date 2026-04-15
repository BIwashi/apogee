# apogee desktop (macOS)

`apogee serve` と同じ collector + 埋め込み Next.js ダッシュボードを、ブラウザ
タブではなくネイティブの macOS ウィンドウで動かすためのシェルです。
[Wails v2](https://wails.io) をライブラリとして利用しており、本体の実装は
[`desktop/main.go`](../desktop/main.go) (`//go:build darwin`) にあります。
`//go:build !darwin` の側には
[`desktop/main_other.go`](../desktop/main_other.go) というスタブを置いてあり、
Linux / Windows の CI ランナーで `go build ./...` が通るように、かつ万が一
そのプラットフォームで起動したときは "macOS only" と明示的にエラーを出します。
Wails 自体は Linux / Windows もサポートしていますが、apogee 側で動作確認
しているのは今のところ Darwin のみです。

`apogee menubar` と違い、desktop シェルは collector の **所有者** です。
DuckDB ストアを直接オープンし、ingest/reconstruct のパイプラインを組み立て、
得られた `chi.Router` を Wails の `AssetServer.Handler` に渡します。
desktop プロセス内に追加の TCP listener は開かれません — WebView のリクエスト
はそのまま router にディスパッチされるため、`/v1/*` と埋め込み SPA は同一の
in-process ハンドラから配信されます。

```
DuckDB store ──▶ collector.New ──▶ Server.Router (chi.Router, http.Handler)
                                      │
                                      ▼
                        Wails AssetServer.Handler
                                      │
                                      ▼
                              WKWebView (native)
```

## 前提

- macOS (Darwin) — Wails は WKWebView で描画します。
- Xcode Command Line Tools (`xcode-select --install`) — cgo / DuckDB / Wails
  の前提。
- `go.mod` に合った Go toolchain。
- Next.js のビルドに Node.js (`web/out` を再生成するときのみ必要)。
- `make desktop-app` / `make desktop-dev` を使うときは
  [`wails` CLI](https://wails.io/docs/gettingstarted/installation):

  ```sh
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  ```

  通常の `make desktop-build` / `make desktop-run` は素の `go build` だけで
  動くので **wails CLI は不要** です。wails CLI が必要になるのは `.app` バン
  ドル生成とホットリロード dev モードのときだけです。

## Homebrew からインストールする

各タグリリースで、既存の `apogee` CLI Formula と一緒に `apogee-desktop`
Cask が
[`BIwashi/homebrew-tap`](https://github.com/BIwashi/homebrew-tap) に発行
されます。

```sh
brew tap BIwashi/tap
brew install --cask apogee-desktop
open -a Apogee
```

Cask は `Apogee.app` を `/Applications` に配置し、同時に `apogee-desktop`
ランチャーを `$PATH` に symlink するので、ターミナルから `apogee-desktop`
を叩いてもウィンドウを起動できます。バンドルは **コード署名 / notarization
未対応** のままなので、Cask の `postflight` フックが `xattr -dr
com.apple.quarantine` を走らせて macOS 15 (Sequoia) 以降でも Gatekeeper に
初回起動を止められないようにしています。GitHub の Release zip を手動で
ダウンロードした場合は、自分で `xattr -dr com.apple.quarantine
/Applications/Apogee.app` を実行する (または古い macOS なら右クリック →
開く) 必要があります。

`apogee` CLI Formula (collector / daemon / hook / menubar / onboard) は
別インストールです: `brew install BIwashi/tap/apogee`。ほとんどのユーザーは
両方入れたいはずです。

## ソースから起動する

```sh
# 埋め込み web バンドルと desktop バイナリをまとめて作る
make desktop-build

# ビルドしてウィンドウを起動。DuckDB ストアは他の apogee コマンドと共有
# (デフォルト ~/.apogee/apogee.duckdb)。APOGEE_DB=/path/to/db で切り替え可能。
make desktop-run
```

ウィンドウはブラウザ UI と同じダークテーマのトークンを使います。いまは
Wails のデフォルト以上の dock / メニューバー拡張は持たず、トラフィックライト
ボタンや OS レベルのウィンドウメニューはすべて標準の Cocoa のままです。

## ローカルで `.app` を作る

Wails CLI を入れているかどうかで 2 択:

```sh
# A — Wails CLI を使う。desktop/build/bin/Apogee.app を生成。フロント
# ビルド / Info.plist / universal fuse まで全部 Wails CLI が面倒を見る。
make desktop-app

# B — goreleaser snapshot。CI のリリース経路をそのまま踏むパターン。
# dist/apogee-desktop-universal_darwin_all/Apogee.app と Cask が読む
# zip アーティファクトを生成。
goreleaser release --snapshot --clean --skip=before,publish,validate
```

B 経路がリリースビルドの正です。`scripts/bundle-desktop-app.sh` 経由で
走るので Wails CLI への依存がなく、`sips` + `iconutil` +
`Info.plist` のヒアドキュメントだけで `.app` を組み立てます。
**コード署名と notarization はまだ未対応** ですが、未署名バンドルでも
ローカル実行は問題なく、Homebrew 配布時は Cask が quarantine xattr を
剥がすので配布も機能します。`scripts/bundle-desktop-app.sh` に
`codesign --deep --sign "Developer ID Application: ..."` を足すのは、
Apple Developer ID を準備したタイミングで次の仕事として入れられます。

## Dev モード (ホットリロード)

Wails の dev モードは WebView 内で描画しつつ、フロントエンドは外部管理の
Next.js dev サーバーから読み込む方式なので、**3 つ**のプロセスを同時に動かす
必要があります。

```sh
# ターミナル 1: collector を :4100 で起動。Next.js dev サーバーの
# /v1/* リライト先 (web/next.config.ts のルール) がここに向きます。
make run-collector

# ターミナル 2: Next.js dev サーバー (:3000)
make web-dev

# ターミナル 3: Wails dev ウィンドウ。desktop/wails.json の
# frontend:dev:serverUrl に従って http://localhost:3000 にプロキシします。
# Wails 自体は Next.js dev サーバーを起動しません — 純粋な attach モードです。
make desktop-dev
```

`web/app/` 配下のファイルを編集すると Next.js の HMR が走り、WKWebView 側も
自動的に更新されます。desktop バイナリ自体はこのフローでは再ビルドされないので、
`desktop/` や `internal/collector/` の Go コードを触ったときは
`make desktop-dev` を一度止めて再起動してください。

UI をイジるだけなら、元々のブラウザフロー
(`make run-collector` + `make web-dev` + `http://localhost:3000`) の方が
手早いです。desktop dev モードは単に WKWebView の chrome を被せるだけなので、
UI 作業の本体にはあまり寄与しません。

## アーキテクチャメモ

- `internal/collector/server.go` に `StartBackground(ctx)` /
  `StopBackground(ctx)` を追加してあります。これは metrics sampler、
  summarizer、HITL expiration ticker、intervention sweeper 等を起動・停止する
  もので、要するに `Run()` から `ListenAndServe` を除いた部分です。desktop
  シェルはこれを Wails の `OnStartup` / `OnShutdown` と紐付けて、worker
  goroutine の寿命をウィンドウの寿命と一致させています。
- Wails の `AssetServer.Handler` には `srv.Router()` をそのまま渡しています。
  この router は `internal/webassets` 経由の埋め込み SPA を `/` で、型付き
  API を `/v1/*` で既に配信しているので、desktop レイヤーで「API / 静的
  アセット」を切り分ける必要はありません。
- DuckDB は **排他的** です。同じ `~/.apogee/apogee.duckdb` に対して
  `apogee serve` と `apogee desktop` を同時に走らせることはできません。並行
  して試したい場合は `APOGEE_DB=:memory:` か別ファイルを指定してください。

## 既知の制約

- macOS 専用。コードは移植可能ですが Linux / Windows 側の動作は未確認。
- コード署名 / notarization なし。配布時には Gatekeeper に弾かれます。
- Wails が提供するデフォルト (EditMenu + WindowMenu) 以上のメニューバー項目
  はまだありません。カスタムメニューやトレイアイコン、`apogee menubar` の
  機能は現時点で desktop シェルとコードを共有していません。
- シングルインスタンスロックなし。2 つの desktop プロセスを同時起動すると
  DuckDB ファイルで競合して、2 つ目がオープン時にエラー終了します。

## なぜ Electron / Tauri ではなく Wails なのか

- **Go ネイティブ。** apogee モジュールは Go なので、Wails を使えば collector
  を IPC やサブプロセス経由ではなく in-process のまま保てる。
- **Chromium ではなく WKWebView。** `.app` のサイズが ~20 MB 以内に収まり、
  OS 付属の WebView を再利用できる。
- **新しいビルド言語が不要。** Tauri なら Rust、Electron なら Node を「シェル
  のために」持ち込むことになる。Wails なら既存の Go + Next.js バンドルだけで
  済む。
