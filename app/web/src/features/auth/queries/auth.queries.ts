/**
 * Auth queries and mutations using TanStack Query
 */
import { createQuery, createMutation } from "@tanstack/svelte-query";
import { queryClient, queryKeys } from "../../../shared/queries";
import { UserInfoSchema } from "../../../shared/types";
import type { UserInfo, ApiKeyResponse } from "../../../shared/types";
import {
  login,
  register,
  getCurrentUser,
  createApiKey,
  listApiKeys,
  deleteApiKey,
} from "../services";
import type { LoginRequest, RegisterRequest, CreateApiKeyRequest } from "../services";
import { authStore } from "../stores/auth.store";

/**
 * Mutation for user login
 * Fetches user info after login and updates auth store
 */
export function createLoginMutation() {
  return createMutation(() => ({
    mutationFn: async (request: LoginRequest) => {
      const authResponse = await login(request);
      const user = await getCurrentUser(authResponse.token);
      return { authResponse, user };
    },
    onSuccess: ({ authResponse, user }: { authResponse: { token: string; refreshToken: string; expiresIn: number }; user: UserInfo }) => {
      authStore.login(authResponse.token, authResponse.refreshToken, authResponse.expiresIn, user);
      queryClient.clear();
    },
  }));
}

/**
 * Mutation for user registration
 * Fetches user info after register and updates auth store
 */
export function createRegisterMutation() {
  return createMutation(() => ({
    mutationFn: async (request: RegisterRequest) => {
      const authResponse = await register(request);
      const user = await getCurrentUser(authResponse.token);
      return { authResponse, user };
    },
    onSuccess: ({ authResponse, user }: { authResponse: { token: string; refreshToken: string; expiresIn: number }; user: UserInfo }) => {
      authStore.login(authResponse.token, authResponse.refreshToken, authResponse.expiresIn, user);
      queryClient.clear();
    },
  }));
}

/**
 * Query for current user info
 */
export function createCurrentUserQuery(enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.auth.user(),
    queryFn: async (): Promise<UserInfo> => {
      const data = await getCurrentUser();
      return UserInfoSchema.parse(data);
    },
    enabled,
    staleTime: 5 * 60 * 1000,
  }));
}

/**
 * Query for API keys list
 */
export function createApiKeysQuery(enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.auth.apiKeys(),
    queryFn: async (): Promise<ApiKeyResponse[]> => {
      return listApiKeys();
    },
    enabled,
  }));
}

/**
 * Mutation for creating an API key
 */
export function createApiKeyMutation() {
  return createMutation(() => ({
    mutationFn: async (request: CreateApiKeyRequest) => {
      return createApiKey(request);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.auth.apiKeys() });
    },
  }));
}

/**
 * Mutation for deleting an API key
 */
export function createDeleteApiKeyMutation() {
  return createMutation(() => ({
    mutationFn: async (keyId: string) => {
      await deleteApiKey(keyId);
      return keyId;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.auth.apiKeys() });
    },
  }));
}
