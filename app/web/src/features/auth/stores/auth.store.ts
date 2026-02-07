/**
 * Auth store — manages authentication state
 */
import { writable } from "svelte/store";
import type { UserInfo } from "../../../shared/types";
import {
  saveAuthTokens,
  clearAuthTokens,
  getAccessToken,
  getUserInfo,
  saveUserInfo,
  isTokenExpired,
} from "../../../shared/auth";

export interface AuthState {
  isAuthenticated: boolean;
  user: UserInfo | null;
  isLoading: boolean;
}

function createAuthStore() {
  const { subscribe, set, update } = writable<AuthState>({
    isAuthenticated: false,
    user: null,
    isLoading: true,
  });

  return {
    subscribe,

    login(token: string, refreshToken: string, expiresIn: number, user: UserInfo) {
      saveAuthTokens(token, refreshToken, expiresIn);
      saveUserInfo(user);
      set({ isAuthenticated: true, user, isLoading: false });
    },

    logout() {
      clearAuthTokens();
      set({ isAuthenticated: false, user: null, isLoading: false });
    },

    /** Restore session from localStorage on app mount */
    restore() {
      const token = getAccessToken();
      const user = getUserInfo();

      if (token && user && !isTokenExpired()) {
        set({ isAuthenticated: true, user, isLoading: false });
      } else {
        clearAuthTokens();
        set({ isAuthenticated: false, user: null, isLoading: false });
      }
    },

    updateUser(user: UserInfo) {
      saveUserInfo(user);
      update((s) => ({ ...s, user }));
    },
  };
}

export const authStore = createAuthStore();
