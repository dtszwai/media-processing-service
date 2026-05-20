import { createConnectTransport } from "@connectrpc/connect-web";
import { API_URL } from "../config/env";

// Shared Connect transport. The local console is unauthenticated in compose;
// the API process injects local tenant/user claims when LOCAL_ONLY=true.
export const connectTransport = createConnectTransport({
  baseUrl: API_URL,
  useBinaryFormat: false,
});
