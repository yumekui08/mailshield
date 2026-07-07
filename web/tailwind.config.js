// 色ランプの生成: 全ステップを src/index.css の CSS 変数に割り当てる。
// テーマ（data-theme 属性）ごとの実際の色値は index.css 側で定義する。
const ramp = (name) =>
  Object.fromEntries(
    [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950].map((step) => [
      step,
      `var(--c-${name}-${step})`,
    ])
  );

/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    // 角丸スケールを全体で抑える（フラットデザイン方針）。
    // 既存コードの rounded-md / rounded-lg 等はそのまま小さな角丸に落ちる。
    borderRadius: {
      none: "0",
      sm: "1px",
      DEFAULT: "2px",
      md: "2px",
      lg: "3px",
      xl: "4px",
      "2xl": "6px",
      "3xl": "8px",
      full: "9999px",
    },
    extend: {
      colors: {
        // gray はページ骨格、blue はブランドアクセント（深いティール）。
        // red/green/yellow/slate はバッジ・アラート用。すべてテーマ対応。
        gray: ramp("gray"),
        blue: ramp("blue"),
        red: ramp("red"),
        green: ramp("green"),
        yellow: ramp("yellow"),
        slate: ramp("slate"),
        surface: "var(--c-surface)",
        // ダッシュボードのチャート系列色（dataviz バリデーター検証済み）
        chart: {
          delivered: "var(--chart-delivered)",
          quarantined: "var(--chart-quarantined)",
          rejected: "var(--chart-rejected)",
        },
      },
    },
  },
  plugins: [],
};
