import { parseErrorMessage } from "./api-utils.js";
import {
  CronWorkflowCanvas,
  createDefaultCronWorkflow,
  validateCronWorkflowSpec,
} from "./cron-workflow.js";
import { DEFAULT_LOCALE, getLocale, isWebMessageKey, setLocale, t } from "./i18n.js";

type Tone = "neutral" | "info" | "error";
type TabKey = "chat" | "cron";
type SettingsSectionKey = "connection" | "identity" | "display" | "models" | "channels" | "workspace";
type ModelsSettingsLevel = "list" | "edit";
type ChannelsSettingsLevel = "list" | "edit";
type WorkspaceSettingsLevel = "list" | "config" | "prompt" | "codex";
type WorkspaceCardKey = "config" | "prompt" | "codex";
type PromptMode = "default" | "codex";
type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";
type ProviderKVKind = "headers" | "aliases";
type WorkspaceEditorMode = "json" | "text";
type CronModalMode = "create" | "edit";

const TRASH_ICON_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path fill="currentColor" d="M7 21q-.825 0-1.412-.587T5 19V6H4V4h5V3h6v1h5v2h-1v13q0 .825-.587 1.413T17 21zM17 6H7v13h10zM9 17h2V8H9zm4 0h2V8h-2zM7 6v13z"/></svg>`;

interface ChatSpec {
  id: string;
  name: string;
  session_id: string;
  user_id: string;
  channel: string;
  updated_at: string;
  meta?: Record<string, unknown>;
}

interface RuntimeContent {
  type?: string;
  text?: string;
}

interface RuntimeMessage {
  id?: string;
  role?: string;
  content?: RuntimeContent[];
  metadata?: Record<string, unknown>;
}

interface ChatHistoryResponse {
  messages: RuntimeMessage[];
}

interface ModelInfo {
  id: string;
  name: string;
  status?: string;
  alias_of?: string;
  capabilities?: ModelCapabilities;
  limit?: ModelLimit;
}

interface ModelModalities {
  text: boolean;
  audio: boolean;
  image: boolean;
  video: boolean;
  pdf: boolean;
}

interface ModelCapabilities {
  temperature: boolean;
  reasoning: boolean;
  attachment: boolean;
  tool_call: boolean;
  input?: ModelModalities;
  output?: ModelModalities;
}

interface ModelLimit {
  context?: number;
  input?: number;
  output?: number;
}

interface ProviderInfo {
  id: string;
  name: string;
  display_name: string;
  openai_compatible?: boolean;
  api_key_prefix?: string;
  models: ModelInfo[];
  headers?: Record<string, string>;
  timeout_ms?: number;
  model_aliases?: Record<string, string>;
  allow_custom_base_url?: boolean;
  enabled?: boolean;
  has_api_key: boolean;
  current_api_key?: string;
  current_base_url?: string;
}

interface ProviderTypeInfo {
  id: string;
  display_name: string;
}

interface ComposerModelOption {
  value: string;
  canonical: string;
  label: string;
}

interface ModelSlotConfig {
  provider_id: string;
  model: string;
}

interface ModelCatalogInfo {
  providers: ProviderInfo[];
  provider_types?: ProviderTypeInfo[];
  defaults: Record<string, string>;
  active_llm?: ModelSlotConfig;
}

interface ActiveModelsInfo {
  active_llm?: ModelSlotConfig;
}

interface DeleteResult {
  deleted: boolean;
}

interface PersistedSettings {
  apiBase?: unknown;
  apiKey?: unknown;
  workspaceCardEnabled?: unknown;
}

interface WorkspaceFileInfo {
  path: string;
  kind: "config" | "skill";
  size: number | null;
}

interface WorkspaceCodexTreeNode {
  name: string;
  path: string;
  folders: WorkspaceCodexTreeNode[];
  files: WorkspaceFileInfo[];
}

interface WorkspaceTextPayload {
  content: string;
}

interface CronScheduleSpec {
  type: string;
  cron: string;
  timezone?: string;
}

interface CronDispatchTarget {
  user_id: string;
  session_id: string;
}

interface CronDispatchSpec {
  type?: string;
  channel?: string;
  target: CronDispatchTarget;
  mode?: string;
  meta?: Record<string, unknown>;
}

interface CronRuntimeSpec {
  max_concurrency?: number;
  timeout_seconds?: number;
  misfire_grace_seconds?: number;
}

interface CronWorkflowViewport {
  pan_x?: number;
  pan_y?: number;
  zoom?: number;
}

interface CronWorkflowNode {
  id: string;
  type: "start" | "text_event" | "delay" | "if_event";
  title?: string;
  x: number;
  y: number;
  text?: string;
  delay_seconds?: number;
  if_condition?: string;
  continue_on_error?: boolean;
}

interface CronWorkflowEdge {
  id: string;
  source: string;
  target: string;
}

interface CronWorkflowSpec {
  version: "v1";
  viewport?: CronWorkflowViewport;
  nodes: CronWorkflowNode[];
  edges: CronWorkflowEdge[];
}

interface CronWorkflowNodeExecution {
  node_id: string;
  node_type: "text_event" | "delay" | "if_event";
  status: "succeeded" | "failed" | "skipped";
  continue_on_error: boolean;
  started_at: string;
  finished_at?: string;
  error?: string;
}

interface CronWorkflowExecution {
  run_id: string;
  started_at: string;
  finished_at?: string;
  had_failures: boolean;
  nodes: CronWorkflowNodeExecution[];
}

interface CronJobSpec {
  id: string;
  name: string;
  enabled: boolean;
  schedule: CronScheduleSpec;
  task_type: "text" | "workflow";
  text?: string;
  workflow?: CronWorkflowSpec;
  dispatch: CronDispatchSpec;
  runtime: CronRuntimeSpec;
  meta?: Record<string, unknown>;
}

interface CronJobState {
  next_run_at?: string;
  last_run_at?: string;
  last_status?: string;
  last_error?: string;
  paused?: boolean;
  last_execution?: CronWorkflowExecution;
}

type QQTargetType = "c2c" | "group" | "guild";
type QQAPIEnvironment = "production" | "sandbox";

interface QQChannelConfig {
  enabled: boolean;
  app_id: string;
  client_secret: string;
  bot_prefix: string;
  target_type: QQTargetType;
  target_id: string;
  api_base: string;
  token_url: string;
  timeout_seconds: number;
}

interface ViewMessage {
  id: string;
  role: "user" | "assistant";
  text: string;
  toolCalls: ViewToolCallNotice[];
  textOrder?: number;
  toolOrder?: number;
  timeline: ViewMessageTimelineEntry[];
}

interface ViewToolCallNotice {
  summary: string;
  raw: string;
  order?: number;
  step?: number;
  toolName?: string;
  outputReady?: boolean;
}

interface ViewMessageTimelineEntry {
  type: "text" | "tool_call";
  order: number;
  text?: string;
  toolCall?: ViewToolCallNotice;
}

interface AgentToolCallPayload {
  name?: string;
  input?: Record<string, unknown>;
}

interface AgentToolResultPayload {
  name?: string;
  ok?: boolean;
  summary?: string;
}

interface AgentStreamEvent {
  type?: string;
  step?: number;
  delta?: string;
  reply?: string;
  tool_call?: AgentToolCallPayload;
  tool_result?: AgentToolResultPayload;
  meta?: Record<string, unknown>;
  raw?: string;
}

interface AgentSystemLayerInfo {
  name?: string;
  role?: string;
  source?: string;
  content_preview?: string;
  estimated_tokens?: number;
}

interface AgentSystemLayersResponse {
  version?: string;
  layers?: AgentSystemLayerInfo[];
  estimated_tokens_total?: number;
}

interface RuntimeConfigFeatureFlags {
  prompt_templates?: boolean;
  prompt_context_introspect?: boolean;
}

interface RuntimeConfigResponse {
  features?: RuntimeConfigFeatureFlags;
}

interface JSONRequestOptions {
  method?: HttpMethod;
  body?: unknown;
  headers?: Record<string, string>;
}

interface UpsertProviderOptions {
  closeAfterSave?: boolean;
  notifyStatus?: boolean;
}

interface CustomSelectInstance {
  container: HTMLDivElement;
  trigger: HTMLDivElement;
  selectedText: HTMLSpanElement;
  optionsList: HTMLDivElement;
  optionsBody: HTMLDivElement;
  searchInput: HTMLInputElement | null;
  isSearchEnabled: boolean;
}

const DEFAULT_API_BASE = "http://127.0.0.1:8088";
const DEFAULT_API_KEY = "";
const DEFAULT_USER_ID = "demo-user";
const DEFAULT_CHANNEL = "console";
const WEB_CHAT_CHANNEL = DEFAULT_CHANNEL;
const DEFAULT_CRON_JOB_ID = "cron-default";
const CRON_META_SYSTEM_DEFAULT = "system_default";
const QQ_CHANNEL = "qq";
const DEFAULT_QQ_API_BASE = "https://api.sgroup.qq.com";
const QQ_SANDBOX_API_BASE = "https://sandbox.api.sgroup.qq.com";
const DEFAULT_QQ_TOKEN_URL = "https://bots.qq.com/app/getAppAccessToken";
const DEFAULT_QQ_TIMEOUT_SECONDS = 8;
const CHAT_LIVE_REFRESH_INTERVAL_MS = 1500;
const PROVIDER_AUTO_SAVE_DELAY_MS = 900;
const REQUEST_SOURCE_HEADER = "X-NextAI-Source";
const REQUEST_SOURCE_WEB = "web";
const CUSTOM_SELECT_OPTIONS_VERTICAL_GAP_PX = 6;
const CUSTOM_SELECT_VISIBLE_OPTIONS_COUNT = 3;
const CUSTOM_SELECT_OPTION_ROW_HEIGHT_PX = 38;
const CUSTOM_SELECT_OPTIONS_LIST_VERTICAL_PADDING_PX = 8;
const CUSTOM_SELECT_MAX_OPTIONS_HEIGHT_PX = CUSTOM_SELECT_VISIBLE_OPTIONS_COUNT * CUSTOM_SELECT_OPTION_ROW_HEIGHT_PX
  + CUSTOM_SELECT_OPTIONS_LIST_VERTICAL_PADDING_PX;
const CUSTOM_SELECT_SEARCH_FIELD_HEIGHT_PX = 40;
const CUSTOM_SELECT_OPTIONS_PANEL_VERTICAL_PADDING_PX = 12;
const CUSTOM_SELECT_MAX_PANEL_HEIGHT_PX = CUSTOM_SELECT_SEARCH_FIELD_HEIGHT_PX
  + CUSTOM_SELECT_OPTIONS_VERTICAL_GAP_PX
  + CUSTOM_SELECT_MAX_OPTIONS_HEIGHT_PX
  + CUSTOM_SELECT_OPTIONS_PANEL_VERTICAL_PADDING_PX;
const SCROLLBAR_ACTIVE_CLASS = "is-scrollbar-scrolling";
const SCROLLBAR_IDLE_HIDE_DELAY_MS = 520;
const DEFAULT_OPENAI_MODEL_IDS = ["gpt-4o-mini", "gpt-4.1-mini"];
const DEFAULT_MODEL_CONTEXT_LIMIT_TOKENS = 128000;
const PROMPT_TEMPLATE_PREFIX = "/prompts:";
const SYSTEM_PROMPT_LAYER_ENDPOINT = "/agent/system-layers";
const SYSTEM_PROMPT_WORKSPACE_FALLBACK_PATHS = ["docs/AI/AGENTS.md", "docs/AI/ai-tools.md"] as const;
const SYSTEM_PROMPT_WORKSPACE_PATH_SET = new Set(SYSTEM_PROMPT_WORKSPACE_FALLBACK_PATHS.map((path) => path.toLowerCase()));
const WORKSPACE_CODEX_PREFIX = "prompts/codex/";
const WORKSPACE_CARD_KEYS: WorkspaceCardKey[] = ["config", "prompt", "codex"];
const DEFAULT_WORKSPACE_CARD_ENABLED: Record<WorkspaceCardKey, boolean> = {
  config: true,
  prompt: true,
  codex: true,
};
const PROMPT_TEMPLATE_NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const PROMPT_TEMPLATE_ARG_KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;
const PROMPT_TEMPLATE_PLACEHOLDER_PATTERN = /\$([A-Za-z_][A-Za-z0-9_]*)/g;
const FEATURE_FLAG_PROMPT_TEMPLATES = "nextai.feature.prompt_templates";
const FEATURE_FLAG_PROMPT_CONTEXT_INTROSPECT = "nextai.feature.prompt_context_introspect";
const PROMPT_MODE_META_KEY = "prompt_mode";
const SETTINGS_KEY = "nextai.web.chat.settings";
const LOCALE_KEY = "nextai.web.locale";
const BUILTIN_PROVIDER_IDS = new Set(["openai"]);
const TABS: TabKey[] = ["chat", "cron"];
const customSelectInstances = new Map<HTMLSelectElement, CustomSelectInstance>();
const scrollbarActivityTimers = new WeakMap<HTMLElement, number>();
let customSelectGlobalEventsBound = false;
let chatLiveRefreshTimer: number | null = null;
let chatLiveRefreshInFlight = false;
let openChatRequestSerial = 0;
let providerAutoSaveTimer: number | null = null;
let providerAutoSaveInFlight = false;
let providerAutoSaveQueued = false;
let activeSettingsSection: SettingsSectionKey = "models";
let syncingComposerModelSelectors = false;
let systemPromptTokensLoaded = false;
let systemPromptTokensInFlight: Promise<void> | null = null;
let systemPromptTokens = 0;
const runtimeFlags: Required<RuntimeConfigFeatureFlags> = {
  prompt_templates: false,
  prompt_context_introspect: false,
};

const apiBaseInput = mustElement<HTMLInputElement>("api-base");
const apiKeyInput = mustElement<HTMLInputElement>("api-key");
const localeSelect = mustElement<HTMLSelectElement>("locale-select");
const promptContextIntrospectInput = mustElement<HTMLInputElement>("feature-prompt-context-introspect");
const reloadChatsButton = mustElement<HTMLButtonElement>("reload-chats");
const settingsToggleButton = mustElement<HTMLButtonElement>("settings-toggle");
const settingsPopover = mustElement<HTMLElement>("settings-popover");
const settingsPopoverCloseButton = mustElement<HTMLButtonElement>("settings-popover-close");
const chatCronToggleButton = mustElement<HTMLButtonElement>("chat-cron-toggle");
const chatSearchToggleButton = mustElement<HTMLButtonElement>("chat-search-toggle");
const searchModal = mustElement<HTMLElement>("search-modal");
const searchModalCloseButton = mustElement<HTMLButtonElement>("search-modal-close-btn");
const modelsSettingsSection = mustElement<HTMLElement>("settings-section-models");
const channelsSettingsSection = mustElement<HTMLElement>("settings-section-channels");
const workspaceSettingsSection = mustElement<HTMLElement>("settings-section-workspace");
const settingsSectionButtons = Array.from(document.querySelectorAll<HTMLButtonElement>("[data-settings-section]"));
const settingsSectionPanels = Array.from(document.querySelectorAll<HTMLElement>("[data-settings-section-panel]"));
const statusLine = mustElement<HTMLElement>("status-line");

const tabButtons = Array.from(document.querySelectorAll<HTMLButtonElement>(".tab-btn"));

const panelChat = mustElement<HTMLElement>("panel-chat");
const panelCron = mustElement<HTMLElement>("panel-cron");

const newChatButton = mustElement<HTMLButtonElement>("new-chat");
const chatList = mustElement<HTMLUListElement>("chat-list");
const chatTitle = mustElement<HTMLElement>("chat-title");
const chatSession = mustElement<HTMLElement>("chat-session");
const chatPromptModeToggle = mustElement<HTMLInputElement>("chat-prompt-mode-toggle");
const searchChatInput = mustElement<HTMLInputElement>("search-chat-input");
const searchChatResults = mustElement<HTMLUListElement>("search-chat-results");
const messageList = mustElement<HTMLUListElement>("message-list");
const composerForm = mustElement<HTMLFormElement>("composer");
const messageInput = mustElement<HTMLTextAreaElement>("message-input");
const sendButton = mustElement<HTMLButtonElement>("send-btn");
const composerAttachButton = mustElement<HTMLButtonElement>("composer-attach-btn");
const composerAttachInput = mustElement<HTMLInputElement>("composer-attach-input");
const composerProviderSelect = mustElement<HTMLSelectElement>("composer-provider-select");
const composerModelSelect = mustElement<HTMLSelectElement>("composer-model-select");
const composerTokenEstimate = mustElement<HTMLElement>("composer-token-estimate");

const refreshModelsButton = mustElement<HTMLButtonElement>("refresh-models");
const modelsAddProviderButton = mustElement<HTMLButtonElement>("models-add-provider-btn");
const modelsProviderList = mustElement<HTMLUListElement>("models-provider-list");
const modelsLevel1View = mustElement<HTMLElement>("models-level1-view");
const modelsLevel2View = mustElement<HTMLElement>("models-level2-view");
const modelsEditProviderMeta = mustElement<HTMLElement>("models-edit-provider-meta");
const modelsProviderModalTitle = mustElement<HTMLElement>("models-provider-modal-title");
const modelsProviderForm = mustElement<HTMLFormElement>("models-provider-form");
const modelsProviderTypeSelect = mustElement<HTMLSelectElement>("models-provider-type-select");
const modelsProviderNameInput = mustElement<HTMLInputElement>("models-provider-name-input");
const modelsProviderAPIKeyInput = mustElement<HTMLInputElement>("models-provider-api-key-input");
const modelsProviderAPIKeyVisibilityButton = mustElement<HTMLButtonElement>("models-provider-api-key-visibility-btn");
const modelsProviderBaseURLInput = mustElement<HTMLInputElement>("models-provider-base-url-input");
const modelsProviderBaseURLPreview = mustElement<HTMLElement>("models-provider-base-url-preview");
const modelsProviderTimeoutMSInput = mustElement<HTMLInputElement>("models-provider-timeout-ms-input");
const modelsProviderEnabledInput = mustElement<HTMLInputElement>("models-provider-enabled-input");
const modelsProviderHeadersRows = mustElement<HTMLElement>("models-provider-headers-rows");
const modelsProviderHeadersAddButton = mustElement<HTMLButtonElement>("models-provider-headers-add-btn");
const modelsProviderAliasesRows = mustElement<HTMLElement>("models-provider-aliases-rows");
const modelsProviderAliasesAddButton = mustElement<HTMLButtonElement>("models-provider-aliases-add-btn");
const modelsProviderCustomModelsField = mustElement<HTMLElement>("models-provider-custom-models-field");
const modelsProviderCustomModelsRows = mustElement<HTMLElement>("models-provider-custom-models-rows");
const modelsProviderCustomModelsAddButton = mustElement<HTMLButtonElement>("models-provider-custom-models-add-btn");
const modelsProviderCancelButton = mustElement<HTMLButtonElement>("models-provider-cancel-btn");

const refreshWorkspaceButton = mustElement<HTMLButtonElement>("refresh-workspace");
const workspaceImportOpenButton = mustElement<HTMLButtonElement>("workspace-import-open-btn");
const channelsEntryList = mustElement<HTMLUListElement>("channels-entry-list");
const channelsLevel1View = mustElement<HTMLElement>("channels-level1-view");
const channelsLevel2View = mustElement<HTMLElement>("channels-level2-view");
const qqChannelForm = mustElement<HTMLFormElement>("qq-channel-form");
const qqChannelEnabledInput = mustElement<HTMLInputElement>("qq-channel-enabled");
const qqChannelAppIDInput = mustElement<HTMLInputElement>("qq-channel-app-id");
const qqChannelClientSecretInput = mustElement<HTMLInputElement>("qq-channel-client-secret");
const qqChannelBotPrefixInput = mustElement<HTMLInputElement>("qq-channel-bot-prefix");
const qqChannelTargetTypeSelect = mustElement<HTMLSelectElement>("qq-channel-target-type");
const qqChannelAPIEnvironmentSelect = mustElement<HTMLSelectElement>("qq-channel-api-env");
const qqChannelTimeoutSecondsInput = mustElement<HTMLInputElement>("qq-channel-timeout-seconds");
const workspaceEntryList = mustElement<HTMLUListElement>("workspace-entry-list");
const workspaceLevel1View = mustElement<HTMLElement>("workspace-level1-view");
const workspaceLevel2ConfigView = mustElement<HTMLElement>("workspace-level2-config-view");
const workspaceLevel2PromptView = mustElement<HTMLElement>("workspace-level2-prompt-view");
const workspaceLevel2CodexView = mustElement<HTMLElement>("workspace-level2-codex-view");
const workspaceFilesBody = mustElement<HTMLUListElement>("workspace-files-body");
const workspacePromptsBody = mustElement<HTMLUListElement>("workspace-prompts-body");
const workspaceCodexTreeBody = mustElement<HTMLUListElement>("workspace-codex-tree-body");
const workspaceCreateFileForm = mustElement<HTMLFormElement>("workspace-create-file-form");
const workspaceNewPathInput = mustElement<HTMLInputElement>("workspace-new-path");
const workspaceEditorModal = mustElement<HTMLElement>("workspace-editor-modal");
const workspaceEditorModalCloseButton = mustElement<HTMLButtonElement>("workspace-editor-modal-close-btn");
const workspaceImportModal = mustElement<HTMLElement>("workspace-import-modal");
const workspaceImportModalCloseButton = mustElement<HTMLButtonElement>("workspace-import-modal-close-btn");
const workspaceEditorForm = mustElement<HTMLFormElement>("workspace-editor-form");
const workspaceFilePathInput = mustElement<HTMLInputElement>("workspace-file-path");
const workspaceFileContentInput = mustElement<HTMLTextAreaElement>("workspace-file-content");
const workspaceSaveFileButton = mustElement<HTMLButtonElement>("workspace-save-file-btn");
const workspaceDeleteFileButton = mustElement<HTMLButtonElement>("workspace-delete-file-btn");
const workspaceImportForm = mustElement<HTMLFormElement>("workspace-import-form");
const workspaceJSONInput = mustElement<HTMLTextAreaElement>("workspace-json");

const refreshCronButton = mustElement<HTMLButtonElement>("refresh-cron");
const cronChatToggleButton = mustElement<HTMLButtonElement>("cron-chat-toggle");
const cronWorkbench = mustElement<HTMLElement>("cron-workbench");
const cronJobsBody = mustElement<HTMLUListElement>("cron-jobs-body");
const cronCreateOpenButton = mustElement<HTMLButtonElement>("cron-create-open-btn");
const cronCreateModal = mustElement<HTMLElement>("cron-create-modal");
const cronCreateModalTitle = mustElement<HTMLElement>("cron-create-modal-title");
const cronCreateModalCloseButton = mustElement<HTMLButtonElement>("cron-create-modal-close-btn");
const cronCreateForm = mustElement<HTMLFormElement>("cron-create-form");
const cronDispatchHint = mustElement<HTMLElement>("cron-dispatch-hint");
const cronIDInput = mustElement<HTMLInputElement>("cron-id");
const cronNameInput = mustElement<HTMLInputElement>("cron-name");
const cronIntervalInput = mustElement<HTMLInputElement>("cron-interval");
const cronSessionIDInput = mustElement<HTMLInputElement>("cron-session-id");
const cronMaxConcurrencyInput = mustElement<HTMLInputElement>("cron-max-concurrency");
const cronTimeoutInput = mustElement<HTMLInputElement>("cron-timeout-seconds");
const cronMisfireInput = mustElement<HTMLInputElement>("cron-misfire-grace");
const cronTaskTypeSelect = mustElement<HTMLSelectElement>("cron-task-type");
const cronTextInput = mustElement<HTMLTextAreaElement>("cron-text");
const cronTextSection = mustElement<HTMLElement>("cron-text-section");
const cronWorkflowSection = mustElement<HTMLElement>("cron-workflow-section");
const cronResetWorkflowButton = mustElement<HTMLButtonElement>("cron-reset-workflow");
const cronWorkflowFullscreenButton = mustElement<HTMLButtonElement>("cron-workflow-fullscreen-btn");
const cronWorkflowViewport = mustElement<HTMLElement>("cron-workflow-viewport");
const cronWorkflowCanvas = mustElement<HTMLElement>("cron-workflow-canvas");
const cronWorkflowEdges = mustElement<SVGSVGElement>("cron-workflow-edges");
const cronWorkflowNodes = mustElement<HTMLElement>("cron-workflow-nodes");
const cronWorkflowNodeEditor = mustElement<HTMLElement>("cron-workflow-node-editor");
const cronWorkflowZoom = mustElement<HTMLElement>("cron-workflow-zoom");
const cronWorkflowExecutionList = mustElement<HTMLUListElement>("cron-workflow-execution-list");
const cronNewSessionButton = mustElement<HTMLButtonElement>("cron-new-session");
const cronSubmitButton = mustElement<HTMLButtonElement>("cron-submit-btn");

const panelByTab: Record<TabKey, HTMLElement> = {
  chat: panelChat,
  cron: panelCron,
};

const state = {
  apiBase: DEFAULT_API_BASE,
  apiKey: DEFAULT_API_KEY,
  userId: DEFAULT_USER_ID,
  channel: DEFAULT_CHANNEL,
  activeTab: "chat" as TabKey,
  tabLoaded: {
    chat: true,
    models: false,
    channels: false,
    workspace: false,
    cron: false,
  },

  chats: [] as ChatSpec[],
  chatSearchQuery: "",
  activeChatId: null as string | null,
  activeSessionId: newSessionID(),
  activePromptMode: "default" as PromptMode,
  messages: [] as ViewMessage[],
  messageOutputOrder: 0,
  sending: false,

  providers: [] as ProviderInfo[],
  providerTypes: [] as ProviderTypeInfo[],
  modelDefaults: {} as Record<string, string>,
  activeLLM: { provider_id: "", model: "" } as ModelSlotConfig,
  selectedProviderID: "",
  modelsSettingsLevel: "list" as ModelsSettingsLevel,
  channelsSettingsLevel: "list" as ChannelsSettingsLevel,
  workspaceSettingsLevel: "list" as WorkspaceSettingsLevel,
  workspaceCardEnabled: { ...DEFAULT_WORKSPACE_CARD_ENABLED },
  providerAPIKeyVisible: true,
  providerModal: {
    open: false,
    mode: "create" as "create" | "edit",
    editingProviderID: "",
  },
  workspaceFiles: [] as WorkspaceFileInfo[],
  workspaceCodexExpandedFolders: new Set<string>(),
  qqChannelConfig: defaultQQChannelConfig(),
  qqChannelAvailable: true,
  activeWorkspacePath: "",
  activeWorkspaceContent: "",
  activeWorkspaceMode: "json" as WorkspaceEditorMode,
  cronJobs: [] as CronJobSpec[],
  cronStates: {} as Record<string, CronJobState>,
  cronModal: {
    mode: "create" as CronModalMode,
    editingJobID: "",
  },
  cronDraftTaskType: "workflow" as "text" | "workflow",
};

let cronWorkflowEditor: CronWorkflowCanvas | null = null;
let cronWorkflowPseudoFullscreen = false;

const bootstrapTask = bootstrap();

async function bootstrap(): Promise<void> {
  initLocale();
  restoreSettings();
  await loadRuntimeConfig();
  bindEvents();
  initAutoHideScrollbars();
  initCronWorkflowEditor();
  initCustomSelects();
  setSettingsPopoverOpen(false);
  setSearchModalOpen(false);
  setActiveSettingsSection(activeSettingsSection);
  setWorkspaceEditorModalOpen(false);
  setWorkspaceImportModalOpen(false);
  setCronCreateModalOpen(false);
  applyLocaleToDocument();
  renderTabPanels();
  renderChatHeader();
  renderChatList();
  renderSearchChatResults();
  renderMessages();
  renderComposerModelSelectors();
  renderComposerTokenEstimate();
  void ensureSystemPromptTokensLoaded();
  renderChannelsPanel();
  renderWorkspacePanel();
  syncCronDispatchHint();
  ensureCronSessionID();
  resetProviderModalForm();
  await syncModelStateOnBoot();
  ensureChatLiveRefreshLoop();

  setStatus(t("status.loadingChats"), "info");
  await reloadChats();
  if (state.chats.length > 0) {
    await openChat(state.chats[0].id);
    setStatus(t("status.loadedRecentChat"), "info");
    return;
  }
  startDraftSession();
  setStatus(t("status.noChatsDraft"), "info");
}

function initAutoHideScrollbars(): void {
  document.addEventListener(
    "scroll",
    (event) => {
      const target = resolveScrollEventTarget(event.target);
      if (!target) {
        return;
      }
      markScrollbarScrolling(target);
    },
    { capture: true, passive: true },
  );

  window.addEventListener(
    "scroll",
    () => {
      const root = (document.scrollingElement ?? document.documentElement) as HTMLElement | null;
      if (!root) {
        return;
      }
      markScrollbarScrolling(root);
    },
    { passive: true },
  );
}

function resolveScrollEventTarget(target: EventTarget | null): HTMLElement | null {
  if (target instanceof HTMLElement) {
    return target;
  }
  if (target instanceof Document) {
    return (target.scrollingElement ?? target.documentElement) as HTMLElement | null;
  }
  return null;
}

function markScrollbarScrolling(element: HTMLElement): void {
  element.classList.add(SCROLLBAR_ACTIVE_CLASS);
  const previousTimer = scrollbarActivityTimers.get(element);
  if (typeof previousTimer === "number") {
    window.clearTimeout(previousTimer);
  }
  const timer = window.setTimeout(() => {
    element.classList.remove(SCROLLBAR_ACTIVE_CLASS);
    scrollbarActivityTimers.delete(element);
  }, SCROLLBAR_IDLE_HIDE_DELAY_MS);
  scrollbarActivityTimers.set(element, timer);
}

function parseFeatureFlagValue(raw: string | null): boolean | null {
  if (raw === null) {
    return null;
  }
  const normalized = raw.trim().toLowerCase();
  if (normalized === "") {
    return null;
  }
  if (normalized === "1" || normalized === "true" || normalized === "yes" || normalized === "on") {
    return true;
  }
  if (normalized === "0" || normalized === "false" || normalized === "no" || normalized === "off") {
    return false;
  }
  return null;
}

function resolveClientFeatureFlag(key: string, runtimeValue: boolean): boolean {
  try {
    const queryValue = parseFeatureFlagValue(new URLSearchParams(window.location.search).get(key));
    if (queryValue !== null) {
      return queryValue;
    }
  } catch {
    // ignore query parsing error
  }

  try {
    const persisted = parseFeatureFlagValue(window.localStorage.getItem(key));
    if (persisted !== null) {
      return persisted;
    }
  } catch {
    // ignore localStorage read error
  }
  return runtimeValue;
}

function parseRuntimeFeatureFlag(value: unknown): boolean {
  return typeof value === "boolean" ? value : false;
}

function applyRuntimeFeatureOverrides(features: RuntimeConfigFeatureFlags): void {
  const runtimePromptTemplates = parseRuntimeFeatureFlag(features.prompt_templates);
  const runtimePromptContextIntrospect = parseRuntimeFeatureFlag(features.prompt_context_introspect);
  runtimeFlags.prompt_templates = resolveClientFeatureFlag(FEATURE_FLAG_PROMPT_TEMPLATES, runtimePromptTemplates);
  runtimeFlags.prompt_context_introspect = resolveClientFeatureFlag(
    FEATURE_FLAG_PROMPT_CONTEXT_INTROSPECT,
    runtimePromptContextIntrospect,
  );
  syncFeatureFlagControls();
}

async function loadRuntimeConfig(): Promise<void> {
  try {
    const payload = await requestJSON<RuntimeConfigResponse>("/runtime-config");
    applyRuntimeFeatureOverrides(payload.features ?? {});
  } catch {
    applyRuntimeFeatureOverrides({});
  }
}

function syncFeatureFlagControls(): void {
  promptContextIntrospectInput.checked = runtimeFlags.prompt_context_introspect;
}

function applyPromptContextIntrospectOverride(enabled: boolean, notify = false): void {
  runtimeFlags.prompt_context_introspect = enabled;
  try {
    window.localStorage.setItem(FEATURE_FLAG_PROMPT_CONTEXT_INTROSPECT, String(enabled));
  } catch {
    // ignore localStorage write error
  }
  syncFeatureFlagControls();
  invalidateSystemPromptTokensCacheAndReload();
  renderComposerTokenEstimate();
  if (notify) {
    setStatus(t(enabled ? "status.promptContextIntrospectEnabled" : "status.promptContextIntrospectDisabled"), "info");
  }
}

function renderComposerTokenEstimate(): void {
  if (!systemPromptTokensLoaded && systemPromptTokensInFlight === null) {
    void ensureSystemPromptTokensLoaded();
  }
  composerTokenEstimate.textContent = t("chat.tokensEstimate", {
    used: formatTokensK(estimateCurrentAIContextTokens()),
    total: formatTokensK(resolveActiveModelContextLimitTokens()),
  });
}

function estimateTokenCount(text: string): number {
  const normalized = text.trim();
  if (normalized === "") {
    return 0;
  }

  const cjkRegex = /[\u3400-\u4DBF\u4E00-\u9FFF\uF900-\uFAFF\u3040-\u30FF\uAC00-\uD7AF]/g;
  const cjkCount = normalized.match(cjkRegex)?.length ?? 0;
  const remaining = normalized.replace(cjkRegex, " ");
  const chunks = remaining.match(/[^\s]+/g) ?? [];

  let estimate = cjkCount;
  for (const chunk of chunks) {
    const compact = chunk.replace(/\s+/g, "");
    if (compact === "") {
      continue;
    }
    estimate += Math.max(1, Math.ceil(compact.length / 4));
  }
  return estimate;
}

function estimateConversationContextTokens(): number {
  let total = 0;
  for (const message of state.messages) {
    total += estimateTokenCount(message.text ?? "");
  }
  return total;
}

function estimateCurrentAIContextTokens(): number {
  const conversationTokens = estimateConversationContextTokens();
  const draftTokens = estimateTokenCount(messageInput.value);
  return systemPromptTokens + conversationTokens + draftTokens;
}

function resolveActiveModelContextLimitTokens(): number {
  const providerID = state.activeLLM.provider_id.trim();
  const modelID = state.activeLLM.model.trim();
  if (providerID === "" || modelID === "") {
    return DEFAULT_MODEL_CONTEXT_LIMIT_TOKENS;
  }
  const provider = state.providers.find((item) => item.id === providerID);
  if (!provider) {
    return DEFAULT_MODEL_CONTEXT_LIMIT_TOKENS;
  }
  const model = provider.models.find((item) => item.id === modelID);
  const contextLimit = model?.limit?.context;
  if (typeof contextLimit !== "number" || !Number.isFinite(contextLimit) || contextLimit <= 0) {
    return DEFAULT_MODEL_CONTEXT_LIMIT_TOKENS;
  }
  return Math.floor(contextLimit);
}

function formatTokensK(tokens: number): string {
  const normalized = Number.isFinite(tokens) ? Math.max(0, tokens) : 0;
  return `${(normalized / 1000).toFixed(1)}k`;
}

function normalizeWorkspacePathKey(path: string): string {
  return normalizeWorkspaceInputPath(path).toLowerCase();
}

function isSystemPromptWorkspacePath(path: string): boolean {
  return SYSTEM_PROMPT_WORKSPACE_PATH_SET.has(normalizeWorkspacePathKey(path));
}

function extractWorkspaceFileText(payload: unknown): string {
  if (typeof payload === "string") {
    return payload;
  }
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return "";
  }
  const record = payload as Record<string, unknown>;
  return typeof record.content === "string" ? record.content : "";
}

async function loadSystemPromptTokens(): Promise<number> {
  if (runtimeFlags.prompt_context_introspect) {
    try {
      const payload = await requestJSON<AgentSystemLayersResponse>(SYSTEM_PROMPT_LAYER_ENDPOINT);
      const total = payload?.estimated_tokens_total;
      if (typeof total === "number" && Number.isFinite(total) && total >= 0) {
        return Math.floor(total);
      }
    } catch {
      // Fallback to legacy estimation if introspection endpoint is unavailable.
    }
  }

  const tokenLoaders = SYSTEM_PROMPT_WORKSPACE_FALLBACK_PATHS.map(async (path) => {
    try {
      const payload = await getWorkspaceFile(path);
      return estimateTokenCount(extractWorkspaceFileText(payload));
    } catch {
      return 0;
    }
  });
  const counts = await Promise.all(tokenLoaders);
  return counts.reduce((sum, count) => sum + count, 0);
}

function ensureSystemPromptTokensLoaded(): Promise<void> {
  if (systemPromptTokensLoaded) {
    return Promise.resolve();
  }
  if (systemPromptTokensInFlight) {
    return systemPromptTokensInFlight;
  }
  systemPromptTokensInFlight = (async () => {
    systemPromptTokens = await loadSystemPromptTokens();
    systemPromptTokensLoaded = true;
    renderComposerTokenEstimate();
  })().finally(() => {
    systemPromptTokensInFlight = null;
  });
  return systemPromptTokensInFlight;
}

function invalidateSystemPromptTokensCacheAndReload(): void {
  systemPromptTokensLoaded = false;
  systemPromptTokens = 0;
  void ensureSystemPromptTokensLoaded();
}

function ensureChatLiveRefreshLoop(): void {
  if (chatLiveRefreshTimer !== null) {
    return;
  }
  chatLiveRefreshTimer = window.setInterval(() => {
    void refreshActiveChatLive();
  }, CHAT_LIVE_REFRESH_INTERVAL_MS);
}

async function refreshActiveChatLive(): Promise<void> {
  if (chatLiveRefreshInFlight || state.activeTab !== "chat" || state.activeChatId === null || state.sending) {
    return;
  }

  const activeChatID = state.activeChatId;
  const prevUpdatedAt = state.chats.find((chat) => chat.id === activeChatID)?.updated_at ?? "";
  chatLiveRefreshInFlight = true;
  try {
    await reloadChats();
    if (state.activeChatId !== activeChatID) {
      return;
    }
    const latest = state.chats.find((chat) => chat.id === activeChatID);
    if (!latest || latest.updated_at === prevUpdatedAt) {
      return;
    }
    const history = await requestJSON<ChatHistoryResponse>(`/chats/${encodeURIComponent(activeChatID)}`);
    if (state.activeChatId !== activeChatID) {
      return;
    }
    state.messages = history.messages.map(toViewMessage);
    renderMessages({ animate: false });
    renderComposerTokenEstimate();
    renderChatHeader();
    renderChatList();
    renderSearchChatResults();
  } catch {
    // Keep polling silent to avoid interrupting foreground interactions.
  } finally {
    chatLiveRefreshInFlight = false;
  }
}

function bindEvents(): void {
  tabButtons.forEach((button) => {
    button.addEventListener("click", () => {
      const tab = button.dataset.tab;
      if (isTabKey(tab)) {
        void switchTab(tab);
      }
    });
  });

  chatCronToggleButton.addEventListener("click", (event) => {
    event.stopPropagation();
    void switchTab("cron");
  });
  cronChatToggleButton.addEventListener("click", (event) => {
    event.stopPropagation();
    void switchTab("chat");
  });

  chatSearchToggleButton.addEventListener("click", (event) => {
    event.stopPropagation();
    const nextOpen = !isSearchModalOpen();
    setSearchModalOpen(nextOpen);
    if (nextOpen) {
      renderSearchChatResults();
      searchChatInput.focus();
      searchChatInput.select();
    }
  });
  searchModal.addEventListener("click", (event) => {
    const target = event.target;
    if (target instanceof Element && target.closest("[data-search-close=\"true\"]")) {
      setSearchModalOpen(false);
      return;
    }
    event.stopPropagation();
  });
  searchModalCloseButton.addEventListener("click", () => {
    setSearchModalOpen(false);
  });

  settingsToggleButton.addEventListener("click", (event) => {
    event.stopPropagation();
    setSettingsPopoverOpen(!isSettingsPopoverOpen());
  });
  settingsSectionButtons.forEach((button) => {
    button.addEventListener("click", () => {
      const section = button.dataset.settingsSection;
      if (!isSettingsSectionKey(section)) {
        return;
      }
      setActiveSettingsSection(section);
    });
  });
  settingsPopover.addEventListener("click", (event) => {
    const target = event.target;
    if (target instanceof Element && target.closest("[data-settings-close=\"true\"]")) {
      setSettingsPopoverOpen(false);
      return;
    }
    event.stopPropagation();
  });
  settingsPopoverCloseButton.addEventListener("click", () => {
    setSettingsPopoverOpen(false);
  });
  document.addEventListener("click", (event) => {
    if (!isSettingsPopoverOpen()) {
      return;
    }
    const target = event.target;
    if (!(target instanceof Node)) {
      return;
    }
    if (
      settingsPopover.contains(target)
      || settingsToggleButton.contains(target)
      || workspaceEditorModal.contains(target)
      || workspaceImportModal.contains(target)
    ) {
      return;
    }
    setSettingsPopoverOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isSettingsPopoverOpen()) {
      return;
    }
    if (isWorkspaceEditorModalOpen() || isWorkspaceImportModalOpen()) {
      return;
    }
    setSettingsPopoverOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isSearchModalOpen()) {
      return;
    }
    setSearchModalOpen(false);
  });

  reloadChatsButton.addEventListener("click", async () => {
    syncControlState();
    setStatus(t("status.refreshingChats"), "info");
    await reloadChats();
    setStatus(t("status.chatsRefreshed"), "info");
  });

  newChatButton.addEventListener("click", () => {
    syncControlState();
    startDraftSession();
    setStatus(t("status.draftReady"), "info");
  });

  chatPromptModeToggle.addEventListener("change", () => {
    setActivePromptMode(chatPromptModeToggle.checked ? "codex" : "default", { announce: true });
  });

  searchChatInput.addEventListener("input", () => {
    state.chatSearchQuery = searchChatInput.value.trim();
    renderSearchChatResults();
  });

  apiBaseInput.addEventListener("change", async () => {
    await handleControlChange(false);
  });
  apiKeyInput.addEventListener("change", async () => {
    await handleControlChange(false);
  });

  localeSelect.addEventListener("change", () => {
    const locale = setLocale(localeSelect.value);
    localeSelect.value = locale;
    localStorage.setItem(LOCALE_KEY, locale);
    applyLocaleToDocument();
    setStatus(
      t("status.localeChanged", {
        localeName: locale === "zh-CN" ? t("locale.zhCN") : t("locale.enUS"),
      }),
      "info",
    );
  });
  promptContextIntrospectInput.addEventListener("change", () => {
    applyPromptContextIntrospectOverride(promptContextIntrospectInput.checked, true);
  });

  composerForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await sendMessage();
  });
  messageInput.addEventListener("keydown", (event) => {
    if (
      event.key !== "Enter" ||
      event.shiftKey ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey ||
      event.isComposing
    ) {
      return;
    }
    event.preventDefault();
    void sendMessage();
  });
  messageInput.addEventListener("input", () => {
    renderComposerTokenEstimate();
  });
  composerAttachButton.addEventListener("click", () => {
    composerAttachInput.click();
  });
  composerAttachInput.addEventListener("change", () => {
    appendComposerAttachmentMentions(composerAttachInput.files);
    composerAttachInput.value = "";
  });
  composerProviderSelect.addEventListener("change", () => {
    void handleComposerProviderSelectChange();
  });
  composerModelSelect.addEventListener("change", () => {
    void handleComposerModelSelectChange();
  });

  refreshModelsButton.addEventListener("click", async () => {
    await refreshModels();
  });
  modelsAddProviderButton.addEventListener("click", () => {
    openProviderModal("create");
  });
  modelsProviderForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await upsertProvider();
  });
  modelsProviderHeadersAddButton.addEventListener("click", () => {
    appendProviderKVRow(modelsProviderHeadersRows, "headers");
    scheduleProviderAutoSave();
  });
  modelsProviderAliasesAddButton.addEventListener("click", () => {
    appendProviderKVRow(modelsProviderAliasesRows, "aliases");
    scheduleProviderAutoSave();
  });
  modelsProviderTypeSelect.addEventListener("change", () => {
    syncProviderCustomModelsField(modelsProviderTypeSelect.value);
    scheduleProviderAutoSave();
  });
  modelsProviderBaseURLInput.addEventListener("input", () => {
    renderProviderBaseURLPreview();
  });
  modelsProviderCustomModelsAddButton.addEventListener("click", () => {
    appendCustomModelRow(modelsProviderCustomModelsRows);
    scheduleProviderAutoSave();
  });
  modelsProviderForm.addEventListener("input", () => {
    scheduleProviderAutoSave();
  });
  modelsProviderForm.addEventListener("change", () => {
    scheduleProviderAutoSave();
  });
  modelsProviderForm.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const formActionButton = target.closest<HTMLButtonElement>("button[data-provider-form-action]");
    if (formActionButton) {
      const action = formActionButton.dataset.providerFormAction ?? "";
      if (action === "toggle-api-key-visibility") {
        setProviderAPIKeyVisibility(!state.providerAPIKeyVisible);
      }
      if (action === "focus-base-url") {
        modelsProviderBaseURLInput.focus();
      }
      return;
    }
    const kvRemoveButton = target.closest<HTMLButtonElement>("button[data-kv-remove]");
    if (kvRemoveButton) {
      const kvRow = kvRemoveButton.closest(".kv-row");
      if (kvRow) {
        const container = kvRow.parentElement;
        kvRow.remove();
        if (container && container.children.length === 0) {
          const kind = container.getAttribute("data-kv-kind");
          if (kind === "headers" || kind === "aliases") {
            appendProviderKVRow(container, kind);
          }
        }
        scheduleProviderAutoSave();
      }
    }

    const customRemoveButton = target.closest<HTMLButtonElement>("button[data-custom-model-remove]");
    if (customRemoveButton) {
      const customRow = customRemoveButton.closest(".custom-model-row");
      if (!customRow) {
        return;
      }
      const customContainer = customRow.parentElement;
      customRow.remove();
      if (customContainer && customContainer.children.length === 0) {
        appendCustomModelRow(customContainer);
      }
      scheduleProviderAutoSave();
    }
  });
  modelsProviderCancelButton.addEventListener("click", () => {
    closeProviderModal();
  });
  modelsProviderList.addEventListener("click", async (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest<HTMLButtonElement>("button[data-provider-action]");
    if (!button) {
      return;
    }
    const providerID = button.dataset.providerId ?? "";
    if (providerID === "") {
      return;
    }
    const action = button.dataset.providerAction;
    if (action === "select") {
      state.selectedProviderID = providerID;
      openProviderModal("edit", providerID);
      return;
    }
    if (action === "edit") {
      openProviderModal("edit", providerID);
      return;
    }
    if (action === "delete") {
      await deleteProvider(providerID);
      return;
    }
  });

  refreshWorkspaceButton.addEventListener("click", async () => {
    await refreshWorkspace();
  });
  workspaceSettingsSection.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const toggleButton = target.closest<HTMLButtonElement>("button[data-workspace-toggle-card]");
    if (toggleButton) {
      const card = toggleButton.dataset.workspaceToggleCard;
      if (!isWorkspaceCardKey(card)) {
        return;
      }
      setWorkspaceCardEnabled(card, !isWorkspaceCardEnabled(card));
      return;
    }
    const button = target.closest<HTMLButtonElement>("button[data-workspace-action]");
    if (!button) {
      return;
    }
    const action = button.dataset.workspaceAction;
    if (action === "open-config") {
      if (!ensureWorkspaceCardEnabled("config")) {
        return;
      }
      setWorkspaceSettingsLevel("config");
      renderWorkspacePanel();
      return;
    }
    if (action === "open-prompt") {
      if (!ensureWorkspaceCardEnabled("prompt")) {
        return;
      }
      setWorkspaceSettingsLevel("prompt");
      renderWorkspacePanel();
      return;
    }
    if (action === "open-codex") {
      if (!ensureWorkspaceCardEnabled("codex")) {
        return;
      }
      setWorkspaceSettingsLevel("codex");
      renderWorkspacePanel();
      return;
    }
    if (action === "back") {
      setWorkspaceSettingsLevel("list");
      renderWorkspacePanel();
    }
  });
  channelsEntryList.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest<HTMLButtonElement>("button[data-channel-action]");
    if (!button) {
      return;
    }
    const action = button.dataset.channelAction;
    if (action !== "open") {
      return;
    }
    setChannelsSettingsLevel("edit");
    renderChannelsPanel();
    qqChannelEnabledInput.focus();
  });
  qqChannelForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await saveQQChannelConfig();
  });
  workspaceImportOpenButton.addEventListener("click", () => {
    setWorkspaceImportModalOpen(true);
  });
  workspaceCreateFileForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await createWorkspaceFile();
  });
  workspaceEditorModal.addEventListener("click", (event) => {
    const target = event.target;
    if (target instanceof Element && target.closest("[data-workspace-editor-close=\"true\"]")) {
      setWorkspaceEditorModalOpen(false);
      return;
    }
    event.stopPropagation();
  });
  workspaceEditorModalCloseButton.addEventListener("click", () => {
    setWorkspaceEditorModalOpen(false);
  });
  workspaceImportModal.addEventListener("click", (event) => {
    const target = event.target;
    if (target instanceof Element && target.closest("[data-workspace-import-close=\"true\"]")) {
      setWorkspaceImportModalOpen(false);
      return;
    }
    event.stopPropagation();
  });
  workspaceImportModalCloseButton.addEventListener("click", () => {
    setWorkspaceImportModalOpen(false);
  });
  const handleWorkspaceFilesClick = async (event: Event): Promise<void> => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const openButton = target.closest<HTMLButtonElement>("button[data-workspace-open]");
    if (openButton) {
      const path = openButton.dataset.workspaceOpen ?? "";
      if (path !== "") {
        await openWorkspaceFile(path);
      }
      return;
    }
    const deleteButton = target.closest<HTMLButtonElement>("button[data-workspace-delete]");
    if (!deleteButton) {
      return;
    }
    const path = deleteButton.dataset.workspaceDelete ?? "";
    if (path === "") {
      return;
    }
    await deleteWorkspaceFile(path);
  };
  workspaceFilesBody.addEventListener("click", (event) => {
    void handleWorkspaceFilesClick(event);
  });
  workspacePromptsBody.addEventListener("click", (event) => {
    void handleWorkspaceFilesClick(event);
  });
  workspaceCodexTreeBody.addEventListener("click", (event) => {
    const target = event.target;
    if (target instanceof Element) {
      const folderToggle = target.closest<HTMLButtonElement>("button[data-workspace-folder-toggle]");
      if (folderToggle) {
        const folderPath = folderToggle.dataset.workspaceFolderToggle ?? "";
        if (folderPath !== "") {
          toggleWorkspaceCodexFolder(folderPath);
        }
        return;
      }
    }
    void handleWorkspaceFilesClick(event);
  });
  workspaceEditorForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await saveWorkspaceFile();
  });
  workspaceDeleteFileButton.addEventListener("click", async () => {
    const path = workspaceFilePathInput.value.trim();
    if (path === "") {
      setStatus(t("error.workspacePathRequired"), "error");
      return;
    }
    await deleteWorkspaceFile(path);
  });
  workspaceImportForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await importWorkspaceJSON();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isWorkspaceEditorModalOpen()) {
      return;
    }
    setWorkspaceEditorModalOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isWorkspaceImportModalOpen()) {
      return;
    }
    setWorkspaceImportModalOpen(false);
  });
  cronCreateOpenButton.addEventListener("click", () => {
    setCronModalMode("create");
    syncCronDispatchHint();
    cronIDInput.value = "";
    cronNameInput.value = "";
    cronIntervalInput.value = "60s";
    ensureCronSessionID();
    cronMaxConcurrencyInput.value = "1";
    cronTimeoutInput.value = "30";
    cronMisfireInput.value = "0";
    cronTextInput.value = "";
    state.cronDraftTaskType = "workflow";
    cronTaskTypeSelect.value = "workflow";
    syncCronTaskModeUI();
    cronWorkflowEditor?.setWorkflow(createDefaultCronWorkflow());
    renderCronExecutionDetails(undefined);
    setCronCreateModalOpen(true);
  });
  cronCreateModalCloseButton.addEventListener("click", () => {
    setCronCreateModalOpen(false);
  });
  cronTaskTypeSelect.addEventListener("change", () => {
    const value = cronTaskTypeSelect.value === "text" ? "text" : "workflow";
    state.cronDraftTaskType = value;
    syncCronTaskModeUI();
  });
  cronResetWorkflowButton.addEventListener("click", () => {
    cronWorkflowEditor?.resetToDefault();
  });
  cronWorkflowFullscreenButton.addEventListener("click", () => {
    void toggleCronWorkflowFullscreen();
  });
  document.addEventListener("fullscreenchange", () => {
    if (!isCronWorkflowNativeFullscreen() && cronWorkflowPseudoFullscreen) {
      setCronWorkflowPseudoFullscreen(false);
      return;
    }
    syncCronWorkflowFullscreenUI();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isCronCreateModalOpen()) {
      return;
    }
    if (isCronWorkflowFullscreenActive()) {
      event.preventDefault();
      void exitCronWorkflowFullscreen();
      return;
    }
    setCronCreateModalOpen(false);
  });

  refreshCronButton.addEventListener("click", async () => {
    await refreshCronJobs();
  });
  cronNewSessionButton.addEventListener("click", () => {
    cronSessionIDInput.value = newSessionID();
  });
  cronCreateForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await saveCronJob();
  });
  cronJobsBody.addEventListener("change", async (event) => {
    const target = event.target;
    if (!(target instanceof HTMLInputElement)) {
      return;
    }
    const jobID = target.dataset.cronToggleEnabled ?? "";
    if (jobID === "") {
      return;
    }
    const enabled = target.checked;
    target.disabled = true;
    const saved = await updateCronJobEnabled(jobID, enabled);
    if (!saved) {
      target.checked = !enabled;
    }
    target.disabled = false;
  });
  cronJobsBody.addEventListener("click", async (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest<HTMLButtonElement>("button[data-cron-run], button[data-cron-edit], button[data-cron-delete]");
    if (!button) {
      return;
    }
    const runJobID = button.dataset.cronRun ?? "";
    if (runJobID !== "") {
      await runCronJob(runJobID);
      return;
    }

    const editJobID = button.dataset.cronEdit ?? "";
    if (editJobID !== "") {
      openCronEditModal(editJobID);
      return;
    }

    const deleteJobID = button.dataset.cronDelete ?? "";
    if (deleteJobID === "") {
      return;
    }
    await deleteCronJob(deleteJobID);
  });
}

function initCustomSelects(): void {
  document.body.classList.add("select-enhanced");
  const selects = Array.from(document.querySelectorAll<HTMLSelectElement>("select"));
  for (const select of selects) {
    if (customSelectInstances.has(select)) {
      continue;
    }
    enhanceSelectControl(select);
  }
  bindCustomSelectGlobalEvents();
  syncAllCustomSelects();
}

function enhanceSelectControl(select: HTMLSelectElement): void {
  const parent = select.parentElement;
  if (!parent) {
    return;
  }
  const isSearchEnabled = select.dataset.selectSearch !== "off";

  const container = document.createElement("div");
  container.className = "custom-select-container";
  container.classList.toggle("without-search", !isSearchEnabled);
  parent.insertBefore(container, select);
  container.appendChild(select);
  select.dataset.customSelectNative = "true";
  select.tabIndex = -1;

  const trigger = document.createElement("div");
  trigger.className = "select-trigger";
  trigger.tabIndex = 0;
  trigger.setAttribute("role", "button");
  trigger.setAttribute("aria-haspopup", "listbox");
  trigger.setAttribute("aria-expanded", "false");

  const selectedText = document.createElement("span");
  selectedText.className = "selected-text";

  const arrow = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  arrow.setAttribute("class", "arrow");
  arrow.setAttribute("viewBox", "0 0 20 20");
  arrow.setAttribute("fill", "currentColor");
  arrow.setAttribute("aria-hidden", "true");

  const arrowPath = document.createElementNS("http://www.w3.org/2000/svg", "path");
  arrowPath.setAttribute("fill-rule", "evenodd");
  arrowPath.setAttribute("d", "M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z");
  arrowPath.setAttribute("clip-rule", "evenodd");
  arrow.appendChild(arrowPath);

  trigger.append(selectedText, arrow);

  const optionsList = document.createElement("div");
  optionsList.className = "options-list";

  const optionsBody = document.createElement("div");
  optionsBody.className = "options-body";
  optionsBody.setAttribute("role", "listbox");

  let searchInput: HTMLInputElement | null = null;
  if (isSearchEnabled) {
    const searchField = document.createElement("div");
    searchField.className = "options-search";
    searchInput = document.createElement("input");
    searchInput.className = "options-search-input";
    searchInput.type = "search";
    searchInput.autocomplete = "off";
    searchInput.spellcheck = false;
    searchInput.placeholder = t("tab.search");
    searchInput.setAttribute("aria-label", t("search.inputLabel"));
    searchField.appendChild(searchInput);
    optionsList.append(searchField);
  }
  optionsList.append(optionsBody);

  container.append(trigger, optionsList);
  customSelectInstances.set(select, {
    container,
    trigger,
    selectedText,
    optionsList,
    optionsBody,
    searchInput,
    isSearchEnabled,
  });

  trigger.addEventListener("click", (event) => {
    event.stopPropagation();
    if (select.disabled || select.options.length === 0) {
      return;
    }
    toggleCustomSelect(select);
  });

  trigger.addEventListener("keydown", (event) => {
    handleCustomSelectTriggerKeydown(event, select);
  });

  optionsList.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const optionElement = target.closest<HTMLElement>(".option");
    if (!optionElement || optionElement.classList.contains("disabled") || optionElement.classList.contains("is-hidden")) {
      return;
    }
    const value = optionElement.dataset.value ?? "";
    selectCustomOption(select, value);
    closeCustomSelect(select);
    trigger.focus();
    event.stopPropagation();
  });

  optionsList.addEventListener(
    "wheel",
    (event) => {
      if (!(event instanceof WheelEvent)) {
        return;
      }
      if (optionsBody.scrollHeight <= optionsBody.clientHeight) {
        return;
      }
      optionsBody.scrollTop += event.deltaY;
      event.preventDefault();
    },
    { passive: false },
  );

  if (searchInput) {
    searchInput.addEventListener("input", () => {
      filterCustomSelectOptions(select, searchInput.value);
    });

    searchInput.addEventListener("keydown", (event) => {
      handleCustomSelectSearchKeydown(event, select);
    });
  }

  select.addEventListener("change", () => {
    syncCustomSelect(select);
  });
}

function bindCustomSelectGlobalEvents(): void {
  if (customSelectGlobalEventsBound) {
    return;
  }
  customSelectGlobalEventsBound = true;

  document.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Node)) {
      closeAllCustomSelects();
      return;
    }
    for (const instance of customSelectInstances.values()) {
      if (instance.container.contains(target)) {
        return;
      }
    }
    closeAllCustomSelects();
  });

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") {
      return;
    }
    closeAllCustomSelects();
  });
}

function handleCustomSelectTriggerKeydown(event: KeyboardEvent, select: HTMLSelectElement): void {
  if (select.disabled || select.options.length === 0) {
    return;
  }
  if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    toggleCustomSelect(select);
    return;
  }
  if (event.key === "Escape") {
    closeCustomSelect(select);
    return;
  }
  if (event.key !== "ArrowDown" && event.key !== "ArrowUp") {
    return;
  }
  event.preventDefault();
  const options = Array.from(select.options).filter((option) => !option.disabled);
  if (options.length === 0) {
    return;
  }
  const selectedIndex = options.findIndex((option) => option.value === select.value);
  const offset = event.key === "ArrowDown" ? 1 : -1;
  const nextIndex = selectedIndex === -1 ? 0 : (selectedIndex + offset + options.length) % options.length;
  const nextValue = options[nextIndex].value;
  selectCustomOption(select, nextValue);
  openCustomSelect(select);
}

function handleCustomSelectSearchKeydown(event: KeyboardEvent, select: HTMLSelectElement): void {
  if (event.key === "Escape") {
    event.preventDefault();
    closeCustomSelect(select);
    const instance = customSelectInstances.get(select);
    instance?.trigger.focus();
    return;
  }
  if (event.key !== "ArrowDown" && event.key !== "ArrowUp") {
    return;
  }
  event.preventDefault();
  const offset = event.key === "ArrowDown" ? 1 : -1;
  const options = getCustomSelectNavigableOptionElements(select);
  if (options.length === 0) {
    return;
  }
  const selectedIndex = options.findIndex((item) => item.classList.contains("selected"));
  const nextIndex = selectedIndex === -1
    ? (offset > 0 ? 0 : options.length - 1)
    : (selectedIndex + offset + options.length) % options.length;
  const nextOption = options[nextIndex];
  if (!nextOption) {
    return;
  }
  const nextValue = nextOption.dataset.value ?? "";
  if (nextValue === "") {
    return;
  }
  selectCustomOption(select, nextValue);
  if (typeof nextOption.scrollIntoView === "function") {
    nextOption.scrollIntoView({ block: "nearest" });
  }
}

function syncAllCustomSelects(): void {
  for (const select of customSelectInstances.keys()) {
    syncCustomSelect(select);
  }
}

function syncCustomSelect(select: HTMLSelectElement): void {
  const instance = customSelectInstances.get(select);
  if (!instance) {
    return;
  }

  instance.optionsBody.innerHTML = "";
  for (const option of Array.from(select.options)) {
    const optionElement = document.createElement("div");
    optionElement.className = "option";
    optionElement.dataset.value = option.value;
    const text = (option.textContent ?? "").trim();
    optionElement.textContent = text;
    optionElement.dataset.searchText = text.toLowerCase();
    optionElement.setAttribute("role", "option");
    optionElement.setAttribute("aria-selected", String(option.selected));
    if (option.disabled) {
      optionElement.classList.add("disabled");
      optionElement.setAttribute("aria-disabled", "true");
    }
    if (option.selected) {
      optionElement.classList.add("selected");
    }
    instance.optionsBody.appendChild(optionElement);
  }

  const selectedOption = Array.from(select.selectedOptions)[0] ?? select.options[select.selectedIndex] ?? select.options[0];
  instance.selectedText.textContent = selectedOption?.textContent?.trim() || "";
  if (instance.searchInput) {
    instance.searchInput.placeholder = t("tab.search");
    instance.searchInput.setAttribute("aria-label", t("search.inputLabel"));
  }
  filterCustomSelectOptions(select, instance.searchInput?.value ?? "");
  instance.container.classList.toggle("is-disabled", select.disabled);
  instance.trigger.setAttribute("aria-disabled", String(select.disabled));
  instance.trigger.tabIndex = select.disabled ? -1 : 0;

  if (select.disabled || select.options.length === 0) {
    closeCustomSelect(select);
  }
}

function selectCustomOption(select: HTMLSelectElement, value: string): void {
  const nextValue = value.trim();
  if (select.value === nextValue) {
    syncCustomSelect(select);
    return;
  }
  select.value = nextValue;
  select.dispatchEvent(new Event("change", { bubbles: true }));
}

function toggleCustomSelect(select: HTMLSelectElement): void {
  const instance = customSelectInstances.get(select);
  if (!instance) {
    return;
  }
  const nextOpen = !instance.container.classList.contains("open");
  if (nextOpen) {
    openCustomSelect(select);
    return;
  }
  closeCustomSelect(select);
}

function openCustomSelect(select: HTMLSelectElement): void {
  const instance = customSelectInstances.get(select);
  if (!instance) {
    return;
  }
  closeAllCustomSelects(select);
  if (instance.searchInput) {
    instance.searchInput.value = "";
  }
  filterCustomSelectOptions(select, "");
  applyCustomSelectOpenDirection(select);
  instance.container.classList.add("open");
  instance.trigger.setAttribute("aria-expanded", "true");
  if (instance.searchInput) {
    instance.searchInput.focus();
  }
}

function closeCustomSelect(select: HTMLSelectElement): void {
  const instance = customSelectInstances.get(select);
  if (!instance) {
    return;
  }
  instance.container.classList.remove("open");
  instance.trigger.setAttribute("aria-expanded", "false");
  if (instance.searchInput && instance.searchInput.value !== "") {
    instance.searchInput.value = "";
    filterCustomSelectOptions(select, "");
  }
}

function closeAllCustomSelects(except?: HTMLSelectElement): void {
  for (const [select] of customSelectInstances.entries()) {
    if (select === except) {
      continue;
    }
    closeCustomSelect(select);
  }
}

function applyCustomSelectOpenDirection(select: HTMLSelectElement): void {
  const instance = customSelectInstances.get(select);
  if (!instance) {
    return;
  }
  const optionCount = Math.max(instance.optionsBody.childElementCount, select.options.length, 1);
  const searchSectionHeight = instance.isSearchEnabled
    ? CUSTOM_SELECT_SEARCH_FIELD_HEIGHT_PX + CUSTOM_SELECT_OPTIONS_VERTICAL_GAP_PX
    : 0;
  const maxPanelHeight = searchSectionHeight
    + CUSTOM_SELECT_MAX_OPTIONS_HEIGHT_PX
    + CUSTOM_SELECT_OPTIONS_PANEL_VERTICAL_PADDING_PX;
  const estimatedOptionsHeight = Math.min(optionCount * CUSTOM_SELECT_OPTION_ROW_HEIGHT_PX, CUSTOM_SELECT_MAX_OPTIONS_HEIGHT_PX);
  const estimatedPanelHeight = Math.min(searchSectionHeight + estimatedOptionsHeight + CUSTOM_SELECT_OPTIONS_PANEL_VERTICAL_PADDING_PX, maxPanelHeight);
  const panelHeight = Math.min(
    Math.max(instance.optionsList.scrollHeight, estimatedPanelHeight),
    maxPanelHeight,
  );
  const requiredSpace = panelHeight + CUSTOM_SELECT_OPTIONS_VERTICAL_GAP_PX;
  const triggerRect = instance.trigger.getBoundingClientRect();
  const bounds = resolveCustomSelectVerticalBounds(instance.container);
  const availableBelow = Math.max(0, bounds.bottom - triggerRect.bottom);
  const availableAbove = Math.max(0, triggerRect.top - bounds.top);
  const shouldOpenUpward = availableBelow < requiredSpace && availableAbove > availableBelow;
  instance.container.classList.toggle("open-upward", shouldOpenUpward);
}

function getCustomSelectNavigableOptionElements(select: HTMLSelectElement): HTMLElement[] {
  const instance = customSelectInstances.get(select);
  if (!instance) {
    return [];
  }
  return Array.from(instance.optionsBody.querySelectorAll<HTMLElement>(".option"))
    .filter((option) => !option.classList.contains("disabled") && !option.classList.contains("is-hidden"));
}

function filterCustomSelectOptions(select: HTMLSelectElement, queryText: string): void {
  const instance = customSelectInstances.get(select);
  if (!instance) {
    return;
  }
  const query = queryText.trim().toLowerCase();
  for (const option of Array.from(instance.optionsBody.querySelectorAll<HTMLElement>(".option"))) {
    const label = option.dataset.searchText ?? "";
    const visible = query === "" || label.includes(query);
    option.classList.toggle("is-hidden", !visible);
    option.setAttribute("aria-hidden", String(!visible));
  }
}

function resolveCustomSelectVerticalBounds(container: HTMLElement): { top: number; bottom: number } {
  const viewportBottom = window.innerHeight || document.documentElement.clientHeight || 0;
  let top = 0;
  let bottom = viewportBottom;
  let current: HTMLElement | null = container.parentElement;
  while (current) {
    const computed = window.getComputedStyle(current);
    if (isClippingOverflowValue(computed.overflow) || isClippingOverflowValue(computed.overflowY)) {
      const rect = current.getBoundingClientRect();
      top = Math.max(top, rect.top);
      bottom = Math.min(bottom, rect.bottom);
    }
    current = current.parentElement;
  }
  if (bottom <= top) {
    return {
      top: 0,
      bottom: viewportBottom,
    };
  }
  return { top, bottom };
}

function isClippingOverflowValue(value: string): boolean {
  const normalized = value.trim().toLowerCase();
  return normalized.includes("hidden")
    || normalized.includes("auto")
    || normalized.includes("scroll")
    || normalized.includes("clip");
}

function isSettingsPopoverOpen(): boolean {
  return !settingsPopover.classList.contains("is-hidden");
}

function isSearchModalOpen(): boolean {
  return !searchModal.classList.contains("is-hidden");
}

function setSearchModalOpen(open: boolean): void {
  searchModal.classList.toggle("is-hidden", !open);
  searchModal.setAttribute("aria-hidden", String(!open));
  chatSearchToggleButton.setAttribute("aria-expanded", String(open));
  document.body.classList.toggle("search-modal-open", open);
}

function isSettingsSectionKey(value: string | undefined): value is SettingsSectionKey {
  return value === "connection" || value === "identity" || value === "display" || value === "models" || value === "channels" || value === "workspace";
}

function setActiveSettingsSection(section: SettingsSectionKey): void {
  activeSettingsSection = section;
  settingsSectionButtons.forEach((button) => {
    const current = button.dataset.settingsSection;
    const active = current === section;
    button.classList.toggle("is-active", active);
    button.setAttribute("aria-selected", String(active));
  });
  settingsSectionPanels.forEach((panel) => {
    const current = panel.dataset.settingsSectionPanel;
    const active = current === section;
    panel.classList.toggle("is-active", active);
    panel.hidden = !active;
  });
  if (section === "models") {
    setModelsSettingsLevel("list");
    if (!state.tabLoaded.models) {
      void refreshModels();
      return;
    }
    renderModelsPanel();
    return;
  }
  if (section === "channels") {
    setChannelsSettingsLevel("list");
    if (!state.tabLoaded.channels) {
      void refreshQQChannelConfig();
      return;
    }
    renderChannelsPanel();
    return;
  }
  if (section === "workspace") {
    setWorkspaceSettingsLevel("list");
    if (!state.tabLoaded.workspace) {
      void refreshWorkspace();
      return;
    }
    renderWorkspacePanel();
  }
}

function setSettingsPopoverOpen(open: boolean): void {
  settingsPopover.classList.toggle("is-hidden", !open);
  settingsPopover.setAttribute("aria-hidden", String(!open));
  settingsToggleButton.setAttribute("aria-expanded", String(open));
  document.body.classList.toggle("settings-open", open);
  if (open) {
    setActiveSettingsSection(activeSettingsSection);
  }
}

function isWorkspaceEditorModalOpen(): boolean {
  return !workspaceEditorModal.classList.contains("is-hidden");
}

function setWorkspaceEditorModalOpen(open: boolean): void {
  workspaceEditorModal.classList.toggle("is-hidden", !open);
  workspaceEditorModal.setAttribute("aria-hidden", String(!open));
  document.body.classList.toggle("workspace-editor-open", open);
}

function isWorkspaceImportModalOpen(): boolean {
  return !workspaceImportModal.classList.contains("is-hidden");
}

function setWorkspaceImportModalOpen(open: boolean): void {
  workspaceImportModal.classList.toggle("is-hidden", !open);
  workspaceImportModal.setAttribute("aria-hidden", String(!open));
  workspaceImportOpenButton.setAttribute("aria-expanded", String(open));
  document.body.classList.toggle("workspace-import-open", open);
}

function isCronCreateModalOpen(): boolean {
  return !cronCreateModal.classList.contains("is-hidden");
}

function setCronCreateModalOpen(open: boolean): void {
  if (!open && isCronWorkflowFullscreenActive()) {
    void exitCronWorkflowFullscreen();
  }
  cronCreateModal.classList.toggle("is-hidden", !open);
  cronCreateModal.setAttribute("aria-hidden", String(!open));
  cronCreateOpenButton.setAttribute("aria-expanded", String(open));
  cronCreateOpenButton.hidden = open;
  cronWorkbench.dataset.cronView = open ? "editor" : "jobs";
}

function supportsNativeCronWorkflowFullscreen(): boolean {
  const section = cronWorkflowSection as HTMLElement & {
    requestFullscreen?: () => Promise<void>;
  };
  return typeof section.requestFullscreen === "function" && typeof document.exitFullscreen === "function" && document.fullscreenEnabled === true;
}

function isCronWorkflowNativeFullscreen(): boolean {
  return document.fullscreenElement === cronWorkflowSection;
}

function isCronWorkflowFullscreenActive(): boolean {
  return isCronWorkflowNativeFullscreen() || cronWorkflowPseudoFullscreen;
}

function syncCronWorkflowFullscreenUI(): void {
  const active = isCronWorkflowFullscreenActive();
  const label = t(active ? "cron.exitFullscreen" : "cron.enterFullscreen");
  cronWorkflowFullscreenButton.textContent = label;
  cronWorkflowFullscreenButton.setAttribute("aria-label", label);
  cronWorkflowFullscreenButton.title = label;
  cronWorkflowFullscreenButton.setAttribute("aria-pressed", String(active));
}

function setCronWorkflowPseudoFullscreen(active: boolean): void {
  cronWorkflowPseudoFullscreen = active;
  cronWorkflowSection.classList.toggle("is-pseudo-fullscreen", active);
  document.body.classList.toggle("cron-workflow-pseudo-fullscreen", active);
  syncCronWorkflowFullscreenUI();
}

async function enterCronWorkflowFullscreen(): Promise<void> {
  if (supportsNativeCronWorkflowFullscreen()) {
    const section = cronWorkflowSection as HTMLElement & {
      requestFullscreen: () => Promise<void>;
    };
    await section.requestFullscreen();
    return;
  }
  setCronWorkflowPseudoFullscreen(true);
}

async function exitCronWorkflowFullscreen(): Promise<void> {
  if (isCronWorkflowNativeFullscreen() && typeof document.exitFullscreen === "function") {
    await document.exitFullscreen();
    return;
  }
  if (cronWorkflowPseudoFullscreen) {
    setCronWorkflowPseudoFullscreen(false);
  }
}

async function toggleCronWorkflowFullscreen(): Promise<void> {
  if (isCronWorkflowFullscreenActive()) {
    await exitCronWorkflowFullscreen();
  } else {
    await enterCronWorkflowFullscreen();
  }
  syncCronWorkflowFullscreenUI();
}

function setCronModalMode(mode: CronModalMode, editingJobID = ""): void {
  state.cronModal.mode = mode;
  state.cronModal.editingJobID = editingJobID;

  const createMode = mode === "create";
  cronIDInput.readOnly = !createMode;
  refreshCronModalTitles();
  syncCronTaskModeUI();
}

function initLocale(): void {
  const savedLocale = localStorage.getItem(LOCALE_KEY);
  const locale = setLocale(savedLocale ?? navigator.language ?? DEFAULT_LOCALE);
  localeSelect.value = locale;
  syncCustomSelect(localeSelect);
}

function applyLocaleToDocument(): void {
  document.documentElement.lang = getLocale();
  document.title = t("app.title");

  document.querySelectorAll<HTMLElement>("[data-i18n]").forEach((element) => {
    const key = element.dataset.i18n;
    if (key && isWebMessageKey(key)) {
      element.textContent = t(key);
    }
  });

  document.querySelectorAll<HTMLElement>("[data-i18n-placeholder]").forEach((element) => {
    const key = element.dataset.i18nPlaceholder;
    if (!key || !isWebMessageKey(key)) {
      return;
    }
    if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement) {
      element.placeholder = t(key);
    }
  });

  document.querySelectorAll<HTMLElement>("[data-i18n-aria-label]").forEach((element) => {
    const key = element.dataset.i18nAriaLabel;
    if (key && isWebMessageKey(key)) {
      element.setAttribute("aria-label", t(key));
    }
  });

  renderProviderTypeOptions();
  renderChatHeader();
  renderChatList();
  renderSearchChatResults();
  renderMessages();
  renderComposerModelSelectors();
  renderComposerTokenEstimate();
  if (state.tabLoaded.models) {
    renderModelsPanel();
  }
  if (state.tabLoaded.channels) {
    renderChannelsPanel();
  }
  if (state.tabLoaded.workspace) {
    renderWorkspacePanel();
  }
  if (state.tabLoaded.cron) {
    renderCronJobs();
    if (state.cronModal.mode === "edit") {
      renderCronExecutionDetails(state.cronStates[state.cronModal.editingJobID]);
    }
  }
  cronWorkflowEditor?.refreshLabels();
  setCronModalMode(state.cronModal.mode, state.cronModal.editingJobID);
  syncCronWorkflowFullscreenUI();
  syncCronDispatchHint();
  syncAllCustomSelects();
  logComposerStatusToConsole();
}

async function handleControlChange(resetDraft: boolean): Promise<void> {
  syncControlState();
  if (resetDraft) {
    startDraftSession();
    ensureCronSessionID();
  }
  syncCronDispatchHint();
  invalidateResourceTabs();

  await reloadChats();
  if (state.activeTab !== "chat") {
    await loadTabData(state.activeTab, true);
  }
}

async function syncModelStateOnBoot(): Promise<void> {
  try {
    await syncModelState({ autoActivate: true });
  } catch {
    // Keep chat usable even if model catalog is temporarily unavailable.
  }
}

async function switchTab(tab: TabKey): Promise<void> {
  if (state.activeTab === tab) {
    return;
  }
  setSearchModalOpen(false);
  setWorkspaceEditorModalOpen(false);
  setWorkspaceImportModalOpen(false);
  if (tab !== "cron") {
    setCronCreateModalOpen(false);
  }
  state.activeTab = tab;
  renderTabPanels();
  await loadTabData(tab);
}

function renderTabPanels(): void {
  tabButtons.forEach((button) => {
    const tab = button.dataset.tab;
    button.classList.toggle("active", tab === state.activeTab);
  });
  const cronActive = state.activeTab === "cron";
  chatCronToggleButton.classList.toggle("is-active", cronActive);
  chatCronToggleButton.setAttribute("aria-pressed", String(cronActive));

  TABS.forEach((tab) => {
    panelByTab[tab].classList.toggle("is-active", tab === state.activeTab);
  });
}

async function loadTabData(tab: TabKey, force = false): Promise<void> {
  try {
    if (tab === "chat") {
      await reloadChats();
      return;
    }
    if (!force && state.tabLoaded[tab]) {
      return;
    }

    switch (tab) {
      case "cron":
        await refreshCronJobs();
        break;
      default:
        break;
    }
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function restoreSettings(): void {
  const raw = localStorage.getItem(SETTINGS_KEY);
  if (raw) {
    try {
      const parsed = JSON.parse(raw) as PersistedSettings;
      if (typeof parsed.apiBase === "string" && parsed.apiBase.trim() !== "") {
        state.apiBase = parsed.apiBase.trim();
      }
      if (typeof parsed.apiKey === "string") {
        state.apiKey = parsed.apiKey.trim();
      }
      state.workspaceCardEnabled = parseWorkspaceCardEnabled(parsed.workspaceCardEnabled);
    } catch {
      localStorage.removeItem(SETTINGS_KEY);
    }
  }
  state.userId = DEFAULT_USER_ID;
  state.channel = WEB_CHAT_CHANNEL;
  apiBaseInput.value = state.apiBase;
  apiKeyInput.value = state.apiKey;
}

function syncControlState(): void {
  state.apiBase = apiBaseInput.value.trim() || DEFAULT_API_BASE;
  state.apiKey = apiKeyInput.value.trim();
  state.userId = DEFAULT_USER_ID;
  state.channel = WEB_CHAT_CHANNEL;
  localStorage.setItem(
    SETTINGS_KEY,
    JSON.stringify({
      apiBase: state.apiBase,
      apiKey: state.apiKey,
      workspaceCardEnabled: state.workspaceCardEnabled,
    }),
  );
}

function invalidateResourceTabs(): void {
  state.tabLoaded.models = false;
  state.tabLoaded.channels = false;
  state.tabLoaded.workspace = false;
  state.tabLoaded.cron = false;
}

async function reloadChats(options: { includeQQHistory?: boolean } = {}): Promise<void> {
  try {
    const includeQQHistory = options.includeQQHistory ?? true;
    const query = new URLSearchParams({
      channel: WEB_CHAT_CHANNEL,
      user_id: state.userId,
    });
    const chatsRequests: Array<Promise<ChatSpec[]>> = [requestJSON<ChatSpec[]>(`/chats?${query.toString()}`)];
    if (includeQQHistory) {
      const qqQuery = new URLSearchParams({ channel: QQ_CHANNEL });
      chatsRequests.push(requestJSON<ChatSpec[]>(`/chats?${qqQuery.toString()}`));
    }
    const chatsGroups = await Promise.all(chatsRequests);
    const chatsByID = new Map<string, ChatSpec>();
    chatsGroups.flat().forEach((chat) => {
      chatsByID.set(chat.id, chat);
    });
    const nextChats = Array.from(chatsByID.values());
    nextChats.sort((a, b) => {
      const ta = Date.parse(a.updated_at);
      const tb = Date.parse(b.updated_at);
      const va = Number.isFinite(ta) ? ta : 0;
      const vb = Number.isFinite(tb) ? tb : 0;
      if (vb !== va) {
        return vb - va;
      }
      return a.id.localeCompare(b.id);
    });
    const prevDigest = state.chats.map((chat) => `${chat.id}:${chat.updated_at}`).join("|");
    const nextDigest = nextChats.map((chat) => `${chat.id}:${chat.updated_at}`).join("|");
    const chatsChanged = prevDigest !== nextDigest;
    state.chats = nextChats;

    let activeChatCleared = false;
    if (state.activeChatId && !state.chats.some((chat) => chat.id === state.activeChatId)) {
      state.activeChatId = null;
      state.activePromptMode = "default";
      activeChatCleared = true;
    }

    if (!chatsChanged && !activeChatCleared) {
      return;
    }
    renderChatList();
    renderSearchChatResults();
    renderChatHeader();
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

async function openChat(chatID: string): Promise<void> {
  const chat = state.chats.find((item) => item.id === chatID);
  if (!chat) {
    setStatus(t("status.chatNotFound", { chatId: chatID }), "error");
    return;
  }

  const requestSerial = ++openChatRequestSerial;
  state.activeChatId = chat.id;
  state.activeSessionId = chat.session_id;
  state.activePromptMode = resolveChatPromptMode(chat.meta);
  renderChatHeader();
  syncActiveChatSelections();

  try {
    const history = await requestJSON<ChatHistoryResponse>(`/chats/${encodeURIComponent(chat.id)}`);
    if (requestSerial !== openChatRequestSerial || state.activeChatId !== chat.id) {
      return;
    }
    state.messages = history.messages.map(toViewMessage);
    renderMessages({ animate: false });
    setStatus(t("status.loadedMessages", { count: history.messages.length }), "info");
  } catch (error) {
    if (requestSerial !== openChatRequestSerial || state.activeChatId !== chat.id) {
      return;
    }
    setStatus(asErrorMessage(error), "error");
  } finally {
    if (requestSerial === openChatRequestSerial && state.activeChatId === chat.id) {
      renderComposerTokenEstimate();
    }
  }
}

async function deleteChat(chatID: string): Promise<void> {
  const chat = state.chats.find((item) => item.id === chatID);
  if (!chat) {
    setStatus(t("status.chatNotFound", { chatId: chatID }), "error");
    return;
  }

  const confirmed = window.confirm(
    t("chat.deleteConfirm", {
      sessionId: chat.session_id,
    }),
  );
  if (!confirmed) {
    return;
  }

  const wasActive = state.activeChatId === chatID;
  try {
    await requestJSON<DeleteResult>(`/chats/${encodeURIComponent(chatID)}`, {
      method: "DELETE",
    });
    await reloadChats();
    if (wasActive) {
      if (state.chats.length > 0) {
        await openChat(state.chats[0].id);
      } else {
        startDraftSession();
      }
    } else {
      renderChatList();
      renderSearchChatResults();
      renderChatHeader();
    }
    setStatus(
      t("status.chatDeleted", {
        sessionId: chat.session_id,
      }),
      "info",
    );
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function startDraftSession(): void {
  openChatRequestSerial += 1;
  state.activeChatId = null;
  state.activeSessionId = newSessionID();
  state.activePromptMode = "default";
  state.messages = [];
  renderChatHeader();
  renderChatList();
  renderSearchChatResults();
  renderMessages();
  renderComposerTokenEstimate();
}

interface PromptTemplateCommand {
  templateName: string;
  args: Map<string, string>;
}

function parsePromptTemplateCommand(inputText: string): PromptTemplateCommand | null {
  const trimmed = inputText.trim();
  if (!trimmed.startsWith(PROMPT_TEMPLATE_PREFIX)) {
    return null;
  }
  const segments = trimmed.split(/\s+/).filter((segment) => segment !== "");
  const command = segments[0] ?? "";
  const templateName = command.slice(PROMPT_TEMPLATE_PREFIX.length).trim();
  if (templateName === "") {
    throw new Error("prompt template name is required");
  }
  if (!PROMPT_TEMPLATE_NAME_PATTERN.test(templateName)) {
    throw new Error(`invalid prompt template name: ${templateName}`);
  }

  const args = new Map<string, string>();
  for (const segment of segments.slice(1)) {
    const sepIndex = segment.indexOf("=");
    if (sepIndex <= 0) {
      throw new Error(`invalid prompt argument: ${segment} (expected KEY=VALUE)`);
    }
    const key = segment.slice(0, sepIndex).trim();
    const value = segment.slice(sepIndex + 1);
    if (!PROMPT_TEMPLATE_ARG_KEY_PATTERN.test(key)) {
      throw new Error(`invalid prompt argument key: ${key}`);
    }
    args.set(key, value);
  }
  return { templateName, args };
}

async function loadPromptTemplateContent(templateName: string): Promise<string> {
  const candidates = [`prompts/${templateName}.md`, `prompt/${templateName}.md`];
  let lastError: unknown = null;
  for (const path of candidates) {
    try {
      const payload = await getWorkspaceFile(path);
      const content = extractWorkspaceFileText(payload);
      if (content.trim() === "") {
        throw new Error(`prompt template is empty: ${path}`);
      }
      return content;
    } catch (error) {
      lastError = error;
    }
  }
  throw new Error(`prompt template not found: ${templateName} (${asErrorMessage(lastError)})`);
}

function applyPromptTemplateArgs(templateContent: string, args: Map<string, string>): string {
  if (/\$[1-9]\b/.test(templateContent) || /\$ARGUMENTS\b/.test(templateContent)) {
    throw new Error("positional prompt arguments are not supported yet");
  }
  const placeholderRegex = /\$([A-Za-z_][A-Za-z0-9_]*)/g;
  const requiredKeys = new Set<string>();
  for (const match of templateContent.matchAll(placeholderRegex)) {
    const key = match[1];
    if (key) {
      requiredKeys.add(key);
    }
  }
  const missingKeys = Array.from(requiredKeys).filter((key) => !args.has(key));
  if (missingKeys.length > 0) {
    throw new Error(`missing prompt arguments: ${missingKeys.join(", ")}`);
  }

  return templateContent.replace(PROMPT_TEMPLATE_PLACEHOLDER_PATTERN, (_match, key: string) => args.get(key) ?? "");
}

async function expandPromptTemplateIfNeeded(inputText: string): Promise<string> {
  if (!runtimeFlags.prompt_templates) {
    return inputText;
  }
  const parsed = parsePromptTemplateCommand(inputText);
  if (!parsed) {
    return inputText;
  }
  const templateContent = await loadPromptTemplateContent(parsed.templateName);
  return applyPromptTemplateArgs(templateContent, parsed.args);
}

async function sendMessage(): Promise<void> {
  await bootstrapTask;
  syncControlState();
  if (state.sending) {
    return;
  }

  const draftText = messageInput.value.trim();
  if (draftText === "") {
    setStatus(t("status.inputRequired"), "error");
    return;
  }

  if (state.apiBase === "") {
    setStatus(t("status.controlsRequired"), "error");
    return;
  }

  let inputText = draftText;
  try {
    inputText = await expandPromptTemplateIfNeeded(draftText);
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return;
  }

  let bizParams: Record<string, unknown> | undefined;
  try {
    bizParams = parseChatBizParams(inputText);
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return;
  }

  state.sending = true;
  sendButton.disabled = true;
  const assistantID = `assistant-${Date.now()}`;

  state.messages = state.messages.concat(
    {
      id: `user-${Date.now()}`,
      role: "user",
      text: inputText,
      toolCalls: [],
      textOrder: nextMessageOutputOrder(),
      timeline: [],
    },
    {
      id: assistantID,
      role: "assistant",
      text: "",
      toolCalls: [],
      timeline: [],
    },
  );
  const latestUserMessage = state.messages[state.messages.length - 2];
  if (latestUserMessage && latestUserMessage.role === "user" && latestUserMessage.textOrder !== undefined) {
    latestUserMessage.timeline = [
      {
        type: "text",
        order: latestUserMessage.textOrder,
        text: latestUserMessage.text,
      },
    ];
  }
  renderMessages();
  messageInput.value = "";
  renderComposerTokenEstimate();
  setStatus(t("status.streamingReply"), "info");

  try {
    await streamReply(
      inputText,
      bizParams,
      (delta) => {
        const target = state.messages.find((item) => item.id === assistantID);
        if (!target) {
          return;
        }
        appendAssistantDelta(target, delta);
        renderMessageInPlace(assistantID);
      },
      (event) => {
        handleToolCallEvent(event, assistantID);
      },
    );
    setStatus(t("status.replyCompleted"), "info");

    await reloadChats();
    const matched = state.chats.find(
      (chat) =>
        chat.session_id === state.activeSessionId &&
        chat.channel === WEB_CHAT_CHANNEL &&
        chat.user_id === state.userId,
    );
    if (matched) {
      state.activeChatId = matched.id;
      state.activeSessionId = matched.session_id;
      renderChatHeader();
      renderChatList();
      renderSearchChatResults();
    }
  } catch (error) {
    const message = asErrorMessage(error);
    fillAssistantErrorMessageIfPending(assistantID, message);
    setStatus(message, "error");
  } finally {
    state.sending = false;
    sendButton.disabled = false;
    renderComposerTokenEstimate();
  }
}

function appendComposerAttachmentMentions(files: FileList | null): void {
  if (!files || files.length === 0) {
    return;
  }
  const mentions: string[] = [];
  for (const file of Array.from(files)) {
    const normalizedName = normalizeAttachmentName(file.name);
    if (normalizedName === "") {
      continue;
    }
    mentions.push(`@${normalizedName}`);
  }
  if (mentions.length === 0) {
    return;
  }
  const mentionLine = mentions.join(" ");
  const existing = messageInput.value.trimEnd();
  messageInput.value = existing === "" ? mentionLine : `${existing}\n${mentionLine}`;
  messageInput.focus();
  const cursor = messageInput.value.length;
  messageInput.setSelectionRange(cursor, cursor);
  renderComposerTokenEstimate();
  setStatus(
    t("status.composerAttachmentsAdded", {
      count: mentions.length,
    }),
    "info",
  );
}

function normalizeAttachmentName(raw: string): string {
  return raw.replace(/[\r\n\t]+/g, " ").trim();
}

async function streamReply(
  userText: string,
  bizParams: Record<string, unknown> | undefined,
  onDelta: (delta: string) => void,
  onEvent?: (event: AgentStreamEvent) => void,
): Promise<void> {
  const payload: Record<string, unknown> = {
    input: [{ role: "user", type: "message", content: [{ type: "text", text: userText }] }],
    session_id: state.activeSessionId,
    user_id: state.userId,
    channel: WEB_CHAT_CHANNEL,
    stream: true,
  };
  payload.biz_params = mergePromptModeBizParams(bizParams, state.activePromptMode);

  const headers = new Headers({
    "content-type": "application/json",
    accept: "text/event-stream,application/json",
  });
  applyAuthHeaders(headers);
  applyRequestSourceHeader(headers);

  const response = await fetch(toAbsoluteURL("/agent/process"), {
    method: "POST",
    headers,
    body: JSON.stringify(payload),
  });

  if (!response.ok) {
    throw new Error(await readErrorMessage(response));
  }
  if (!response.body) {
    throw new Error(t("error.streamUnsupported"));
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let doneReceived = false;

  while (!doneReceived) {
    const chunk = await reader.read();
    if (chunk.done) {
      break;
    }
    buffer += decoder.decode(chunk.value, { stream: true }).replaceAll("\r", "");
    const result = consumeSSEBuffer(buffer, onDelta, onEvent);
    buffer = result.rest;
    doneReceived = result.done;
  }

  buffer += decoder.decode().replaceAll("\r", "");
  if (!doneReceived && buffer.trim() !== "") {
    const result = consumeSSEBuffer(`${buffer}\n\n`, onDelta, onEvent);
    doneReceived = result.done;
  }

  if (!doneReceived) {
    throw new Error(t("error.sseEndedEarly"));
  }
}

function parseChatBizParams(inputText: string): Record<string, unknown> | undefined {
  const trimmed = inputText.trim();
  if (!trimmed.startsWith("/shell")) {
    return undefined;
  }
  const command = trimmed.slice("/shell".length).trim();
  if (command === "") {
    throw new Error("shell command is required after /shell");
  }
  return {
    tool: {
      name: "shell",
      input: {
        command,
      },
    },
  };
}

function consumeSSEBuffer(
  raw: string,
  onDelta: (delta: string) => void,
  onEvent?: (event: AgentStreamEvent) => void,
): { done: boolean; rest: string } {
  let buffer = raw;
  let done = false;
  while (!done) {
    const boundary = buffer.indexOf("\n\n");
    if (boundary < 0) {
      break;
    }
    const block = buffer.slice(0, boundary);
    buffer = buffer.slice(boundary + 2);
    done = consumeSSEBlock(block, onDelta, onEvent) || done;
  }
  return { done, rest: buffer };
}

function consumeSSEBlock(block: string, onDelta: (delta: string) => void, onEvent?: (event: AgentStreamEvent) => void): boolean {
  if (block.trim() === "") {
    return false;
  }
  const dataLines: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).trimStart());
    }
  }
  if (dataLines.length === 0) {
    return false;
  }
  const data = dataLines.join("\n");
  if (data === "[DONE]") {
    return true;
  }
  let payload: AgentStreamEvent;
  try {
    payload = JSON.parse(data) as AgentStreamEvent;
  } catch {
    onDelta(data);
    return false;
  }
  payload.raw = data;
  if (payload.type === "error") {
    if (onEvent) {
      onEvent(payload);
    }
    const message = typeof payload.meta?.message === "string" ? payload.meta.message.trim() : "";
    const code = typeof payload.meta?.code === "string" ? payload.meta.code.trim() : "";
    if (code !== "" && message !== "") {
      throw new Error(`${code}: ${message}`);
    }
    if (message !== "") {
      throw new Error(message);
    }
    throw new Error(t("error.sseUnexpectedError"));
  }
  if (typeof payload.type === "string" && onEvent) {
    onEvent(payload);
  }
  if (typeof payload.delta === "string") {
    onDelta(payload.delta);
  }
  return false;
}

function renderChatList(): void {
  chatList.innerHTML = "";
  if (state.chats.length === 0) {
    const li = document.createElement("li");
    li.className = "chat-empty-fill";
    li.setAttribute("aria-hidden", "true");
    chatList.appendChild(li);
    return;
  }

  state.chats.forEach((chat) => {
    const li = document.createElement("li");
    li.className = "chat-list-item";

    const actions = document.createElement("div");
    actions.className = "chat-item-actions";

    const button = document.createElement("button");
    button.type = "button";
    button.className = "chat-item-btn";
    button.dataset.chatId = chat.id;
    if (chat.id === state.activeChatId) {
      button.classList.add("active");
    }
    button.addEventListener("click", () => {
      void openChat(chat.id);
    });

    const title = document.createElement("span");
    title.className = "chat-title";
    title.textContent = chat.name || t("chat.unnamed");

    const meta = document.createElement("span");
    meta.className = "chat-meta";
    meta.textContent = t("chat.meta", {
      updatedAt: compactTime(chat.updated_at),
    });

    button.append(title, meta);
    actions.appendChild(button);

    const deleteLabel = t("chat.delete");
    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    deleteButton.className = "chat-delete-btn";
    deleteButton.setAttribute("aria-label", deleteLabel);
    deleteButton.title = deleteLabel;
    deleteButton.innerHTML = TRASH_ICON_SVG;
    deleteButton.addEventListener("click", async (event) => {
      event.stopPropagation();
      await deleteChat(chat.id);
    });
    actions.appendChild(deleteButton);

    li.appendChild(actions);
    chatList.appendChild(li);
  });
  syncActiveChatSelections();
}

function renderSearchChatResults(): void {
  searchChatResults.innerHTML = "";
  searchChatInput.value = state.chatSearchQuery;

  if (state.chats.length === 0) {
    appendEmptyItem(searchChatResults, t("search.emptyChats"));
    return;
  }

  const filteredChats = filterChatsForSearch(state.chatSearchQuery);
  if (filteredChats.length === 0) {
    appendEmptyItem(
      searchChatResults,
      t("search.noResults", {
        query: state.chatSearchQuery,
      }),
    );
    return;
  }

  filteredChats.forEach((chat) => {
    const li = document.createElement("li");
    li.className = "search-result-item";

    const button = document.createElement("button");
    button.type = "button";
    button.className = "chat-item-btn search-result-btn";
    button.dataset.chatId = chat.id;
    if (chat.id === state.activeChatId) {
      button.classList.add("active");
    }
    button.addEventListener("click", async () => {
      await openChat(chat.id);
      setSearchModalOpen(false);
    });

    const title = document.createElement("span");
    title.className = "chat-title";
    title.textContent = chat.name || t("chat.unnamed");

    const meta = document.createElement("span");
    meta.className = "chat-meta";
    meta.textContent = t("search.meta", {
      sessionId: chat.session_id,
      channel: chat.channel,
      userId: chat.user_id,
      updatedAt: compactTime(chat.updated_at),
    });

    button.append(title, meta);
    li.appendChild(button);
    searchChatResults.appendChild(li);
  });
  syncActiveChatSelections();
}

function filterChatsForSearch(query: string): ChatSpec[] {
  const normalizedQuery = query.trim().toLowerCase();
  if (normalizedQuery === "") {
    return state.chats;
  }
  return state.chats.filter((chat) => buildChatSearchText(chat).includes(normalizedQuery));
}

function buildChatSearchText(chat: ChatSpec): string {
  return [chat.name, chat.session_id, chat.user_id, chat.channel, resolveChatCronJobID(chat.meta)].join(" ").toLowerCase();
}

function resolveChatCronJobID(meta: Record<string, unknown> | undefined): string {
  if (!meta) {
    return "";
  }
  const raw = meta.cron_job_id;
  if (typeof raw === "string") {
    return raw;
  }
  if (typeof raw === "number") {
    return String(raw);
  }
  return "";
}

function normalizePromptMode(raw: unknown): PromptMode {
  if (typeof raw !== "string") {
    return "default";
  }
  return raw.trim().toLowerCase() === "codex" ? "codex" : "default";
}

function resolveChatPromptMode(meta: Record<string, unknown> | undefined): PromptMode {
  return normalizePromptMode(meta?.[PROMPT_MODE_META_KEY]);
}

function mergePromptModeBizParams(
  bizParams: Record<string, unknown> | undefined,
  promptMode: PromptMode,
): Record<string, unknown> {
  const merged: Record<string, unknown> = bizParams ? { ...bizParams } : {};
  merged[PROMPT_MODE_META_KEY] = promptMode;
  return merged;
}

function setActivePromptMode(nextMode: PromptMode, options: { announce?: boolean } = {}): void {
  const normalized = nextMode === "codex" ? "codex" : "default";
  const changed = state.activePromptMode !== normalized;
  state.activePromptMode = normalized;
  const active = state.chats.find((chat) => chat.id === state.activeChatId);
  if (active) {
    if (!active.meta) {
      active.meta = {};
    }
    active.meta[PROMPT_MODE_META_KEY] = normalized;
  }
  renderChatHeader();
  if (options.announce && changed) {
    setStatus(t(normalized === "codex" ? "status.promptModeCodexEnabled" : "status.promptModeDefaultEnabled"), "info");
  }
}

function renderChatHeader(): void {
  const active = state.chats.find((chat) => chat.id === state.activeChatId);
  if (active) {
    state.activePromptMode = resolveChatPromptMode(active.meta);
  }
  chatTitle.textContent = active ? active.name : t("chat.draftTitle");
  const sessionId = state.activeSessionId;
  chatSession.textContent = sessionId;
  chatSession.title = sessionId;
  chatPromptModeToggle.checked = state.activePromptMode === "codex";
  chatPromptModeToggle.setAttribute("aria-checked", chatPromptModeToggle.checked ? "true" : "false");
}

function syncActiveChatSelections(): void {
  const activeChatID = state.activeChatId ?? "";
  chatList.querySelectorAll<HTMLButtonElement>(".chat-item-btn[data-chat-id]").forEach((button) => {
    const chatID = button.dataset.chatId ?? "";
    button.classList.toggle("active", chatID !== "" && chatID === activeChatID);
  });
  searchChatResults.querySelectorAll<HTMLButtonElement>(".search-result-btn[data-chat-id]").forEach((button) => {
    const chatID = button.dataset.chatId ?? "";
    button.classList.toggle("active", chatID !== "" && chatID === activeChatID);
  });
}

function renderMessages(options: { animate?: boolean } = {}): void {
  const animate = options.animate ?? true;
  messageList.innerHTML = "";
  if (state.messages.length === 0) {
    const empty = document.createElement("li");
    empty.className = "message-empty-fill";
    empty.setAttribute("aria-hidden", "true");
    messageList.appendChild(empty);
    return;
  }

  for (const message of state.messages) {
    const item = document.createElement("li");
    item.className = `message ${message.role}`;
    if (!animate) {
      item.classList.add("no-anim");
    }
    item.dataset.messageId = message.id;
    renderMessageNode(item, message);
    messageList.appendChild(item);
  }
  messageList.scrollTop = messageList.scrollHeight;
}

function renderMessageInPlace(messageID: string): void {
  const target = state.messages.find((item) => item.id === messageID);
  if (!target) {
    return;
  }
  const items = Array.from(messageList.querySelectorAll<HTMLLIElement>(".message"));
  const node = items.find((item) => item.dataset.messageId === messageID);
  if (!node) {
    renderMessages();
    return;
  }
  renderMessageNode(node, target);
  messageList.scrollTop = messageList.scrollHeight;
}

function nextMessageOutputOrder(): number {
  state.messageOutputOrder += 1;
  return state.messageOutputOrder;
}

function handleToolCallEvent(event: AgentStreamEvent, assistantID: string): void {
  if (event.type === "tool_call") {
    const notice = formatToolCallNotice(event);
    if (!notice) {
      return;
    }
    appendToolCallNoticeToAssistant(assistantID, notice);
    return;
  }
  if (event.type === "tool_result") {
    applyToolResultEvent(event, assistantID);
  }
}

function appendToolCallNoticeToAssistant(assistantID: string, notice: ViewToolCallNotice): void {
  const target = state.messages.find((item) => item.id === assistantID);
  if (!target) {
    return;
  }
  if (notice.summary === "" || notice.raw === "") {
    return;
  }
  const order = nextMessageOutputOrder();
  const noticeWithOrder: ViewToolCallNotice = {
    ...notice,
    order,
  };
  if (target.toolOrder === undefined) {
    target.toolOrder = order;
  }
  target.toolCalls = target.toolCalls.concat(noticeWithOrder);
  target.timeline = target.timeline.concat({
    type: "tool_call",
    order,
    toolCall: noticeWithOrder,
  });
  renderMessageInPlace(assistantID);
}

function appendAssistantDelta(message: ViewMessage, delta: string): void {
  if (delta === "") {
    return;
  }
  if (message.textOrder === undefined) {
    message.textOrder = nextMessageOutputOrder();
  }
  message.text += delta;

  const timeline = message.timeline;
  const last = timeline[timeline.length - 1];
  if (last && last.type === "text") {
    last.text = `${last.text ?? ""}${delta}`;
    return;
  }
  const order = nextMessageOutputOrder();
  timeline.push({
    type: "text",
    order,
    text: delta,
  });
}

function fillAssistantErrorMessageIfPending(assistantID: string, rawMessage: string): void {
  const target = state.messages.find((item) => item.id === assistantID);
  if (!target || target.role !== "assistant" || target.text.trim() !== "") {
    return;
  }
  const message = rawMessage.trim();
  if (message === "") {
    return;
  }
  appendAssistantDelta(target, message);
  renderMessageInPlace(assistantID);
}

function formatToolCallNotice(event: AgentStreamEvent): ViewToolCallNotice | null {
  const toolName = normalizeToolName(event.tool_call?.name);
  const detail = formatToolCallDetail(event);
  if (detail === "") {
    return null;
  }
  return {
    summary: formatToolCallSummary(event.tool_call),
    raw: detail,
    step: parsePositiveInteger(event.step),
    toolName: toolName === "" ? undefined : toolName,
    outputReady: toolName === "shell" ? false : true,
  };
}

function formatToolCallDetail(event: AgentStreamEvent): string {
  const toolName = normalizeToolName(event.tool_call?.name);
  if (toolName === "shell") {
    return t("chat.toolCallOutputPending");
  }
  return formatToolCallRaw(event);
}

function formatToolCallRaw(event: AgentStreamEvent): string {
  const raw = typeof event.raw === "string" ? event.raw.trim() : "";
  if (raw !== "") {
    return raw;
  }
  if (event.tool_call) {
    try {
      return JSON.stringify({
        type: "tool_call",
        step: event.step,
        tool_call: event.tool_call,
      });
    } catch {
      return "";
    }
  }
  return "";
}

function applyToolResultEvent(event: AgentStreamEvent, assistantID: string): void {
  const toolName = normalizeToolName(event.tool_result?.name);
  if (toolName !== "shell") {
    return;
  }
  const output = formatToolResultOutput(event.tool_result);
  const target = state.messages.find((item) => item.id === assistantID);
  if (!target) {
    return;
  }
  const step = parsePositiveInteger(event.step);
  const notice = findPendingToolCallNotice(target.toolCalls, toolName, step);
  if (notice) {
    notice.raw = output;
    notice.outputReady = true;
    renderMessageInPlace(assistantID);
    return;
  }
  appendToolCallNoticeToAssistant(assistantID, {
    summary: "bash",
    raw: output,
    step,
    toolName,
    outputReady: true,
  });
}

function findPendingToolCallNotice(
  notices: ViewToolCallNotice[],
  toolName: string,
  step?: number,
): ViewToolCallNotice | undefined {
  for (let idx = notices.length - 1; idx >= 0; idx -= 1) {
    const item = notices[idx];
    if (item.toolName !== toolName || item.outputReady) {
      continue;
    }
    if (step !== undefined && item.step !== undefined && item.step !== step) {
      continue;
    }
    return item;
  }
  return undefined;
}

function normalizeToolName(value: unknown): string {
  if (typeof value !== "string") {
    return "";
  }
  return value.trim();
}

function formatToolResultOutput(toolResult?: AgentToolResultPayload): string {
  const summary = typeof toolResult?.summary === "string" ? toolResult.summary.trim() : "";
  if (summary !== "") {
    return summary;
  }
  return t("chat.toolCallOutputUnavailable");
}

function formatToolCallSummary(toolCall?: AgentToolCallPayload): string {
  const name = typeof toolCall?.name === "string" ? toolCall.name.trim() : "";
  if (name === "shell") {
    const command = extractShellCommand(toolCall?.input);
    return command === "" ? "bash" : `bash ${command}`;
  }
  if (name === "view") {
    const filePath = extractToolFilePath(toolCall?.input);
    if (filePath !== "") {
      return t("chat.toolCallViewPath", { path: filePath });
    }
    return t("chat.toolCallView");
  }
  if (name === "edit" || name === "exit") {
    const filePath = extractToolFilePath(toolCall?.input);
    if (filePath !== "") {
      return t("chat.toolCallEditPath", { path: filePath });
    }
    return t("chat.toolCallEdit");
  }
  return t("chat.toolCallNotice", { target: name || "tool" });
}

function extractToolFilePath(input?: Record<string, unknown>): string {
  if (!input || typeof input !== "object") {
    return "";
  }
  const directPath = input.path;
  if (typeof directPath === "string" && directPath.trim() !== "") {
    return directPath.trim();
  }
  const nested = input.input;
  if (nested && typeof nested === "object" && !Array.isArray(nested)) {
    const nestedPath = extractToolFilePath(nested as Record<string, unknown>);
    if (nestedPath !== "") {
      return nestedPath;
    }
  }
  const items = input.items;
  if (!Array.isArray(items)) {
    return "";
  }
  for (const item of items) {
    if (!item || typeof item !== "object" || Array.isArray(item)) {
      continue;
    }
    const path = (item as { path?: unknown }).path;
    if (typeof path === "string" && path.trim() !== "") {
      return path.trim();
    }
  }
  return "";
}

function extractShellCommand(input?: Record<string, unknown>): string {
  if (!input || typeof input !== "object") {
    return "";
  }
  const direct = input.command;
  if (typeof direct === "string" && direct.trim() !== "") {
    return direct.trim();
  }
  const items = input.items;
  if (!Array.isArray(items) || items.length === 0) {
    return "";
  }
  const first = items[0];
  if (!first || typeof first !== "object" || Array.isArray(first)) {
    return "";
  }
  const command = (first as { command?: unknown }).command;
  if (typeof command !== "string") {
    return "";
  }
  return command.trim();
}

function renderMessageNode(node: HTMLLIElement, message: ViewMessage): void {
  node.innerHTML = "";

  const orderedTimeline = buildOrderedTimeline(message);
  if (orderedTimeline.length === 0) {
    if (message.role === "assistant") {
      const placeholder = document.createElement("div");
      placeholder.className = "message-text";
      placeholder.textContent = t("common.ellipsis");
      node.appendChild(placeholder);
    }
    return;
  }

  for (const entry of orderedTimeline) {
    if (entry.type === "text") {
      const textValue = entry.text ?? "";
      if (textValue === "") {
        continue;
      }
      const text = document.createElement("div");
      text.className = "message-text";
      text.textContent = textValue;
      node.appendChild(text);
      continue;
    }

    const toolCall = entry.toolCall;
    if (!toolCall) {
      continue;
    }
    const toolCallList = document.createElement("div");
    toolCallList.className = "tool-call-list";

    const details = document.createElement("details");
    details.className = "tool-call-entry";

    const summary = document.createElement("summary");
    summary.className = "tool-call-summary";
    summary.textContent = toolCall.summary;

    const raw = document.createElement("pre");
    raw.className = "tool-call-raw";
    raw.textContent = toolCall.raw;

    details.append(summary, raw);
    toolCallList.appendChild(details);
    node.appendChild(toolCallList);
  }
}

function buildOrderedTimeline(message: ViewMessage): ViewMessageTimelineEntry[] {
  const fromTimeline = normalizeTimeline(message.timeline);
  if (fromTimeline.length > 0) {
    return fromTimeline;
  }

  const fallback: ViewMessageTimelineEntry[] = [];
  if (message.text !== "") {
    fallback.push({
      type: "text",
      order: message.textOrder ?? Number.MAX_SAFE_INTEGER - 1,
      text: message.text,
    });
  }
  for (const toolCall of message.toolCalls) {
    fallback.push({
      type: "tool_call",
      order: toolCall.order ?? message.toolOrder ?? Number.MAX_SAFE_INTEGER,
      toolCall,
    });
  }
  return normalizeTimeline(fallback);
}

function normalizeTimeline(entries: ViewMessageTimelineEntry[]): ViewMessageTimelineEntry[] {
  const normalized = entries
    .filter((entry) => entry.order > 0)
    .slice()
    .sort((left, right) => left.order - right.order);
  if (normalized.length < 2) {
    return normalized;
  }

  const merged: ViewMessageTimelineEntry[] = [];
  for (const entry of normalized) {
    const last = merged[merged.length - 1];
    if (entry.type === "text" && last && last.type === "text") {
      last.text = `${last.text ?? ""}${entry.text ?? ""}`;
      continue;
    }
    merged.push({ ...entry });
  }
  return merged;
}

async function refreshModels(options: { silent?: boolean } = {}): Promise<void> {
  syncControlState();
  try {
    const result = await syncModelState({ autoActivate: true });
    state.tabLoaded.models = true;
    renderModelsPanel();
    if (!options.silent) {
      setStatus(
        t(result.source === "catalog" ? "status.providersLoadedCatalog" : "status.providersLoadedLegacy", {
          count: result.providers.length,
        }),
        "info",
      );
    }
  } catch (error) {
    if (!options.silent) {
      setStatus(asErrorMessage(error), "error");
    }
  }
}

async function syncModelState(options: { autoActivate: boolean }): Promise<{
  providers: ProviderInfo[];
  providerTypes: ProviderTypeInfo[];
  defaults: Record<string, string>;
  activeLLM: ModelSlotConfig;
  source: "catalog" | "legacy";
}> {
  const result = await loadModelCatalog();
  state.providers = result.providers;
  state.providerTypes = result.providerTypes;
  state.modelDefaults = result.defaults;
  state.activeLLM = result.activeLLM;
  renderComposerModelSelectors();

  if (options.autoActivate) {
    const autoActivated = await maybeAutoActivateModel(result.providers, result.defaults, result.activeLLM);
    if (autoActivated) {
      state.activeLLM = autoActivated;
      renderComposerModelSelectors();
      return {
        ...result,
        activeLLM: autoActivated,
      };
    }
  }

  return result;
}

async function maybeAutoActivateModel(
  providers: ProviderInfo[],
  defaults: Record<string, string>,
  activeLLM: ModelSlotConfig,
): Promise<ModelSlotConfig | null> {
  if (activeLLM.provider_id !== "" && activeLLM.model !== "") {
    return null;
  }
  const candidate = pickAutoActiveModelCandidate(providers, defaults);
  if (!candidate) {
    return null;
  }
  try {
    const out = await requestJSON<ActiveModelsInfo>("/models/active", {
      method: "PUT",
      body: {
        provider_id: candidate.providerID,
        model: candidate.modelID,
      },
    });
    const normalized = normalizeModelSlot(out.active_llm);
    if (normalized.provider_id === "" || normalized.model === "") {
      return null;
    }
    return normalized;
  } catch {
    return null;
  }
}

function pickAutoActiveModelCandidate(
  providers: ProviderInfo[],
  defaults: Record<string, string>,
): { providerID: string; modelID: string } | null {
  let fallback: { providerID: string; modelID: string } | null = null;
  for (const provider of providers) {
    if (provider.enabled === false || provider.has_api_key !== true) {
      continue;
    }
    if (provider.models.length === 0) {
      continue;
    }
    const defaultModel = (defaults[provider.id] ?? "").trim();
    if (defaultModel !== "" && provider.models.some((model) => model.id === defaultModel)) {
      return {
        providerID: provider.id,
        modelID: defaultModel,
      };
    }
    if (!fallback) {
      const firstModel = provider.models[0]?.id?.trim() ?? "";
      if (firstModel !== "") {
        fallback = {
          providerID: provider.id,
          modelID: firstModel,
        };
      }
    }
  }
  return fallback;
}

async function loadModelCatalog(): Promise<{
  providers: ProviderInfo[];
  providerTypes: ProviderTypeInfo[];
  defaults: Record<string, string>;
  activeLLM: ModelSlotConfig;
  source: "catalog" | "legacy";
}> {
  try {
    const catalog = await requestJSON<ModelCatalogInfo>("/models/catalog");
    const providers = normalizeProviders(catalog.providers);
    const providerTypes = normalizeProviderTypes(catalog.provider_types);
    return {
      providers,
      providerTypes,
      defaults: normalizeDefaults(catalog.defaults, providers),
      activeLLM: normalizeModelSlot(catalog.active_llm),
      source: "catalog",
    };
  } catch {
    const providersRaw = await requestJSON<ProviderInfo[]>("/models");
    const providers = normalizeProviders(providersRaw);
    const activeResult = await requestJSON<ActiveModelsInfo>("/models/active");
    return {
      providers,
      providerTypes: fallbackProviderTypes(providers),
      defaults: buildDefaultMapFromProviders(providers),
      activeLLM: normalizeModelSlot(activeResult.active_llm),
      source: "legacy",
    };
  }
}

function listSelectableProviders(): ProviderInfo[] {
  return state.providers.filter((provider) => provider.enabled !== false && provider.models.length > 0);
}

function appendSelectOption(select: HTMLSelectElement, value: string, label: string): void {
  const option = document.createElement("option");
  option.value = value;
  option.textContent = label;
  select.appendChild(option);
}

function resolveComposerProvider(providers: ProviderInfo[]): ProviderInfo | null {
  if (providers.length === 0) {
    return null;
  }
  const active = providers.find((provider) => provider.id === state.activeLLM.provider_id);
  if (active) {
    return active;
  }
  const selected = providers.find((provider) => provider.id === composerProviderSelect.value.trim());
  if (selected) {
    return selected;
  }
  const withDefault = providers.find((provider) => {
    const defaultModel = (state.modelDefaults[provider.id] ?? "").trim();
    return defaultModel !== "" && provider.models.some((model) => model.id === defaultModel);
  });
  return withDefault ?? providers[0];
}

function formatComposerModelLabel(model: ModelInfo): string {
  const modelID = model.id.trim();
  if (modelID !== "") {
    return modelID;
  }
  return (model.name ?? "").trim();
}

function resolveComposerModelCanonicalID(model: ModelInfo): string {
  const modelID = model.id.trim();
  if (modelID === "") {
    return "";
  }
  const aliasOf = (model.alias_of ?? "").trim();
  if (aliasOf !== "" && aliasOf !== modelID) {
    return aliasOf;
  }
  return modelID;
}

function buildComposerModelOptions(provider: ProviderInfo): ComposerModelOption[] {
  const optionsByCanonical = new Map<string, ComposerModelOption>();
  for (const model of provider.models) {
    const modelID = model.id.trim();
    if (modelID === "") {
      continue;
    }
    const canonicalID = resolveComposerModelCanonicalID(model) || modelID;
    const option: ComposerModelOption = {
      value: modelID,
      canonical: canonicalID,
      label: formatComposerModelLabel(model),
    };
    const existing = optionsByCanonical.get(canonicalID);
    if (!existing) {
      optionsByCanonical.set(canonicalID, option);
      continue;
    }
    const existingIsAlias = existing.value !== existing.canonical;
    const currentIsAlias = option.value !== option.canonical;
    if (!existingIsAlias && currentIsAlias) {
      optionsByCanonical.set(canonicalID, option);
    }
  }
  return Array.from(optionsByCanonical.values());
}

function resolveComposerModelValue(options: ComposerModelOption[], requestedModelID: string): string {
  const modelID = requestedModelID.trim();
  if (modelID === "") {
    return "";
  }
  if (options.some((option) => option.value === modelID)) {
    return modelID;
  }
  const byCanonical = options.find((option) => option.canonical === modelID);
  return byCanonical?.value ?? "";
}

function resolveComposerModelID(provider: ProviderInfo, options: ComposerModelOption[]): string {
  const activeModel = state.activeLLM.provider_id === provider.id ? state.activeLLM.model.trim() : "";
  const activeValue = resolveComposerModelValue(options, activeModel);
  if (activeValue !== "") {
    return activeValue;
  }
  const selectedModel = composerModelSelect.value.trim();
  const selectedValue = resolveComposerModelValue(options, selectedModel);
  if (selectedValue !== "") {
    return selectedValue;
  }
  const defaultModel = (state.modelDefaults[provider.id] ?? "").trim();
  const defaultValue = resolveComposerModelValue(options, defaultModel);
  if (defaultValue !== "") {
    return defaultValue;
  }
  return options[0]?.value ?? "";
}

function renderComposerModelOptions(provider: ProviderInfo | null): string {
  composerModelSelect.innerHTML = "";
  if (!provider || provider.models.length === 0) {
    appendSelectOption(composerModelSelect, "", t("models.noModelOption"));
    composerModelSelect.value = "";
    composerModelSelect.disabled = true;
    syncCustomSelect(composerModelSelect);
    return "";
  }

  const options = buildComposerModelOptions(provider);
  if (options.length === 0) {
    appendSelectOption(composerModelSelect, "", t("models.noModelOption"));
    composerModelSelect.value = "";
    composerModelSelect.disabled = true;
    syncCustomSelect(composerModelSelect);
    return "";
  }

  for (const option of options) {
    appendSelectOption(composerModelSelect, option.value, option.label);
  }

  const resolvedModelID = resolveComposerModelID(provider, options);
  composerModelSelect.value = resolvedModelID;
  composerModelSelect.disabled = resolvedModelID === "";
  syncCustomSelect(composerModelSelect);
  return resolvedModelID;
}

function renderComposerModelSelectors(): void {
  const providers = listSelectableProviders();
  syncingComposerModelSelectors = true;
  try {
    composerProviderSelect.innerHTML = "";
    if (providers.length === 0) {
      appendSelectOption(composerProviderSelect, "", t("models.noProviderOption"));
      composerProviderSelect.value = "";
      composerProviderSelect.disabled = true;
      renderComposerModelOptions(null);
      syncCustomSelect(composerProviderSelect);
      return;
    }

    for (const provider of providers) {
      appendSelectOption(composerProviderSelect, provider.id, formatProviderLabel(provider));
    }

    const selectedProvider = resolveComposerProvider(providers);
    if (!selectedProvider) {
      composerProviderSelect.value = "";
      composerProviderSelect.disabled = true;
      renderComposerModelOptions(null);
      syncCustomSelect(composerProviderSelect);
      return;
    }

    composerProviderSelect.value = selectedProvider.id;
    composerProviderSelect.disabled = false;
    syncCustomSelect(composerProviderSelect);
    renderComposerModelOptions(selectedProvider);
  } finally {
    syncingComposerModelSelectors = false;
    renderComposerTokenEstimate();
  }
}

async function handleComposerProviderSelectChange(): Promise<void> {
  if (syncingComposerModelSelectors) {
    return;
  }
  const providers = listSelectableProviders();
  const selectedProvider = providers.find((provider) => provider.id === composerProviderSelect.value.trim()) ?? null;
  syncingComposerModelSelectors = true;
  let selectedModelID = "";
  try {
    selectedModelID = renderComposerModelOptions(selectedProvider);
  } finally {
    syncingComposerModelSelectors = false;
  }
  if (!selectedProvider || selectedModelID === "") {
    return;
  }
  await setActiveModel(selectedProvider.id, selectedModelID);
}

async function handleComposerModelSelectChange(): Promise<void> {
  if (syncingComposerModelSelectors) {
    return;
  }
  const providerID = composerProviderSelect.value.trim();
  const modelID = composerModelSelect.value.trim();
  if (providerID === "" || modelID === "") {
    setStatus(t("error.providerAndModelRequired"), "error");
    return;
  }
  await setActiveModel(providerID, modelID);
}

async function setActiveModel(providerID: string, modelID: string): Promise<boolean> {
  const normalizedProviderID = providerID.trim();
  const normalizedModelID = modelID.trim();
  if (normalizedProviderID === "" || normalizedModelID === "") {
    setStatus(t("error.providerAndModelRequired"), "error");
    return false;
  }
  if (state.activeLLM.provider_id === normalizedProviderID && state.activeLLM.model === normalizedModelID) {
    state.selectedProviderID = normalizedProviderID;
    return true;
  }
  syncControlState();
  try {
    const out = await requestJSON<ActiveModelsInfo>("/models/active", {
      method: "PUT",
      body: {
        provider_id: normalizedProviderID,
        model: normalizedModelID,
      },
    });
    const normalized = normalizeModelSlot(out.active_llm);
    state.activeLLM =
      normalized.provider_id === "" || normalized.model === ""
        ? {
            provider_id: normalizedProviderID,
            model: normalizedModelID,
          }
        : normalized;
    state.selectedProviderID = state.activeLLM.provider_id;
    renderComposerModelSelectors();
    if (state.tabLoaded.models) {
      renderModelsPanel();
    }
    setStatus(
      t("status.activeModelUpdated", {
        providerId: state.activeLLM.provider_id,
        model: state.activeLLM.model,
      }),
      "info",
    );
    return true;
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    renderComposerModelSelectors();
    return false;
  }
}

function renderModelsPanel(): void {
  syncSelectedProviderID();
  renderProviderNavigation();
  renderModelsSettingsLevel();
  renderProviderBaseURLPreview();
  setProviderAPIKeyVisibility(state.providerAPIKeyVisible);
}

function setChannelsSettingsLevel(level: ChannelsSettingsLevel): void {
  state.channelsSettingsLevel = level === "edit" ? "edit" : "list";
  const showEdit = state.channelsSettingsLevel === "edit";
  channelsLevel1View.hidden = showEdit;
  channelsLevel2View.hidden = !showEdit;
  channelsSettingsSection.classList.toggle("is-level2-active", showEdit);
}

function renderChannelNavigation(): void {
  channelsEntryList.innerHTML = "";

  const entry = document.createElement("li");
  entry.className = "models-provider-card-entry";

  const button = document.createElement("button");
  button.type = "button";
  button.className = "models-provider-card channels-entry-card";
  button.dataset.channelAction = "open";
  button.dataset.channelId = QQ_CHANNEL;
  if (state.channelsSettingsLevel === "edit") {
    button.classList.add("is-selected");
  }
  button.setAttribute("aria-pressed", String(state.channelsSettingsLevel === "edit"));

  const title = document.createElement("span");
  title.className = "models-provider-card-title";
  title.textContent = t("workspace.qqChannelTitle");

  const enabledMeta = document.createElement("span");
  enabledMeta.className = "models-provider-card-meta";
  enabledMeta.textContent = t("models.enabledLine", {
    enabled: state.qqChannelConfig.enabled ? t("common.yes") : t("common.no"),
  });

  const environmentMeta = document.createElement("span");
  environmentMeta.className = "models-provider-card-meta";
  const environment = resolveQQAPIEnvironment(state.qqChannelConfig.api_base);
  const environmentLabel = environment === "sandbox"
    ? t("workspace.qqAPIEnvironmentSandbox")
    : t("workspace.qqAPIEnvironmentProduction");
  environmentMeta.textContent = `${t("workspace.qqAPIEnvironment")}: ${environmentLabel}`;

  button.append(title, enabledMeta, environmentMeta);
  entry.appendChild(button);
  channelsEntryList.appendChild(entry);
}

function renderChannelsPanel(): void {
  renderChannelNavigation();
  renderQQChannelConfig();
  setChannelsSettingsLevel(state.channelsSettingsLevel);
}

function setWorkspaceSettingsLevel(level: WorkspaceSettingsLevel): void {
  state.workspaceSettingsLevel = level === "config" || level === "prompt" || level === "codex" ? level : "list";
  const showList = state.workspaceSettingsLevel === "list";
  workspaceLevel1View.hidden = !showList;
  workspaceLevel2ConfigView.hidden = state.workspaceSettingsLevel !== "config";
  workspaceLevel2PromptView.hidden = state.workspaceSettingsLevel !== "prompt";
  workspaceLevel2CodexView.hidden = state.workspaceSettingsLevel !== "codex";
  workspaceSettingsSection.classList.toggle("is-level2-active", !showList);
}

function parseWorkspaceCardEnabled(raw: unknown): Record<WorkspaceCardKey, boolean> {
  const next = { ...DEFAULT_WORKSPACE_CARD_ENABLED };
  if (!raw || typeof raw !== "object") {
    return next;
  }
  const source = raw as Record<string, unknown>;
  for (const card of WORKSPACE_CARD_KEYS) {
    if (typeof source[card] === "boolean") {
      next[card] = source[card] as boolean;
    }
  }
  return next;
}

function isWorkspaceCardKey(value: string | undefined): value is WorkspaceCardKey {
  return value === "config" || value === "prompt" || value === "codex";
}

function isWorkspaceCardEnabled(card: WorkspaceCardKey): boolean {
  return state.workspaceCardEnabled[card] !== false;
}

function resolveWorkspaceCardTitle(card: WorkspaceCardKey): string {
  if (card === "config") {
    return t("workspace.configCardTitle");
  }
  if (card === "prompt") {
    return t("workspace.promptCardTitle");
  }
  return t("workspace.codexCardTitle");
}

function ensureWorkspaceCardEnabled(card: WorkspaceCardKey): boolean {
  if (isWorkspaceCardEnabled(card)) {
    return true;
  }
  setStatus(t("status.workspaceCardBlocked", { card: resolveWorkspaceCardTitle(card) }), "info");
  return false;
}

function setWorkspaceCardEnabled(card: WorkspaceCardKey, enabled: boolean): void {
  if (state.workspaceCardEnabled[card] === enabled) {
    return;
  }
  state.workspaceCardEnabled[card] = enabled;
  if (!enabled && state.workspaceSettingsLevel === card) {
    setWorkspaceSettingsLevel("list");
  }
  syncControlState();
  renderWorkspacePanel();
  setStatus(
    t(enabled ? "status.workspaceCardEnabled" : "status.workspaceCardDisabled", {
      card: resolveWorkspaceCardTitle(card),
    }),
    "info",
  );
}

function appendWorkspaceNavigationCard(
  card: WorkspaceCardKey,
  action: "open-config" | "open-prompt" | "open-codex",
  selected: boolean,
  titleText: string,
  descText: string,
  fileCount: number,
): void {
  const enabled = isWorkspaceCardEnabled(card);
  const entry = document.createElement("li");
  entry.className = "models-provider-card-entry workspace-entry-card-entry";

  const button = document.createElement("button");
  button.type = "button";
  button.className = "models-provider-card channels-entry-card workspace-entry-card";
  button.dataset.workspaceAction = action;
  button.disabled = !enabled;
  button.setAttribute("aria-disabled", String(!enabled));
  if (selected) {
    button.classList.add("is-selected");
  }
  if (!enabled) {
    button.classList.add("is-disabled");
  }
  button.setAttribute("aria-pressed", String(selected));

  const title = document.createElement("span");
  title.className = "models-provider-card-title";
  title.textContent = titleText;

  const desc = document.createElement("span");
  desc.className = "models-provider-card-meta";
  desc.textContent = descText;

  const status = document.createElement("span");
  status.className = "models-provider-card-meta workspace-entry-card-status";
  status.textContent = enabled ? t("workspace.cardEnabled") : t("workspace.cardDisabled");

  const countMeta = document.createElement("span");
  countMeta.className = "models-provider-card-meta";
  countMeta.textContent = t("workspace.cardFileCount", { count: fileCount });

  button.append(title, desc, status, countMeta);
  entry.appendChild(button);

  const toggleButton = document.createElement("button");
  toggleButton.type = "button";
  toggleButton.className = "secondary-btn workspace-entry-toggle-btn";
  toggleButton.dataset.workspaceToggleCard = card;
  toggleButton.setAttribute("aria-pressed", String(enabled));
  toggleButton.setAttribute("aria-label", `${enabled ? t("workspace.disableCard") : t("workspace.enableCard")} ${titleText}`);
  toggleButton.textContent = enabled ? t("workspace.disableCard") : t("workspace.enableCard");
  entry.appendChild(toggleButton);

  workspaceEntryList.appendChild(entry);
}

function renderWorkspaceNavigation(configCount: number, promptCount: number, codexCount: number): void {
  workspaceEntryList.innerHTML = "";
  appendWorkspaceNavigationCard(
    "config",
    "open-config",
    state.workspaceSettingsLevel === "config",
    t("workspace.configCardTitle"),
    t("workspace.briefGeneric"),
    configCount,
  );
  appendWorkspaceNavigationCard(
    "prompt",
    "open-prompt",
    state.workspaceSettingsLevel === "prompt",
    t("workspace.promptCardTitle"),
    t("workspace.briefAITools"),
    promptCount,
  );
  appendWorkspaceNavigationCard(
    "codex",
    "open-codex",
    state.workspaceSettingsLevel === "codex",
    t("workspace.codexCardTitle"),
    t("workspace.briefCodex"),
    codexCount,
  );
}

function setProviderAPIKeyVisibility(visible: boolean): void {
  state.providerAPIKeyVisible = visible;
  modelsProviderAPIKeyInput.type = visible ? "text" : "password";
  modelsProviderAPIKeyVisibilityButton.classList.toggle("is-active", visible);
  modelsProviderAPIKeyVisibilityButton.setAttribute("aria-pressed", String(visible));
}

function renderProviderBaseURLPreview(): void {
  const base = modelsProviderBaseURLInput.value.trim().replace(/\/+$/g, "");
  modelsProviderBaseURLPreview.textContent = base === "" ? "/responses" : `${base}/responses`;
}

function setModelsSettingsLevel(level: ModelsSettingsLevel): void {
  const canShowEdit =
    level === "edit" &&
    (state.providerModal.open ||
      (state.providers.some((provider) => provider.id === state.selectedProviderID) && state.selectedProviderID !== ""));
  state.modelsSettingsLevel = canShowEdit ? "edit" : "list";
  const showEdit = state.modelsSettingsLevel === "edit";
  modelsLevel1View.hidden = showEdit;
  modelsLevel2View.hidden = !showEdit;
  modelsSettingsSection.classList.toggle("is-level2-active", showEdit);
}

function renderModelsSettingsLevel(): void {
  if (state.providerModal.open && state.providerModal.mode === "create") {
    modelsEditProviderMeta.textContent = t("models.addProvider");
  } else {
    const selected = state.providers.find((provider) => provider.id === state.selectedProviderID);
    modelsEditProviderMeta.textContent = selected ? formatProviderLabel(selected) : "";
  }
  setModelsSettingsLevel(state.modelsSettingsLevel);
}

function syncSelectedProviderID(): void {
  if (state.providers.length === 0) {
    state.selectedProviderID = "";
    return;
  }
  if (state.providers.some((provider) => provider.id === state.selectedProviderID)) {
    return;
  }
  if (state.activeLLM.provider_id !== "" && state.providers.some((provider) => provider.id === state.activeLLM.provider_id)) {
    state.selectedProviderID = state.activeLLM.provider_id;
    return;
  }
  state.selectedProviderID = state.providers[0].id;
}

function renderProviderNavigation(): void {
  modelsProviderList.innerHTML = "";
  if (state.providers.length === 0) {
    appendEmptyItem(modelsProviderList, t("models.emptyProviders"));
    return;
  }

  for (const provider of state.providers) {
    const entry = document.createElement("li");
    entry.className = "models-provider-card-entry";

    const button = document.createElement("button");
    button.type = "button";
    button.className = "models-provider-card";
    if (provider.id === state.selectedProviderID) {
      button.classList.add("is-selected");
    }
    button.dataset.providerAction = "select";
    button.dataset.providerId = provider.id;
    button.setAttribute("aria-pressed", String(provider.id === state.selectedProviderID));

    const title = document.createElement("span");
    title.className = "models-provider-card-title";
    title.textContent = formatProviderLabel(provider);

    const enabledMeta = document.createElement("span");
    enabledMeta.className = "models-provider-card-meta";
    enabledMeta.textContent = t("models.enabledLine", {
      enabled: provider.enabled === false ? t("common.no") : t("common.yes"),
    });

    const keyMeta = document.createElement("span");
    keyMeta.className = "models-provider-card-meta";
    keyMeta.textContent = provider.has_api_key ? t("models.apiKeyConfigured", { value: t("models.apiKeyMasked") }) : t("models.apiKeyUnset");

    const providerTypeMeta = document.createElement("span");
    providerTypeMeta.className = "models-provider-card-meta";
    providerTypeMeta.textContent = t("models.providerTypeLine", {
      providerType: providerTypeDisplayName(resolveProviderType(provider)),
    });

    const deleteButton = document.createElement("button");
    const deleteLabel = t("models.deleteProvider");
    deleteButton.type = "button";
    deleteButton.className = "models-provider-card-delete chat-delete-btn";
    deleteButton.dataset.providerAction = "delete";
    deleteButton.dataset.providerId = provider.id;
    deleteButton.setAttribute("aria-label", deleteLabel);
    deleteButton.title = deleteLabel;
    deleteButton.innerHTML = TRASH_ICON_SVG;

    button.append(title, enabledMeta, keyMeta, providerTypeMeta);
    entry.append(button, deleteButton);
    modelsProviderList.appendChild(entry);
  }
}

function renderProviderTypeOptions(selectedType?: string): void {
  const options = state.providerTypes.length > 0 ? state.providerTypes : fallbackProviderTypes(state.providers);
  if (options.length === 0) {
    modelsProviderTypeSelect.innerHTML = "";
    syncCustomSelect(modelsProviderTypeSelect);
    return;
  }
  const requestedType = normalizeProviderTypeValue(selectedType ?? modelsProviderTypeSelect.value);
  const hasRequestedType = requestedType ? options.some((item) => item.id === requestedType) : false;
  const activeType = hasRequestedType ? requestedType : options[0].id;
  modelsProviderTypeSelect.innerHTML = "";
  for (const option of options) {
    const element = document.createElement("option");
    element.value = option.id;
    element.textContent = option.display_name;
    modelsProviderTypeSelect.appendChild(element);
  }
  if (requestedType && !hasRequestedType) {
    const selectedOption = document.createElement("option");
    selectedOption.value = requestedType;
    selectedOption.textContent = requestedType;
    modelsProviderTypeSelect.appendChild(selectedOption);
  }
  modelsProviderTypeSelect.value = activeType;
  syncCustomSelect(modelsProviderTypeSelect);
}

function normalizeProviderTypeValue(value: string): string {
  const normalized = value.trim().toLowerCase();
  if (normalized === "openai-compatible") {
    return normalized;
  }
  if (normalized === "openai") {
    return normalized;
  }
  return normalized;
}

function normalizeProviderIDValue(value: string): string {
  return value.trim().toLowerCase();
}

function slugifyProviderID(value: string): string {
  return normalizeProviderIDValue(value)
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function ensureUniqueProviderID(baseProviderID: string): string {
  const base = normalizeProviderIDValue(baseProviderID);
  if (base === "") {
    return "";
  }
  const existing = new Set(
    state.providers
      .map((provider) => normalizeProviderIDValue(provider.id))
      .filter((providerID) => providerID !== ""),
  );
  if (!existing.has(base)) {
    return base;
  }
  let suffix = 2;
  while (existing.has(`${base}-${suffix}`)) {
    suffix += 1;
  }
  return `${base}-${suffix}`;
}

function resolveProviderIDForUpsert(selectedProviderType: string): string {
  if (state.providerModal.mode === "edit" && state.providerModal.editingProviderID !== "") {
    return state.providerModal.editingProviderID;
  }
  if (selectedProviderType === "") {
    return "";
  }
  if (selectedProviderType === "openai") {
    return ensureUniqueProviderID("openai");
  }
  const baseProviderID = slugifyProviderID(modelsProviderNameInput.value) || slugifyProviderID(selectedProviderType) || "provider";
  return ensureUniqueProviderID(baseProviderID);
}

function resolveOpenAIDuplicateModelAliases(): Record<string, string> {
  const modelIDs = (state.providers.find((provider) => provider.id === "openai")?.models ?? [])
    .map((model) => model.id.trim())
    .filter((modelID) => modelID !== "");
  const source = modelIDs.length > 0 ? modelIDs : DEFAULT_OPENAI_MODEL_IDS;
  const out: Record<string, string> = {};
  for (const modelID of source) {
    out[modelID] = modelID;
  }
  return out;
}

function providerSupportsCustomModels(providerTypeID: string): boolean {
  const normalized = normalizeProviderTypeValue(providerTypeID);
  return normalized !== "";
}

function syncProviderCustomModelsField(providerTypeID: string): void {
  const enabled = providerSupportsCustomModels(providerTypeID);
  modelsProviderCustomModelsField.hidden = false;
  modelsProviderCustomModelsAddButton.disabled = !enabled;
  for (const input of Array.from(modelsProviderCustomModelsRows.querySelectorAll<HTMLInputElement>("input[data-custom-model-input=\"true\"]"))) {
    input.disabled = !enabled;
  }
  if (!enabled) {
    resetProviderCustomModelsEditor();
  }
}

function isProviderAutoSaveEnabled(): boolean {
  return state.providerModal.open && state.providerModal.mode === "edit" && state.providerModal.editingProviderID !== "";
}

function resetProviderAutoSaveScheduling(): void {
  if (providerAutoSaveTimer !== null) {
    window.clearTimeout(providerAutoSaveTimer);
    providerAutoSaveTimer = null;
  }
  providerAutoSaveQueued = false;
}

function scheduleProviderAutoSave(): void {
  if (!isProviderAutoSaveEnabled()) {
    return;
  }
  if (providerAutoSaveTimer !== null) {
    window.clearTimeout(providerAutoSaveTimer);
  }
  providerAutoSaveTimer = window.setTimeout(() => {
    providerAutoSaveTimer = null;
    void flushProviderAutoSave();
  }, PROVIDER_AUTO_SAVE_DELAY_MS);
}

async function flushProviderAutoSave(): Promise<void> {
  if (!isProviderAutoSaveEnabled()) {
    return;
  }
  if (providerAutoSaveInFlight) {
    providerAutoSaveQueued = true;
    return;
  }

  providerAutoSaveInFlight = true;
  try {
    await upsertProvider({
      closeAfterSave: false,
      notifyStatus: false,
    });
  } finally {
    providerAutoSaveInFlight = false;
    if (providerAutoSaveQueued) {
      providerAutoSaveQueued = false;
      scheduleProviderAutoSave();
    }
  }
}

function resolveProviderType(provider: ProviderInfo): string {
  if (provider.id === "openai") {
    return "openai";
  }
  if (provider.openai_compatible) {
    return "openai-compatible";
  }
  return provider.id;
}

function fallbackProviderTypes(providers: ProviderInfo[]): ProviderTypeInfo[] {
  const seen = new Set<string>();
  const out: ProviderTypeInfo[] = [];

  const pushType = (id: string, displayName: string): void => {
    const normalized = normalizeProviderTypeValue(id);
    if (normalized === "" || seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    out.push({
      id: normalized,
      display_name: displayName.trim() || providerTypeDisplayName(normalized),
    });
  };

  pushType("openai", t("models.providerTypeOpenAI"));
  pushType("openai-compatible", t("models.providerTypeOpenAICompatible"));

  for (const provider of providers) {
    if (provider.id === "openai") {
      pushType("openai", t("models.providerTypeOpenAI"));
      continue;
    }
    if (provider.openai_compatible) {
      pushType("openai-compatible", t("models.providerTypeOpenAICompatible"));
      continue;
    }
    pushType(provider.id, provider.display_name || provider.name || provider.id);
  }

  return out;
}

function normalizeProviderTypes(providerTypesRaw?: ProviderTypeInfo[]): ProviderTypeInfo[] {
  if (!Array.isArray(providerTypesRaw)) {
    return fallbackProviderTypes([]);
  }
  const seen = new Set<string>();
  const out: ProviderTypeInfo[] = [];
  for (const item of providerTypesRaw) {
    const id = normalizeProviderTypeValue(item?.id ?? "");
    if (id === "" || seen.has(id)) {
      continue;
    }
    seen.add(id);
    const displayName = (item?.display_name ?? "").trim() || providerTypeDisplayName(id);
    out.push({
      id,
      display_name: displayName,
    });
  }
  if (out.length === 0) {
    return fallbackProviderTypes([]);
  }
  return out;
}

function providerTypeDisplayName(providerTypeID: string): string {
  if (providerTypeID === "openai") {
    return t("models.providerTypeOpenAI");
  }
  if (providerTypeID === "openai-compatible") {
    return t("models.providerTypeOpenAICompatible");
  }
  return providerTypeID;
}

function resetProviderModalForm(): void {
  renderProviderTypeOptions("openai");
  modelsProviderTypeSelect.disabled = false;
  modelsProviderNameInput.value = "";
  modelsProviderAPIKeyInput.value = "";
  modelsProviderBaseURLInput.value = "";
  modelsProviderTimeoutMSInput.value = "";
  modelsProviderEnabledInput.checked = true;
  resetProviderKVEditor(modelsProviderHeadersRows, "headers");
  resetProviderKVEditor(modelsProviderAliasesRows, "aliases");
  resetProviderCustomModelsEditor();
  syncProviderCustomModelsField("openai");
  setProviderAPIKeyVisibility(true);
  renderProviderBaseURLPreview();
}

function openProviderModal(mode: "create" | "edit", providerID = ""): void {
  resetProviderAutoSaveScheduling();
  state.providerModal.mode = mode;
  state.providerModal.open = true;
  state.providerModal.editingProviderID = providerID;

  if (mode === "create") {
    resetProviderModalForm();
    modelsProviderModalTitle.textContent = t("models.addProviderTitle");
  } else {
    state.selectedProviderID = providerID;
    modelsProviderModalTitle.textContent = t("models.editProviderTitle");
    populateProviderForm(providerID);
  }
  setModelsSettingsLevel("edit");
  renderModelsSettingsLevel();
  if (mode === "create") {
    modelsProviderTypeSelect.focus();
  } else {
    modelsProviderNameInput.focus();
  }
}

function closeProviderModal(): void {
  resetProviderAutoSaveScheduling();
  state.providerModal.open = false;
  state.providerModal.editingProviderID = "";
  setProviderAPIKeyVisibility(true);
  setModelsSettingsLevel("list");
  renderModelsSettingsLevel();
}

function populateProviderForm(providerID: string): void {
  const provider = state.providers.find((item) => item.id === providerID);
  if (!provider) {
    setStatus(t("status.providerNotFound", { providerId: providerID }), "error");
    return;
  }
  renderProviderTypeOptions(resolveProviderType(provider));
  modelsProviderTypeSelect.disabled = true;
  modelsProviderNameInput.value = provider.display_name ?? provider.name ?? provider.id;
  modelsProviderAPIKeyInput.value = "";
  modelsProviderBaseURLInput.value = provider.current_base_url ?? "";
  modelsProviderEnabledInput.checked = provider.enabled !== false;
  modelsProviderTimeoutMSInput.value = typeof provider.timeout_ms === "number" ? String(provider.timeout_ms) : "";
  populateProviderHeaderRows(provider);
  populateProviderAliasRows(provider);
  populateProviderCustomModelsRows(provider);
  syncProviderCustomModelsField(resolveProviderType(provider));
  setProviderAPIKeyVisibility(true);
  renderProviderBaseURLPreview();
  setStatus(t("status.providerLoadedForEdit", { providerId: provider.id }), "info");
}

async function upsertProvider(options: UpsertProviderOptions = {}): Promise<boolean> {
  const closeAfterSave = options.closeAfterSave ?? true;
  const notifyStatus = options.notifyStatus ?? true;
  if (closeAfterSave) {
    resetProviderAutoSaveScheduling();
  }
  syncControlState();
  const selectedProviderType = normalizeProviderTypeValue(modelsProviderTypeSelect.value);
  const providerID = resolveProviderIDForUpsert(selectedProviderType);
  if (providerID === "") {
    if (notifyStatus) {
      setStatus(t("error.providerTypeRequired"), "error");
    }
    return false;
  }

  let timeoutMS = 0;
  const timeoutRaw = modelsProviderTimeoutMSInput.value.trim();
  if (timeoutRaw !== "") {
    const parsed = Number.parseInt(timeoutRaw, 10);
    if (Number.isNaN(parsed) || parsed < 0) {
      if (notifyStatus) {
        setStatus(t("error.providerTimeoutInvalid"), "error");
      }
      return false;
    }
    timeoutMS = parsed;
  }

  let headers: Record<string, string> | undefined;
  try {
    headers = collectProviderKVMap(modelsProviderHeadersRows, {
      invalidKey: t("error.invalidProviderHeadersKey"),
      invalidValue: (key) => t("error.invalidProviderHeadersValue", { key }),
    });
  } catch (error) {
    if (notifyStatus) {
      setStatus(asErrorMessage(error), "error");
    }
    return false;
  }

  let aliases: Record<string, string> | undefined;
  try {
    aliases = collectProviderKVMap(modelsProviderAliasesRows, {
      invalidKey: t("error.invalidProviderAliasesKey"),
      invalidValue: (key) => t("error.invalidProviderAliasesValue", { key }),
    });
  } catch (error) {
    if (notifyStatus) {
      setStatus(asErrorMessage(error), "error");
    }
    return false;
  }

  const customModelsEnabled =
    state.providerModal.mode === "create"
      ? providerSupportsCustomModels(selectedProviderType)
      : providerSupportsCustomModels(providerID);
  let customModels: string[] | undefined;
  if (customModelsEnabled) {
    try {
      customModels = collectCustomModelIDs(modelsProviderCustomModelsRows);
    } catch (error) {
      if (notifyStatus) {
        setStatus(asErrorMessage(error), "error");
      }
      return false;
    }
  }

  const payload: Record<string, unknown> = {
    enabled: modelsProviderEnabledInput.checked,
    display_name: modelsProviderNameInput.value.trim(),
  };
  const apiKey = modelsProviderAPIKeyInput.value.trim();
  if (apiKey !== "") {
    payload.api_key = apiKey;
  }
  const baseURL = modelsProviderBaseURLInput.value.trim();
  if (baseURL !== "") {
    payload.base_url = baseURL;
  }
  payload.timeout_ms = timeoutMS;
  payload.headers = headers ?? {};
  const mergedAliases: Record<string, string> = {};
  if (aliases) {
    Object.assign(mergedAliases, aliases);
  }
  if (customModels) {
    for (const modelID of customModels) {
      if (mergedAliases[modelID] === undefined) {
        mergedAliases[modelID] = modelID;
      }
    }
  }
  if (
    state.providerModal.mode === "create" &&
    selectedProviderType === "openai" &&
    providerID !== "openai" &&
    Object.keys(mergedAliases).length === 0
  ) {
    Object.assign(mergedAliases, resolveOpenAIDuplicateModelAliases());
  }
  payload.model_aliases = mergedAliases;

  try {
    const out = await requestJSON<ProviderInfo>(`/models/${encodeURIComponent(providerID)}/config`, {
      method: "PUT",
      body: payload,
    });
    state.selectedProviderID = out.id ?? providerID;
    if (closeAfterSave) {
      setModelsSettingsLevel("list");
    }
    await refreshModels({ silent: !notifyStatus });
    if (closeAfterSave) {
      closeProviderModal();
      modelsProviderAPIKeyInput.value = "";
    }
    if (notifyStatus) {
      setStatus(
        t(state.providerModal.mode === "create" ? "status.providerCreated" : "status.providerUpdated", {
          providerId: out.id,
        }),
        "info",
      );
    }
    return true;
  } catch (error) {
    if (notifyStatus) {
      setStatus(asErrorMessage(error), "error");
    }
    return false;
  }
}

async function deleteProvider(providerID: string): Promise<void> {
  if (!window.confirm(t("models.deleteProviderConfirm", { providerId: providerID }))) {
    return;
  }
  syncControlState();
  try {
    const out = await requestJSON<DeleteResult>(`/models/${encodeURIComponent(providerID)}`, {
      method: "DELETE",
    });
    await refreshModels();
    if (state.providerModal.open && state.providerModal.editingProviderID === providerID) {
      closeProviderModal();
    }
    setStatus(t(out.deleted ? "status.providerDeleted" : "status.providerDeleteSkipped", { providerId: providerID }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function resetProviderKVEditor(container: HTMLElement, kind: ProviderKVKind): void {
  container.innerHTML = "";
  appendProviderKVRow(container, kind);
}

function resetProviderCustomModelsEditor(): void {
  modelsProviderCustomModelsRows.innerHTML = "";
  appendCustomModelRow(modelsProviderCustomModelsRows);
}

function collectProviderModelAliases(provider: ProviderInfo): Map<string, string> {
  const aliases = new Map<string, string>();
  for (const [alias, target] of Object.entries(provider.model_aliases ?? {})) {
    const aliasID = alias.trim();
    const targetID = target.trim();
    if (aliasID === "" || targetID === "") {
      continue;
    }
    aliases.set(aliasID, targetID);
  }
  for (const model of provider.models) {
    const alias = model.id.trim();
    if (alias === "" || aliases.has(alias)) {
      continue;
    }
    const target = (model.alias_of ?? "").trim();
    if (target !== "") {
      aliases.set(alias, target);
      continue;
    }
    if (!BUILTIN_PROVIDER_IDS.has(provider.id)) {
      aliases.set(alias, alias);
    }
  }
  return aliases;
}

function populateProviderHeaderRows(provider: ProviderInfo): void {
  const headerEntries = Object.entries(provider.headers ?? {})
    .map(([key, value]) => [key.trim(), value.trim()] as const)
    .filter(([key, value]) => key !== "" && value !== "")
    .sort(([left], [right]) => left.localeCompare(right));

  modelsProviderHeadersRows.innerHTML = "";
  if (headerEntries.length === 0) {
    appendProviderKVRow(modelsProviderHeadersRows, "headers");
    return;
  }
  for (const [key, value] of headerEntries) {
    appendProviderKVRow(modelsProviderHeadersRows, "headers", key, value);
  }
}

function populateProviderAliasRows(provider: ProviderInfo): void {
  const aliases = collectProviderModelAliases(provider);
  const aliasEntries = Array.from(aliases.entries())
    .filter(([alias, target]) => BUILTIN_PROVIDER_IDS.has(provider.id) || alias !== target)
    .sort(([left], [right]) => left.localeCompare(right));

  modelsProviderAliasesRows.innerHTML = "";
  if (aliasEntries.length === 0) {
    appendProviderKVRow(modelsProviderAliasesRows, "aliases");
    return;
  }
  for (const [alias, target] of aliasEntries) {
    appendProviderKVRow(modelsProviderAliasesRows, "aliases", alias, target);
  }
}

function populateProviderCustomModelsRows(provider: ProviderInfo): void {
  resetProviderCustomModelsEditor();

  const customModelIDs = Array.from(collectProviderModelAliases(provider).entries())
    .filter(([alias, target]) => alias === target)
    .map(([alias]) => alias)
    .sort((left, right) => left.localeCompare(right));
  if (customModelIDs.length === 0) {
    return;
  }

  modelsProviderCustomModelsRows.innerHTML = "";
  for (const modelID of customModelIDs) {
    appendCustomModelRow(modelsProviderCustomModelsRows, modelID);
  }
}

function appendProviderKVRow(container: HTMLElement, kind: ProviderKVKind, key = "", value = ""): void {
  const row = document.createElement("div");
  row.className = "kv-row";

  const keyInput = document.createElement("input");
  keyInput.type = "text";
  keyInput.className = "kv-key-input";
  keyInput.value = key;
  keyInput.setAttribute("data-kv-field", "key");
  keyInput.setAttribute("data-i18n-placeholder", "models.kvKeyPlaceholder");
  keyInput.placeholder = t("models.kvKeyPlaceholder");

  const valueInput = document.createElement("input");
  valueInput.type = "text";
  valueInput.className = "kv-value-input";
  valueInput.value = value;
  valueInput.setAttribute("data-kv-field", "value");
  valueInput.setAttribute("data-i18n-placeholder", kind === "headers" ? "models.kvHeaderValuePlaceholder" : "models.kvAliasValuePlaceholder");
  valueInput.placeholder = kind === "headers" ? t("models.kvHeaderValuePlaceholder") : t("models.kvAliasValuePlaceholder");

  const removeButton = document.createElement("button");
  removeButton.type = "button";
  removeButton.className = "secondary-btn";
  removeButton.setAttribute("data-i18n", "models.removeKVRow");
  removeButton.textContent = t("models.removeKVRow");
  removeButton.dataset.kvRemove = "true";

  row.append(keyInput, valueInput, removeButton);
  container.appendChild(row);
}

function appendCustomModelRow(container: HTMLElement, modelID = ""): void {
  const row = document.createElement("div");
  row.className = "custom-model-row";

  const modelInput = document.createElement("input");
  modelInput.type = "text";
  modelInput.className = "custom-model-input";
  modelInput.value = modelID;
  modelInput.setAttribute("data-custom-model-input", "true");
  modelInput.setAttribute("data-i18n-placeholder", "models.customModelPlaceholder");
  modelInput.placeholder = t("models.customModelPlaceholder");

  const removeButton = document.createElement("button");
  removeButton.type = "button";
  removeButton.className = "secondary-btn";
  removeButton.setAttribute("data-i18n", "models.removeKVRow");
  removeButton.textContent = t("models.removeKVRow");
  removeButton.dataset.customModelRemove = "true";

  row.append(modelInput, removeButton);
  container.appendChild(row);
}

function collectCustomModelIDs(container: HTMLElement): string[] | undefined {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const input of Array.from(container.querySelectorAll<HTMLInputElement>("input[data-custom-model-input=\"true\"]"))) {
    const modelID = input.value.trim();
    if (modelID === "" || seen.has(modelID)) {
      continue;
    }
    seen.add(modelID);
    out.push(modelID);
  }
  if (out.length === 0) {
    return undefined;
  }
  return out;
}

function collectProviderKVMap(
  container: HTMLElement,
  messages: {
    invalidKey: string;
    invalidValue: (key: string) => string;
  },
): Record<string, string> | undefined {
  const out: Record<string, string> = {};

  for (const row of Array.from(container.querySelectorAll<HTMLElement>(".kv-row"))) {
    const keyInput = row.querySelector<HTMLInputElement>("input[data-kv-field=\"key\"]");
    const valueInput = row.querySelector<HTMLInputElement>("input[data-kv-field=\"value\"]");
    if (!keyInput || !valueInput) {
      continue;
    }
    const key = keyInput.value.trim();
    const value = valueInput.value.trim();
    if (key === "" && value === "") {
      continue;
    }
    if (key === "") {
      throw new Error(messages.invalidKey);
    }
    if (value === "") {
      throw new Error(messages.invalidValue(key));
    }
    out[key] = value;
  }

  if (Object.keys(out).length === 0) {
    return undefined;
  }
  return out;
}

function defaultQQChannelConfig(): QQChannelConfig {
  return {
    enabled: false,
    app_id: "",
    client_secret: "",
    bot_prefix: "",
    target_type: "c2c",
    target_id: "",
    api_base: DEFAULT_QQ_API_BASE,
    token_url: DEFAULT_QQ_TOKEN_URL,
    timeout_seconds: DEFAULT_QQ_TIMEOUT_SECONDS,
  };
}

function normalizeQQTargetType(raw: unknown): QQTargetType {
  const value = typeof raw === "string" ? raw.trim().toLowerCase() : "";
  if (value === "group") {
    return "group";
  }
  if (value === "guild" || value === "channel" || value === "dm") {
    return "guild";
  }
  return "c2c";
}

function normalizeQQChannelConfig(raw: unknown): QQChannelConfig {
  const fallback = defaultQQChannelConfig();
  const parsed = toRecord(raw);
  if (!parsed) {
    return fallback;
  }
  return {
    enabled: parsed.enabled === true || parsed.enabled === "true" || parsed.enabled === 1,
    app_id: typeof parsed.app_id === "string" ? parsed.app_id.trim() : "",
    client_secret: typeof parsed.client_secret === "string" ? parsed.client_secret.trim() : "",
    bot_prefix: typeof parsed.bot_prefix === "string" ? parsed.bot_prefix : "",
    target_type: normalizeQQTargetType(parsed.target_type),
    target_id: typeof parsed.target_id === "string" ? parsed.target_id.trim() : "",
    api_base: typeof parsed.api_base === "string" && parsed.api_base.trim() !== "" ? parsed.api_base.trim() : fallback.api_base,
    token_url: typeof parsed.token_url === "string" && parsed.token_url.trim() !== "" ? parsed.token_url.trim() : fallback.token_url,
    timeout_seconds: parseIntegerInput(String(parsed.timeout_seconds ?? ""), fallback.timeout_seconds, 1),
  };
}

function normalizeURLForCompare(raw: string): string {
  return raw.trim().toLowerCase().replace(/\/+$/, "");
}

function resolveQQAPIEnvironment(apiBase: string): QQAPIEnvironment {
  const normalized = normalizeURLForCompare(apiBase);
  if (normalized === normalizeURLForCompare(QQ_SANDBOX_API_BASE)) {
    return "sandbox";
  }
  return "production";
}

function resolveQQAPIBase(environment: QQAPIEnvironment): string {
  if (environment === "sandbox") {
    return QQ_SANDBOX_API_BASE;
  }
  return DEFAULT_QQ_API_BASE;
}

function renderQQChannelConfig(): void {
  const cfg = state.qqChannelConfig;
  const available = state.qqChannelAvailable;

  qqChannelEnabledInput.checked = cfg.enabled;
  qqChannelAppIDInput.value = cfg.app_id;
  qqChannelClientSecretInput.value = cfg.client_secret;
  qqChannelBotPrefixInput.value = cfg.bot_prefix;
  qqChannelTargetTypeSelect.value = cfg.target_type;
  qqChannelAPIEnvironmentSelect.value = resolveQQAPIEnvironment(cfg.api_base);
  qqChannelTimeoutSecondsInput.value = String(cfg.timeout_seconds);

  const controls: Array<HTMLInputElement | HTMLSelectElement | HTMLButtonElement> = [
    qqChannelEnabledInput,
    qqChannelAppIDInput,
    qqChannelClientSecretInput,
    qqChannelBotPrefixInput,
    qqChannelTargetTypeSelect,
    qqChannelAPIEnvironmentSelect,
    qqChannelTimeoutSecondsInput,
  ];
  for (const control of controls) {
    control.disabled = !available;
  }
  syncCustomSelect(qqChannelTargetTypeSelect);
  syncCustomSelect(qqChannelAPIEnvironmentSelect);
}

async function refreshQQChannelConfig(options: { silent?: boolean } = {}): Promise<void> {
  try {
    const raw = await requestJSON<unknown>("/config/channels/qq");
    state.qqChannelConfig = normalizeQQChannelConfig(raw);
    state.qqChannelAvailable = true;
    state.tabLoaded.channels = true;
    renderChannelsPanel();
    if (!options.silent) {
      setStatus(t("status.qqChannelLoaded"), "info");
    }
  } catch (error) {
    state.qqChannelConfig = defaultQQChannelConfig();
    state.qqChannelAvailable = false;
    state.tabLoaded.channels = false;
    renderChannelsPanel();
    if (!options.silent) {
      setStatus(asErrorMessage(error), "error");
    }
  }
}

function collectQQChannelFormConfig(): QQChannelConfig {
  const apiEnvironment: QQAPIEnvironment = qqChannelAPIEnvironmentSelect.value === "sandbox" ? "sandbox" : "production";
  return {
    enabled: qqChannelEnabledInput.checked,
    app_id: qqChannelAppIDInput.value.trim(),
    client_secret: qqChannelClientSecretInput.value.trim(),
    bot_prefix: qqChannelBotPrefixInput.value,
    target_type: normalizeQQTargetType(qqChannelTargetTypeSelect.value),
    target_id: state.qqChannelConfig.target_id,
    api_base: resolveQQAPIBase(apiEnvironment),
    token_url: state.qqChannelConfig.token_url || DEFAULT_QQ_TOKEN_URL,
    timeout_seconds: parseIntegerInput(qqChannelTimeoutSecondsInput.value, DEFAULT_QQ_TIMEOUT_SECONDS, 1),
  };
}

async function saveQQChannelConfig(): Promise<void> {
  syncControlState();
  if (!state.qqChannelAvailable) {
    setStatus(t("error.qqChannelUnavailable"), "error");
    return;
  }
  const payload = collectQQChannelFormConfig();
  try {
    const out = await requestJSON<unknown>("/config/channels/qq", {
      method: "PUT",
      body: payload,
    });
    state.qqChannelConfig = normalizeQQChannelConfig(out ?? payload);
    state.qqChannelAvailable = true;
    setChannelsSettingsLevel("list");
    renderChannelsPanel();
    setStatus(t("status.qqChannelSaved"), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

async function refreshWorkspace(options: { silent?: boolean } = {}): Promise<void> {
  syncControlState();
  try {
    const files = await listWorkspaceFiles();
    state.workspaceFiles = files;
    pruneWorkspaceCodexExpandedFolders(files);
    if (state.activeWorkspacePath !== "" && !files.some((file) => file.path === state.activeWorkspacePath)) {
      clearWorkspaceSelection();
    }
    renderWorkspacePanel();
    state.tabLoaded.workspace = true;
    if (!options.silent) {
      setStatus(t("status.workspaceFilesLoaded", { count: files.length }), "info");
    }
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

function renderWorkspacePanel(): void {
  renderWorkspaceFiles();
  renderWorkspaceEditor();
  setWorkspaceSettingsLevel(state.workspaceSettingsLevel);
}

function renderWorkspaceFiles(): void {
  const { configFiles, promptFiles, codexFiles } = splitWorkspaceFiles(state.workspaceFiles);
  renderWorkspaceNavigation(configFiles.length, promptFiles.length, codexFiles.length);
  renderWorkspaceFileRows(workspaceFilesBody, configFiles, t("workspace.emptyConfig"));
  renderWorkspaceFileRows(workspacePromptsBody, promptFiles, t("workspace.emptyPrompt"));
  renderWorkspaceCodexTree(workspaceCodexTreeBody, codexFiles, t("workspace.emptyCodex"));
}

function splitWorkspaceFiles(
  files: WorkspaceFileInfo[],
): { configFiles: WorkspaceFileInfo[]; promptFiles: WorkspaceFileInfo[]; codexFiles: WorkspaceFileInfo[] } {
  const configFiles: WorkspaceFileInfo[] = [];
  const promptFiles: WorkspaceFileInfo[] = [];
  const codexFiles: WorkspaceFileInfo[] = [];
  for (const file of files) {
    if (isWorkspaceCodexFile(file)) {
      codexFiles.push(file);
      continue;
    }
    if (isWorkspacePromptFile(file)) {
      promptFiles.push(file);
      continue;
    }
    configFiles.push(file);
  }
  return { configFiles, promptFiles, codexFiles };
}

function renderWorkspaceCodexTree(targetBody: HTMLUListElement, files: WorkspaceFileInfo[], emptyText: string): void {
  targetBody.innerHTML = "";
  const tree = buildWorkspaceCodexTree(files);
  if (tree.length === 0) {
    appendEmptyItem(targetBody, emptyText);
    return;
  }
  for (const node of tree) {
    appendWorkspaceCodexFolderNode(targetBody, node, 0);
  }
}

function buildWorkspaceCodexTree(files: WorkspaceFileInfo[]): WorkspaceCodexTreeNode[] {
  type MutableNode = {
    name: string;
    path: string;
    folders: Map<string, MutableNode>;
    files: WorkspaceFileInfo[];
  };
  const root: MutableNode = {
    name: "",
    path: "",
    folders: new Map<string, MutableNode>(),
    files: [],
  };

  for (const file of files) {
    if (!isWorkspaceCodexFile(file)) {
      continue;
    }
    const normalizedPath = normalizeWorkspaceInputPath(file.path);
    const relativePath = normalizedPath.slice(WORKSPACE_CODEX_PREFIX.length);
    const parts = relativePath.split("/").filter((part) => part !== "");
    if (parts.length === 0) {
      continue;
    }
    const fileName = parts.pop() ?? "";
    if (fileName === "") {
      continue;
    }
    let cursor = root;
    let folderPath = "";
    for (const part of parts) {
      folderPath = folderPath === "" ? part : `${folderPath}/${part}`;
      let next = cursor.folders.get(part);
      if (!next) {
        next = {
          name: part,
          path: folderPath,
          folders: new Map<string, MutableNode>(),
          files: [],
        };
        cursor.folders.set(part, next);
      }
      cursor = next;
    }
    cursor.files.push(file);
  }

  const freezeTree = (node: MutableNode): WorkspaceCodexTreeNode => {
    const folders = Array.from(node.folders.values())
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((child) => freezeTree(child));
    const sortedFiles = [...node.files].sort((a, b) => a.path.localeCompare(b.path));
    return {
      name: node.name,
      path: node.path,
      folders,
      files: sortedFiles,
    };
  };

  return Array.from(root.folders.values())
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((node) => freezeTree(node));
}

function appendWorkspaceCodexFolderNode(targetBody: HTMLUListElement, node: WorkspaceCodexTreeNode, depth: number): void {
  const entry = document.createElement("li");
  entry.className = "workspace-codex-tree-node workspace-codex-tree-folder";

  const toggleButton = document.createElement("button");
  toggleButton.type = "button";
  toggleButton.className = "workspace-codex-folder-toggle";
  toggleButton.dataset.workspaceFolderToggle = node.path;
  toggleButton.dataset.workspaceFolderPath = node.path;
  toggleButton.dataset.workspaceFolderDepth = String(depth + 1);

  const expanded = isWorkspaceCodexFolderExpanded(node.path);
  toggleButton.classList.toggle("is-expanded", expanded);
  toggleButton.setAttribute("aria-expanded", String(expanded));

  const prefix = document.createElement("span");
  prefix.className = "workspace-codex-folder-prefix";
  prefix.textContent = expanded ? "▾" : "▸";

  const title = document.createElement("span");
  title.className = "workspace-codex-folder-title mono";
  title.textContent = node.name;

  const countMeta = document.createElement("span");
  countMeta.className = "workspace-codex-folder-meta";
  countMeta.textContent = t("workspace.cardFileCount", { count: countWorkspaceCodexNodeFiles(node) });

  toggleButton.append(prefix, title, countMeta);
  entry.appendChild(toggleButton);

  const children = document.createElement("ul");
  children.className = "workspace-codex-tree-children";
  children.hidden = !expanded;

  for (const folder of node.folders) {
    appendWorkspaceCodexFolderNode(children, folder, depth + 1);
  }
  for (const file of node.files) {
    const fileEntry = document.createElement("li");
    fileEntry.className = "workspace-codex-tree-node workspace-codex-tree-file";

    const fileButton = document.createElement("button");
    fileButton.type = "button";
    fileButton.className = "workspace-codex-file-open";
    fileButton.dataset.workspaceOpen = file.path;
    if (file.path === state.activeWorkspacePath) {
      fileButton.classList.add("is-selected");
    }
    const fileName = file.path.split("/").pop() ?? file.path;
    fileButton.textContent = fileName;
    fileButton.title = file.path;
    fileEntry.appendChild(fileButton);
    children.appendChild(fileEntry);
  }

  if (children.childElementCount > 0) {
    entry.appendChild(children);
  }
  targetBody.appendChild(entry);
}

function countWorkspaceCodexNodeFiles(node: WorkspaceCodexTreeNode): number {
  let count = node.files.length;
  for (const folder of node.folders) {
    count += countWorkspaceCodexNodeFiles(folder);
  }
  return count;
}

function isWorkspaceCodexFolderExpanded(path: string): boolean {
  return state.workspaceCodexExpandedFolders.has(path);
}

function toggleWorkspaceCodexFolder(path: string): void {
  if (state.workspaceCodexExpandedFolders.has(path)) {
    state.workspaceCodexExpandedFolders.delete(path);
  } else {
    state.workspaceCodexExpandedFolders.add(path);
  }
  renderWorkspaceFiles();
}

function pruneWorkspaceCodexExpandedFolders(files: WorkspaceFileInfo[]): void {
  const validPaths = new Set<string>();
  const topLevelPaths = new Set<string>();
  for (const file of files) {
    if (!isWorkspaceCodexFile(file)) {
      continue;
    }
    const normalizedPath = normalizeWorkspaceInputPath(file.path);
    const relativePath = normalizedPath.slice(WORKSPACE_CODEX_PREFIX.length);
    const parts = relativePath.split("/").filter((part) => part !== "");
    let folderPath = "";
    for (let index = 0; index < parts.length - 1; index += 1) {
      folderPath = folderPath === "" ? parts[index] : `${folderPath}/${parts[index]}`;
      if (index === 0) {
        topLevelPaths.add(folderPath);
      }
      validPaths.add(folderPath);
    }
  }
  for (const path of Array.from(state.workspaceCodexExpandedFolders)) {
    if (!validPaths.has(path)) {
      state.workspaceCodexExpandedFolders.delete(path);
    }
  }
  if (state.workspaceCodexExpandedFolders.size === 0) {
    for (const topPath of topLevelPaths) {
      state.workspaceCodexExpandedFolders.add(topPath);
    }
  }
}

function renderWorkspaceFileRows(
  targetBody: HTMLUListElement,
  files: WorkspaceFileInfo[],
  emptyText: string,
): void {
  targetBody.innerHTML = "";
  if (files.length === 0) {
    appendEmptyItem(targetBody, emptyText);
    return;
  }

  files.forEach((file) => {
    const entry = document.createElement("li");
    entry.className = "models-provider-card-entry workspace-file-card-entry";

    const openButton = document.createElement("button");
    openButton.type = "button";
    openButton.className = "models-provider-card workspace-file-open-card";
    openButton.dataset.workspaceOpen = file.path;
    if (file.path === state.activeWorkspacePath) {
      openButton.classList.add("is-selected");
    }
    openButton.setAttribute("aria-pressed", String(file.path === state.activeWorkspacePath));

    const pathTitle = document.createElement("span");
    pathTitle.className = "models-provider-card-title mono workspace-file-card-path";
    pathTitle.textContent = file.path;

    const summaryMeta = document.createElement("span");
    summaryMeta.className = "models-provider-card-meta";
    summaryMeta.textContent = resolveWorkspaceFileSummary(file);

    const sizeMeta = document.createElement("span");
    sizeMeta.className = "models-provider-card-meta";
    sizeMeta.textContent = `${t("workspace.size")}: ${file.size === null ? t("common.none") : String(file.size)}`;

    openButton.append(pathTitle, summaryMeta, sizeMeta);

    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    const deleteLabel = t("workspace.deleteFile");
    deleteButton.className = "models-provider-card-delete chat-delete-btn workspace-file-card-delete";
    deleteButton.dataset.workspaceDelete = file.path;
    deleteButton.setAttribute("aria-label", deleteLabel);
    deleteButton.title = deleteLabel;
    deleteButton.innerHTML = TRASH_ICON_SVG;
    deleteButton.disabled = file.kind !== "skill";

    entry.append(openButton, deleteButton);
    targetBody.appendChild(entry);
  });
}

function resolveWorkspaceFileSummary(file: WorkspaceFileInfo): string {
  const path = normalizeWorkspacePathKey(file.path);
  if (path === "config/envs.json") {
    return t("workspace.briefEnvs");
  }
  if (path === "config/channels.json") {
    return t("workspace.briefChannels");
  }
  if (path === "config/models.json") {
    return t("workspace.briefModels");
  }
  if (path === "config/active-llm.json") {
    return t("workspace.briefActiveLLM");
  }
  if (file.kind === "skill") {
    return t("workspace.briefSkill");
  }
  if (path.startsWith(WORKSPACE_CODEX_PREFIX)) {
    return t("workspace.briefCodex");
  }
  if (path.startsWith("docs/ai/") || path.startsWith("prompts/") || path.startsWith("prompt/")) {
    return t("workspace.briefAITools");
  }
  return t("workspace.briefGeneric");
}

function isWorkspaceCodexFile(file: WorkspaceFileInfo): boolean {
  return normalizeWorkspacePathKey(file.path).startsWith(WORKSPACE_CODEX_PREFIX);
}

function isWorkspacePromptFile(file: WorkspaceFileInfo): boolean {
  const path = normalizeWorkspacePathKey(file.path);
  if (path.startsWith(WORKSPACE_CODEX_PREFIX)) {
    return false;
  }
  if (file.kind === "skill") {
    return true;
  }
  if (path.startsWith("docs/ai/") && (path.endsWith(".md") || path.endsWith(".markdown"))) {
    return true;
  }
  return path.startsWith("prompts/") || path.startsWith("prompt/");
}

function renderWorkspaceEditor(): void {
  const hasActiveFile = state.activeWorkspacePath !== "";
  const canDelete = hasActiveFile && isWorkspaceSkillPath(state.activeWorkspacePath);
  workspaceFilePathInput.value = state.activeWorkspacePath;
  workspaceFileContentInput.value = state.activeWorkspaceContent;
  workspaceFileContentInput.disabled = !hasActiveFile;
  workspaceSaveFileButton.disabled = !hasActiveFile;
  workspaceDeleteFileButton.disabled = !canDelete;
}

async function createWorkspaceFile(): Promise<void> {
  syncControlState();
  const path = normalizeWorkspaceInputPath(workspaceNewPathInput.value);
  if (path === "") {
    setStatus(t("error.workspacePathRequired"), "error");
    return;
  }
  if (!isWorkspaceSkillPath(path)) {
    setStatus(t("error.workspaceCreateOnlySkill"), "error");
    return;
  }

  try {
    await putWorkspaceFile(path, createWorkspaceSkillTemplate(path));
    workspaceNewPathInput.value = "";
    state.activeWorkspacePath = path;
    state.activeWorkspaceContent = JSON.stringify(createWorkspaceSkillTemplate(path), null, 2);
    state.activeWorkspaceMode = "json";
    await refreshWorkspace({ silent: true });
    setWorkspaceEditorModalOpen(true);
    setStatus(t("status.workspaceFileCreated", { path }), "info");
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

async function openWorkspaceFile(path: string, options: { silent?: boolean } = {}): Promise<void> {
  syncControlState();
  try {
    const payload = await getWorkspaceFile(path);
    state.activeWorkspacePath = path;
    const prepared = prepareWorkspaceEditorPayload(payload);
    state.activeWorkspaceContent = prepared.content;
    state.activeWorkspaceMode = prepared.mode;
    renderWorkspacePanel();
    setWorkspaceEditorModalOpen(true);
    if (!options.silent) {
      setStatus(t("status.workspaceFileLoaded", { path }), "info");
    }
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

async function saveWorkspaceFile(): Promise<void> {
  syncControlState();
  const path = normalizeWorkspaceInputPath(workspaceFilePathInput.value);
  if (path === "") {
    setStatus(t("error.workspacePathRequired"), "error");
    return;
  }

  let payload: unknown;
  if (state.activeWorkspaceMode === "text") {
    payload = { content: workspaceFileContentInput.value };
  } else {
    try {
      payload = JSON.parse(workspaceFileContentInput.value);
    } catch {
      setStatus(t("error.workspaceInvalidJSON"), "error");
      return;
    }
  }
  try {
    await putWorkspaceFile(path, payload);
    state.activeWorkspacePath = path;
    const prepared = prepareWorkspaceEditorPayload(payload);
    state.activeWorkspaceContent = prepared.content;
    state.activeWorkspaceMode = prepared.mode;
    await refreshWorkspace({ silent: true });
    if (isSystemPromptWorkspacePath(path)) {
      invalidateSystemPromptTokensCacheAndReload();
    }
    setStatus(t("status.workspaceFileSaved", { path }), "info");
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

async function deleteWorkspaceFile(path: string): Promise<void> {
  syncControlState();
  const confirmed = window.confirm(t("workspace.deleteFileConfirm", { path }));
  if (!confirmed) {
    return;
  }

  try {
    await deleteWorkspaceFileRequest(path);
    if (state.activeWorkspacePath === path) {
      clearWorkspaceSelection();
    }
    await refreshWorkspace({ silent: true });
    setStatus(t("status.workspaceFileDeleted", { path }), "info");
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

async function importWorkspaceJSON(): Promise<void> {
  syncControlState();
  const raw = workspaceJSONInput.value.trim();
  if (raw === "") {
    setStatus(t("error.workspaceJSONRequired"), "error");
    return;
  }

  let payload: unknown;
  try {
    payload = JSON.parse(raw);
  } catch {
    setStatus(t("error.workspaceInvalidJSON"), "error");
    return;
  }

  try {
    await requestJSON<unknown>("/workspace/import", {
      method: "POST",
      body:
        payload && typeof payload === "object" && "mode" in (payload as Record<string, unknown>)
          ? payload
          : {
              mode: "replace",
              payload,
            },
    });
    clearWorkspaceSelection();
    await refreshWorkspace({ silent: true });
    setWorkspaceImportModalOpen(false);
    setStatus(t("status.workspaceImportDone"), "info");
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

async function listWorkspaceFiles(): Promise<WorkspaceFileInfo[]> {
  const raw = await requestJSON<unknown>("/workspace/files");
  return normalizeWorkspaceFiles(raw);
}

async function getWorkspaceFile(path: string): Promise<unknown> {
  return requestJSON<unknown>(`/workspace/files/${encodeURIComponent(path)}`);
}

async function putWorkspaceFile(path: string, payload: unknown): Promise<void> {
  await requestJSON<unknown>(`/workspace/files/${encodeURIComponent(path)}`, {
    method: "PUT",
    body: payload,
  });
}

async function deleteWorkspaceFileRequest(path: string): Promise<void> {
  await requestJSON<unknown>(`/workspace/files/${encodeURIComponent(path)}`, {
    method: "DELETE",
  });
}

function normalizeWorkspaceFiles(raw: unknown): WorkspaceFileInfo[] {
  const rows: unknown[] = [];
  if (Array.isArray(raw)) {
    rows.push(...raw);
  } else if (raw && typeof raw === "object") {
    const obj = raw as Record<string, unknown>;
    if (Array.isArray(obj.files)) {
      rows.push(...obj.files);
    } else if (obj.files && typeof obj.files === "object") {
      rows.push(...Object.entries(obj.files as Record<string, unknown>).map(([path, value]) => ({ path, value })));
    }
    if (Array.isArray(obj.items)) {
      rows.push(...obj.items);
    }
    if (rows.length === 0) {
      rows.push(...Object.entries(obj).map(([path, value]) => ({ path, value })));
    }
  }

  const byPath = new Map<string, WorkspaceFileInfo>();
  for (const row of rows) {
    let path = "";
    let kind: "config" | "skill" = "config";
    let size: number | null = null;

    if (typeof row === "string") {
      path = row.trim();
    } else if (row && typeof row === "object") {
      const item = row as Record<string, unknown>;
      if (typeof item.path === "string") {
        path = item.path.trim();
      } else if (typeof item.name === "string") {
        path = item.name.trim();
      } else if (typeof item.file === "string") {
        path = item.file.trim();
      }

      if (typeof item.size === "number" && Number.isFinite(item.size)) {
        size = item.size;
      } else if (typeof item.bytes === "number" && Number.isFinite(item.bytes)) {
        size = item.bytes;
      }

      if (item.kind === "skill") {
        kind = "skill";
      } else if (item.kind === "config") {
        kind = "config";
      }
    }

    if (path === "") {
      continue;
    }
    if (kind === "config" && path.startsWith("skills/") && path.endsWith(".json")) {
      kind = "skill";
    }
    const prev = byPath.get(path);
    if (!prev || (prev.size === null && size !== null)) {
      byPath.set(path, { path, kind, size });
    }
  }

  return Array.from(byPath.values()).sort((a, b) => a.path.localeCompare(b.path));
}

function clearWorkspaceSelection(): void {
  state.activeWorkspacePath = "";
  state.activeWorkspaceContent = "";
  state.activeWorkspaceMode = "json";
}

function prepareWorkspaceEditorPayload(payload: unknown): { content: string; mode: WorkspaceEditorMode } {
  const textPayload = asWorkspaceTextPayload(payload);
  if (textPayload) {
    return {
      content: textPayload.content,
      mode: "text",
    };
  }
  return {
    content: JSON.stringify(payload, null, 2),
    mode: "json",
  };
}

function asWorkspaceTextPayload(payload: unknown): WorkspaceTextPayload | null {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return null;
  }
  const record = payload as Record<string, unknown>;
  const keys = Object.keys(record);
  if (keys.length !== 1 || keys[0] !== "content") {
    return null;
  }
  return typeof record.content === "string" ? { content: record.content } : null;
}

function normalizeWorkspaceInputPath(path: string): string {
  return path.trim().replace(/^\/+/, "");
}

function isWorkspaceSkillPath(path: string): boolean {
  if (!path.startsWith("skills/") || !path.endsWith(".json")) {
    return false;
  }
  const name = path.slice("skills/".length, path.length - ".json".length).trim();
  return name !== "" && !name.includes("/");
}

function createWorkspaceSkillTemplate(path: string): Record<string, unknown> {
  const normalized = normalizeWorkspaceInputPath(path);
  const name = normalized.slice("skills/".length, normalized.length - ".json".length).trim();
  if (name === "") {
    throw new Error(t("error.workspacePathRequired"));
  }
  return {
    name,
    content: "# new skill",
    source: "customized",
    references: {},
    scripts: {},
    enabled: true,
  };
}

function initCronWorkflowEditor(): void {
  cronWorkflowEditor = new CronWorkflowCanvas({
    viewport: cronWorkflowViewport,
    canvas: cronWorkflowCanvas,
    edgesLayer: cronWorkflowEdges,
    nodesLayer: cronWorkflowNodes,
    nodeEditor: cronWorkflowNodeEditor,
    zoomLabel: cronWorkflowZoom,
    onStatus: (message, tone) => {
      setStatus(message, tone);
    },
  });
}

function syncCronTaskModeUI(): void {
  const mode: "text" | "workflow" = cronTaskTypeSelect.value === "text" ? "text" : "workflow";
  state.cronDraftTaskType = mode;
  if (mode !== "workflow" && isCronWorkflowFullscreenActive()) {
    void exitCronWorkflowFullscreen();
  }
  cronTextSection.classList.toggle("is-hidden", mode !== "text");
  cronWorkflowSection.classList.toggle("is-hidden", mode !== "workflow");
  cronTextSection.setAttribute("aria-hidden", String(mode !== "text"));
  cronWorkflowSection.setAttribute("aria-hidden", String(mode !== "workflow"));
  refreshCronModalTitles();
  syncCustomSelect(cronTaskTypeSelect);
}

function refreshCronModalTitles(): void {
  const createMode = state.cronModal.mode === "create";
  const workflowMode = state.cronDraftTaskType === "workflow";
  const titleKey = createMode
    ? (workflowMode ? "cron.createWorkflowJob" : "cron.createTextJob")
    : (workflowMode ? "cron.updateWorkflowJob" : "cron.updateTextJob");
  const submitKey = createMode ? "cron.submitCreate" : "cron.submitUpdate";
  cronCreateModalTitle.textContent = t(titleKey);
  cronSubmitButton.textContent = t(submitKey);
}

function renderCronExecutionDetails(stateValue: CronJobState | undefined): void {
  cronWorkflowExecutionList.innerHTML = "";
  const execution = stateValue?.last_execution;
  if (!execution || !Array.isArray(execution.nodes) || execution.nodes.length === 0) {
    const empty = document.createElement("li");
    empty.className = "hint";
    empty.textContent = t("cron.executionEmpty");
    cronWorkflowExecutionList.appendChild(empty);
    return;
  }

  execution.nodes.forEach((item) => {
    const row = document.createElement("li");
    row.className = "cron-execution-item";
    row.dataset.status = item.status;
    const summary = document.createElement("div");
    summary.className = "cron-execution-summary";
    summary.textContent = t("cron.executionSummary", {
      nodeId: item.node_id,
      nodeType: formatCronWorkflowNodeType(item.node_type),
      status: formatCronWorkflowNodeStatus(item.status),
    });
    row.appendChild(summary);
    if (item.error && item.error.trim() !== "") {
      const err = document.createElement("div");
      err.className = "cron-execution-error";
      err.textContent = item.error;
      row.appendChild(err);
    }
    cronWorkflowExecutionList.appendChild(row);
  });
}

function syncCronDispatchHint(): void {
  cronDispatchHint.textContent = t("workspace.dispatchHint", {
    userId: state.userId,
    channel: state.channel,
  });
}

function ensureCronSessionID(): void {
  if (cronSessionIDInput.value.trim() === "") {
    cronSessionIDInput.value = newSessionID();
  }
}

function openCronEditModal(jobID: string): void {
  const job = state.cronJobs.find((item) => item.id === jobID);
  if (!job) {
    setStatus(t("error.cronJobNotFound", { jobId: jobID }), "error");
    return;
  }

  state.cronDraftTaskType = job.task_type === "text" ? "text" : "workflow";
  cronTaskTypeSelect.value = state.cronDraftTaskType;
  setCronModalMode("edit", jobID);

  cronIDInput.value = job.id;
  cronNameInput.value = job.name;
  cronIntervalInput.value = job.schedule.cron ?? "";
  cronSessionIDInput.value = job.dispatch.target.session_id ?? "";
  cronMaxConcurrencyInput.value = String(job.runtime.max_concurrency ?? 1);
  cronTimeoutInput.value = String(job.runtime.timeout_seconds ?? 30);
  cronMisfireInput.value = String(job.runtime.misfire_grace_seconds ?? 0);
  if (state.cronDraftTaskType === "text") {
    cronTextInput.value = job.text ?? "";
  } else {
    const loadedWorkflow = job.workflow ?? createDefaultCronWorkflow();
    const issue = validateCronWorkflowSpec(loadedWorkflow);
    if (issue) {
      setStatus(t("error.cronWorkflowInvalid", { reason: issue }), "error");
      cronWorkflowEditor?.setWorkflow(createDefaultCronWorkflow());
    } else {
      cronWorkflowEditor?.setWorkflow(loadedWorkflow);
    }
  }

  renderCronExecutionDetails(state.cronStates[jobID]);
  syncCronDispatchHint();
  setCronCreateModalOpen(true);
}

async function refreshCronJobs(): Promise<void> {
  syncControlState();
  syncCronDispatchHint();
  ensureCronSessionID();

  try {
    const jobs = await requestJSON<CronJobSpec[]>("/cron/jobs");
    state.cronJobs = jobs;

    const statePairs = await Promise.all(
      jobs.map(async (job) => {
        try {
          const jobState = await requestJSON<CronJobState>(`/cron/jobs/${encodeURIComponent(job.id)}/state`);
          return [job.id, jobState] as const;
        } catch {
          return [job.id, null] as const;
        }
      }),
    );

    const stateMap: Record<string, CronJobState> = {};
    for (const [jobID, jobState] of statePairs) {
      if (jobState) {
        stateMap[jobID] = jobState;
      }
    }

    state.cronStates = stateMap;
    state.tabLoaded.cron = true;
    renderCronJobs();
    if (state.cronModal.mode === "edit") {
      renderCronExecutionDetails(state.cronStates[state.cronModal.editingJobID]);
    }
    setStatus(t("status.cronJobsLoaded", { count: jobs.length }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function renderCronJobs(): void {
  cronJobsBody.innerHTML = "";
  if (state.cronJobs.length === 0) {
    const entry = document.createElement("li");
    entry.className = "cron-job-card-entry";
    const card = document.createElement("article");
    card.className = "cron-job-card cron-job-card-empty";
    card.textContent = t("cron.empty");
    entry.appendChild(card);
    cronJobsBody.appendChild(entry);
    return;
  }

  state.cronJobs.forEach((job) => {
    const entry = document.createElement("li");
    entry.className = "cron-job-card-entry";

    const card = document.createElement("article");
    card.className = "cron-job-card";

    const jobState = state.cronStates[job.id];
    const nextRun = jobState?.next_run_at;
    const statusText = formatCronStatus(jobState);

    const head = document.createElement("div");
    head.className = "cron-job-card-head";

    const title = document.createElement("h4");
    title.className = "cron-job-card-title";
    title.textContent = job.name.trim() === "" ? job.id : job.name;

    const enabled = document.createElement("label");
    enabled.className = "cron-job-card-enabled";
    const enabledLabel = document.createElement("span");
    enabledLabel.textContent = t("cron.enabled");
    const enabledToggle = document.createElement("input");
    enabledToggle.type = "checkbox";
    enabledToggle.className = "cron-enabled-toggle-input";
    enabledToggle.checked = job.enabled;
    enabledToggle.dataset.cronToggleEnabled = job.id;
    enabledToggle.setAttribute("aria-label", `${t("cron.enabled")} ${job.id}`);
    enabled.append(enabledLabel, enabledToggle);
    head.append(title, enabled);

    const metaList = document.createElement("ul");
    metaList.className = "detail-list cron-job-card-meta-list";
    metaList.append(
      createCronJobCardMetaItem(t("cron.id"), job.id, { mono: true }),
      createCronJobCardMetaItem(t("cron.type"), job.task_type),
      createCronJobCardMetaItem(t("cron.nextRun"), nextRun ? compactTime(nextRun) : t("common.none")),
      createCronJobCardMetaItem(t("cron.status"), statusText, { title: jobState?.last_error }),
    );

    const actions = document.createElement("div");
    actions.className = "actions-row cron-job-card-actions";

    const runBtn = document.createElement("button");
    runBtn.type = "button";
    runBtn.className = "secondary-btn";
    runBtn.dataset.cronRun = job.id;
    runBtn.textContent = t("cron.run");

    const editBtn = document.createElement("button");
    editBtn.type = "button";
    editBtn.className = "secondary-btn";
    editBtn.dataset.cronEdit = job.id;
    editBtn.textContent = t("cron.edit");

    const deleteBtn = document.createElement("button");
    deleteBtn.type = "button";
    deleteBtn.className = "danger-btn";
    deleteBtn.dataset.cronDelete = job.id;
    deleteBtn.textContent = t("cron.delete");
    if (isDefaultCronJob(job)) {
      deleteBtn.disabled = true;
      deleteBtn.title = t("cron.deleteDisabledDefault");
    }

    actions.append(runBtn, editBtn, deleteBtn);

    card.append(head, metaList, actions);
    entry.appendChild(card);
    cronJobsBody.appendChild(entry);
  });
}

function createCronJobCardMetaItem(
  label: string,
  value: string,
  options: { mono?: boolean; title?: string } = {},
): HTMLLIElement {
  const row = document.createElement("li");
  row.className = "cron-job-card-meta-row";

  const key = document.createElement("span");
  key.className = "cron-job-card-meta-key";
  key.textContent = label;

  const valueSpan = document.createElement("span");
  valueSpan.className = "cron-job-card-meta-value";
  if (options.mono) {
    valueSpan.classList.add("mono");
  }
  valueSpan.textContent = value;
  if (options.title) {
    valueSpan.title = options.title;
  }

  row.append(key, valueSpan);
  return row;
}

function formatCronStatus(stateValue: CronJobState | undefined): string {
  if (!stateValue?.last_status) {
    return t("common.none");
  }
  const normalized = stateValue.last_status.trim().toLowerCase();
  if (normalized === "running") {
    return t("cron.statusRunning");
  }
  if (normalized === "succeeded") {
    return t("cron.statusSucceeded");
  }
  if (normalized === "failed") {
    return t("cron.statusFailed");
  }
  if (normalized === "paused") {
    return t("cron.statusPaused");
  }
  if (normalized === "resumed") {
    return t("cron.statusResumed");
  }
  return stateValue.last_status;
}

function formatCronWorkflowNodeType(value: CronWorkflowNodeExecution["node_type"]): string {
  if (value === "text_event") {
    return t("cron.nodeTypeTextEvent");
  }
  if (value === "delay") {
    return t("cron.nodeTypeDelay");
  }
  if (value === "if_event") {
    return t("cron.nodeTypeIfEvent");
  }
  return value;
}

function formatCronWorkflowNodeStatus(value: CronWorkflowNodeExecution["status"]): string {
  if (value === "succeeded") {
    return t("cron.statusSucceeded");
  }
  if (value === "failed") {
    return t("cron.statusFailed");
  }
  if (value === "skipped") {
    return t("cron.statusSkipped");
  }
  return value;
}

function isDefaultCronJob(job: CronJobSpec): boolean {
  if (job.id === DEFAULT_CRON_JOB_ID) {
    return true;
  }
  const marker = job.meta?.[CRON_META_SYSTEM_DEFAULT];
  return marker === true || marker === "true";
}

async function saveCronJob(): Promise<boolean> {
  syncControlState();

  const id = cronIDInput.value.trim();
  const name = cronNameInput.value.trim();
  const intervalText = cronIntervalInput.value.trim();
  const sessionID = cronSessionIDInput.value.trim();
  const text = cronTextInput.value.trim();
  const taskType: "text" | "workflow" = cronTaskTypeSelect.value === "text" ? "text" : "workflow";
  state.cronDraftTaskType = taskType;

  if (id === "" || name === "") {
    setStatus(t("error.cronIdNameRequired"), "error");
    return false;
  }
  if (intervalText === "") {
    setStatus(t("error.cronScheduleRequired"), "error");
    return false;
  }
  if (sessionID === "") {
    setStatus(t("error.cronSessionRequired"), "error");
    return false;
  }
  if (taskType === "text" && text === "") {
    setStatus(t("error.cronTextRequired"), "error");
    return false;
  }

  let workflowPayload: CronWorkflowSpec | undefined;
  if (taskType === "workflow") {
    if (!cronWorkflowEditor) {
      setStatus(t("error.cronWorkflowEditorMissing"), "error");
      return false;
    }
    workflowPayload = cronWorkflowEditor.getWorkflow();
    const issue = validateCronWorkflowSpec(workflowPayload);
    if (issue) {
      setStatus(t("error.cronWorkflowInvalid", { reason: issue }), "error");
      return false;
    }
  }

  const maxConcurrency = parseIntegerInput(cronMaxConcurrencyInput.value, 1, 1);
  const timeoutSeconds = parseIntegerInput(cronTimeoutInput.value, 30, 1);
  const misfireGraceSeconds = parseIntegerInput(cronMisfireInput.value, 0, 0);
  const editing = state.cronModal.mode === "edit";
  const existingJob = state.cronJobs.find((job) => job.id === state.cronModal.editingJobID);

  const payload: CronJobSpec = {
    id,
    name,
    enabled: existingJob?.enabled ?? true,
    schedule: {
      type: existingJob?.schedule.type ?? "interval",
      cron: intervalText,
      timezone: existingJob?.schedule.timezone ?? "",
    },
    task_type: taskType,
    text: taskType === "text" ? text : undefined,
    workflow: taskType === "workflow" ? workflowPayload : undefined,
    dispatch: {
      type: existingJob?.dispatch.type ?? "channel",
      channel: state.channel,
      target: {
        user_id: state.userId,
        session_id: sessionID,
      },
      mode: existingJob?.dispatch.mode ?? "",
      meta: existingJob?.dispatch.meta ?? {},
    },
    runtime: {
      max_concurrency: maxConcurrency,
      timeout_seconds: timeoutSeconds,
      misfire_grace_seconds: misfireGraceSeconds,
    },
    meta: existingJob?.meta ?? {},
  };

  try {
    if (editing) {
      await requestJSON<CronJobSpec>(`/cron/jobs/${encodeURIComponent(id)}`, {
        method: "PUT",
        body: payload,
      });
    } else {
      await requestJSON<CronJobSpec>("/cron/jobs", {
        method: "POST",
        body: payload,
      });
    }
    await refreshCronJobs();
    if (!editing) {
      setCronModalMode("edit", id);
    }
    renderCronExecutionDetails(state.cronStates[id]);
    setStatus(t(editing ? "status.cronUpdated" : "status.cronCreated", { jobId: id }), "info");
    return true;
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return false;
  }
}

async function updateCronJobEnabled(jobID: string, enabled: boolean): Promise<boolean> {
  const existingJob = state.cronJobs.find((job) => job.id === jobID);
  if (!existingJob) {
    setStatus(t("error.cronJobNotFound", { jobId: jobID }), "error");
    return false;
  }
  const payload: CronJobSpec = {
    ...existingJob,
    enabled,
    schedule: {
      type: existingJob.schedule.type ?? "interval",
      cron: existingJob.schedule.cron ?? "",
      timezone: existingJob.schedule.timezone ?? "",
    },
    dispatch: {
      type: existingJob.dispatch.type ?? "channel",
      channel: existingJob.dispatch.channel ?? state.channel,
      target: {
        user_id: existingJob.dispatch.target.user_id ?? state.userId,
        session_id: existingJob.dispatch.target.session_id ?? "",
      },
      mode: existingJob.dispatch.mode ?? "",
      meta: existingJob.dispatch.meta ?? {},
    },
    runtime: {
      max_concurrency: existingJob.runtime.max_concurrency ?? 1,
      timeout_seconds: existingJob.runtime.timeout_seconds ?? 30,
      misfire_grace_seconds: existingJob.runtime.misfire_grace_seconds ?? 0,
    },
    meta: existingJob.meta ?? {},
  };

  if (payload.dispatch.target.session_id.trim() === "") {
    setStatus(t("error.cronSessionRequired"), "error");
    return false;
  }

  try {
    await requestJSON<CronJobSpec>(`/cron/jobs/${encodeURIComponent(jobID)}`, {
      method: "PUT",
      body: payload,
    });
    await refreshCronJobs();
    return true;
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return false;
  }
}

async function deleteCronJob(jobID: string): Promise<void> {
  syncControlState();
  const job = state.cronJobs.find((item) => item.id === jobID);
  if ((job && isDefaultCronJob(job)) || jobID === DEFAULT_CRON_JOB_ID) {
    setStatus(t("error.cronDeleteDefaultProtected", { jobId: jobID }), "error");
    return;
  }
  if (!window.confirm(t("cron.deleteConfirm", { jobId: jobID }))) {
    return;
  }

  try {
    const result = await requestJSON<DeleteResult>(`/cron/jobs/${encodeURIComponent(jobID)}`, {
      method: "DELETE",
    });
    await refreshCronJobs();
    if (state.cronModal.mode === "edit" && state.cronModal.editingJobID === jobID) {
      setCronCreateModalOpen(false);
      setCronModalMode("create");
      renderCronExecutionDetails(undefined);
    }
    const messageKey = result.deleted ? "status.cronDeleted" : "status.cronDeleteSkipped";
    setStatus(t(messageKey, { jobId: jobID }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

async function runCronJob(jobID: string): Promise<void> {
  syncControlState();
  try {
    const result = await requestJSON<{ started: boolean }>(`/cron/jobs/${encodeURIComponent(jobID)}/run`, {
      method: "POST",
    });
    await refreshCronJobs();
    await reloadChats();
    setStatus(t("status.cronRunRequested", { jobId: jobID, started: String(result.started) }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

async function requestJSON<T>(path: string, options: JSONRequestOptions = {}): Promise<T> {
  const response = await fetch(toAbsoluteURL(path), buildRequestInit(options));

  if (!response.ok) {
    throw new Error(await readErrorMessage(response));
  }

  const raw = await response.text();
  if (raw.trim() === "") {
    return null as T;
  }

  try {
    return JSON.parse(raw) as T;
  } catch {
    throw new Error(
      t("error.invalidJSONResponse", {
        snippet: raw.slice(0, 180),
      }),
    );
  }
}

function buildRequestInit(options: JSONRequestOptions): RequestInit {
  const headers = new Headers(options.headers ?? {});
  if (!headers.has("accept")) {
    headers.set("accept", "application/json");
  }
  if (!headers.has("accept-language")) {
    headers.set("accept-language", getLocale());
  }
  applyAuthHeaders(headers);
  applyRequestSourceHeader(headers);

  let body: BodyInit | undefined;
  if (options.body !== undefined) {
    if (options.body instanceof FormData) {
      body = options.body;
    } else {
      headers.set("content-type", "application/json");
      body = JSON.stringify(options.body);
    }
  }

  return {
    method: options.method ?? "GET",
    headers,
    body,
  };
}

function applyAuthHeaders(headers: Headers): void {
  if (headers.has("x-api-key") || headers.has("authorization")) {
    return;
  }
  if (state.apiKey !== "") {
    headers.set("X-API-Key", state.apiKey);
  }
}

function applyRequestSourceHeader(headers: Headers): void {
  if (!headers.has(REQUEST_SOURCE_HEADER)) {
    headers.set(REQUEST_SOURCE_HEADER, REQUEST_SOURCE_WEB);
  }
}

async function readErrorMessage(response: Response): Promise<string> {
  const fallback = t("error.requestFailed", { status: response.status });
  try {
    const raw = await response.text();
    return parseErrorMessage(raw, response.status, fallback);
  } catch {
    return fallback;
  }
}

function parseIntegerInput(raw: string, fallback: number, min: number): number {
  const trimmed = raw.trim();
  if (trimmed === "") {
    return fallback;
  }
  const parsed = Number.parseInt(trimmed, 10);
  if (Number.isNaN(parsed)) {
    return fallback;
  }
  if (parsed < min) {
    return min;
  }
  return parsed;
}

function normalizeProviders(providers: ProviderInfo[]): ProviderInfo[] {
  return providers.map((provider) => ({
    ...provider,
    name: provider.name?.trim() || provider.id,
    display_name: provider.display_name?.trim() || provider.name?.trim() || provider.id,
    openai_compatible: provider.openai_compatible ?? false,
    models: Array.isArray(provider.models) ? provider.models : [],
    headers: normalizeProviderHeadersMap(provider.headers),
    timeout_ms: normalizeProviderTimeoutMS(provider.timeout_ms),
    model_aliases: normalizeProviderAliasMap(provider.model_aliases),
    enabled: provider.enabled ?? true,
    current_api_key: provider.current_api_key ?? "",
    current_base_url: provider.current_base_url ?? "",
  }));
}

function normalizeProviderHeadersMap(raw: unknown): Record<string, string> | undefined {
  const parsed = toRecord(raw);
  if (!parsed) {
    return undefined;
  }
  const headers: Record<string, string> = {};
  for (const [key, value] of Object.entries(parsed)) {
    const headerKey = key.trim();
    const headerValue = typeof value === "string" ? value.trim() : "";
    if (headerKey === "" || headerValue === "") {
      continue;
    }
    headers[headerKey] = headerValue;
  }
  if (Object.keys(headers).length === 0) {
    return undefined;
  }
  return headers;
}

function normalizeProviderTimeoutMS(raw: unknown): number | undefined {
  if (typeof raw !== "number" || !Number.isFinite(raw) || raw < 0) {
    return undefined;
  }
  return Math.trunc(raw);
}

function normalizeProviderAliasMap(raw: unknown): Record<string, string> | undefined {
  const parsed = toRecord(raw);
  if (!parsed) {
    return undefined;
  }
  const aliases: Record<string, string> = {};
  for (const [key, value] of Object.entries(parsed)) {
    const alias = key.trim();
    const target = typeof value === "string" ? value.trim() : "";
    if (alias === "" || target === "") {
      continue;
    }
    aliases[alias] = target;
  }
  if (Object.keys(aliases).length === 0) {
    return undefined;
  }
  return aliases;
}

function formatProviderLabel(provider: ProviderInfo): string {
  return provider.display_name?.trim() || provider.name?.trim() || provider.id;
}

function normalizeDefaults(defaults: Record<string, string>, providers: ProviderInfo[]): Record<string, string> {
  const normalized: Record<string, string> = {};
  for (const [providerID, modelID] of Object.entries(defaults ?? {})) {
    if (providerID.trim() === "" || modelID.trim() === "") {
      continue;
    }
    normalized[providerID] = modelID;
  }
  for (const provider of providers) {
    if (normalized[provider.id]) {
      continue;
    }
    if (provider.models.length > 0) {
      normalized[provider.id] = provider.models[0].id;
    }
  }
  return normalized;
}

function buildDefaultMapFromProviders(providers: ProviderInfo[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const provider of providers) {
    if (provider.models.length > 0) {
      out[provider.id] = provider.models[0].id;
    }
  }
  return out;
}

function normalizeModelSlot(raw?: ModelSlotConfig): ModelSlotConfig {
  return {
    provider_id: raw?.provider_id?.trim() ?? "",
    model: raw?.model?.trim() ?? "",
  };
}

function formatModelEntry(model: ModelInfo): string {
  return model.id.trim() || (model.name ?? "").trim();
}

function formatCapabilities(capabilities?: ModelCapabilities): string {
  if (!capabilities) {
    return "";
  }
  const tags: string[] = [];
  if (capabilities.temperature) {
    tags.push("temp");
  }
  if (capabilities.reasoning) {
    tags.push("reason");
  }
  if (capabilities.attachment) {
    tags.push("attach");
  }
  if (capabilities.tool_call) {
    tags.push("tool");
  }
  const input = formatModalities(capabilities.input);
  if (input !== "") {
    tags.push(`in:${input}`);
  }
  const output = formatModalities(capabilities.output);
  if (output !== "") {
    tags.push(`out:${output}`);
  }
  return tags.join("|");
}

function formatModalities(modalities?: ModelModalities): string {
  if (!modalities) {
    return "";
  }
  const out: string[] = [];
  if (modalities.text) {
    out.push("text");
  }
  if (modalities.image) {
    out.push("image");
  }
  if (modalities.audio) {
    out.push("audio");
  }
  if (modalities.video) {
    out.push("video");
  }
  if (modalities.pdf) {
    out.push("pdf");
  }
  return out.join("+");
}

function appendEmptyItem(list: HTMLElement, text: string): void {
  const item = document.createElement("li");
  item.className = "message-empty";
  item.textContent = text;
  list.appendChild(item);
}

function setStatus(message: string, tone: Tone = "neutral"): void {
  statusLine.textContent = message;
  statusLine.classList.remove("error", "info");
  if (tone === "error" || tone === "info") {
    statusLine.classList.add(tone);
  }
  const payload = {
    tone,
    message,
    at: new Date().toISOString(),
  };
  if (tone === "error") {
    console.error("[NextAI][status]", payload);
    return;
  }
  console.log("[NextAI][status]", payload);
}

function logComposerStatusToConsole(): void {
  console.log("[NextAI][chat-composer-status]", {
    statusLocal: t("chat.statusLocal"),
    statusFullAccess: t("chat.statusFullAccess"),
  });
}

function toRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function parsePositiveInteger(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) {
    const n = Math.trunc(value);
    return n > 0 ? n : undefined;
  }
  if (typeof value === "string") {
    const parsed = Number.parseInt(value.trim(), 10);
    if (Number.isFinite(parsed) && parsed > 0) {
      return parsed;
    }
  }
  return undefined;
}

function buildToolCallNoticeFromRaw(raw: string): ViewToolCallNotice | null {
  const normalized = raw.trim();
  if (normalized === "") {
    return null;
  }
  let summary = t("chat.toolCallNotice", { target: "tool" });
  let detail = normalized;
  let toolName = "";
  let outputReady = true;
  let step: number | undefined;
  try {
    const payload = JSON.parse(normalized) as AgentStreamEvent;
    step = parsePositiveInteger(payload.step);
    if (payload.type === "tool_call") {
      summary = formatToolCallSummary(payload.tool_call);
      toolName = normalizeToolName(payload.tool_call?.name);
      if (toolName === "shell") {
        detail = t("chat.toolCallOutputUnavailable");
      }
      outputReady = toolName === "shell";
    } else if (payload.type === "tool_result") {
      toolName = normalizeToolName(payload.tool_result?.name);
      if (toolName === "shell") {
        summary = "bash";
        detail = formatToolResultOutput(payload.tool_result);
      }
      outputReady = true;
    } else {
      summary = formatToolCallSummary(payload.tool_call);
    }
  } catch {
    // ignore invalid raw payload and keep fallback summary
  }
  return {
    summary,
    raw: detail,
    step,
    toolName: toolName === "" ? undefined : toolName,
    outputReady,
  };
}

function parsePersistedToolCallNotices(metadata: Record<string, unknown> | null): ViewToolCallNotice[] {
  if (!metadata) {
    return [];
  }
  const raw = metadata.tool_call_notices;
  if (!Array.isArray(raw)) {
    return [];
  }
  const notices: ViewToolCallNotice[] = [];
  for (const item of raw) {
    if (typeof item === "string") {
      const notice = buildToolCallNoticeFromRaw(item);
      if (notice) {
        notices.push(notice);
      }
      continue;
    }
    const obj = toRecord(item);
    if (!obj) {
      continue;
    }
    const rawText = typeof obj.raw === "string" ? obj.raw : "";
    const notice = buildToolCallNoticeFromRaw(rawText);
    if (notice) {
      const persistedOrder = parsePositiveInteger(obj.order);
      if (persistedOrder !== undefined) {
        notice.order = persistedOrder;
      }
      notices.push(notice);
    }
  }
  return notices;
}

function toViewMessage(message: RuntimeMessage): ViewMessage {
  const joined = (message.content ?? [])
    .map((item) => item.text ?? "")
    .join("")
    .trim();
  const metadata = toRecord(message.metadata);
  const toolCalls = parsePersistedToolCallNotices(metadata);
  const persistedTextOrder = parsePositiveInteger(metadata?.text_order);
  const persistedToolOrder = parsePositiveInteger(metadata?.tool_order);
  const textOrder = joined === "" ? undefined : (persistedTextOrder ?? nextMessageOutputOrder());
  const orderedToolCalls = withResolvedToolCallOrder(toolCalls, persistedToolOrder);
  const toolOrder = orderedToolCalls.length === 0 ? undefined : orderedToolCalls[0].order;
  const timeline: ViewMessageTimelineEntry[] = [];
  if (joined !== "" && textOrder !== undefined) {
    timeline.push({
      type: "text",
      order: textOrder,
      text: joined,
    });
  }
  for (const toolCall of orderedToolCalls) {
    timeline.push({
      type: "tool_call",
      order: toolCall.order ?? nextMessageOutputOrder(),
      toolCall,
    });
  }
  return {
    id: message.id || `msg-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    role: message.role === "user" ? "user" : "assistant",
    text: joined,
    toolCalls: orderedToolCalls,
    textOrder,
    toolOrder,
    timeline,
  };
}

