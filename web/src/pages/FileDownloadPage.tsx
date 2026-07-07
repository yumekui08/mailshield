import { useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { useQuery, useMutation } from "@tanstack/react-query";
import { Shield, Download, FileX, Clock, AlertTriangle, LogIn, Mail, KeyRound, RefreshCw } from "lucide-react";
import { Card, CardContent } from "../components/ui/card";
import {
  getPublicAttachmentsInfo,
  getPublicAttachmentsInfoByOTP,
  getAttachments,
  getPublicAttachmentDownloadURL,
  getAttachmentDownloadURL,
  getOTPAttachmentDownloadURL,
  requestOTP,
  verifyOTP,
  ApiError,
  type Attachment,
} from "../lib/api";

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function AttachmentList({
  attachments,
  downloadURL,
}: {
  attachments: Attachment[];
  downloadURL: (filename: string) => string;
}) {
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-gray-500 mb-1">
        {attachments.length} 件のファイルをダウンロードできます
      </p>
      {attachments.map((att) => (
        <div
          key={att.id}
          className="flex items-center justify-between gap-3 rounded-lg border border-gray-200 p-3"
        >
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium text-gray-900">{att.filename}</p>
            <p className="text-xs text-gray-400">
              {att.content_type} · {formatSize(att.size_bytes)}
            </p>
          </div>
          {att.is_disabled ? (
            <span className="shrink-0 text-xs text-red-500 font-medium">無効化済み</span>
          ) : (
            <a
              href={downloadURL(att.filename)}
              download={att.filename}
              className="shrink-0 inline-flex items-center gap-1 rounded-md border border-gray-200 bg-surface px-3 py-1.5 text-xs font-medium hover:bg-gray-50 transition-colors"
            >
              <Download className="h-3.5 w-3.5" />
              ダウンロード
            </a>
          )}
        </div>
      ))}
    </div>
  );
}

type OTPStep = "email" | "code" | "done";

