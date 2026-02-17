import type { Locale } from "../i18n.js";

export interface ChatSpec {
  id: string;
  name: string;
  session_id: string;
  user_id: string;
  channel: string;
  updated_at?: string;
}

export interface RuntimeContent {
  type?: string;
  text?: string;
}

export interface RuntimeMessage {
  role?: string;
  content?: RuntimeContent[];
}

export interface ChatHistoryResponse {
  messages?: RuntimeMessage[];
}

export interface AgentStreamEvent {
  type?: string;
  delta?: string;
  reply?: string;
  meta?: {
    code?: string;
    message?: string;
    [k: string]: unknown;
  };
  [k: string]: unknown;
}

export interface TUIMessage {
  role: "user" | "assistant";
  text: string;
  pending?: boolean;
  failed?: boolean;
}

export interface TUISettings {
  apiBase: string;
  apiKey: string;
  userID: string;
  channel: string;
  locale: Locale;
}

export interface TUIBootstrapOptions {
  sessionID?: string;
  userID: string;
  channel: string;
  apiBase: string;
  apiKey: string;
  locale: Locale;
}
