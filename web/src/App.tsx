import { BrowserRouter, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { ThemeProvider, useTheme } from "./lib/theme";
import { Layout } from "./components/Layout";
import { LoginPage } from "./pages/LoginPage";
import { SetupPage } from "./pages/SetupPage";
import { DashboardPage } from "./pages/DashboardPage";
import { MessagesPage } from "./pages/MessagesPage";
import { MessageDetailPage } from "./pages/MessageDetailPage";
import { QuarantinePage } from "./pages/QuarantinePage";
import { QuarantineDetailPage } from "./pages/QuarantineDetailPage";
import { UsersPage } from "./pages/UsersPage";
import { MailboxesPage } from "./pages/MailboxesPage";
import { AuditLogsPage } from "./pages/AuditLogsPage";
import { APIKeysPage } from "./pages/APIKeysPage";
import { SimulatePage } from "./pages/SimulatePage";
import { PolicyPage } from "./pages/PolicyPage";
import { WorkerInstancesPage } from "./pages/WorkerInstancesPage";
import { VariablesPage } from "./pages/VariablesPage";
import { ApprovalsPage } from "./pages/ApprovalsPage";
import { DelayedPage } from "./pages/DelayedPage";
import { ApprovalDetailPage } from "./pages/ApprovalDetailPage";
import { FileDownloadPage } from "./pages/FileDownloadPage";
import { ForgotPasswordPage } from "./pages/ForgotPasswordPage";
import { ResetPasswordPage } from "./pages/ResetPasswordPage";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: false,
    },
  },
});

// Toaster はテーマに追従させる（sonner の theme プロパティ）
function ThemedToaster() {
  const { theme } = useTheme();
  const isDark = theme === "dark" || theme === "dark-gray";
  return <Toaster richColors position="top-right" theme={isDark ? "dark" : "light"} />;
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/setup" element={<SetupPage />} />
          <Route path="/forgot-password" element={<ForgotPasswordPage />} />
          <Route path="/reset-password" element={<ResetPasswordPage />} />
          <Route path="/files/:token" element={<FileDownloadPage />} />
          <Route element={<Layout />}>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/messages" element={<MessagesPage />} />
            <Route path="/messages/:id" element={<MessageDetailPage />} />
            <Route path="/quarantine" element={<QuarantinePage />} />
            <Route path="/quarantine/:id" element={<QuarantineDetailPage />} />
            <Route path="/users" element={<UsersPage />} />
            <Route path="/mailboxes" element={<MailboxesPage />} />
            <Route path="/audit-logs" element={<AuditLogsPage />} />
            <Route path="/api-keys" element={<APIKeysPage />} />
            <Route path="/simulate" element={<SimulatePage />} />
            <Route path="/policy" element={<PolicyPage />} />
            <Route path="/worker-instances" element={<WorkerInstancesPage />} />
            <Route path="/variables" element={<VariablesPage />} />
            <Route path="/approvals" element={<ApprovalsPage />} />
            <Route path="/approvals/:id" element={<ApprovalDetailPage />} />
            <Route path="/delayed" element={<DelayedPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
      <ThemedToaster />
      </ThemeProvider>
    </QueryClientProvider>
  );
}
