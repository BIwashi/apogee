# 横断的なサイドドロワー

PR #36 では、ダッシュボード上のすべてのテーブルで Datadog 風の「行クリック →
右側からスライドインする詳細ドロワー」パターンを導入します。`/agents` や
`/sessions`、セッション詳細の Turns タブ、ターン詳細のスパンツリーで行をクリック
すると、ビューポートの右端からドロワーがスライドインします。現在のページは
アンマウントされないため、フィルタ・スクロール位置・SSE 購読といった作業
コンテキストはすべてそのまま保持されます。

## URL コントラクト

ドロワーの状態は完全にクエリ文字列に保持されるため、深いリンクがそのまま
動作し、ブラウザの戻るボタンでドロワーを閉じることができ、ドロワー内部での
再帰的な遷移でも新しいパネルがフラッシュすることはありません。

```
?drawer=agent&drawer_id=<agent_id>
?drawer=session&drawer_id=<session_id>
?drawer=turn&drawer_sess=<session_id>&drawer_turn=<turn_id>
?drawer=span&drawer_trace_id=<trace_id>&drawer_span_id=<span_id>
```

ドロワー向けのクエリパラメータはすべて `drawer_*` プレフィックスで名前空間を
分けているため、`/session?id=…&drawer=agent&drawer_id=…` のように、ページ自体が
すでに `id` / `sess` を使っているルートでも横断ドロワーをそのまま重ねられます。

`/sessions?drawer=agent&drawer_id=ag-1234` をリロードすると、セッションカタログの上に
エージェントドロワーがポップアップした状態でロードされるため、そのまま
チームメイトに共有できます。`Esc` を押すか背景をクリックすると、
`router.replace()` でドロワーが閉じ、ページのリロードは発生しません。

## 行クリック vs フルページ

ドロワーを開く各行は、フルページへのアンカーとしても機能します。通常の左クリック
ではドロワーがポップアップし (`event.preventDefault()` で抑止)、`Cmd+Click` /
`Shift+Click` / ミドルクリック / 右クリック → 「新しいタブで開く」は従来通り
動作します。フルページを並べて見ながら作業したい運用者を、ダッシュボードが
邪魔することはありません。

## セッションラベル

生の `sess-…` 文字列だけでは何のセッションか分かりません。セッション ID を
表示するすべてのテーブルでは、新しい `SessionLabel` コンポーネントを使い、
source_app と `/v1/sessions/:id/summary` から取得した 1 行ヘッドラインを
併せて表示します。同じセッションを参照する複数の行は 1 つの SWR キャッシュ
キーを共有するため、ネットワークアクセスは ID ごとに 1 回だけです。

## バックエンド

ドロワーに必要な 2 本の読み取り専用ルートと 1 本の write-trigger ルートを追加しました。

- `GET /v1/agents/:id/detail` — エージェント行に加え、親と直接の子、過去 20
  ターン分の関連ターン、ツール別ヒストグラムを返します。レスポンスの
  `agent` フィールドには `agent_summaries` から読んだ LLM 生成の
  `title` / `role` / `summary_model` / `summary_at` が含まれます。
- `POST /v1/agents/:id/summarize` — エージェント単位の Haiku title/role
  ワーカーを手動で再実行します。`202 Accepted` で返り、ドロワーの Summary
  セクションは `session.updated` SSE broadcast で再検証されます。
- `GET /v1/spans/:trace_id/:span_id/detail` — 単一のスパンに加え、親 (トレース
  ルートなら nil) と直接の子を返し、ドロワーの「Parent」タブが 1 リクエストで
  クリックスルー遷移を描画できるようにします。

読み取り系エンドポイントは既存の `spans` / `turns` と PR #100 で追加された
`agent_summaries` テーブルを集約するクエリで、PR #100 の追加以外の
スキーママイグレーションは発生しません。

## AgentDrawer Summary セクション

PR #100 は AgentDrawer の先頭に Summary セクションを追加します。エージェント
の LLM 生成 title を先頭に、role を 2 行目に、生成メタデータ
(`generated_at` / `model`) と `POST /v1/agents/:id/summarize` を叩く
`Regenerate` ボタンを並べます。`/agents` カタログも同様に、各行が
`agent_type` の生文字列ではなく `title` を先頭に表示するため、同じ
セッション内の並列 main エージェントが同じ見た目になりません。
