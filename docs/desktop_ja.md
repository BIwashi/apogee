# apogee desktop (macOS)

`apogee serve` と同じダッシュボードを、ブラウザタブではなく Wails v2 の
WKWebView を使ったネイティブの macOS ウィンドウで表示するためのシェル
です。実装は [`desktop/`](../desktop) (`//go:build darwin`) にあり、
`//go:build !darwin` 側には [`desktop/main_other.go`](../desktop/main_other.go)
というスタブを置いて、Linux / Windows の CI ランナーで
`go build ./...` が通るようにしてあります。Wails 自体は Linux / Windows
もサポートしていますが、apogee 側で動作確認しているのは Darwin だけ
です。

## ランタイムモード

desktop シェルは **proxy ファースト** です。起動時にローカルの
`apogee daemon` を probe し、daemon が応答すれば in-process の
`net/http/httputil.ReverseProxy` で `http://127.0.0.1:4100` にリバース
プロキシする薄い WKWebView ラッパーとして振る舞います。desktop
プロセス側には DuckDB も collector もバックグラウンド worker も居ません
— 全部 daemon 側に任せます。

```
                          ┌──────────────────────────────┐
                          │  Claude Code hook processes  │
                          └──────────────┬───────────────┘
                                         │  POST /v1/events
                                         ▼
┌──────────────────┐   GET /v1/*   ┌──────────────────────┐
│   Apogee.app     │ ────────────▶ │  apogee daemon       │
│   (WKWebView)    │ ◀──────────── │  127.0.0.1:4100      │
│   ReverseProxy   │   SSE stream  │  DuckDB + collector  │
└──────────────────┘               └──────────────────────┘
```

なぜ proxy ファーストか: Claude Code の hook は `apogee onboard` 時に
daemon (`127.0.0.1:4100`) に events を送るよう設定されます。もし
desktop シェルが自前で collector + DuckDB を持ってしまうと、desktop
側の DB は空のまま、実際の events は daemon 側の DB に書き込まれて
しまいます。また desktop UI から投げた operator intervention は hook
が見ている daemon に届かないので、Claude Code セッションには何も
伝わりません。proxy モードは desktop シェルを "daemon の状態を見る
ための窓" として扱うことで、hook の topology と矛盾しないメンタル
モデルに収まります。

### 初回 bootstrap フロー

daemon に届かないときは、cask で入れたばかりの (apogee を全く
セットアップしていない) ユーザーでもワンクリックで使える状態まで
自動で到達するよう、初回セットアップのフローが走ります:

1. `osascript` 経由でネイティブの Cocoa 確認ダイアログを出します:
   "apogee is not set up on this machine. Set it up now?"
2. **Set up** を押すと、シェルは subprocess で `apogee onboard --yes`
   を実行します。cask は `BIwashi/tap/apogee` 公式 formula に
   依存しているので、CLI は必ず `PATH` にあります。
3. `apogee onboard --yes` が Claude Code の hook を user scope で
   登録し、daemon を launchd user service として install + start し、
   summarizer をローカルの `claude` CLI 向けに設定します。
4. シェルは `http://127.0.0.1:4100/v1/healthz` を 30 秒のリミットで
   polling し、daemon が応答するのを待ちます。
5. `~/.apogee/installed-by-desktop` マーカーファイルを書き出します。
   これで cask の uninstall 時に「desktop シェル由来の daemon なら
   自分が責任を持って片付ける」という判断ができます。
6. proxy モードに遷移します。既にセットアップ済みのユーザーと同じ
   最短パスに合流します。

**Cancel** を押した場合は何もせず clean exit します。

### 設定

環境変数は 1 つだけです。デフォルトポート以外で daemon を動かす
稀なケース用:

| 環境変数 | デフォルト | 挙動 |
|---|---|---|
| `APOGEE_DAEMON_ADDR` | `127.0.0.1:4100` | reverse proxy の転送先と reachability probe のターゲット。`host:port` 形式で、スキーマは含めません。 |

desktop シェルには **モード切替フラグも DuckDB パス指定もありません**。
自分で DB を open せず、collector を構築せず、worker goroutine も
起動しません — UI に表示される値はすべて `APOGEE_DAEMON_ADDR` の
daemon から来ます。この "1 つしかないランタイムモデル" が、.app と
`apogee serve` / `apogee daemon`、そして走っている Claude Code
hook がクリーンに共存する理由であり、同時にシッピングバイナリが
~10 MB に収まる (DuckDB 静的 lib をリンクしない) 理由でもあります。

## Homebrew からインストール

