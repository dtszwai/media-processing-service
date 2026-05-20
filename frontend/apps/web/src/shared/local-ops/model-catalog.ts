import type { GenerationProviderModels } from "./types";

export const LOCAL_MODEL_CATALOG: GenerationProviderModels[] = [
  {
    outputType: "image",
    provider: "codex",
    models: ["gpt-5.5"],
    defaultModel: "gpt-5.5",
  },
  {
    outputType: "image",
    provider: "simulated",
    models: ["simulated-v1"],
    defaultModel: "simulated-v1",
  },
  {
    outputType: "audio",
    provider: "notebooklm",
    models: ["notebooklm-default"],
    defaultModel: "notebooklm-default",
  },
  {
    outputType: "audio",
    provider: "simulated",
    models: ["simulated-v1"],
    defaultModel: "simulated-v1",
  },
];
