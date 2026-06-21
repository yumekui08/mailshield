import { NavLink } from "react-router-dom";
import { Shield, LogOut, Users, Inbox, LayoutDashboard, Mail, ClipboardList, Key, FlaskConical } from "lucide-react";
import { cn } from "../lib/utils";
import { Badge } from "./ui/badge";
import { useMe, useLogout } from "../hooks/useAuth";
import type { Role } from "../types";

const roleLabel: Record<Role, string> = {
  admin: "管理者",
  operator: "オペレーター",
  viewer: "閲覧者",
};

export function Sidebar() {
  const { data: user } = useMe();
  const logout = useLogout();

  return (
    <aside className="flex h-full w-60 flex-col bg-slate-900 text-slate-100">
      <div className="flex items-center gap-2 px-5 py-5 border-b border-slate-700">
        <Shield className="h-6 w-6 text-blue-400 shrink-0" />
        <div>
          <div className="font-bold text-white leading-tight">MailShield</div>
          <div className="text-xs text-slate-400">v0.1.0</div>
        </div>
      </div>

      <nav className="flex-1 px-3 py-4 space-y-1">
        <NavLink
          to="/"
          end
          className={({ isActive }) =>
            cn(
              "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
              isActive
                ? "bg-slate-700 text-white"
                : "text-slate-300 hover:bg-slate-800 hover:text-white"
            )
          }
        >
          <LayoutDashboard className="h-4 w-4" />
          ダッシュボード
        </NavLink>

        <NavLink
          to="/messages"
          className={({ isActive }) =>
            cn(
              "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
              isActive
                ? "bg-slate-700 text-white"
                : "text-slate-300 hover:bg-slate-800 hover:text-white"
            )
          }
        >
          <Mail className="h-4 w-4" />
          メール処理ログ
        </NavLink>

        <NavLink
          to="/quarantine"
          className={({ isActive }) =>
            cn(
              "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
              isActive
                ? "bg-slate-700 text-white"
                : "text-slate-300 hover:bg-slate-800 hover:text-white"
            )
          }
        >
          <Shield className="h-4 w-4" />
          隔離メール
        </NavLink>

        {(user?.role === "admin" || user?.role === "operator") && (
          <NavLink
            to="/mailboxes"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
                isActive
                  ? "bg-slate-700 text-white"
                  : "text-slate-300 hover:bg-slate-800 hover:text-white"
              )
            }
          >
            <Inbox className="h-4 w-4" />
            メールボックス
          </NavLink>
        )}

        {user?.role === "admin" && (
          <NavLink
            to="/users"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
                isActive
                  ? "bg-slate-700 text-white"
                  : "text-slate-300 hover:bg-slate-800 hover:text-white"
              )
            }
          >
            <Users className="h-4 w-4" />
            ユーザー管理
          </NavLink>
        )}

        {(user?.role === "admin" || user?.role === "operator") && (
          <NavLink
            to="/simulate"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
                isActive
                  ? "bg-slate-700 text-white"
                  : "text-slate-300 hover:bg-slate-800 hover:text-white"
              )
            }
          >
            <FlaskConical className="h-4 w-4" />
            ポリシーシミュレーター
          </NavLink>
        )}

        {user?.role === "admin" && (
          <NavLink
            to="/audit-logs"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
                isActive
                  ? "bg-slate-700 text-white"
                  : "text-slate-300 hover:bg-slate-800 hover:text-white"
              )
            }
          >
            <ClipboardList className="h-4 w-4" />
            監査ログ
          </NavLink>
        )}

        {user?.role === "admin" && (
          <NavLink
            to="/api-keys"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
                isActive
                  ? "bg-slate-700 text-white"
                  : "text-slate-300 hover:bg-slate-800 hover:text-white"
              )
            }
          >
            <Key className="h-4 w-4" />
            API キー
          </NavLink>
        )}

      </nav>

      {user && (
        <div className="border-t border-slate-700 px-4 py-4 space-y-2">
          <div className="text-xs text-slate-400 truncate">{user.email}</div>
          <div className="flex items-center justify-between">
            <Badge variant="slate">{roleLabel[user.role]}</Badge>
            <button
              onClick={() => logout.mutate()}
              disabled={logout.isPending}
              className="flex items-center gap-1 text-xs text-slate-400 hover:text-white transition-colors disabled:opacity-50"
              aria-label="ログアウト"
            >
              <LogOut className="h-3.5 w-3.5" />
              ログアウト
            </button>
          </div>
        </div>
      )}
    </aside>
  );
}
