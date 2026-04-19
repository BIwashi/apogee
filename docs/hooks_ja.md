[English version / 英語版](./hooks.md)

# apogee hook の契約

Claude Code の hook エントリポイントは `apogee` バイナリそのものです。別途 hook スクリプトも、Python ランタイムも、hook 用の埋め込み FS も、展開する `hooks/` ディレクトリもありません。`apogee init` が実行中バイナリの絶対パスに `hook --event <X> --server-url ...` を付けたものを `.claude/settings.json` に書き込み、それでインストールは完了です。

本ドキュメントはエンドツーエンドの契約を説明します。Claude Code が stdin で渡すもの、apogee が stdout に書くもの、events POST のワイヤー形式、intervention claim フロー、動かない hook のデバッグ方法までカバーします。

---

## ワイヤー契約

Claude Code の hook は JSON over stdio です。hook イベントごとに Claude Code は次を行います。

1. `.claude/settings.json` の `hooks.<event>` 配列を引く。
2. 各エントリの `command` を、hook ペイロードを単一の JSON オブジェクトとして stdin に流しつつ実行する。
3. stdout を読む。`PreToolUse` で `decision: "block"` が返る、あるいは `UserPromptSubmit` で `hookSpecificOutput.additionalContext` が含まれていれば Claude Code はそれを尊重する。
4. 非 0 終了は hook 失敗扱いで、イベントによってはエージェントがブロックされる。

apogee はこの契約を厳密に守ります。`apogee hook` 呼び出しは必ず次の順で動きます。

1. stdin から hook ペイロード全体を読む。
2. （`PreToolUse` / `UserPromptSubmit` のみ）`POST /v1/sessions/<session_id>/interventions/claim` で operator intervention の claim を試みる。
3. 元の stdin ペイロード（素通し）または Claude Code decision JSON（claim 成功時）を stdout に書く。
4. hook テレメトリを `/v1/events` に POST する。
5. 終了コードは常に 0。転送失敗は stderr に残して `runHook` を `return nil` で抜ける。失敗する hook は Claude Code を壊すからで、それは観測ツールのやるべきことではない。

実装は [`internal/cli/hook.go`](../internal/cli/hook.go) に、テストは [`internal/cli/hook_test.go`](../internal/cli/hook_test.go) にあります。

---

## `apogee init` 経由のインストール

```sh
apogee init                # 既定: user スコープ (~/.claude/settings.json)
apogee init --scope project
apogee init --dry-run
apogee init --force        # 旧 python3 send_event.py 行もまとめて除去
```

