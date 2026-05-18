import { createConnectTransport } from "@connectrpc/connect-web";
import { API_URL } from "../config/env";

// Shared Connect transport. The console is LOCAL_ONLY and unauthenticated —
// the API process is gated by env, not by request-side credentials.
export const connectTransport = createConnectTransport({
  baseUrl: API_URL,
  useBinaryFormat: false,
});
