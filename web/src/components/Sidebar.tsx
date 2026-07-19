import { NavLink } from "react-router-dom";
import {
  Shield,
  LogOut,
  Users,
  Inbox,
  LayoutDashboard,
  Mail,
  ClipboardList,
  Key,
  FlaskConical,
  SlidersHorizontal,
  ClipboardCheck,
  ShieldAlert,
  Clock,
  SunMoon,
  Boxes,
  Variable,
} from "lucide-react";
import { cn } from "../lib/utils";
import { useMe, useLogout } from "../hooks/useAuth";
import { useTheme, THEMES, type Theme } from "../lib/theme";
import type { Role } from "../types";

const roleLabel: Record<Role, string> = {
  admin: "管理者",
  operator: "オペレーター",
  viewer: "閲覧者",
};

interface NavItem {
  to: string;
  label: string;
  icon: React.ElementType;
  end?: boolean;
  roles?: Role[]; // 省略時は全ロール
}

interface NavGroup {
  heading?: string;
  items: NavItem[];
}

const navGroups: NavGroup[] = [
  {
    items: [{ to: "/", label: "ダッシュボード", icon: LayoutDashboard, end: true }],
  },
  {
    heading: "メールフロー",
    items: [
      { to: "/messages", label: "処理ログ", icon: Mail },
      { to: "/quarantine", label: "隔離メール", icon: ShieldAlert },
      { to: "/approvals", label: "承認フロー", icon: ClipboardCheck },
      { to: "/delayed", label: "送信待ち", icon: Clock },
    ],
  },
  {
    heading: "運用",
    items: [
      { to: "/mailboxes", label: "メールボックス", icon: Inbox, roles: ["admin", "operator"] },
      { to: "/policy", label: "ポリシー", icon: SlidersHorizontal, roles: ["admin", "operator"] },
      { to: "/simulate", label: "ポリシーシミュレーター", icon: FlaskConical, roles: ["admin", "operator"] },
    ],
  },
  {
    heading: "設定",
    items: [
      { to: "/worker-instances", label: "ワーカーインスタンス", icon: Boxes, roles: ["admin", "operator"] },
      { to: "/variables", label: "設定変数", icon: Variable, roles: ["admin", "operator"] },
    ],
  },
  {
    heading: "システム管理",
    items: [
      { to: "/users", label: "ユーザー", icon: Users, roles: ["admin"] },
      { to: "/api-keys", label: "API キー", icon: Key, roles: ["admin"] },
      { to: "/audit-logs", label: "監査ログ", icon: ClipboardList, roles: ["admin"] },
    ],
  },
];

export function Sidebar() {
  const { data: user } = useMe();
  const logout = useLogout();
  const { theme, setTheme } = useTheme();

  const visibleGroups = navGroups
    .map((g) => ({
      ...g,
      items: g.items.filter((item) => !item.roles || (user && item.roles.includes(user.role))),
    }))
    .filter((g) => g.items.length > 0);

  return (
    <aside className="flex h-full w-60 flex-col border-r border-gray-200 bg-surface">
      <div className="flex items-center gap-2.5 px-5 pb-5 pt-6">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-blue-600">
          <Shield className="h-[18px] w-[18px] text-white" />
        </div>
        <div>
          <div className="text-sm font-bold leading-tight tracking-wide text-gray-900">
            MailShield
          </div>
          <div className="text-[11px] text-gray-400">Mail Gateway v0.1.0</div>
        </div>
      </div>

      <nav className="flex-1 space-y-5 overflow-y-auto px-3 pb-4">
        {visibleGroups.map((group, gi) => (
          <div key={gi}>
            {group.heading && (
              <div className="mb-1 px-3 text-[10px] font-semibold uppercase tracking-[0.12em] text-gray-400">
                {group.heading}
              </div>
            )}
            <div className="space-y-0.5">
              {group.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  className={({ isActive }) =>
                    cn(
                      "relative flex items-center gap-2.5 rounded-md px-3 py-2 text-[13px] transition-colors",
                      isActive
                        ? "bg-blue-50 font-medium text-blue-800"
                        : "text-gray-600 hover:bg-gray-100 hover:text-gray-900"
                    )
                  }
                >
                  {({ isActive }) => (
                    <>
                      {isActive && (
                        <span className="absolute inset-y-1 left-0 w-0.5 bg-blue-600" />
                      )}
                      <item.icon
                        className={cn("h-4 w-4 shrink-0", isActive ? "text-blue-700" : "text-gray-400")}
                      />
                      {item.label}
                    </>
                  )}
                </NavLink>
              ))}
            </div>
          </div>
        ))}
      </nav>

      <div className="flex items-center gap-2 border-t border-gray-200 px-4 py-2.5">
        <SunMoon className="h-3.5 w-3.5 shrink-0 text-gray-400" aria-hidden />
        <select
          value={theme}
          onChange={(e) => setTheme(e.target.value as Theme)}
          aria-label="テーマ"
          className="h-6 w-full rounded border border-gray-200 bg-surface px-1 text-[11px] text-gray-600 focus-visible:border-blue-500 focus-visible:outline-none"
        >
          {THEMES.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
      </div>

      {user && (
        <div className="border-t border-gray-200 px-4 py-3.5">
          <div className="truncate text-xs text-gray-700">{user.email}</div>
          <div className="mt-1.5 flex items-center justify-between">
            <span className="rounded border border-gray-200 bg-gray-50 px-1.5 py-0.5 text-[11px] text-gray-600">
              {roleLabel[user.role]}
            </span>
            <button
              onClick={() => logout.mutate()}
              disabled={logout.isPending}
              className="flex items-center gap-1 text-xs text-gray-400 transition-colors hover:text-gray-700 disabled:opacity-50"
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
