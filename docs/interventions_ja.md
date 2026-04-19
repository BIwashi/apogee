[English version / 英語版](./interventions.md)

# Operator Interventions

apogee はすでに Human-In-The-Loop パイプラインを備えていて、Claude Code が権限を聞いてきたときにオペレーターが返答できます。**Operator Interventions** はその逆方向です。オペレーターが実行中の Claude Code セッション *に向けて* メッセージを投入すると、次の hook 発火でそのメッセージが Claude Code decision として Claude Code に返されます。

Intervention が Claude Code を壊すことはありません。すべては既存の `.claude/settings.json` の hook を経由し、hook スクリプトは失敗時に素通しモードへ劣化します。

## ライフサイクル

```
queued → claimed → delivered → consumed
                            ↘ expired
                            ↘ cancelled
```

- **queued** — オペレーターが `POST /v1/interventions` で投入した直後。まだどの hook も拾っていない。
- **claimed** — 受信した hook が `POST /v1/sessions/<session_id>/interventions/claim` で行を atomic に取得した状態。他の並行 hook は `claimed` を見てスキップする。
- **delivered** — hook が Claude Code decision JSON を stdout に書き、`POST /v1/interventions/<id>/delivered` で報告できた。
- **consumed** — reconstructor が同じセッションの下流 hook イベント（`PostToolUse`、`Stop` など）を観測した。Claude Code が block / context injection を受け取って処理したことの best-effort プロキシ。
- **expired** — 自動 expire の TTL が切れた、あるいは intervention のスコープが終了した（ターン終了 / セッション終了）。
- **cancelled** — オペレーターが queued または claimed のうちに撤回した。

## デリバリーモード

| モード | hook の経路 | Claude Code への出力 |
| ---------- | ------------------ | ------------------------------------------------------------ |
| `interrupt`| `PreToolUse`       | `{"decision":"block","reason":"<message>"}`                  |
| `context`  | `UserPromptSubmit` | `{"hookSpecificOutput":{"additionalContext":"<message>"}}`   |
| `both`     | どちらか        | 先に来た hook が勝ち。`PreToolUse` が優先される              |

`interrupt` は既定値で、唯一「エージェントを実行中に意図的に止める」モードです。`context` は「背景ヒント」で、次のユーザーターンでエージェントが考慮できます。`both` は安全網で、ツール呼び出しが直前に発生すれば interrupt、そうでなければ次のユーザーターンで追加コンテキストとして注入します。

## スコープ

- **this_turn** — 投入時に走っていた `turn_id` にバインド。そのターンが閉じたら expire。
- **this_session** — delivered / cancelled / セッション終了まで生き続ける。

`this_turn` が既定で、オペレーターの典型的な意図（「このターンの挙動が怪しいから今すぐ止めたい」）に一致します。

## 緊急度

`high` / `normal` / `low` はそれぞれ attention エンジンの `intervene_now` / `watch` / `healthy` バケットに対応します。緊急度は claim の優先度にも反映され、high な intervention は同じセッションの normal や low よりも先に claim されます。

## 自動 expire TTL

各 intervention は `created_at + TTL`（既定 10 分、`[interventions]` 設定ブロックで調整可）の `auto_expire_at` を持ちます。バックグラウンドスイーパーが 15 秒ごとに走り、期限切れで終端状態でない行を `expired` に倒します。

## HTTP API

| method | path | 用途 |
| ------ | -------------------------------------------------- | --------------------------------- |
| POST   | `/v1/interventions`                                | オペレーターによる新規投入（201） |
| GET    | `/v1/interventions/{id}`                           | 単一取得 |
| POST   | `/v1/interventions/{id}/cancel`                    | オペレーターによるキャンセル（終端状態なら 409） |
| POST   | `/v1/interventions/{id}/delivered`                 | hook 側の delivered 報告 |
| POST   | `/v1/interventions/{id}/consumed`                  | hook / reconstructor 側の consumed 報告 |
| POST   | `/v1/sessions/{id}/interventions/claim`            | hook 向けの atomic "1 件ください"（該当なしは 204） |
| GET    | `/v1/sessions/{id}/interventions`                  | セッションの全 intervention 一覧 |
| GET    | `/v1/sessions/{id}/interventions/pending`          | 非終端の intervention 一覧（ホットパス） |
| GET    | `/v1/turns/{turn_id}/interventions`                | ターンにスコープされた pending 一覧 |

