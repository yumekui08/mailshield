import { useState, useEffect } from "react";
import { useNavigate, useSearchParams, Link } from "react-router-dom";
import { Shield } from "lucide-react";
import { toast } from "sonner";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { useProviders, useLoginStandalone, useMe } from "../hooks/useAuth";
import { getOIDCLoginURL } from "../lib/api";

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const redirectTo = searchParams.get("redirect") ?? "/quarantine";

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  const { data: me } = useMe();
  const { data: providers, isLoading: loadingProviders } = useProviders();
  const loginMutation = useLoginStandalone();

  // すでにログイン済みならリダイレクト先へ
  useEffect(() => {
    if (me) navigate(redirectTo, { replace: true });
  }, [me, navigate, redirectTo]);

  // セットアップが必要なら /setup へ
  useEffect(() => {
    if (providers?.setup_required) navigate("/setup", { replace: true });
  }, [providers, navigate]);

  // ローカルログイン（email + password フォーム）の実体は standalone か LDAP bind 認証。
  // directory.source によってどちらか一方だけが有効になるが、フロントエンドから見た
  // フォーム・エンドポイントは同一なので区別せず扱う。
  const hasStandalone = providers?.providers.some((p) => p.id === "standalone");
  const hasLdap = providers?.providers.some((p) => p.id === "ldap");
  const hasLocalLogin = hasStandalone || hasLdap;
  const hasOIDC = providers?.providers.some((p) => p.id === "oidc");

  function handleStandaloneLogin(e: React.FormEvent) {
    e.preventDefault();
    loginMutation.mutate(
      { email, password },
      {
        onSuccess: () => navigate(redirectTo, { replace: true }),
        onError: (err) => toast.error(`ログイン失敗: ${err.message}`),
      }
    );
  }

  function handleOIDCLogin() {
    window.location.href = getOIDCLoginURL(redirectTo);
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
              <h1 className="text-2xl font-bold text-gray-900">MailShield</h1>
              <p className="text-sm text-gray-500 mt-1">セキュアなメールゲートウェイ</p>
            </div>
          </div>

          {loadingProviders ? (
            <p className="text-sm text-gray-400">読み込み中...</p>
          ) : (
            <div className="w-full flex flex-col gap-4">
              {hasLocalLogin && (
                <form onSubmit={handleStandaloneLogin} className="flex flex-col gap-3">
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
                    placeholder="パスワード"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    autoComplete="current-password"
                    required
                  />
                  <Button
                    type="submit"
                    className="w-full"
                    disabled={loginMutation.isPending}
                  >
                    {loginMutation.isPending ? "サインイン中..." : "サインイン"}
                  </Button>
                  {/* パスワードリセットは standalone（bcrypt）専用。LDAP bind 認証では
                      パスワードの真実の源が LDAP 側にあるため MailShield からリセットできない。 */}
                  {hasStandalone && (
                    <div className="text-right">
                      <Link
                        to="/forgot-password"
                        className="text-xs text-blue-600 hover:underline"
                      >
                        パスワードを忘れた場合
                      </Link>
                    </div>
                  )}
                </form>
              )}

              {hasLocalLogin && hasOIDC && (
                <div className="flex items-center gap-2">
                  <hr className="flex-1 border-gray-200" />
                  <span className="text-xs text-gray-400">または</span>
                  <hr className="flex-1 border-gray-200" />
                </div>
              )}

              {hasOIDC && (
                <Button
                  variant="outline"
                  onClick={handleOIDCLogin}
                  className="w-full"
                >
                  SSOでサインイン
                </Button>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