タグ付きリリースごとに
[`BIwashi/homebrew-tap`](https://github.com/BIwashi/homebrew-tap) へ
`apogee-desktop` Cask が `apogee` CLI Formula と一緒に発行されます:

```sh
brew tap BIwashi/tap
brew install --cask apogee-desktop
open -a Apogee
```

Cask は `depends_on formula: BIwashi/tap/apogee` を宣言しているので、
cask だけ指定しても CLI が自動的に一緒に入ります。`postflight` で
staged bundle に対して `xattr -dr com.apple.quarantine` を走らせるので、
macOS 15 Gatekeeper が未署名 `.app` の初回起動を止めることはありません。

GitHub Release の zip を手動でダウンロードした場合は、自分で
`xattr -dr com.apple.quarantine /Applications/Apogee.app` を実行するか
(古い macOS なら右クリック → 開く) 必要があります。

### アップデート

```sh
brew upgrade --cask apogee-desktop
```

brew はこれを uninstall + install のペアとして処理します。cask の
`uninstall_preflight` は `bootout` で daemon を止めるものの、
`~/Library/LaunchAgents/dev.biwashi.apogee.plist` は意図的に残します。
これにより、次回 `Apogee.app` を起動したとき既存の daemon 設定を
そのまま拾えるので、`apogee onboard` が再実行されません。

### アンインストール

```sh
# 通常: daemon 停止 (desktop シェルが install したものであれば)、
# .app 削除、/opt/homebrew/bin/apogee-desktop ランチャー削除。
# ~/.apogee は残すので、observability 履歴は失われません。
brew uninstall --cask apogee-desktop

# 完全削除: 上記に加えて ~/.apogee (DuckDB ストア + ログ + config) と
# LaunchAgents plist も削除。observability 履歴は失われます。
brew uninstall --zap --cask apogee-desktop
```

通常 uninstall は `~/.apogee/installed-by-desktop` マーカーがある時
だけ daemon を片付けます。つまり **desktop-first ユーザー**
(`brew install --cask apogee-desktop` から始めた人) は cask 削除
だけで綺麗にロールバックでき、**CLI-first ユーザー**
(`apogee onboard` や `apogee daemon install` をターミナルから実行
していた人) は cask を消しても daemon は残ります。CLI formula は
`brew uninstall BIwashi/tap/apogee` で独立して削除できます。

## ソースから起動

```sh
# desktop バイナリをビルドします。Wails v2 で必須な -tags production
# は Makefile 側で設定済み。desktop シェルは internal/webassets を
# import しないので、`make build-web` への依存もありません。
make desktop-build

# ビルドして起動。daemon が動いていれば即 proxy モードで
# 127.0.0.1:4100 に繋ぎます。daemon 未起動なら初回 bootstrap フローに
# 入ります。APOGEE_DAEMON_ADDR でデフォルトポートを上書きできます。
make desktop-run
```

ウィンドウはブラウザ UI と同じダークテーマのトークンを使います。
今のところ dock やカスタムメニューバーは追加しておらず、トラフィック
ライトボタンや OS メニューは標準の Cocoa のままです。

## ローカルで `.app` を作る

Wails CLI を入れているかどうかで 2 択:

```sh
# A — Wails CLI を使う。desktop/build/bin/Apogee.app を生成。
# Wails のツールチェーンで iterate したい時に便利。
make desktop-app

# B — goreleaser snapshot。CI のリリース経路をそのままなぞるので
# authoritative です。universal バイナリ → scripts/bundle-desktop-app.sh
# で Apogee.app + launcher shim にラップ → zip 生成 →
# dist/homebrew/Casks/apogee-desktop.rb 再生成まで。
goreleaser release --snapshot --clean --skip=before,publish,validate
```

B がリリースビルドの正です。Wails CLI を一切呼ばず、
`scripts/bundle-desktop-app.sh` が `sips` + `iconutil` + `Info.plist`
のヒアドキュメントで `.app` を組み立てます。コード署名と notarization
は未対応ですが、配布時には Cask の `postflight` が quarantine xattr を
剥がすので初回起動も通ります。

## Dev モード (ホットリロード)

Wails dev モードは WebView 内で描画しつつ、フロントエンドは外部管理
の Next.js dev サーバーから読み込む方式なので、**3 つ** のプロセスを
同時に走らせる必要があります:

```sh
# ターミナル 1: collector を :4100 で起動。これが proxy の転送先
# かつ Next.js dev サーバーの /v1/* リライト先。
make run-collector

# ターミナル 2: Next.js dev サーバー (:3000)
make web-dev

# ターミナル 3: Wails dev ウィンドウ。desktop/wails.json の
# frontend:dev:serverUrl に従って http://localhost:3000 にプロキシ。
# Wails 自体は Next.js dev サーバーを起動しません。
make desktop-dev
```

`web/app/` 配下のファイルを編集すると Next.js HMR が走り WKWebView
側も自動更新されます。desktop バイナリ自体は再ビルドされないので、
`desktop/` や `internal/collector/` の Go コードを触ったら
`make desktop-dev` を再起動してください。

UI を触るだけなら元々のブラウザフロー
(`make run-collector` + `make web-dev` + `http://localhost:3000`)
が一番手早いです。desktop dev モードは単に WKWebView の chrome を
被せるだけなので UI 作業の本体にはあまり寄与しません。

## アーキテクチャメモ

- **Proxy handler**: `desktop/runmodes.go` の `runProxy()` が
  `httputil.NewSingleHostReverseProxy(target)` を `FlushInterval: -1`
  で包みます。SSE ストリーム (`/v1/events/stream`,
  `/v1/interventions/stream`) が buffer されないようにするためです。
- **Bootstrap**: `desktop/bootstrap.go` の `runBootstrap()` が初回
  フローを担当します。ネイティブダイアログは全部 `osascript` 経由
  なので cgo / AppKit バインディングを触りません。subprocess 完了後は
  `runProxy()` を呼ぶだけで、"setup 後モード" という別物ではなく
  warm start と同じ proxy モードに合流します。
- **in-process collector を持たない**: desktop シェルは
  `internal/collector`・`internal/store/duckdb`・`internal/webassets`
  を一切 import しません。リリースバイナリは ~10 MB (DuckDB 静的 lib
  を抱え込むと ~60 MB になる) で、DuckDB ファイルを所有するプロセスは
  プロセスツリーの中で daemon 1 つだけです。
- **UniformTypeIdentifiers フレームワークリンク**:
  `desktop/cgo_darwin.go` に `#cgo darwin LDFLAGS: -framework
  UniformTypeIdentifiers` 宣言があります。これが無いと Wails の
  WebKit バインディングが持つ `_OBJC_CLASS_$_UTType` への weak
  reference が `-ldflags "-s -w"` で strip され、リンクが
  `Undefined symbols for architecture arm64` で失敗します。Wails v2
  と Xcode 15 で見つかった既知の quirk です。

## 前提 (ソースビルド)

- macOS (Darwin) — Wails は WKWebView で描画します。
- Xcode Command Line Tools (`xcode-select --install`) — cgo /
  DuckDB / Wails の前提。
- `go.mod` に合った Go toolchain。
- Next.js のビルドに Node.js (`web/out` を再生成するときのみ)。
- `make desktop-app` / `make desktop-dev` を使うときは
  [`wails` CLI](https://wails.io/docs/gettingstarted/installation):

  ```sh
  brew install wails
  ```

  通常の `make desktop-build` / `make desktop-run` は
  `go build -tags production` だけで動くので **wails CLI は不要** です。

## 既知の制約

- **macOS 専用**。コードは移植可能ですが Linux / Windows 側は未確認。
- **コード署名 / notarization なし**。Cask が install 時に
  `com.apple.quarantine` を剥がすので Gatekeeper は初回起動を
  通しますが、GitHub Release zip を手動ダウンロードした場合は
  `xattr -dr com.apple.quarantine /Applications/Apogee.app` を
  自分で叩く必要があります。
- **カスタムメニューなし**。Wails デフォルト (EditMenu + WindowMenu)
  だけです。`apogee menubar` とのコード共有もしていません。
- **daemon が必須**。desktop シェルは daemon が居ない状態では
  ダッシュボードを表示できません。初回起動ユーザーには bootstrap
  フローが daemon を install してくれますが、cask を残したまま
  daemon だけ uninstall した場合は、再度 install するまでウィンドウ
  が空になります。

## なぜ Electron / Tauri ではなく Wails なのか

- **Go ネイティブ**。apogee モジュールは Go なので、desktop シェルを
  標準ライブラリと `wails/v2` 以外に依存させずに済み、結果として
  ~10 MB の reverse-proxy WKWebView ラッパーに収まっています。
- **Chromium ではなく WKWebView**。`.app` のサイズが ~8 MB 圧縮に
  収まり、OS 付属の WebView を再利用できます。Chromium 同梱の
  Electron なら ~150 MB 級。
- **新しいビルド言語が不要**。Tauri なら Rust、Electron なら Node
  を持ち込むことになる。Wails なら pure Go です。