function OTPFlow({ token }: { token: string }) {
  const [step, setStep] = useState<OTPStep>("email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [sessionId, setSessionId] = useState("");
  const [errorMsg, setErrorMsg] = useState("");

  const attachmentsQuery = useQuery({
    queryKey: ["otp-attachments", token, sessionId],
    queryFn: () => getPublicAttachmentsInfoByOTP(token, sessionId),
    enabled: step === "done" && !!sessionId,
    retry: false,
  });

  const requestMutation = useMutation({
    mutationFn: () => requestOTP(token, email),
    onSuccess: () => {
      setErrorMsg("");
      setCode("");
      setStep("code");
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 400) {
        setErrorMsg("このファイルへのアクセス権がありません。受信したメールアドレスを入力してください。");
      } else {
        setErrorMsg("OTP の送信に失敗しました。もう一度お試しください。");
      }
    },
  });

  const verifyMutation = useMutation({
    mutationFn: () => verifyOTP(token, email, code),
    onSuccess: (data) => {
      setErrorMsg("");
      setSessionId(data.session_id);
      setStep("done");
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 429) {
        setErrorMsg("試行回数の上限に達しました。「コードを再送信」して新しいコードを取得してください。");
        setStep("email");
      } else {
        setErrorMsg("コードが正しくありません。もう一度確認してください。");
      }
    },
  });

  if (step === "done") {
    const atts = attachmentsQuery.data?.attachments;
    if (attachmentsQuery.isLoading) {
      return (
        <div className="flex flex-col items-center gap-3 py-8 text-gray-400">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-300 border-t-blue-500" />
          <p className="text-sm">ファイル一覧を取得中...</p>
        </div>
      );
    }
    if (!atts || atts.length === 0) {
      return (
        <div className="flex flex-col items-center gap-3 py-8 text-gray-400">
          <FileX className="h-10 w-10" />
          <p className="text-sm">ファイルが見つかりません</p>
          <p className="text-xs">リンクの有効期限が切れているか、すでに削除されています</p>
        </div>
      );
    }
    return (
      <AttachmentList
        attachments={atts}
        downloadURL={(filename) => getOTPAttachmentDownloadURL(token, filename, sessionId)}
      />
    );
  }

  if (step === "email") {
    return (
      <div className="flex flex-col gap-4">
        <div className="flex flex-col items-center gap-2 text-center">
          <div className="flex h-10 w-10 items-center justify-center rounded-full bg-blue-50">
            <Mail className="h-5 w-5 text-blue-600" />
          </div>
          <p className="text-sm font-medium text-gray-900">メールアドレスで認証</p>
          <p className="text-xs text-gray-500">
            このファイルを受け取ったメールアドレスを入力してください。<br />
            認証コードをメールで送信します。
          </p>
        </div>
        {errorMsg && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-xs text-red-600">{errorMsg}</p>
        )}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setErrorMsg("");
            requestMutation.mutate();
          }}
          className="flex flex-col gap-3"
        >
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="your@email.com"
            className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
          <button
            type="submit"
            disabled={requestMutation.isPending || !email}
            className="flex items-center justify-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 transition-colors disabled:opacity-50"
          >
            {requestMutation.isPending ? (
              <div className="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent" />
            ) : (
              <Mail className="h-4 w-4" />
            )}
            認証コードを送信
          </button>
        </form>
      </div>
    );
  }

  // step === "code"
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col items-center gap-2 text-center">
        <div className="flex h-10 w-10 items-center justify-center rounded-full bg-blue-50">
          <KeyRound className="h-5 w-5 text-blue-600" />
        </div>
        <p className="text-sm font-medium text-gray-900">認証コードを入力</p>
        <p className="text-xs text-gray-500">
          <span className="font-medium">{email}</span> に送信した<br />
          6桁のコードを入力してください（10分間有効）
        </p>
      </div>
      {errorMsg && (
        <p className="rounded-md bg-red-50 px-3 py-2 text-xs text-red-600">{errorMsg}</p>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setErrorMsg("");
          verifyMutation.mutate();
        }}
        className="flex flex-col gap-3"
      >
        <input
          type="text"
          inputMode="numeric"
          pattern="[0-9]{6}"
          maxLength={6}
          required
          value={code}
          onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
          placeholder="123456"
          className="w-full rounded-md border border-gray-300 px-3 py-2 text-center text-2xl font-mono tracking-widest focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
        />
        <button
          type="submit"
          disabled={verifyMutation.isPending || code.length !== 6}
          className="flex items-center justify-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 transition-colors disabled:opacity-50"
        >
          {verifyMutation.isPending ? (
            <div className="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent" />
          ) : (
            <KeyRound className="h-4 w-4" />
          )}
          確認
        </button>
      </form>
      <button
        type="button"
        onClick={() => {
          setErrorMsg("");
          setCode("");
          requestMutation.mutate();
        }}
        disabled={requestMutation.isPending}
        className="flex items-center justify-center gap-1 text-xs text-gray-400 hover:text-gray-600 transition-colors disabled:opacity-50"
      >
        <RefreshCw className="h-3.5 w-3.5" />
        コードを再送信
      </button>
    </div>
  );
}

