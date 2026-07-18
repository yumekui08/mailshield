import { useState } from "react";
import { Plus, Trash2, Copy, Check } from "lucide-react";
import { useAPIKeys, useCreateAPIKey, useRevokeAPIKey } from "../hooks/useAPIKeys";
import { usePagedList } from "../hooks/usePagedList";
import { useMe } from "../hooks/useAuth";
import { PageHeader } from "../components/PageHeader";
import { Pagination } from "../components/Pagination";
import { Select } from "../components/ui/select";
import { Button } from "../components/ui/button";
import { Badge } from "../components/ui/badge";
import { Input } from "../components/ui/input";
import { Skeleton } from "../components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../components/ui/table";
import type { Role, CreateAPIKeyRequest } from "../types";

function formatDate(iso: string | null): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleString("ja-JP", { timeZone: "Asia/Tokyo" });
}

function roleBadge(role: Role) {
  if (role === "admin") return <Badge variant="red">admin</Badge>;
  if (role === "operator") return <Badge variant="blue">operator</Badge>;
  return <Badge variant="default">viewer</Badge>;
}

function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard.writeText(value).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };
  return (
    <button onClick={copy} className="ml-2 text-gray-400 hover:text-gray-700">
      {copied ? <Check size={14} className="text-green-500" /> : <Copy size={14} />}
    </button>
  );
}

export function APIKeysPage() {
  const { data: me } = useMe();
  const { data, isLoading } = useAPIKeys();
  const createMutation = useCreateAPIKey();
  const revokeMutation = useRevokeAPIKey();

  const [showForm, setShowForm] = useState(false);
  const [name, setName] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [expiresAt, setExpiresAt] = useState("");
  const [newKey, setNewKey] = useState<string | null>(null);

  const keys = data?.data ?? [];
  const { pageItems: pagedKeys, meta, setPage } = usePagedList(keys, 20);

  if (me?.role !== "admin") {
    return (
      <div className="p-6 text-gray-500">この画面は管理者のみ表示できます。</div>
    );
  }

  const handleCreate = async () => {
    if (!name.trim()) return;
    const req: CreateAPIKeyRequest = { name: name.trim(), role };
    if (expiresAt) req.expires_at = new Date(expiresAt).toISOString();
    const res = await createMutation.mutateAsync(req);
    setNewKey(res.key);
    setName("");
    setRole("viewer");
    setExpiresAt("");
    setShowForm(false);
  };

  const handleRevoke = (id: string, keyName: string) => {
    if (!confirm(`API キー「${keyName}」を失効させますか？この操作は取り消せません。`)) return;
    revokeMutation.mutate(id);
  };

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="API キー管理"
        description="REST API を機械間連携で利用するための Bearer トークン"
        count={data ? keys.length : undefined}
        actions={
          <Button onClick={() => setShowForm(!showForm)}>
            <Plus size={14} className="mr-1" />
            新規発行
          </Button>
        }
      />

      {newKey && (
        <div className="bg-yellow-50 border border-yellow-300 rounded p-4 space-y-2">
          <p className="font-semibold text-yellow-800">API キーが発行されました</p>
          <p className="text-sm text-yellow-700">
            このキーは一度しか表示されません。必ずコピーして安全な場所に保管してください。
          </p>
          <div className="flex items-center font-mono text-sm bg-surface border rounded px-3 py-2">
            <span className="break-all">{newKey}</span>
            <CopyButton value={newKey} />
          </div>
          <Button variant="outline" size="sm" onClick={() => setNewKey(null)}>
            閉じる
          </Button>
        </div>
      )}

      {showForm && (
        <div className="border border-gray-200 rounded-lg p-4 space-y-4 bg-surface">
          <p className="font-medium text-sm">新しい API キーを発行</p>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div>
              <label className="text-xs text-gray-500 mb-1 block">名前 *</label>
              <Input
                placeholder="CI/CD用 など"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div>
              <label className="text-xs text-gray-500 mb-1 block">ロール</label>
              <Select value={role} onChange={(e) => setRole(e.target.value as Role)}>
                <option value="viewer">viewer</option>
                <option value="operator">operator</option>
                <option value="admin">admin</option>
              </Select>
            </div>
            <div>
              <label className="text-xs text-gray-500 mb-1 block">有効期限（任意）</label>
              <Input
                type="datetime-local"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
              />
            </div>
          </div>
          <div className="flex gap-2">
            <Button
              size="sm"
              onClick={handleCreate}
              disabled={!name.trim() || createMutation.isPending}
            >
              発行
            </Button>
            <Button variant="outline" size="sm" onClick={() => setShowForm(false)}>
              キャンセル
            </Button>
          </div>
        </div>
      )}

      <div className="border border-gray-200 rounded-lg bg-surface overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名前</TableHead>
              <TableHead>ロール</TableHead>
              <TableHead>最終使用</TableHead>
              <TableHead>有効期限</TableHead>
              <TableHead>状態</TableHead>
              <TableHead>発行日時</TableHead>
              <TableHead className="w-16" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: 3 }).map((_, i) => (
                <TableRow key={i}>
                  {Array.from({ length: 7 }).map((_, j) => (
                    <TableCell key={j}>
                      <Skeleton className="h-4 w-24" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : pagedKeys.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-gray-400 py-8">
                  API キーがありません
                </TableCell>
              </TableRow>
            ) : (
              pagedKeys.map((k) => (
                <TableRow key={k.id} className={k.revoked_at ? "opacity-50" : ""}>
                  <TableCell className="font-medium">{k.name}</TableCell>
                  <TableCell>{roleBadge(k.role)}</TableCell>
                  <TableCell className="text-sm">{formatDate(k.last_used_at)}</TableCell>
                  <TableCell className="text-sm">{formatDate(k.expires_at)}</TableCell>
                  <TableCell>
                    {k.revoked_at ? (
                      <Badge variant="red">失効済み</Badge>
                    ) : (
                      <Badge variant="green">有効</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-sm">{formatDate(k.created_at)}</TableCell>
                  <TableCell>
                    {!k.revoked_at && (
                      <button
                        onClick={() => handleRevoke(k.id, k.name)}
                        className="text-red-500 hover:text-red-700"
                        title="失効させる"
                      >
                        <Trash2 size={14} />
                      </button>
                    )}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        {data && (
          <Pagination
            meta={meta}
            onPageChange={setPage}
            className="border-t border-gray-200 bg-gray-50 px-3 py-2"
          />
        )}
      </div>
    </div>
  );
}
