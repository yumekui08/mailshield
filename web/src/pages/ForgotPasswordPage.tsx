import { useState } from "react";
import { Link } from "react-router-dom";
import { Shield, ArrowLeft, Mail } from "lucide-react";
import { Card, CardContent } from "../components/ui/card";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { forgotPassword } from "../lib/api";

export function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setIsLoading(true);
    try {
      await forgotPassword(email);
      setSubmitted(true);
    } catch {
      setError("処理に失敗しました。もう一度お試しください。");
    } finally {
      setIsLoading(false);
    }
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
              <h1 className="text-xl font-bold text-gray-900">パスワードリセット</h1>
              <p className="text-sm text-gray-500 mt-1">MailShield</p>
            </div>
          </div>

          {submitted ? (
            <div className="w-full flex flex-col items-center gap-4 text-center">
              <div className="flex h-12 w-12 items-center justify-center rounded-full bg-green-50">
                <Mail className="h-6 w-6 text-green-600" />
              </div>
              <p className="text-sm text-gray-700">
                メールアドレスが登録済みの場合、パスワードリセットのリンクを送信しました。
              </p>
              <p className="text-xs text-gray-400">
                メールが届かない場合は迷惑メールフォルダをご確認ください。
              </p>
              <Link to="/login" className="text-sm text-blue-600 hover:underline flex items-center gap-1">
                <ArrowLeft className="h-3.5 w-3.5" />
                ログインに戻る
              </Link>
            </div>
          ) : (
            <div className="w-full flex flex-col gap-4">
              <p className="text-sm text-gray-600 text-center">
                登録済みのメールアドレスを入力してください。パスワードリセットのリンクを送信します。
              </p>
              {error && (
                <p className="rounded-md bg-red-50 px-3 py-2 text-xs text-red-600">{error}</p>
              )}
              <form onSubmit={handleSubmit} className="flex flex-col gap-3">
                <Input
                  type="email"
                  placeholder="メールアドレス"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  autoComplete="email"
                  required
                />
                <Button type="submit" className="w-full" disabled={isLoading}>
                  {isLoading ? "送信中..." : "リセットリンクを送信"}
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