function withResolvedToolCallOrder(toolCalls: ViewToolCallNotice[], persistedToolOrder?: number): ViewToolCallNotice[] {
  if (toolCalls.length === 0) {
    return [];
  }
  const resolved: ViewToolCallNotice[] = [];
  let cursor = persistedToolOrder;
  for (const toolCall of toolCalls) {
    const parsedOrder = parsePositiveInteger(toolCall.order);
    if (parsedOrder !== undefined) {
      cursor = parsedOrder;
      resolved.push({
        ...toolCall,
        order: parsedOrder,
      });
      continue;
    }
    if (cursor === undefined) {
      cursor = nextMessageOutputOrder();
    } else {
      cursor += 1;
    }
    resolved.push({
      ...toolCall,
      order: cursor,
    });
  }
  return resolved;
}

function toAbsoluteURL(path: string): string {
  const base = state.apiBase.replace(/\/+$/, "");
  return `${base}${path}`;
}

function asErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function asWorkspaceErrorMessage(error: unknown): string {
  if (!(error instanceof Error)) {
    return asErrorMessage(error);
  }
  const raw = error.message.trim().toLowerCase();
  if (raw === "404 page not found") {
    return t("error.workspaceEndpointMissing", {
      endpoint: "/workspace/files",
      apiBase: state.apiBase,
    });
  }
  return error.message;
}

function compactTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString(getLocale(), { hour12: false });
}

function isTabKey(value: string | undefined): value is TabKey {
  return value === "chat" || value === "cron";
}

function newSessionID(): string {
  const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789";
  const length = 6;
  const bytes = new Uint8Array(length);
  if (typeof crypto !== "undefined" && typeof crypto.getRandomValues === "function") {
    crypto.getRandomValues(bytes);
  } else {
    for (let index = 0; index < bytes.length; index += 1) {
      bytes[index] = Math.floor(Math.random() * 256);
    }
  }
  let id = "";
  for (const value of bytes) {
    id += alphabet[value % alphabet.length];
  }
  return id;
}

function mustElement<T extends Element>(id: string): T {
  const element = document.getElementById(id);
  if (!element) {
    throw new Error(t("error.missingElement", { id }));
  }
  return element as unknown as T;
}
