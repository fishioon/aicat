import { shallowRef } from "vue";

// How a provider is spoken to; mirrors internal/provider. "chat" is OpenAI
// Chat Completions without the Responses API.
export type Protocol = "anthropic" | "openai" | "chat";
export const protocolNames: Record<Protocol, string> = {
  anthropic: "Anthropic 接口",
  openai: "OpenAI 接口",
  chat: "OpenAI 接口（仅 Chat）",
};
// What an agent sets to reach a provider's mount.
export const protocolEnv: Record<Protocol, string> = {
  anthropic: "ANTHROPIC_BASE_URL",
  openai: "OPENAI_BASE_URL",
  chat: "OPENAI_BASE_URL",
};

// A vendor the page can fill in, leaving only the key to paste: its
// endpoints by protocol (the first is the default) and where it issues keys.
// Endpoints and key pages follow Magpie's presets (MIT,
// github.com/yetone/magpie internal/provider/presets.go).
export interface ProviderPreset {
  id: string;
  name: string;
  keys: string;
  endpoints: readonly (readonly [Protocol, string])[];
}
export const providerPresets: readonly ProviderPreset[] = [
  {
    id: "anthropic",
    name: "Anthropic",
    keys: "https://console.anthropic.com/settings/keys",
    endpoints: [["anthropic", "https://api.anthropic.com"]],
  },
  {
    id: "openai",
    name: "OpenAI",
    keys: "https://platform.openai.com/api-keys",
    endpoints: [["openai", "https://api.openai.com/v1"]],
  },
  {
    id: "google",
    name: "Google Gemini",
    keys: "https://aistudio.google.com/apikey",
    endpoints: [
      ["chat", "https://generativelanguage.googleapis.com/v1beta/openai"],
    ],
  },
  {
    id: "deepseek",
    name: "DeepSeek",
    keys: "https://platform.deepseek.com/api_keys",
    endpoints: [
      ["openai", "https://api.deepseek.com/v1"],
      ["anthropic", "https://api.deepseek.com/anthropic"],
    ],
  },
  {
    id: "xai",
    name: "xAI",
    keys: "https://console.x.ai",
    endpoints: [
      ["openai", "https://api.x.ai/v1"],
      ["anthropic", "https://api.x.ai"],
    ],
  },
  {
    id: "kimi",
    name: "Kimi",
    keys: "https://platform.moonshot.cn/console/api-keys",
    endpoints: [
      ["chat", "https://api.moonshot.cn/v1"],
      ["anthropic", "https://api.moonshot.cn/anthropic"],
    ],
  },
  {
    id: "zhipu",
    name: "智谱 GLM",
    keys: "https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys",
    endpoints: [
      ["chat", "https://open.bigmodel.cn/api/paas/v4"],
      ["anthropic", "https://open.bigmodel.cn/api/anthropic"],
    ],
  },
  {
    id: "qwen",
    name: "阿里云百炼",
    keys: "https://bailian.console.aliyun.com/?tab=model#/api-key",
    endpoints: [
      ["chat", "https://dashscope.aliyuncs.com/compatible-mode/v1"],
      ["anthropic", "https://dashscope.aliyuncs.com/apps/anthropic"],
    ],
  },
  {
    id: "volcengine",
    name: "火山方舟",
    keys: "https://ark.volcengine.com/region:cn-beijing/apikey",
    endpoints: [["openai", "https://ark.cn-beijing.volces.com/api/v3"]],
  },
  {
    id: "minimax",
    name: "MiniMax",
    keys: "https://platform.minimaxi.com/user-center/basic-information/interface-key",
    endpoints: [
      ["chat", "https://api.minimaxi.com/v1"],
      ["anthropic", "https://api.minimaxi.com/anthropic"],
    ],
  },
  {
    id: "siliconflow",
    name: "SiliconFlow",
    keys: "https://cloud.siliconflow.cn/account/ak",
    endpoints: [["chat", "https://api.siliconflow.cn/v1"]],
  },
  {
    id: "openrouter",
    name: "OpenRouter",
    keys: "https://openrouter.ai/keys",
    endpoints: [
      ["chat", "https://openrouter.ai/api/v1"],
      ["anthropic", "https://openrouter.ai/api"],
    ],
  },
  {
    id: "groq",
    name: "Groq",
    keys: "https://console.groq.com/keys",
    endpoints: [["openai", "https://api.groq.com/openai/v1"]],
  },
  {
    id: "mistral",
    name: "Mistral",
    keys: "https://console.mistral.ai/api-keys",
    endpoints: [["chat", "https://api.mistral.ai/v1"]],
  },
];
// A provider of the user's own: any service speaking one of the two protocols.
export const customEndpoints: ProviderPreset["endpoints"] = [
  ["openai", ""],
  ["anthropic", ""],
];

// The saved provider the provider page is editing; empty adds a new one.
export const providerId = shallowRef("");