実行後の `settings.json` の hooks セクションはこのような形になります。

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/usr/local/bin/apogee hook --event PreToolUse --server-url http://localhost:4100/v1/events"
          }
        ]
      }
    ],
    "PostToolUse": [
      { "hooks": [ { "type": "command", "command": "/usr/local/bin/apogee hook --event PostToolUse --server-url http://localhost:4100/v1/events" } ] }
    ],
    ...
  }
}
```

ここに書かれるバイナリ絶対パスは、init 実行時点の `os.Executable()` の出力です。brew インストールから `apogee init` を走らせれば `/opt/homebrew/bin/apogee` を指し、ローカル開発の `./bin/apogee` から走らせればその dev バイナリを指します。コントリビューターが brew の本番インストールを汚さずにローカルコレクターで Claude Code を観測できるのはこの仕組みによるものです。

既定スコープは `user`。マシン上の全 Claude Code プロジェクトを 1 度のインストールでカバーし、hook は実行時に `source_app` を導出するので、プロジェクトごとの正しいラベルが自動で付きます。

---

## サポートする hook イベント

正式リストは [`internal/cli/init.go::HookEvents`](../internal/cli/init.go) にあります。

| イベント | 役割 |
| --- | --- |
| `SessionStart` | 新しい Claude Code セッション開始 |
| `SessionEnd` | セッション終了 / 終端 |
| `UserPromptSubmit` | ユーザーがプロンプト送信 → ターンルート span を open |
| `PreToolUse` | ツール呼び出し直前 → tool span を open |
| `PostToolUse` | ツール呼び出し成功 → tool span を close |
| `PostToolUseFailure` | ツール呼び出し失敗 → ERROR 状態で close |
| `PermissionRequest` | オペレーター向けの HITL 権限要求 |
| `Notification` | ターン中に出る Claude Code 通知 |
| `SubagentStart` | subagent 生成 → subagent span を open |
| `SubagentStop` | subagent 回収 → subagent span を close |
| `Stop` | ターン終了 → ルート span を close |
| `PreCompact` | コンテキスト窓の compaction 直前 |

`apogee init` は上記の順番で 1 行ずつ書き込みます。

---

## `source_app` の動的導出

`apogee hook --event X` は呼び出し時に次の順序で `source_app` ラベルを決めます。

1. `$APOGEE_SOURCE_APP` — 明示的な上書き。
2. `basename $(git rev-parse --show-toplevel)` — git リポジトリ内なら、そのリポ名。
3. `basename $PWD` — そうでなければ現在ディレクトリ名。
4. 最後の手段として `"unknown"` リテラル。

これが、既定の `apogee init` が `--source-app` を固定しない理由です。user スコープに 1 回インストールすれば、各プロジェクトに自動的に正しいラベルが付きます。`~/work/my-api` で `claude` を起動すれば `source_app=my-api`、`~/work/apogee` で起動すれば `source_app=apogee` になり、プロジェクト設定は一切不要です。

固定ラベルも引き続き使えます。`apogee init --source-app foo` で各 hook コマンドに `--source-app foo` が書き込まれ、実行時導出はスキップされます。

導出ヘルパーは [`internal/cli/hook.go`](../internal/cli/hook.go) の `deriveSourceAppRuntime` です。

---

## intervention claim フロー

`PreToolUse` と `UserPromptSubmit` は operator intervention を運ぶことができます。テレメトリを POST する前に、hook は次を行います。

1. stdin ペイロードから `session_id`（と任意で `turn_id`）を取り出す。
2. `POST /v1/sessions/<session_id>/interventions/claim` に `{"hook_event": "<event>", "turn_id": "..."}` を 1.5 秒の予算で送る。
3. `204 No Content` なら素通しモード — stdin を stdout にエコーし、hook イベントを通常どおり POST する。
4. `200 OK` で claim 済みの行が返ってきたら、`delivery_mode` を見る。
   - `PreToolUse` で `interrupt` → stdout に `{"decision":"block","reason":"<message>"}` を書く。
   - `UserPromptSubmit` で `context` → stdout に `{"hookSpecificOutput":{"additionalContext":"<message>"}}` を書く。
   - `both` → 先に発火した hook が勝ち。`PreToolUse` が優先される。
   claim されたモードが hook と合わない場合（例: `PreToolUse` に `context`）、hook はログを残して素通しし、スイーパーがいずれ行を expire します。
5. ベストエフォートで `POST /v1/interventions/<id>/delivered` を呼び、コレクター側で行を `delivered` に遷移させて `intervention.delivered` を SSE 配信する。

完全なライフサイクルと REST サーフェスは [`interventions.md`](interventions.md) を、実装は [`internal/cli/hook.go`](../internal/cli/hook.go) の `maybeClaimIntervention` / `decisionForMode` / `markDelivered` を参照してください。

---

## ワイヤー形式 — `POST /v1/events`

すべての hook イベントは 1 POST を生みます。ボディは JSON で、形は [`internal/ingest/payload.go::HookEvent`](../internal/ingest/payload.go) と一致します。

```json
{
  "source_app": "my-api",
  "session_id": "sess-01HXYZ...",
  "hook_event_type": "PreToolUse",
  "timestamp": 1713138123456,

  "tool_name": "Bash",
  "tool_use_id": "tool_01HXYZ...",
  "payload": { ... stdin の JSON そのまま ... }
}
```

トップレベルフィールドは常に存在します。`flatHookFields`（`tool_name`、`tool_use_id`、`error`、`is_interrupt`、`agent_id`、`agent_type`、`stop_hook_active`、`notification_type`、`custom_instructions`、`source`、`reason`、`model_name`、`prompt` など）に入っているキーは stdin ペイロードからトップレベルに昇格され、コレクター側が `payload` を触らずに読めるようになります。stdin の生バイトは `payload` に丸ごと保存され、reconstructor は Claude Code が出した通りのバイト列を見ることができます。

Content-Type は `application/json`、User-Agent は `apogee-hook/<build-version>` です。

---

## デバッグ

### 1. hook はそもそも発火しているか？

適当な Claude Code 操作（REPL で `ls` を呼ぶなど）を一発走らせ、直近のログを確認します。

```sh
curl -s 'http://127.0.0.1:4100/v1/sessions/recent' | jq '.[0:3]'
```

返事が空なら Claude Code は一度も hook を発火していません。`.claude/settings.json` を確認しましょう。

```sh
jq '.hooks | keys' ~/.claude/settings.json
```

[サポートする hook イベント](#サポートする-hook-イベント)の 12 種がすべて揃っている必要があります。

### 2. コレクターに到達できているか？

空ペイロードで手動実行します。

```sh
echo '{"session_id":"s-debug","hook_event_type":"UserPromptSubmit"}' \
  | apogee hook --event UserPromptSubmit --server-url http://127.0.0.1:4100/v1/events
