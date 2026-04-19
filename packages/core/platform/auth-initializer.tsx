"use client";

import { useEffect, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getApi } from "../api";
import { useAuthStore } from "../auth";
import { configStore } from "../config";
import { workspaceKeys } from "../workspace/queries";
import { createLogger } from "../logger";
import { defaultStorage } from "./storage";
import { setCurrentWorkspace } from "./workspace-storage";
import type { StorageAdapter } from "../types/storage";

const logger = createLogger("auth");

export function AuthInitializer({
  children,
  onLogin,
  onLogout,
  storage = defaultStorage,
  cookieAuth,
}: {
  children: ReactNode;
  onLogin?: () => void;
  onLogout?: () => void;
  storage?: StorageAdapter;
  cookieAuth?: boolean;
}) {
  const qc = useQueryClient();

  useEffect(() => {
    const api = getApi();

    // Fetch app config (CDN domain, etc.) in the background — non-blocking.
    api.getConfig().then((cfg) => {
      if (cfg.cdn_domain) configStore.getState().setCdnDomain(cfg.cdn_domain);
    }).catch(() => { /* config is optional — legacy file card matching degrades gracefully */ });

    if (cookieAuth) {
      // Cookie mode: the HttpOnly cookie is sent automatically by the browser.
      // Call the API to check if the session is still valid.
      //
      // Seed the workspace list into React Query so the URL-driven layout can
      // resolve the slug without a second fetch. The active workspace itself
      // is derived from the URL by [workspaceSlug]/layout.tsx — no imperative
      // selection here.
      Promise.all([api.getMe(), api.listWorkspaces()])
        .then(([user, wsList]) => {
          onLogin?.();
          useAuthStore.setState({ user, isLoading: false });
          qc.setQueryData(workspaceKeys.list(), wsList);
        })
        .catch((err) => {
          logger.error("cookie auth init failed", err);
          onLogout?.();
          useAuthStore.setState({ user: null, isLoading: false });
        });
      return;
    }

    // Token mode: read from localStorage (Electron / legacy).
    let token = storage.getItem("aurion_token");

    // Auto-login: if no token in storage but one is baked in at build time
    // via NEXT_PUBLIC_AUTO_TOKEN, inject it so the user lands directly in
    // the app without logging in.  Next.js inlines NEXT_PUBLIC_* at build
    // time, so we reference the env var directly (no runtime `process` check).
    const autoToken = process.env.NEXT_PUBLIC_AUTO_TOKEN ?? "";
    if (!token && autoToken) {
      token = autoToken.trim();
      storage.setItem("aurion_token", token);
    }

    if (!token) {
      onLogout?.();
      useAuthStore.setState({ isLoading: false });
      return;
    }

    api.setToken(token);

    Promise.all([api.getMe(), api.listWorkspaces()])
      .then(([user, wsList]) => {
        onLogin?.();
        useAuthStore.setState({ user, isLoading: false });
        // Seed React Query cache so the URL-driven layout can resolve the
        // slug without a second fetch.
        qc.setQueryData(workspaceKeys.list(), wsList);
      })
      .catch((err) => {
        logger.error("auth init failed", err);
        api.setToken(null);
        setCurrentWorkspace(null, null);
        storage.removeItem("aurion_token");
        onLogout?.();
        useAuthStore.setState({ user: null, isLoading: false });
      });
  }, []);

  return <>{children}</>;
}
