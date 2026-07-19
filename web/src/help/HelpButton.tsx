import { useState } from "react";
import { HelpCircle, X, PlayCircle } from "lucide-react";
import { Button } from "../components/ui/button";
import { helpContent, type HelpKey } from "./content";
import { Tour } from "./Tour";

// HelpButton は画面右上に表示する「?」ボタン。クリックでその画面のマニュアルを
// スライドパネルで開き、ツアーが定義されていればスポットライトガイドを開始できる。
export function HelpButton({ helpKey }: { helpKey: HelpKey }) {
  const [panelOpen, setPanelOpen] = useState(false);
  const [tourOpen, setTourOpen] = useState(false);
  const content = helpContent[helpKey];
  if (!content) return null;

  function startTour() {
    setPanelOpen(false);
    setTourOpen(true);
  }

  return (
    <>
      <button
        type="button"
        onClick={() => setPanelOpen(true)}
        aria-label="この画面のヘルプを表示"
        title="この画面のヘルプ"
        className="flex h-8 w-8 items-center justify-center rounded-md border border-gray-300 bg-surface text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800"
      >
        <HelpCircle className="h-4 w-4" />
      </button>

      {panelOpen && (
        <div className="fixed inset-0 z-50 flex justify-end">
          <div
            className="fixed inset-0 bg-black/30"
            onClick={() => setPanelOpen(false)}
            aria-hidden="true"
          />
          <div className="relative z-10 flex h-full w-full max-w-md flex-col overflow-y-auto border-l border-gray-200 bg-surface">
            <div className="flex items-center justify-between border-b px-6 py-4">
              <div className="flex items-center gap-2">
                <HelpCircle className="h-5 w-5 text-blue-600" />
                <div>
                  <div className="font-semibold text-gray-900">{content.title}</div>
                  <div className="mt-0.5 text-xs text-gray-500">画面ヘルプ</div>
                </div>
              </div>
              <button
                onClick={() => setPanelOpen(false)}
                className="text-gray-400 transition-colors hover:text-gray-600"
                aria-label="閉じる"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            <div className="flex-1 space-y-5 px-6 py-5">
              <p className="text-sm leading-relaxed text-gray-700">{content.summary}</p>

              {content.tour && content.tour.length > 0 && (
                <Button variant="outline" className="w-full" onClick={startTour}>
                  <PlayCircle className="mr-1.5 h-4 w-4" />
                  ガイドツアーを開始
                </Button>
              )}

              {content.sections.map((section) => (
                <div key={section.heading}>
                  <h3 className="text-sm font-semibold text-gray-900">{section.heading}</h3>
                  <ul className="mt-1.5 space-y-1.5">
                    {section.items.map((item, i) => (
                      <li key={i} className="flex gap-2 text-[13px] leading-relaxed text-gray-600">
                        <span className="mt-1 h-1 w-1 shrink-0 rounded-full bg-gray-400" />
                        <span>{item}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {tourOpen && content.tour && (
        <Tour steps={content.tour} onClose={() => setTourOpen(false)} />
      )}
    </>
  );
}