```

stdout は入力をそのままエコー（または intervention が当たれば decision JSON）し、stderr は空のはずです。stderr に `apogee hook:` で始まる行が出ていたら、それが hook 内部で飲み込まれた転送失敗です。終了コードは 0 でも POST は届いていません。

コレクターを直接叩きます。

```sh
curl -s http://127.0.0.1:4100/v1/healthz
```

### 3. reconstructor が span を組み立てているか？

tool 名から該当 span を探します。

```sh
curl -s 'http://127.0.0.1:4100/v1/turns/active' | jq '.[0].turn_id'
```

そのターンの span ツリーを取得します。

```sh
curl -s "http://127.0.0.1:4100/v1/turns/<turn_id>/spans" | jq .
```

hook は発火してコレクターに届いているのに span が現れない場合、reconstructor がバリデーションエラーに当たっています。サーバーログの `reconstructor: ...` 行を見てください。

### 4. intervention claim は動いているか？

実行中のターンに対して `POST /v1/interventions` で intervention を投入し、次の hook を観察します。

```sh
curl -s http://127.0.0.1:4100/v1/interventions \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"<sess>","message":"Stop and reconsider","delivery_mode":"interrupt","scope":"this_turn","urgency":"high"}'
```

次の `PreToolUse` hook がこれを claim し、stdout に `{"decision":"block","reason":"Stop and reconsider"}` が流れ、ダッシュボード上で行が `delivered` に遷移するはずです。

---

## 契約の不変条件

- **終了コードは常に 0。** あらゆる失敗パスは stderr にログを出してから `runHook` を `return nil` で抜けます。
- **stdin は逐語的にエコーする。** claim された intervention が置き換える場合を除き、stdout は stdin と一致します。stdout が stdin と違うのは claim の時だけです。
- **テレメトリ POST はベストエフォート。** コレクター障害で hook が止まることはありません。claim は 1.5 秒、events POST は `--timeout`（既定 2 秒）でそれぞれタイムアウトします。
- **ディスクへの副作用はない。** hook はファイルを書きません。永続状態はコレクター側だけに存在し、hook 自体は stdin の hook ペイロードと stderr ストリームにしか触りません。
