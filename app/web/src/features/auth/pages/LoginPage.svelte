<script lang="ts">
  import { createLoginMutation } from "../queries";

  interface Props {
    navigate: (path: string) => void;
  }

  let { navigate }: Props = $props();

  let email = $state("");
  let password = $state("");
  let errorMessage = $state("");

  const loginMutation = createLoginMutation();

  let isValid = $derived(email.includes("@") && password.length >= 8);

  async function handleSubmit(e: Event) {
    e.preventDefault();
    errorMessage = "";

    loginMutation.mutate(
      { email, password },
      {
        onSuccess: () => {
          navigate("/");
        },
        onError: (error: Error) => {
          if (error.message === "Authentication required") {
            errorMessage = "Invalid email or password.";
          } else {
            errorMessage = error.message || "Login failed. Please try again.";
          }
        },
      },
    );
  }
</script>

<div class="min-h-[80vh] flex items-center justify-center">
  <div class="w-full max-w-sm">
    <div class="bg-white rounded-lg border border-gray-200 p-8">
      <div class="text-center mb-8">
        <svg class="w-10 h-10 text-gray-800 mx-auto mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.5"
            d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
          ></path>
        </svg>
        <h1 class="text-xl font-semibold text-gray-900">Sign in</h1>
        <p class="text-sm text-gray-500 mt-1">Media Processing Service</p>
      </div>

      {#if errorMessage}
        <div class="mb-4 p-3 bg-red-50 border border-red-200 rounded-md">
          <p class="text-sm text-red-700">{errorMessage}</p>
        </div>
      {/if}

      <form onsubmit={handleSubmit} class="space-y-4">
        <div>
          <label for="email" class="block text-sm font-medium text-gray-700 mb-1">Email</label>
          <input
            id="email"
            type="email"
            bind:value={email}
            required
            class="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-gray-400 focus:border-transparent"
            placeholder="you@example.com"
          />
        </div>

        <div>
          <label for="password" class="block text-sm font-medium text-gray-700 mb-1">Password</label>
          <input
            id="password"
            type="password"
            bind:value={password}
            required
            minlength="8"
            class="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-gray-400 focus:border-transparent"
            placeholder="Min. 8 characters"
          />
        </div>

        <button
          type="submit"
          disabled={!isValid || loginMutation.isPending}
          class="w-full py-2 px-4 bg-gray-900 text-white text-sm font-medium rounded-md hover:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-gray-400 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {#if loginMutation.isPending}
            Signing in...
          {:else}
            Sign in
          {/if}
        </button>
      </form>

      <p class="mt-6 text-center text-sm text-gray-500">
        Don't have an account?
        <button
          type="button"
          onclick={() => navigate("/register")}
          class="text-gray-900 font-medium hover:underline"
        >
          Create account
        </button>
      </p>
    </div>
  </div>
</div>
