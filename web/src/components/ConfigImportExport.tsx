import { useRef, useState } from "react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { Download, Upload } from "lucide-react";
import { exportConfigBundle, importConfigBundle } from "../lib/api";
import { Button } from "./ui/button";

// ConfigImportExport は設定バンドル（ワーカーインスタンス・変数・ルーティング）の
// エクスポート（ダウンロード）とインポート（ファイル選択）を提供する共通ツールバー。
export function ConfigImportExport() {
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);

  async function handleExport() {
    setBusy(true);
    try {
      const text = await exportConfigBundle();
      const blob = new Blob([text], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "mailshield-config.json";
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      toast.error(`エクスポートに失敗しました: ${(err as Error).message}`);
    } finally {
      setBusy(false);
    }
  }

  async function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = ""; // 同じファイルを連続で選べるようリセット
    if (!file) return;
    setBusy(true);
    try {
      const text = await file.text();
      const res = await importConfigBundle(text);
      qc.invalidateQueries({ queryKey: ["worker-instances"] });
      qc.invalidateQueries({ queryKey: ["config-variables"] });
      qc.invalidateQueries({ queryKey: ["routings"] });
      if (res.errors.length > 0) {
        toast.warning(
          `作成 ${res.created} / 更新 ${res.updated} 件。${res.errors.length} 件のエラー: ${res.errors[0]}`
        );
      } else {
        toast.success(`インポート完了: 作成 ${res.created} / 更新 ${res.updated} 件`);
      }
    } catch (err) {
      toast.error(`インポートに失敗しました: ${(err as Error).message}`);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex items-center gap-2">
      <Button variant="outline" size="sm" onClick={handleExport} disabled={busy}>
        <Download className="h-3.5 w-3.5 mr-1" />
        エクスポート
      </Button>
      <Button variant="outline" size="sm" onClick={() => fileRef.current?.click()} disabled={busy}>
        <Upload className="h-3.5 w-3.5 mr-1" />
        インポート
      </Button>
      <input
        ref={fileRef}
        type="file"
        accept="application/json,.json"
        className="hidden"
        onChange={handleFile}
      />
    </div>
  );
}