### POST /v1/interventions のリクエストボディ

```json
{
  "session_id": "sess-abc",
  "turn_id": "turn-xyz",
  "message": "Stop, reconsider your approach",
  "delivery_mode": "interrupt",
  "scope": "this_turn",
  "urgency": "normal",
  "operator_id": "op-alice",
  "notes": "",
  "ttl_seconds": 600
}
```

バリデーション:
- `message` は必須。最大 4096 文字（設定可能）。
- `delivery_mode` は `interrupt | context | both` のいずれか。
- `scope` は `this_turn | this_session` のいずれか。`this_session` の場合 `turn_id` はサーバー側で無視される。
- `urgency` は `high | normal | low` のいずれか。

## hook 側の契約

Go ネイティブの `apogee hook` サブコマンド（実装は [`internal/cli/hook.go`](../internal/cli/hook.go)）がクライアント側を担当します。バイナリ自身が Claude Code の hook エントリポイントで、`.claude/settings.json` には次のような行が並び、

```
apogee hook --event PreToolUse --server-url http://localhost:4100/v1/events
```

`apogee init` が実行中バイナリの絶対パスを書き込みます。

1. `PreToolUse` と `UserPromptSubmit` では、hook サブコマンドはテレメトリを `/v1/events` に POST する *前* に operator intervention の claim を試みる。
2. `POST /v1/sessions/<sid>/interventions/claim` を、hook イベントと（任意の）turn id つきで呼ぶ。ベース URL は `--server-url` の末尾 `/v1/events` を剥がしたもの。
3. 204 なら素通しモードのまま。stdin を stdout にエコーし、通常どおり hook イベントを POST する。
4. 200 なら Claude Code decision JSON を組み立てる。`PreToolUse` かつ `interrupt` / `both` の場合は `{"decision":"block","reason":"..."}`、`UserPromptSubmit` かつ `context` / `both` の場合は `{"hookSpecificOutput":{"additionalContext":"..."}}`。これを stdin エコーの代わりに stdout へ書く。
5. 続けて `POST /v1/interventions/<id>/delivered` を呼び、コレクターは行を `delivered` に遷移させて `intervention.delivered` を SSE 配信する。

どのネットワーク呼び出しもベストエフォートです。エラーは stderr にログして飲み込むので、Claude Code の hook パイプラインが壊れることはなく、hook サブコマンドは常に exit 0 を返します。

## SSE イベント

状態遷移ごとに 1 イベントを、既存の `/v1/events/stream` チャネルで配信します。

- `intervention.submitted`
- `intervention.claimed`
- `intervention.delivered`
- `intervention.consumed`
- `intervention.expired`
- `intervention.cancelled`

6 つすべてが同じワイヤーエンベロープ（`{type, at, data}`）を共有し、`data.intervention` に `web/app/lib/api-types.ts :: Intervention` と一致する形のオブジェクトを持ちます。

## 設定

`~/.apogee/config.toml`:

```toml
[interventions]
auto_expire_ttl_seconds = 600
sweep_interval_seconds = 15
both_fallback_after_seconds = 60
max_message_chars = 4096
```

各フィールドは環境変数でも上書きできます。

- `APOGEE_INTERVENTIONS_TTL_SECONDS`
- `APOGEE_INTERVENTIONS_SWEEP_SECONDS`
- `APOGEE_INTERVENTIONS_BOTH_FALLBACK_SECONDS`
- `APOGEE_INTERVENTIONS_MAX_MESSAGE_CHARS`

