import { createClient } from "@connectrpc/connect";
import { GenerationService } from "@media-service/api-client/gen/mediaservice/generation/v1/generation_pb.js";
import { connectTransport } from "./http/connect";

export const generationClient = createClient(GenerationService, connectTransport);
