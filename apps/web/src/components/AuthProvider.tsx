"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { logout as apiLogout, me, type UserDTO } from "@/lib/auth";
import { ApiError } from "@/lib/http";

/**
 * 全局登录状态。
 *
 * 说明：会话保存在 HttpOnly Cookie 中，前端无法直接读取，
 * 只能在挂载后向服务端询问（/users/me）。服务端渲染时 status 恒为 "loading"，
 * 避免 SSR 与客户端 hydration 结果不一致（hydration mismatch）。
 */

type AuthStatus = "loading" | "authenticated" | "anonymous";

type AuthContextValue = {
  status: AuthStatus;
  user: UserDTO | null;
  /** 登录/注册成功后调用，立即把会话写入全局状态。 */
  setSession: (user: UserDTO) => void;
  /** 清除令牌并回到未登录状态。 */
  signOut: () => void;
  /** 重新从服务端拉取当前用户（用于资料更新后刷新）。 */
  refresh: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [user, setUser] = useState<UserDTO | null>(null);

  // 首次挂载时尝试恢复会话。无令牌则直接判定为未登录。
  useEffect(() => {
    let cancelled = false;

    async function restore() {
      // 会话在 HttpOnly Cookie 里，前端无法直接判断是否存在，
      // 只能向服务端询问；未登录时 /users/me 返回 401，me() 归一为 null。
      try {
        const current = await me();
        if (cancelled) return;
        if (current) {
          setUser(current);
          setStatus("authenticated");
        } else {
          setUser(null);
          setStatus("anonymous");
        }
      } catch (error) {
        if (cancelled) return;
        // 网络或服务端故障：不清令牌，降级为未登录但不误导用户。
        if (error instanceof ApiError && error.status >= 500) {
          setStatus("anonymous");
          return;
        }
        setStatus("anonymous");
      }
    }

    void restore();
    return () => {
      cancelled = true;
    };
  }, []);

  const setSession = useCallback((next: UserDTO) => {
    setUser(next);
    setStatus("authenticated");
  }, []);

  const signOut = useCallback(async () => {
    // 先清服务端 Cookie，再复位本地状态；即使请求失败也要让界面退出登录。
    await apiLogout();
    setUser(null);
    setStatus("anonymous");
  }, []);

  const refresh = useCallback(async () => {
    const current = await me();
    if (current) {
      setUser(current);
      setStatus("authenticated");
    } else {
      setUser(null);
      setStatus("anonymous");
    }
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ status, user, setSession, signOut, refresh }),
    [status, user, setSession, signOut, refresh],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth 必须在 <AuthProvider> 内使用");
  }
  return ctx;
}
