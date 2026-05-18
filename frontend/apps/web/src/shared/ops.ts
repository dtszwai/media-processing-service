// Typed Connect client for OpsService. The console is LOCAL_ONLY — the API
// process refuses to mount this surface when LOCAL_ONLY=false, so no auth
// interceptor is wired here.
import { createClient } from "@connectrpc/connect";
import { OpsService } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
import { GenerationService } from "@media-service/api-client/gen/mediaservice/generation/v1/generation_pb.js";
import { connectTransport } from "./http/connect";

export const opsClient = createClient(OpsService, connectTransport);
export const generationClient = createClient(GenerationService, connectTransport);
