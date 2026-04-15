[English version / 英語版](./design-tokens.md)

# apogee デザイントークン

apogee のビジュアル識別は、NASA Artemis Graphic Standards Guide（2021 年 9 月版）から継承しており、これは姉妹プロジェクト [aperion](https://github.com/BIwashi/aperion) と共有されているパレットです。本ドキュメントがデザインシステムの正式仕様です。整合を取るべきソースは次の 3 つです。

1. `web/app/globals.css` — CSS 変数とユーティリティクラス
2. `web/app/lib/design-tokens.ts` — 型付きの TypeScript 再エクスポート
3. このファイル

どれか一つを変更したら、残る二つも同じコミットで更新してください。

---

## カラーパレット

### Artemis コア

| Token            | Hex       | Role                                             |
| ---------------- | --------- | ------------------------------------------------ |
| `artemis.red`    | `#FC3D21` | エラー、重大アラート、intervene-now              |
| `artemis.blue`   | `#0B3D91` | プライマリブランド、リンク、アクティブナビ       |
| `artemis.earth`  | `#27AAE1` | セカンダリアクセント、情報、チャートハイライト   |
| `artemis.shadow` | `#58595B` | ミュートテキスト、暗い背景上のボーダー           |
| `artemis.space`  | `#A7A9AC` | セカンダリテキスト、ラベル、プレースホルダー     |
| `artemis.white`  | `#FFFFFF` | 暗背景のプライマリテキスト                       |
| `artemis.black`  | `#000000` | 最も深い背景の参照値                             |

### ダークサーフェス

| Token                  | Hex       | 用途                                    |
| ---------------------- | --------- | --------------------------------------- |
| `surface.deepspace`    | `#06080f` | ページ背景                              |
| `surface.surface`      | `#0c1018` | カード、サイドバー                      |
| `surface.raised`       | `#141a24` | ホバー、浮いたカード                    |
| `surface.overlay`      | `#1c2333` | モーダル、ドロップダウン                |
| `surface.border`       | `#1e2a3a` | カードボーダー、区切り線                |
| `surface.borderBright` | `#2a3a50` | アクティブボーダー、フォーカスリング    |

### セマンティックステータス

| Token             | Hex       | 出典               | 用途                         |
| ----------------- | --------- | ------------------ | ---------------------------- |
| `status.critical` | `#FC3D21` | NASA Red           | ツール失敗、intervene now    |
| `status.warning`  | `#E08B27` | Warm Earth shift   | 権限要求、watch              |
| `status.success`  | `#27E0A1` | Cool complement    | セッション完了、healthy      |
| `status.info`     | `#27AAE1` | Earth Blue         | 実行中、情報                 |
| `status.muted`    | `#58595B` | Shadow Gray        | オフライン、非アクティブ     |

### アクセントグラデーション

ブランドの瞬間（ヒーロー見出し、セクション見出し横のブランドバー、アクティブなナビインジケーター）にだけ使います。

```
linear-gradient(135deg, #0B3D91 0%, #27AAE1 50%, #FC3D21 100%)
```

---

## ライトテーマ（PR #33）

apogee は PR #33 までダーク専用でした。2 つめのパレットは
`web/app/globals.css` の `:root[data-theme="light"]` にぶら下がり、
`web/app/lib/theme.tsx` の `ThemeProvider` が駆動します。ユーザー向け
コントロールは 3 か所あります。

1. TopRibbon の三状態トグル（`Monitor` / `Sun` / `Moon` アイコン）
2. `/settings` の **Appearance** セグメント
3. `/styleguide` の先頭にあるトグル — デザイナーがページを離れずに
   両テーマの全トークンを確認できます

`preference` は `system → light → dark` の順で循環します。`system`
は `matchMedia` リスナー経由で `prefers-color-scheme` を追跡し、
`apogee:theme` localStorage キーを削除します。`light` / `dark` は
明示的な上書きとして永続化されます。`app/layout.tsx` のインライン
スクリプトは React がハイドレートする**前**に `data-theme` をセット
するため、初回ペイントで誤テーマがフラッシュすることはありません。

### パレット比較

| Token             | Dark       | Light      | 役割                     |
| ----------------- | ---------- | ---------- | ------------------------ |
| `--bg-deepspace`  | `#06080f`  | `#f8fafc`  | ページ背景               |
| `--bg-surface`    | `#0c1018`  | `#ffffff`  | カード/サイドバー背景    |
| `--bg-raised`     | `#141a24`  | `#f1f5f9`  | ホバー/浮上面            |
| `--bg-overlay`    | `#1c2333`  | `#ffffff`  | モーダル/ドロップダウン  |
| `--border`        | `#1e2a3a`  | `#e2e8f0`  | 既定のボーダー           |
| `--border-bright` | `#2a3a50`  | `#cbd5e1`  | アクティブボーダー       |
| `--artemis-white` | `#ffffff`  | `#0f172a`  | プライマリ文字（反転）   |
| `--artemis-space` | `#A7A9AC`  | `#475569`  | セカンダリ文字           |
| `--artemis-shadow`| `#58595B`  | `#64748b`  | ターシャリ文字           |
| `--artemis-red`   | `#FC3D21`  | `#FC3D21`  | アクセント（共有）       |
| `--artemis-blue`  | `#0B3D91`  | `#0B3D91`  | アクセント（共有）       |
| `--artemis-earth` | `#27AAE1`  | `#1d91c9`  | アクセント（少し濃く）   |
| `--status-critical` | `#FC3D21` | `#dc2626` | critical                 |
| `--status-warning`  | `#E08B27` | `#d97706` | warning                  |
| `--status-success`  | `#27E0A1` | `#15803d` | success                  |
| `--status-info`     | `#27AAE1` | `#0e7fbf` | info                     |
| `--status-muted`    | `#58595B` | `#64748b` | muted                    |

### シャドウ/オーバーレイ

ライト版のドロップシャドウは純黒ではなく柔らかなスレートです。

| Token                | Dark                             | Light                             |
| -------------------- | -------------------------------- | --------------------------------- |
| `--shadow-sm`        | `0 1px 2px rgba(0,0,0,0.4)`     | `0 1px 2px rgba(15,23,42,0.08)`  |
| `--shadow-md`        | `0 4px 12px rgba(0,0,0,0.5)`    | `0 4px 12px rgba(15,23,42,0.08)` |
| `--shadow-lg`        | `0 12px 32px rgba(0,0,0,0.6)`   | `0 12px 32px rgba(15,23,42,0.12)`|
| `--overlay-backdrop` | `rgba(0,0,0,0.60)`              | `rgba(15,23,42,0.35)`             |

### 設計原則

- **ダークが引き続き既定値。** 属性なしの `:root` はダークに解決されます。
  `data-theme="light"` を付与するとライトに切り替わります。
- **アクセントはブランド維持。** NASA の赤と青は両テーマで同じで、
  earth のみ明度を数段階下げてチャートが白背景でも読めるようにします。
- **ステータスは色相を保ち明度だけ変える。** 5 つのセマンティック
  ステータス（critical / warning / success / info / muted）は
  認識性を保ったまま明るい面で視認できるように調整しています。
- **構造変更なし。** 既に全コンポーネントが CSS 変数を使っているため、
  PR はパレット派生と配線だけです。

---

## タイポグラフィ

### ディスプレイ — Space Grotesk

- ファイル: `web/public/fonts/SpaceGrotesk-Medium.ttf`（ウェイト 500）と
  `web/public/fonts/SpaceGrotesk-Bold.ttf`（ウェイト 700）。どちらも上流の
  variable font から instancing したもの。
- ライセンス: SIL Open Font License 1.1。同じディレクトリの
  `web/public/fonts/SpaceGrotesk-OFL.txt` にライセンス本文を同梱しています。
  Space Grotesk の作者は Florian Karsten
  （上流: https://github.com/floriankarsten/space-grotesk）。OFL により、
  ライセンスファイルをフォントバイナリと一緒に配布する限り apogee 内への
  再配布が認められています。
- ウェイト: ヒーロー／セクション見出しには `700`、10–12 px の ALL-CAPS
  ラベルには `500` を使用。
- `text-transform: uppercase`
- `letter-spacing: 0.12em`（ヒーロー系は `0.16em`–`0.20em` まで拡大）
- CSS クラス: `.font-display`
- ルール: ディスプレイフォントは**見出し専用** — uppercase、1〜3 語、
  大きめのディスプレイサイズで使用。長い見出し（recap 本文、プロンプト）や
  数語を超えるラベルは body スタックを使うこと。

### 本文 — システムフォントスタック

```css
font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans",
  Helvetica, Arial, sans-serif;
```

### 等幅

```css
font-family: ui-monospace, "SF Mono", Menlo, monospace;
```

イベント id、セッション id、タイムスタンプ、コード形の値はこのフォントで表示します。

### スケール

| レベル   | サイズ     | ウェイト      | 用途               |
| ------- | -------- | ------------- | ------------------ |
| Display | 20–60 px | 700 (Space Grotesk) | ヒーロー、ページタイトル |
| Heading | 14–16 px | 600           | セクションヘッダー |
| Body    | 13 px    | 400           | 既定テキスト       |
| Caption | 11 px    | 400           | ラベル、タイムスタンプ |
| Tiny    | 10 px    | 500           | バッジ、タグ       |

---

## アイコン

- ライブラリ: `lucide-react` のみ。UI クロム上で絵文字は使わない。
- 既定サイズ: **16 px**
- 既定ストローク幅: **1.5**
- 超高密度の行（表セル、インラインタグ）では同じストローク幅で 13–14 px まで落とす。

---

## Hook イベントカタログ

Claude Code の各 hook イベントはセマンティックトーンと lucide アイコンにマップされます。正式なソースは `web/app/lib/event-types.ts` で、本表はそれをミラーする必要があります。

| イベント             | トーン     | アイコン        | 補足                               |
| -------------------- | ---------- | --------------- | ---------------------------------- |
| `PreToolUse`         | `info`     | `Wrench`        | ツール呼び出し直前                 |
| `PostToolUse`        | `info`     | `Wrench`        | ツール呼び出し成功後               |
| `PostToolUseFailure` | `critical` | `AlertOctagon`  | ツールがエラーを返した             |
| `UserPromptSubmit`   | `info`     | `MessageSquare` | ユーザーが新しいプロンプトを送信   |
| `Notification`       | `warning`  | `Bell`          | ユーザーへの通知                   |
| `PermissionRequest`  | `warning`  | `Shield`        | 人間の判断待ち                     |
| `SessionStart`       | `earth`    | `PlayCircle`    | セッション開始                     |
| `SessionEnd`         | `muted`    | `StopCircle`    | セッション終了                     |
| `Stop`               | `earth`    | `Octagon`       | エージェントがターン終端に到達     |
| `SubagentStart`      | `accent`   | `Users`         | subagent 生成                      |
| `SubagentStop`       | `accent`   | `UserCheck`     | subagent 回収                      |
| `PreCompact`         | `muted`    | `Minimize2`     | 履歴 compaction 直前               |

トーンはセマンティックステータスに 2 つ追加したものです。

- `earth` — `artemis.earth` を使った軽めの情報バリアント。成功でも警戒でもないライフサイクルマーカー向け。
- `accent` — 青寄りのブランドグラデーション。subagent イベントに予約し、長いタイムライン上でネストした作業が視覚的に区別できるようにします。

---

## セッションパレット

apogee はセッション id ごとに決定的な色を割り当て、1 つのセッションが任意のチャートで常に同じ線色を描けるようにします。パレットは一定の明度（L ≈ 70%）・彩度（C ≈ 0.16）で 36 度ずつ離した OKLCH 色相環の 10 色歩きです。16 進値は丸めた後にハードコードされているので、プラットフォーム間で一致します。

| スロット | Hex       | 色相       | フレーバー   |
| ---- | --------- | ---------- | ------------ |
| 1    | `#5BB8F0` | 240        | cyan-blue    |
| 2    | `#7FAEF6` | 264        | periwinkle   |
| 3    | `#A8A2F1` | 288        | lavender     |
| 4    | `#CE97D9` | 312        | orchid       |
| 5    | `#E894B4` | 336        | rose         |
| 6    | `#F29A85` | 0          | salmon       |
| 7    | `#E5A962` | 48         | amber        |
| 8    | `#BDB84D` | 96         | citron       |
| 9    | `#7FC96E` | 132        | leaf         |
| 10   | `#4BD2A5` | 168        | seafoam      |

`web/app/lib/design-tokens.ts` の `sessionColor(sessionId)` を使って id をスロットにマップしてください。ハッシュは id 文字列に対する小さな FNV-1a ウォークなので、リロード・サーバー・クライアントタブ横断で同じ id は常に同じ色に解決されます。

これらの色は **チャート専用** です。セマンティックなステータスに使ってはいけません（そのときは `status.*`）。10 色を越えて拡張しないでください。より多くの系列が必要ならセッションをグループ化してください。

---

## 使用例

### ブランドバー付きセクションヘッダー

```tsx
<SectionHeader
  title="Live pulse"
  subtitle="Event stream from the collector."
/>
```

### イベントバッジ

```tsx
import { getEventType } from "@/app/lib/event-types";
import EventTypeBadge from "@/app/components/EventTypeBadge";

const spec = getEventType("PostToolUseFailure")!;
<EventTypeBadge spec={spec} />;
```

### ステータスピル

```tsx
<StatusPill tone="warning">permission requested</StatusPill>
```

### セッションカラー

```ts
import { sessionColor } from "@/app/lib/design-tokens";

const color = sessionColor(session.id); // 決定的な hex
```

---

## 原則

1. **ダークファースト。** 長時間モニタリング向けの設計。
2. **情報密度。** オペレーターは空白ではなくデータを欲しがる。
3. **一目で分かるステータス。** カラーコードは Artemis パレットに一貫して従う。`critical` / `warning` / `success` / `info` / `muted` はそれぞれ 1 つずつ。
4. **絵文字は使わない。** 常に禁止。アイコンは lucide-react のみ。
5. **権威はディスプレイフォントに宿る。** ディスプレイフォント（タイポグラフィ節を参照）はタイトル、ブランドマーク、短いセクションヘッダーだけに使う。
