import { useEffect, useLayoutEffect, useState } from "react";
import { X } from "lucide-react";
import { Button } from "../components/ui/button";
import type { TourStep } from "./types";

interface TourProps {
  steps: TourStep[];
  onClose: () => void;
}

interface Rect {
  top: number;
  left: number;
  width: number;
  height: number;
}

// Tour は画面上の要素をスポットライト表示しながら順に説明するガイドツアー。
// step.target が見つかればその要素をハイライトし、無ければ画面中央にツールチップだけ出す。
export function Tour({ steps, onClose }: TourProps) {
  const [index, setIndex] = useState(0);
  const [rect, setRect] = useState<Rect | null>(null);

  const step = steps[index];
  const isFirst = index === 0;
  const isLast = index === steps.length - 1;

  // 対象要素の位置を測る（スクロール・リサイズで追従）。
  useLayoutEffect(() => {
    let raf = 0;
    function measure() {
      if (!step?.target) {
        setRect(null);
        return;
      }
      const el = document.querySelector(step.target);
      if (!el) {
        setRect(null);
        return;
      }
      el.scrollIntoView({ behavior: "smooth", block: "center", inline: "nearest" });
      raf = requestAnimationFrame(() => {
        const r = el.getBoundingClientRect();
        setRect({ top: r.top, left: r.left, width: r.width, height: r.height });
      });
    }
    measure();
    window.addEventListener("resize", measure);
    window.addEventListener("scroll", measure, true);
    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener("resize", measure);
      window.removeEventListener("scroll", measure, true);
    };
  }, [step?.target]);

  // Esc で閉じる / 矢印で移動
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowRight" && !isLast) setIndex((i) => i + 1);
      else if (e.key === "ArrowLeft" && !isFirst) setIndex((i) => i - 1);
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [isFirst, isLast, onClose]);

  if (!step) return null;

  const pad = 6;
  const highlight: Rect | null = rect
    ? {
        top: rect.top - pad,
        left: rect.left - pad,
        width: rect.width + pad * 2,
        height: rect.height + pad * 2,
      }
    : null;

  // ツールチップの位置: 対象の下（入りきらなければ上）。対象が無ければ中央。
  const tooltipStyle: React.CSSProperties = highlight
    ? (() => {
        const below = highlight.top + highlight.height + 12;
        const spaceBelow = window.innerHeight - below;
        const left = Math.min(
          Math.max(12, highlight.left),
          Math.max(12, window.innerWidth - 360 - 12)
        );
        if (spaceBelow > 180) return { top: below, left };
        return { top: Math.max(12, highlight.top - 180), left };
      })()
    : {
        top: "50%",
        left: "50%",
        transform: "translate(-50%, -50%)",
      };

  return (
    <div className="fixed inset-0 z-[100]">
      {/* スポットライト: 対象を box-shadow のくり抜きで強調。対象なしなら全面ディム。 */}
      {highlight ? (
        <div
          className="pointer-events-none fixed rounded-md transition-all duration-200"
          style={{
            top: highlight.top,
            left: highlight.left,
            width: highlight.width,
            height: highlight.height,
            boxShadow: "0 0 0 9999px rgba(15, 23, 42, 0.55)",
            outline: "2px solid rgba(59, 130, 246, 0.9)",
          }}
        />
      ) : (
        <div className="fixed inset-0 bg-slate-900/55" />
      )}

      {/* クリックで閉じられる透明レイヤー（ツールチップより下） */}
      <button
        className="absolute inset-0 h-full w-full cursor-default"
        aria-label="ツアーを閉じる"
        onClick={onClose}
      />

      {/* ツールチップカード */}
      <div
        className="fixed w-[360px] max-w-[calc(100vw-24px)] rounded-lg border border-gray-200 bg-surface p-4 shadow-xl"
        style={tooltipStyle}
        role="dialog"
        aria-label="ガイドツアー"
      >
        <div className="flex items-start justify-between gap-2">
          <h3 className="text-sm font-semibold text-gray-900">{step.title}</h3>
          <button
            onClick={onClose}
            className="text-gray-400 transition-colors hover:text-gray-600"
            aria-label="閉じる"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <p className="mt-1.5 text-[13px] leading-relaxed text-gray-600">{step.body}</p>
        <div className="mt-3 flex items-center justify-between">
          <span className="text-xs tabular-nums text-gray-400">
            {index + 1} / {steps.length}
          </span>
          <div className="flex items-center gap-2">
            {!isFirst && (
              <Button variant="outline" size="sm" onClick={() => setIndex((i) => i - 1)}>
                戻る
              </Button>
            )}
            {isLast ? (
              <Button size="sm" onClick={onClose}>
                完了
              </Button>
            ) : (
              <Button size="sm" onClick={() => setIndex((i) => i + 1)}>
                次へ
              </Button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
