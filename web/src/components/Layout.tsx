import { Outlet, Navigate } from "react-router-dom";
import { Sidebar } from "./Sidebar";
import { useMe } from "../hooks/useAuth";
import { ApiError } from "../lib/api";
import { Skeleton } from "./ui/skeleton";

export function Layout() {
  const { data: user, isLoading, error } = useMe();

  if (isLoading) {
    return (
      <div className="flex h-screen bg-gray-50 items-center justify-center">
        <div className="space-y-3 w-64">
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-3/4" />
          <Skeleton className="h-8 w-1/2" />
        </div>
      </div>
    );
  }

  if (error instanceof ApiError && error.status === 401) {
    return <Navigate to="/login" replace />;
  }

  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />
      <main className="flex-1 overflow-auto bg-gray-50">
        <Outlet />
      </main>
    </div>
  );
}
