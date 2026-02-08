<script lang="ts">
  import { createRegisterMutation } from "../queries";

  interface Props {
    navigate: (path: string) => void;
  }

  let { navigate }: Props = $props();

  let tenantName = $state("");
  let email = $state("");
  let password = $state("");
  let errorMessage = $state("");

  const registerMutation = createRegisterMutation();

  let isValid = $derived(tenantName.length >= 2 && email.includes("@") && password.length >= 8);

  async function handleSubmit(e: Event) {
    e.preventDefault();
    errorMessage = "";

    registerMutation.mutate(
      { tenantName, email, password },
      {
        onSuccess: () => {
          navigate("/media/images");
        },
        onError: (error: Error) => {
          errorMessage = error.message || "Registration failed. Please try again.";
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
        <h1 class="text-xl font-semibold text-gray-900">Create account</h1>
        <p class="text-sm text-gray-500 mt-1">Media Processing Service</p>
      </div>

      {#if errorMessage}
        <div class="mb-4 p-3 bg-red-50 border border-red-200 rounded-md">
          <p class="text-sm text-red-700">{errorMessage}</p>
        </div>
      {/if}

      <form onsubmit={handleSubmit} class="space-y-4">
        <div>
          <label for="tenantName" class="block text-sm font-medium text-gray-700 mb-1">Organization name</label>
          <input
            id="tenantName"
            type="text"
            bind:value={tenantName}
            required
            minlength="2"
            class="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-gray-400 focus:border-transparent"
          />
        </div>

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
          disabled={!isValid || registerMutation.isPending}
          class="w-full py-2 px-4 bg-gray-900 text-white text-sm font-medium rounded-md hover:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-gray-400 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {#if registerMutation.isPending}
            Creating account...
          {:else}
            Create account
          {/if}
        </button>
      </form>

      <p class="mt-6 text-center text-sm text-gray-500">
        Already have an account?
        <button type="button" onclick={() => navigate("/login")} class="text-gray-900 font-medium hover:underline">
          Sign in
        </button>
      </p>
    </div>
  </div>
</div>