優先順: env > TOML > 既定（summarizer と同じ）。

## 失敗モード

- **コレクター不到達**: hook は stderr にログを出して素通しを返します。ダッシュボード上では intervention は queued のまま残り、次の hook が到達した時点で retroactively に claim されます。
- **hook が claim 後 delivered 前にクラッシュ**: 行は `claimed` のまま残り、自動 expire スイーパーがいずれ `expired` に倒します。
- **claimed の途中でオペレーターがキャンセル**: 行は `cancelled` に遷移。その後の `delivered` POST は 409 で拒否されます。
- **下流の hook が一切発火しない**: 行は `delivered` のまま残り、ターンが閉じたときに reconstructor の `ExpireForTurn` が倒すか、TTL スイーパーが拾います。

UI（PR #15）は `delivered` をソフト終端として扱うべきです。メッセージはすでにエージェントに見せているからです。`consumed` は Claude Code が先へ進んだ視覚的な確認として予約します。

## UI ウォークスルー

PR #15 は `web/app/components/` 以下の 3 つの React コンポーネントと、それをターン詳細ページに貼り付ける composite セクションを追加しました。

- **`InterventionComposer`** — キーボード優先のフォーム。textarea と 3 つのラジオグループ（delivery mode / scope / urgency）、4096 文字の上限に対するライブ文字数、urgency を反映する 3px の左ボーダー色。`Ctrl/Cmd+Enter` で送信、`Esc` でクリア。親が `autoFocus` を渡せばマウント時に textarea へフォーカスする。
- **`InterventionQueue`** — セッションに紐づく `queued` / `claimed` / `delivered` 行のライブカード。`/v1/sessions/<id>/interventions/pending` を 2 秒間隔の SWR で引き、該当する `intervention.*` SSE イベントが来るたびに `mutate()` する。各行に staleness ピルが付き、30 秒で `waiting 45s`、120 秒で `stalled — no hook activity` に昇格する。
- **`InterventionTimeline`** — 終端行（`consumed` / `expired` / `cancelled`）のコンパクトな履歴カード。queue と同じチップ / アイコン語彙を使い、既定で 20 行に折り畳み、`show more` ボタンで展開できる。
- **`OperatorQueueSection`** — composite の糊。composer と queue を横並び（1100px 未満でスタック）に、その下に timeline を置き、`web/app/turn/page.tsx` の Recap + HITL グリッドの上にマウントする。リアクティブな HITL よりオペレーター発のアクションが先に並ぶ。

ターン詳細ページのヘッダーには、現在のターンに queued intervention が 30 秒を超えたときだけ脈打つ staleness チップが出ます。チップは 30 秒で warning トーンの `OPERATOR WAITING · 45s`、120 秒で critical トーンの `NO HOOK ACTIVITY · 2m 12s` になり、アイドルセッションの安全網を明示的に可視化します。

セッション詳細ページでは Overview タブに **Intervention summary** カードがあり、`N queued · M in flight · K lifetime` を表示し、`Open composer →` リンクで最新の実行中ターン（`?compose=1` 付き）へ直接ジャンプできます。Turns タブには実行中ターンごとに **Intervene** ボタンが並び、同じディープリンクでターン詳細を開きます。

### キーボードショートカット

ターン詳細ページでの `Alt+I` は、現在のセレクションに関わらず composer を開いてフォーカスします。ショートカットは `OPERATOR QUEUE` セクションヘッダーの横に `kbd` ヒントとして表示されます。

### ディープリンク

- `/turn?sess=<sess>&turn=<turn>&compose=1` — ターン詳細を composer にフォーカスして開く。
- `/session?id=<sess>&tab=turns` — 実行中ターンリストへ飛ぶ。行ごとの Intervene ボタンから composer を開ける。
