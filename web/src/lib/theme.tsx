import {
  createContext,
  useCallback,
  useContext,
  useState,
  type ReactNode,
} from "react";

export type Theme = "light" | "light-gray" | "dark-gray" | "dark";

export const THEMES: { value: Theme; label: string }[] = [
  { value: "light", label: "ライト" },
  { value: "light-gray", label: "ライトグレー" },
  { value: "dark-gray", label: "ダークグレー" },
  { value: "dark", label: "ダーク" },
];

const STORAGE_KEY = "mailshield-theme";

function isTheme(v: string | null): v is Theme {
  return THEMES.some((t) => t.value === v);
}

/** 保存済みテーマを読み、<html data-theme> に反映する。React マウント前に呼ぶ。 */
export function initTheme(): Theme {
  let stored: string | null = null;
  try {
    stored = localStorage.getItem(STORAGE_KEY);
  } catch {
    // プライベートモード等で localStorage が使えない場合はデフォルトのまま
  }
  const theme: Theme = isTheme(stored) ? stored : "light";
  document.documentElement.dataset.theme = theme;
  return theme;
}

interface ThemeContextValue {
  theme: Theme;
  setTheme: (t: Theme) => void;
}

const ThemeContext = createContext<ThemeContextValue>({
  theme: "light",
  setTheme: () => {},
});

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(
    () => (document.documentElement.dataset.theme as Theme) || "light"
  );

  const setTheme = useCallback((t: Theme) => {
    setThemeState(t);
    document.documentElement.dataset.theme = t;
    try {
      localStorage.setItem(STORAGE_KEY, t);
    } catch {
      // 保存失敗時はセッション内のみ有効
    }
  }, []);

  return (
    <ThemeContext.Provider value={{ theme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  return useContext(ThemeContext);
}
