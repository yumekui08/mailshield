/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      colors: {
        // 既存ページの gray-* / blue-* ユーティリティを一括で
        // MailShield の配色（温かみのあるニュートラル + 深いティール）に差し替える。
        // 新規コードも同じユーティリティ名を使うこと。
        gray: {
          50: "#f7f6f2",
          100: "#efede6",
          200: "#e3e0d6",
          300: "#cfcbbe",
          400: "#a8a496",
          500: "#8a8578",
          600: "#6b675c",
          700: "#524f46",
          800: "#37352e",
          900: "#23221c",
          950: "#171610",
        },
        blue: {
          50: "#eef7f5",
          100: "#d7ece8",
          200: "#b0d9d2",
          300: "#7fbfb5",
          400: "#4a9d92",
          500: "#2d8177",
          600: "#196b62",
          700: "#14574f",
          800: "#12463f",
          900: "#103a35",
          950: "#082220",
        },
        // ダッシュボードのチャート系列色（dataviz バリデーター検証済み・ライトサーフェス #fdfdfb）
        chart: {
          delivered: "#0e8a4f",
          quarantined: "#d24343",
          rejected: "#3d72ad",
        },
        surface: "#fdfdfb",
      },
    },
  },
  plugins: [],
};
