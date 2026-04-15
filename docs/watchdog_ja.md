# Watchdog — 統計的な異常検知

apogee には Datadog Watchdog にインスパイアされた異常検知が組み込まれて
います。バックグラウンドのワーカーが `metric_points` を読み、直近 60 秒
のウィンドウを過去 24 時間のローリングベースラインと比較し、ウィンドウ
の値が標準偏差の 3 倍を超えて外れたタイミングで `watchdog_signals`
テーブルに行を追記します。

検知ロジックは `internal/watchdog/` にあります:

- `zscore.go` — ベースラインの平均・標準偏差の計算と z-score の閾値判定。
- `watchdog.go` — `Worker` 型と、`Tick`（既定 60 秒）ごとに監視対象の
  メトリクスを評価し、シグナルを DuckDB に書き込み、SSE ハブで配信する
  ループ。

永続化は `internal/store/duckdb/watchdog_signals.go`（CRUD と spell の
ヘルパー）にあり、スキーマは既存のテーブルと並んで
`internal/store/duckdb/schema.sql` に定義されています。ダッシュボードは
`web/app/components/WatchdogBell.tsx` と
`web/app/components/WatchdogDrawer.tsx` を通じてシグナルを表示します。

## シグナルの定義

watchdog のシグナルとは、統計的に異常な `metric_points` の datapoint
を指します。計算手順は次の通りです:

1. 与えられた `(metric_name, labels_json)` タプルについて、直近 60 秒
   のサンプルをすべて読み込みます — これが **ウィンドウ** です。
2. 同じタプルの直近 24 時間のサンプルをすべて読み込みます — これが
   **ベースライン** です。
3. ベースラインの平均 (`μ`) と母集団標準偏差 (`σ`) を計算します。
   サンプル数が 3 未満、あるいは分散がゼロの場合は退化したベースライン
   としてスキップされ、z-score は計算されません。
4. ウィンドウの各サンプルについて `z = (x − μ) / σ` を計算します。
5. ウィンドウ内で観測された `|z|` の最大値を追跡し、`SignalThreshold`
   (3.0) を超えていればシグナルを発火します。

シグナルは **spell ごとに 1 度だけ** 発火します。spell は、ウィンドウ
の全サンプルが `|z| < NormalThreshold` (1.5) を `NormalForWait`（既定
3 分）以上維持したときに終了します。終了後は新しい spell が始まる可能
性があります。spell が開いている間、検知器はそれ以降の deviation を
握りつぶすので、ベルが毎ティック点滅することはありません。

### 重要度

| `|z|` の幅       | 重要度     |
|------------------|------------|
| `[3, 5)`         | `info`     |
| `[5, 8)`         | `warning`  |
| `[8, ∞)`         | `critical` |

## 監視対象メトリクス

`DefaultMonitoredMetrics()` には `internal/metrics/collector.go` が
fleet 全体で書き出している 4 つのゲージ／カウンタが登録されています。
各エントリは `(value, mean, stddev)` を引数とする `fmt.Sprintf` の
ヘッドライン・テンプレートを持ちます:

- `apogee.turns.active` — `Active turns spiked to %.0f (baseline %.1f ± %.1f)`
- `apogee.errors.rate` — `Error rate surge — %.1f/s vs baseline %.2f/s (±%.2f)`
- `apogee.tools.rate` — `Unusual tool activity — %.1f/s vs baseline %.2f/s (±%.2f)`
- `apogee.hitl.pending` — `HITL backlog growing — %.0f pending vs baseline %.1f (±%.2f)`

`source_app` ごとなど、追加のラベル付きタプルを監視したい運用者は
`Worker` のフィールドを直接拡張してください — `Metrics` は公開された
スライスです。

## HTTP インターフェース

- `GET /v1/watchdog/signals?status=unacked&limit=N`
  最近のシグナルを新しい順に返します。クエリパラメータ:
  - `status=unacked` で `acknowledged = FALSE` の行のみに絞り込みます。
  - `severity=info|warning|critical` で重要度を絞り込みます。
  - `limit` でレスポンス件数を制限します（既定 50、最大 200）。
- `POST /v1/watchdog/signals/:id/ack`
  `acknowledged` を TRUE にし、`acknowledged_at` を打刻します。
  冪等です — 2 度目の ack は no-op で、現在の行を返します。

SSE には新しいイベント `watchdog.signal` が 1 種類追加されます。
ペイロードは `internal/sse.WatchdogSnapshot` をミラーしており、ラベル
はフラットなオブジェクトに展開され、`evidence` フィールドはドロワーの
スパークラインが消費する `{ window, baseline, z }` 型を持ちます。

## 設定

すべての可変パラメータは `Worker` 構造体に直接定義されているので、
テストでピン留めしたり、運用者がコードでオーバーライドしたりできます:

| フィールド       | 既定値           | 用途                                           |
|------------------|------------------|------------------------------------------------|
| `Tick`           | 60s              | 検知ループの実行間隔。                          |
| `Window`         | 60s              | 評価ウィンドウの長さ。                          |
| `Baseline`       | 24h              | 履歴ベースラインの長さ。                        |
| `NormalForWait`  | 3min             | spell を閉じるまでに必要な静穏時間。             |
| `Metrics`        | 4 件             | 1 ティックで評価するタプル一覧。                |
| `Clock`          | `time.Now().UTC()` | テストで差し替え可能な時刻ソース。           |

## 検証手順

1. `go vet ./... && go build ./... && go test ./... -race -count=1`
2. collector を起動し、metric_points のサンプラーを 1 分以上回したあと、
   1 つのセッションに対して `/v1/events` を 100 回 POST するなどして
   `apogee.tools.rate` を逸脱させます。次のティックで
   `GET /v1/watchdog/signals` にシグナルが現れるはずです。
3. Web ダッシュボードでベルに `1` のバッジが付き、ドロワーにシグナル
   が一覧表示され、Acknowledge ボタンを押すとバッジが 0 に戻ることを
   確認します。
