# Design: Qauipsiz

Запертая система. Каждый прогон Hallmark читает её первой, страницы её разделяют.
Меняется осознанно: файл и есть правило.

## System
- Genre: modern-minimal
- Shape of the first page: Map / Diagram (лендинг); остальные 28 — app screens
- Theme: Wayfind (deal 3/3, seed `prompt-1f0e·19f2e99b·2026-11-01·r0`)
- Axes: light 95.5 250 / mono / cool 264 · posture: committed
- Colourway: `platform` в светлой теме, `night` в тёмной, по `prefers-color-scheme`, без переключателя

## Tokens
`web/static/tokens.css` — источник правды. Роли:
```css
:root {
  --color-paper: oklch(95.5% 0.006 250);  --color-paper-2: oklch(91% 0.009 250);  --color-paper-3: oklch(99.5% 0.005 250);
  --color-ink: oklch(16% 0.014 262);  --color-ink-2: oklch(29% 0.014 262);  --color-muted: oklch(47% 0.016 260);
  --color-rule: oklch(80% 0.011 250);  --color-ink-rule: oklch(34% 0.014 262);
  --color-accent: oklch(51% 0.22 264);  --color-accent-2: oklch(81% 0.16 82);  --color-accent-ink: oklch(99.5% 0.003 250);  --color-focus: oklch(51% 0.22 264);
  --font-display: "IBM Plex Mono", ui-monospace, monospace;  --font-body: var(--font-display);  --font-outlier: var(--font-body);
  --radius-card: 0;  --radius-pill: 999px;  --rule-hair: 1px;  --rule-bar: 2px;  --shadow-card: 0 24px 40px -24px oklch(8% 0.01 260 / 0.6);
}
```
Шкала отступов, типографическая шкала, easing и длительности — из каркаса, живут в `tokens.css`.

**Гарнитура.** Chivo Mono из темы заменён на IBM Plex Mono: кириллицы в Chivo Mono нет вовсе,
а интерфейс обязан нести ә ғ қ ң ө ұ ү һ і. Plex Mono покрывает все 18 казахских букв
(cyrillic + cyrillic-ext), self-hosted woff2, 400/500/700. Дисплейный вес 700 вместо 800: у Plex Mono нет 800.

**Сигнальные цвета.** Тема даёт кобальт (живое) и янтарь (цифры на чернилах). Красный и зелёный
добавлены сверх темы осознанно: статус обязан читаться цветом **и** текстом, в том числе на солнце
(`--color-danger`, `--color-warn`, `--color-ok`, все на AA). Это отступление от do-not темы
«никогда третий яркий цвет», и оно стоит в стампе.

## Controls
- Primary: заливка `--color-accent` · radius 0 · 48–52px на публичных страницах, 36–40px в приложении при тап-зоне 48px
- Secondary: прозрачная с `--rule-bar` чернильной границей · та же высота и radius
- Выбор (`.tile`, `.seg`): квадратные ячейки с общими 2px границами, выбранная заливается чернилами
- Поля: квадратные, 2px чернильная граница на `--color-paper-3`

## Motion
- Character: still, по-табельному жёстко: смена цвета `--dur-micro` в `steps(1)`, строки обновляются на месте,
  ни одного reveal на скролле; при `prefers-reduced-motion` всё заканчивается сразу.

## Exports
`web/static/tokens.css` в этом проекте. Tailwind, DTCG и shadcn добавляются сюда по запросу.
