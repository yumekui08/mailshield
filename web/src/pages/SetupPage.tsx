import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Shield } from "lucide-react";
import { toast } from "sonner";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { useSetup } from "../hooks/useAuth";

export function SetupPage() {
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [displayName, setDisplayName] = useState("");
  const setupMutation = useSetup();

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (password !== confirm) {
      toast.error("パスワードが一致しません");
      return;
    }
    if (password.length < 8) {
      toast.error("パスワードは8文字以上にしてください");
      return;
    }
    setupMutation.mutate(
      { email, password, displayName: displayName || undefined },
      {
        onSuccess: () => {
          toast.success("管理者ユーザーを作成しました。ログインしてください。");
          navigate("/login", { replace: true });
        },
        onError: (err) => toast.error(`セットアップ失敗: ${err.message}`),
      }
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
              <h1 className="text-2xl font-bold text-gray-900">初期セットアップ</h1>
              <p className="text-sm text-gray-500 mt-1">
                管理者アカウントを作成してください
              </p>
            </div>
          </div>

          <form onSubmit={handleSubmit} className="w-full flex flex-col gap-3">
            <Input
              type="text"
              placeholder="表示名（任意）"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              autoComplete="name"
            />
            <Input
              type="email"
              placeholder="メールアドレス"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="email"
              required
            />
            <Input
              type="password"
              placeholder="パスワード（8文字以上）"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              required
            />
            <Input
              type="password"
              placeholder="パスワード（確認）"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password"
              required
            />
            <Button
              type="submit"
              className="w-full"
              disabled={setupMutation.isPending}
            >
              {setupMutation.isPending ? "作成中..." : "管理者を作成"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
