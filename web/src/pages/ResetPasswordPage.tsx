import { useState } from "react";
import { useSearchParams, useNavigate, Link } from "react-router-dom";
import { Shield, ArrowLeft, CheckCircle, AlertTriangle } from "lucide-react";
import { Card, CardContent } from "../components/ui/card";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { resetPassword, ApiError } from "../lib/api";

export function ResetPasswordPage() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const token = searchParams.get("token") ?? "";

  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");

    if (password.length < 8) {
      setError("パスワードは8文字以上にしてください");
      return;
    }
    if (password !== confirm) {
      setError("パスワードが一致しません");
      return;
    }

    setIsLoading(true);
    try {
      await resetPassword(token, password);
      setDone(true);
    } catch (err) {
      if (err instanceof ApiError && err.status === 400) {
        setError("リセットリンクが無効または期限切れです。再度パスワードリセットを申請してください。");
      } else {
        setError("処理に失敗しました。もう一度お試しください。");
      }
    } finally {
      setIsLoading(false);
    }
  }

  if (!token) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gray-100">
        <Card className="w-full max-w-sm">
          <CardContent className="pt-8 pb-8 px-8 flex flex-col items-center gap-4 text-center">
            <AlertTriangle className="h-10 w-10 text-red-400" />
            <p className="text-sm text-gray-700">リセットリンクが正しくありません。</p>
            <Link to="/forgot-password" className="text-sm text-blue-600 hover:underline">
              パスワードリセットを再申請
            </Link>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-100">
      <Card className="w-full max-w-sm">
        <CardContent className="pt-8 pb-8 px-8 flex flex-col items-center gap-6">
          <div className="flex flex-col items-center gap-3">
            <div className="flex h-14 w-14 items-center justify-center rounded-full bg-blue-600">
              <Shield className="h-8 w-8 text-white" />
            </div>
            <div className="text-center">
              <h1 className="text-xl font-bold text-gray-900">新しいパスワードを設定</h1>
              <p className="text-sm text-gray-500 mt-1">MailShield</p>
            </div>
          </div>

          {done ? (
            <div className="w-full flex flex-col items-center gap-4 text-center">
              <div className="flex h-12 w-12 items-center justify-center rounded-full bg-green-50">
                <CheckCircle className="h-6 w-6 text-green-600" />
              </div>
              <p className="text-sm text-gray-700">パスワードを更新しました。</p>
              <Button className="w-full" onClick={() => navigate("/login")}>
                ログインする
              </Button>
            </div>
          ) : (
            <div className="w-full flex flex-col gap-4">
              {error && (
                <p className="rounded-md bg-red-50 px-3 py-2 text-xs text-red-600">{error}</p>
              )}
              <form onSubmit={handleSubmit} className="flex flex-col gap-3">
                <Input
                  type="password"
                  placeholder="新しいパスワード（8文字以上）"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                  required
                  minLength={8}
                />
                <Input
                  type="password"
                  placeholder="パスワードを再入力"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                  autoComplete="new-password"
                  required
                />
                <Button type="submit" className="w-full" disabled={isLoading}>
                  {isLoading ? "更新中..." : "パスワードを更新"}
                </Button>
              </form>
              <Link to="/login" className="text-xs text-gray-500 hover:underline flex items-center gap-1 justify-center">
                <ArrowLeft className="h-3 w-3" />
                ログインに戻る
              </Link>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