export function FileDownloadPage() {
  const { token } = useParams<{ token: string }>();
  const navigate = useNavigate();

  const publicQuery = useQuery({
    queryKey: ["public-attachments-info", token],
    queryFn: () => getPublicAttachmentsInfo(token!),
    enabled: !!token,
    retry: false,
  });

  const mode = publicQuery.data?.mode;

  const authQuery = useQuery({
    queryKey: ["auth-attachments", token],
    queryFn: () => getAttachments(token!),
    enabled: !!token && mode === "auth" && !publicQuery.isLoading,
    retry: false,
  });

  const isLoading = publicQuery.isLoading || (mode === "auth" && authQuery.isLoading);
  const isError = publicQuery.isError;

  const loginURL = `/login?redirect=${encodeURIComponent(`/files/${token}`)}`;

  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-gray-100 p-4">
      <div className="w-full max-w-lg">
        <div className="flex flex-col items-center gap-2 mb-6">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-blue-600">
            <Shield className="h-7 w-7 text-white" />
          </div>
          <h1 className="text-xl font-bold text-gray-900">MailShield 添付ファイルダウンロード</h1>
          <p className="text-sm text-gray-500 flex items-center gap-1">
            <Clock className="h-4 w-4" />
            セキュリティポリシーにより分離された添付ファイルです
          </p>
        </div>

        <Card>
          <CardContent className="pt-6 pb-6">
            {isLoading && (
              <div className="flex flex-col items-center gap-3 py-8 text-gray-400">
                <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-300 border-t-blue-500" />
                <p className="text-sm">読み込み中...</p>
              </div>
            )}

            {isError && (
              <div className="flex flex-col items-center gap-3 py-8 text-red-500">
                <AlertTriangle className="h-10 w-10" />
                <p className="text-sm font-medium">ファイルの取得に失敗しました</p>
                <p className="text-xs text-gray-400">リンクの有効期限が切れているか、URLが正しくありません</p>
              </div>
            )}

            {/* mode=simple */}
            {!isLoading && !isError && mode === "simple" && (
              <>
                {publicQuery.data?.attachments?.length === 0 ? (
                  <div className="flex flex-col items-center gap-3 py-8 text-gray-400">
                    <FileX className="h-10 w-10" />
                    <p className="text-sm">ファイルが見つかりません</p>
                    <p className="text-xs">リンクの有効期限が切れているか、すでに削除されています</p>
                  </div>
                ) : (
                  <AttachmentList
                    attachments={publicQuery.data!.attachments!}
                    downloadURL={(filename) => getPublicAttachmentDownloadURL(token!, filename)}
                  />
                )}
              </>
            )}

            {/* mode=auth */}
            {!isLoading && !isError && mode === "auth" && (
              <>
                {authQuery.isLoading && (
                  <div className="flex flex-col items-center gap-3 py-8 text-gray-400">
                    <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-300 border-t-blue-500" />
                    <p className="text-sm">認証情報を確認中...</p>
                  </div>
                )}
                {!authQuery.isLoading && authQuery.isError && (
                  (() => {
                    const err = authQuery.error;
                    const is401 = err instanceof ApiError && err.status === 401;
                    const is403 = err instanceof ApiError && err.status === 403;
                    if (is401 || is403) {
                      return (
                        <div className="flex flex-col items-center gap-4 py-8">
                          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-gray-100">
                            <LogIn className="h-6 w-6 text-gray-500" />
                          </div>
                          <div className="text-center">
                            <p className="text-sm font-medium text-gray-900">
                              {is403 ? "このファイルへのアクセス権がありません" : "ログインが必要です"}
                            </p>
                            <p className="mt-1 text-xs text-gray-400">
                              {is403
                                ? "宛先メールボックスへのアクセス権を持つアカウントでログインしてください"
                                : "このファイルをダウンロードするには MailShield へのログインが必要です"}
                            </p>
                          </div>
                          {is401 && (
                            <button
                              onClick={() => navigate(loginURL)}
                              className="inline-flex items-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 transition-colors"
                            >
                              <LogIn className="h-4 w-4" />
                              ログイン
                            </button>
                          )}
                        </div>
                      );
                    }
                    return (
                      <div className="flex flex-col items-center gap-3 py-8 text-red-500">
                        <AlertTriangle className="h-10 w-10" />
                        <p className="text-sm font-medium">ファイルの取得に失敗しました</p>
                      </div>
                    );
                  })()
                )}
                {!authQuery.isLoading && !authQuery.isError && authQuery.data && (
                  authQuery.data.length === 0 ? (
                    <div className="flex flex-col items-center gap-3 py-8 text-gray-400">
                      <FileX className="h-10 w-10" />
                      <p className="text-sm">ファイルが見つかりません</p>
                    </div>
                  ) : (
                    <AttachmentList
                      attachments={authQuery.data}
                      downloadURL={(filename) => getAttachmentDownloadURL(token!, filename)}
                    />
                  )
                )}
              </>
            )}

            {/* mode=otp */}
            {!isLoading && !isError && mode === "otp" && (
              <OTPFlow token={token!} />
            )}
          </CardContent>
        </Card>

        <p className="mt-4 text-center text-xs text-gray-400">
          このリンクはセキュリティポリシーにより有効期限があります
        </p>
      </div>
    </div>
  );
}
