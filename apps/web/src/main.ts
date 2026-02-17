import { parseErrorMessage } from "./api-utils.js";
import { DEFAULT_LOCALE, getLocale, isWebMessageKey, setLocale, t } from "./i18n.js";

type Tone = "neutral" | "info" | "error";
type TabKey = "chat" | "search" | "models" | "channels" | "workspace" | "cron";
type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";
type ProviderKVKind = "headers" | "aliases";
type WorkspaceEditorMode = "json" | "text";
type CronModalMode = "create" | "edit";

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

interface WorkspaceFileInfo {
  path: string;
  kind: "config" | "skill";
  size: number | null;
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

interface CronJobSpec {
  id: string;
  name: string;
  enabled: boolean;
  schedule: CronScheduleSpec;
  task_type: string;
  text?: string;
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

interface JSONRequestOptions {
  method?: HttpMethod;
  body?: unknown;
  headers?: Record<string, string>;
}

interface CustomSelectInstance {
  container: HTMLDivElement;
  trigger: HTMLDivElement;
  selectedText: HTMLSpanElement;
  optionsList: HTMLDivElement;
}

const DEFAULT_API_BASE = "http://127.0.0.1:8088";
const DEFAULT_API_KEY = "";
const DEFAULT_USER_ID = "demo-user";
const DEFAULT_CHANNEL = "console";
const WEB_CHAT_CHANNEL = DEFAULT_CHANNEL;
const QQ_CHANNEL = "qq";
const DEFAULT_QQ_API_BASE = "https://api.sgroup.qq.com";
const QQ_SANDBOX_API_BASE = "https://sandbox.api.sgroup.qq.com";
const DEFAULT_QQ_TOKEN_URL = "https://bots.qq.com/app/getAppAccessToken";
const DEFAULT_QQ_TIMEOUT_SECONDS = 8;
const CHAT_LIVE_REFRESH_INTERVAL_MS = 1500;
const REQUEST_SOURCE_HEADER = "X-NextAI-Source";
const REQUEST_SOURCE_WEB = "web";
const SETTINGS_KEY = "nextai.web.chat.settings";
const LOCALE_KEY = "nextai.web.locale";
const BUILTIN_PROVIDER_IDS = new Set(["openai"]);
const TABS: TabKey[] = ["chat", "search", "models", "channels", "workspace", "cron"];
const customSelectInstances = new Map<HTMLSelectElement, CustomSelectInstance>();
let customSelectGlobalEventsBound = false;
let chatLiveRefreshTimer: number | null = null;
let chatLiveRefreshInFlight = false;

const apiBaseInput = mustElement<HTMLInputElement>("api-base");
const apiKeyInput = mustElement<HTMLInputElement>("api-key");
const userIdInput = mustElement<HTMLInputElement>("user-id");
const channelInput = mustElement<HTMLInputElement>("channel");
const localeSelect = mustElement<HTMLSelectElement>("locale-select");
const reloadChatsButton = mustElement<HTMLButtonElement>("reload-chats");
const settingsToggleButton = mustElement<HTMLButtonElement>("settings-toggle");
const settingsPopover = mustElement<HTMLElement>("settings-popover");
const settingsPopoverCloseButton = mustElement<HTMLButtonElement>("settings-popover-close");
const statusLine = mustElement<HTMLElement>("status-line");

const tabButtons = Array.from(document.querySelectorAll<HTMLButtonElement>(".tab-btn"));

const panelChat = mustElement<HTMLElement>("panel-chat");
const panelSearch = mustElement<HTMLElement>("panel-search");
const panelModels = mustElement<HTMLElement>("panel-models");
const panelChannels = mustElement<HTMLElement>("panel-channels");
const panelWorkspace = mustElement<HTMLElement>("panel-workspace");
const panelCron = mustElement<HTMLElement>("panel-cron");

const newChatButton = mustElement<HTMLButtonElement>("new-chat");
const chatList = mustElement<HTMLUListElement>("chat-list");
const chatTitle = mustElement<HTMLElement>("chat-title");
const chatSession = mustElement<HTMLElement>("chat-session");
const searchChatInput = mustElement<HTMLInputElement>("search-chat-input");
const searchChatResults = mustElement<HTMLUListElement>("search-chat-results");
const messageList = mustElement<HTMLUListElement>("message-list");
const composerForm = mustElement<HTMLFormElement>("composer");
const messageInput = mustElement<HTMLTextAreaElement>("message-input");
const sendButton = mustElement<HTMLButtonElement>("send-btn");

const refreshModelsButton = mustElement<HTMLButtonElement>("refresh-models");
const modelsAddProviderButton = mustElement<HTMLButtonElement>("models-add-provider-btn");
const modelsActiveForm = mustElement<HTMLFormElement>("models-active-form");
const modelsActiveProviderSelect = mustElement<HTMLSelectElement>("models-active-provider-select");
const modelsActiveModelSelect = mustElement<HTMLSelectElement>("models-active-model-select");
const modelsActiveModelManualInput = mustElement<HTMLInputElement>("models-active-model-manual-input");
const modelsSetActiveButton = mustElement<HTMLButtonElement>("models-set-active-btn");
const modelsActiveSummary = mustElement<HTMLElement>("models-active-summary");
const modelsProviderList = mustElement<HTMLUListElement>("models-provider-list");
const modelsProviderModal = mustElement<HTMLElement>("models-provider-modal");
const modelsProviderModalTitle = mustElement<HTMLElement>("models-provider-modal-title");
const modelsProviderModalCloseButton = mustElement<HTMLButtonElement>("models-provider-modal-close-btn");
const modelsProviderForm = mustElement<HTMLFormElement>("models-provider-form");
const modelsProviderTypeSelect = mustElement<HTMLSelectElement>("models-provider-type-select");
const modelsProviderNameInput = mustElement<HTMLInputElement>("models-provider-name-input");
const modelsProviderAPIKeyInput = mustElement<HTMLInputElement>("models-provider-api-key-input");
const modelsProviderBaseURLInput = mustElement<HTMLInputElement>("models-provider-base-url-input");
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
const refreshQQChannelButton = mustElement<HTMLButtonElement>("refresh-qq-channel");
const qqChannelForm = mustElement<HTMLFormElement>("qq-channel-form");
const qqChannelEnabledInput = mustElement<HTMLInputElement>("qq-channel-enabled");
const qqChannelAppIDInput = mustElement<HTMLInputElement>("qq-channel-app-id");
const qqChannelClientSecretInput = mustElement<HTMLInputElement>("qq-channel-client-secret");
const qqChannelBotPrefixInput = mustElement<HTMLInputElement>("qq-channel-bot-prefix");
const qqChannelTargetTypeSelect = mustElement<HTMLSelectElement>("qq-channel-target-type");
const qqChannelAPIEnvironmentSelect = mustElement<HTMLSelectElement>("qq-channel-api-env");
const qqChannelTimeoutSecondsInput = mustElement<HTMLInputElement>("qq-channel-timeout-seconds");
const workspaceFilesBody = mustElement<HTMLTableSectionElement>("workspace-files-body");
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
const cronJobsBody = mustElement<HTMLTableSectionElement>("cron-jobs-body");
const cronCreateOpenButton = mustElement<HTMLButtonElement>("cron-create-open-btn");
const cronCreateModal = mustElement<HTMLElement>("cron-create-modal");
const cronCreateModalTitle = mustElement<HTMLElement>("cron-create-modal-title");
const cronCreateModalCloseButton = mustElement<HTMLButtonElement>("cron-create-modal-close-btn");
const cronCreateForm = mustElement<HTMLFormElement>("cron-create-form");
const cronCreateFormTitle = mustElement<HTMLElement>("cron-create-form-title");
const cronDispatchHint = mustElement<HTMLElement>("cron-dispatch-hint");
const cronIDInput = mustElement<HTMLInputElement>("cron-id");
const cronNameInput = mustElement<HTMLInputElement>("cron-name");
const cronIntervalInput = mustElement<HTMLInputElement>("cron-interval");
const cronSessionIDInput = mustElement<HTMLInputElement>("cron-session-id");
const cronMaxConcurrencyInput = mustElement<HTMLInputElement>("cron-max-concurrency");
const cronTimeoutInput = mustElement<HTMLInputElement>("cron-timeout-seconds");
const cronMisfireInput = mustElement<HTMLInputElement>("cron-misfire-grace");
const cronEnabledInput = mustElement<HTMLInputElement>("cron-enabled");
const cronTextInput = mustElement<HTMLTextAreaElement>("cron-text");
const cronNewSessionButton = mustElement<HTMLButtonElement>("cron-new-session");
const cronSubmitButton = mustElement<HTMLButtonElement>("cron-submit-btn");

const panelByTab: Record<TabKey, HTMLElement> = {
  chat: panelChat,
  search: panelSearch,
  models: panelModels,
  channels: panelChannels,
  workspace: panelWorkspace,
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
    search: true,
    models: false,
    channels: false,
    workspace: false,
    cron: false,
  },

  chats: [] as ChatSpec[],
  chatSearchQuery: "",
  activeChatId: null as string | null,
  activeSessionId: newSessionID(),
  messages: [] as ViewMessage[],
  messageOutputOrder: 0,
  sending: false,

  providers: [] as ProviderInfo[],
  providerTypes: [] as ProviderTypeInfo[],
  modelDefaults: {} as Record<string, string>,
  activeLLM: { provider_id: "", model: "" } as ModelSlotConfig,
  providerModal: {
    open: false,
    mode: "create" as "create" | "edit",
    editingProviderID: "",
  },
  workspaceFiles: [] as WorkspaceFileInfo[],
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
};

const bootstrapTask = bootstrap();

async function bootstrap(): Promise<void> {
  initLocale();
  restoreSettings();
  bindEvents();
  initCustomSelects();
  setSettingsPopoverOpen(false);
  setWorkspaceEditorModalOpen(false);
  setWorkspaceImportModalOpen(false);
  setCronCreateModalOpen(false);
  applyLocaleToDocument();
  renderTabPanels();
  renderChatHeader();
  renderChatList();
  renderSearchChatResults();
  renderMessages();
  renderQQChannelConfig();
  renderWorkspaceFiles();
  renderWorkspaceEditor();
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
    state.messages = history.messages.map(toViewMessage);
    renderMessages();
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

  settingsToggleButton.addEventListener("click", (event) => {
    event.stopPropagation();
    setSettingsPopoverOpen(!isSettingsPopoverOpen());
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
    if (settingsPopover.contains(target) || settingsToggleButton.contains(target)) {
      return;
    }
    setSettingsPopoverOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isSettingsPopoverOpen()) {
      return;
    }
    setSettingsPopoverOpen(false);
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

  userIdInput.addEventListener("change", async () => {
    await handleControlChange(true);
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

  refreshModelsButton.addEventListener("click", async () => {
    await refreshModels();
  });
  modelsActiveForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await setActiveModel();
  });
  modelsActiveProviderSelect.addEventListener("change", () => {
    const providerID = modelsActiveProviderSelect.value.trim();
    renderActiveModelOptions(providerID);
    modelsActiveModelManualInput.value = "";
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
  });
  modelsProviderAliasesAddButton.addEventListener("click", () => {
    appendProviderKVRow(modelsProviderAliasesRows, "aliases");
  });
  modelsProviderTypeSelect.addEventListener("change", () => {
    syncProviderCustomModelsField(modelsProviderTypeSelect.value);
  });
  modelsProviderCustomModelsAddButton.addEventListener("click", () => {
    appendCustomModelRow(modelsProviderCustomModelsRows);
  });
  modelsProviderForm.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
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
    }
  });
  modelsProviderModalCloseButton.addEventListener("click", () => {
    closeProviderModal();
  });
  modelsProviderCancelButton.addEventListener("click", () => {
    closeProviderModal();
  });
  modelsProviderModal.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    if (target.closest("[data-modal-close=\"true\"]")) {
      closeProviderModal();
    }
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
  refreshQQChannelButton.addEventListener("click", async () => {
    await refreshQQChannelConfig();
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
  workspaceFilesBody.addEventListener("click", async (event) => {
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
    ensureCronSessionID();
    setCronCreateModalOpen(true);
  });
  cronCreateModal.addEventListener("click", (event) => {
    const target = event.target;
    if (target instanceof Element && target.closest("[data-cron-create-close=\"true\"]")) {
      setCronCreateModalOpen(false);
      return;
    }
    event.stopPropagation();
  });
  cronCreateModalCloseButton.addEventListener("click", () => {
    setCronCreateModalOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isCronCreateModalOpen()) {
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
    const created = await saveCronJob();
    if (created) {
      setCronCreateModalOpen(false);
    }
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
  const selects = Array.from(document.querySelectorAll<HTMLSelectElement>(".controls select, .stack-form select"));
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

  const container = document.createElement("div");
  container.className = "custom-select-container";
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
  optionsList.setAttribute("role", "listbox");

  container.append(trigger, optionsList);
  customSelectInstances.set(select, {
    container,
    trigger,
    selectedText,
    optionsList,
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
    if (!optionElement || optionElement.classList.contains("disabled")) {
      return;
    }
    const value = optionElement.dataset.value ?? "";
    selectCustomOption(select, value);
    closeCustomSelect(select);
    trigger.focus();
    event.stopPropagation();
  });

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

  instance.optionsList.innerHTML = "";
  for (const option of Array.from(select.options)) {
    const optionElement = document.createElement("div");
    optionElement.className = "option";
    optionElement.dataset.value = option.value;
    optionElement.textContent = option.textContent ?? "";
    optionElement.setAttribute("role", "option");
    optionElement.setAttribute("aria-selected", String(option.selected));
    if (option.disabled) {
      optionElement.classList.add("disabled");
      optionElement.setAttribute("aria-disabled", "true");
    }
    if (option.selected) {
      optionElement.classList.add("selected");
    }
    instance.optionsList.appendChild(optionElement);
  }

  const selectedOption = Array.from(select.selectedOptions)[0] ?? select.options[select.selectedIndex] ?? select.options[0];
  instance.selectedText.textContent = selectedOption?.textContent?.trim() || "";
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
  instance.container.classList.add("open");
  instance.trigger.setAttribute("aria-expanded", "true");
}

function closeCustomSelect(select: HTMLSelectElement): void {
  const instance = customSelectInstances.get(select);
  if (!instance) {
    return;
  }
  instance.container.classList.remove("open");
  instance.trigger.setAttribute("aria-expanded", "false");
}

function closeAllCustomSelects(except?: HTMLSelectElement): void {
  for (const [select, instance] of customSelectInstances.entries()) {
    if (select === except) {
      continue;
    }
    instance.container.classList.remove("open");
    instance.trigger.setAttribute("aria-expanded", "false");
  }
}

function isSettingsPopoverOpen(): boolean {
  return !settingsPopover.classList.contains("is-hidden");
}

function setSettingsPopoverOpen(open: boolean): void {
  settingsPopover.classList.toggle("is-hidden", !open);
  settingsPopover.setAttribute("aria-hidden", String(!open));
  settingsToggleButton.setAttribute("aria-expanded", String(open));
  document.body.classList.toggle("settings-open", open);
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
  cronCreateModal.classList.toggle("is-hidden", !open);
  cronCreateModal.setAttribute("aria-hidden", String(!open));
  cronCreateOpenButton.setAttribute("aria-expanded", String(open));
  document.body.classList.toggle("cron-create-open", open);
}

function setCronModalMode(mode: CronModalMode, editingJobID = ""): void {
  state.cronModal.mode = mode;
  state.cronModal.editingJobID = editingJobID;

  const createMode = mode === "create";
  const titleKey = createMode ? "cron.createTextJob" : "cron.updateTextJob";
  const submitKey = createMode ? "cron.submitCreate" : "cron.submitUpdate";

  cronCreateModalTitle.textContent = t(titleKey);
  cronCreateFormTitle.textContent = t(titleKey);
  cronSubmitButton.textContent = t(submitKey);
  cronIDInput.readOnly = !createMode;
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
  if (state.tabLoaded.models) {
    renderModelsPanel();
  }
  if (state.tabLoaded.channels) {
    renderQQChannelConfig();
  }
  if (state.tabLoaded.workspace) {
    renderWorkspaceFiles();
    renderWorkspaceEditor();
  }
  if (state.tabLoaded.cron) {
    renderCronJobs();
  }
  setCronModalMode(state.cronModal.mode, state.cronModal.editingJobID);
  syncCronDispatchHint();
  syncAllCustomSelects();
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
  if (tab !== "workspace") {
    setWorkspaceEditorModalOpen(false);
    setWorkspaceImportModalOpen(false);
  }
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

  TABS.forEach((tab) => {
    panelByTab[tab].classList.toggle("is-active", tab === state.activeTab);
  });
}

async function loadTabData(tab: TabKey, force = false): Promise<void> {
  try {
    if (tab === "chat" || tab === "search") {
      await reloadChats();
      return;
    }
    if (!force && state.tabLoaded[tab]) {
      return;
    }

    switch (tab) {
      case "models":
        await refreshModels();
        break;
      case "channels":
        await refreshQQChannelConfig();
        break;
      case "workspace":
        await refreshWorkspace();
        break;
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
      const parsed = JSON.parse(raw) as Partial<typeof state>;
      if (typeof parsed.apiBase === "string" && parsed.apiBase.trim() !== "") {
        state.apiBase = parsed.apiBase.trim();
      }
      if (typeof parsed.apiKey === "string") {
        state.apiKey = parsed.apiKey.trim();
      }
      if (typeof parsed.userId === "string" && parsed.userId.trim() !== "") {
        state.userId = parsed.userId.trim();
      }
    } catch {
      localStorage.removeItem(SETTINGS_KEY);
    }
  }
  state.channel = WEB_CHAT_CHANNEL;
  apiBaseInput.value = state.apiBase;
  apiKeyInput.value = state.apiKey;
  userIdInput.value = state.userId;
  channelInput.value = WEB_CHAT_CHANNEL;
  channelInput.readOnly = true;
}

function syncControlState(): void {
  state.apiBase = apiBaseInput.value.trim() || DEFAULT_API_BASE;
  state.apiKey = apiKeyInput.value.trim();
  state.userId = userIdInput.value.trim() || DEFAULT_USER_ID;
  state.channel = WEB_CHAT_CHANNEL;
  channelInput.value = WEB_CHAT_CHANNEL;
  localStorage.setItem(
    SETTINGS_KEY,
    JSON.stringify({
      apiBase: state.apiBase,
      apiKey: state.apiKey,
      userId: state.userId,
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

  state.activeChatId = chat.id;
  state.activeSessionId = chat.session_id;
  renderChatHeader();
  renderChatList();
  renderSearchChatResults();

  try {
    const history = await requestJSON<ChatHistoryResponse>(`/chats/${encodeURIComponent(chat.id)}`);
    state.messages = history.messages.map(toViewMessage);
    renderMessages();
    setStatus(t("status.loadedMessages", { count: history.messages.length }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
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
  state.activeChatId = null;
  state.activeSessionId = newSessionID();
  state.messages = [];
  renderChatHeader();
  renderChatList();
  renderSearchChatResults();
  renderMessages();
}

async function sendMessage(): Promise<void> {
  await bootstrapTask;
  syncControlState();
  if (state.sending) {
    return;
  }

  const inputText = messageInput.value.trim();
  if (inputText === "") {
    setStatus(t("status.inputRequired"), "error");
    return;
  }

  if (state.apiBase === "" || state.userId === "") {
    setStatus(t("status.controlsRequired"), "error");
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
    setStatus(asErrorMessage(error), "error");
  } finally {
    state.sending = false;
    sendButton.disabled = false;
  }
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
  if (bizParams && Object.keys(bizParams).length > 0) {
    payload.biz_params = bizParams;
  }

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
    li.className = "message-empty";
    li.textContent = t("chat.emptyByFilter");
    chatList.appendChild(li);
    return;
  }

  state.chats.forEach((chat, index) => {
    const li = document.createElement("li");
    li.className = "chat-list-item";
    li.style.animationDelay = `${Math.min(index * 24, 180)}ms`;

    const actions = document.createElement("div");
    actions.className = "chat-item-actions";

    const button = document.createElement("button");
    button.type = "button";
    button.className = "chat-item-btn";
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
      sessionId: chat.session_id,
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
    deleteButton.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path fill="currentColor" d="M7 21q-.825 0-1.412-.587T5 19V6H4V4h5V3h6v1h5v2h-1v13q0 .825-.587 1.413T17 21zM17 6H7v13h10zM9 17h2V8H9zm4 0h2V8h-2zM7 6v13z"/></svg>`;
    deleteButton.addEventListener("click", async (event) => {
      event.stopPropagation();
      await deleteChat(chat.id);
    });
    actions.appendChild(deleteButton);

    li.appendChild(actions);
    chatList.appendChild(li);
  });
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
    if (chat.id === state.activeChatId) {
      button.classList.add("active");
    }
    button.addEventListener("click", async () => {
      await openChat(chat.id);
      await switchTab("chat");
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
}

function filterChatsForSearch(query: string): ChatSpec[] {
  const normalizedQuery = query.trim().toLowerCase();
  if (normalizedQuery === "") {
    return state.chats;
  }
  return state.chats.filter((chat) => buildChatSearchText(chat).includes(normalizedQuery));
}

function buildChatSearchText(chat: ChatSpec): string {
  return [chat.name, chat.session_id, chat.id, chat.user_id, chat.channel, stringifyChatMeta(chat.meta)].join(" ").toLowerCase();
}

function stringifyChatMeta(meta: Record<string, unknown> | undefined): string {
  if (!meta) {
    return "";
  }
  try {
    return JSON.stringify(meta);
  } catch {
    return "";
  }
}

function renderChatHeader(): void {
  const active = state.chats.find((chat) => chat.id === state.activeChatId);
  chatTitle.textContent = active ? active.name : t("chat.draftTitle");
  chatSession.textContent = state.activeSessionId;
}

function renderMessages(): void {
  messageList.innerHTML = "";
  if (state.messages.length === 0) {
    const empty = document.createElement("li");
    empty.className = "message-empty";
    empty.textContent = t("chat.emptyMessages");
    messageList.appendChild(empty);
    return;
  }

  for (const message of state.messages) {
    const item = document.createElement("li");
    item.className = `message ${message.role}`;
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
  if (event.type !== "tool_call") {
    return;
  }
  const notice = formatToolCallNotice(event);
  if (!notice) {
    return;
  }
  appendToolCallNoticeToAssistant(assistantID, notice);
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

function formatToolCallNotice(event: AgentStreamEvent): ViewToolCallNotice | null {
  const raw = formatToolCallRaw(event);
  if (raw === "") {
    return null;
  }
  return {
    summary: formatToolCallSummary(event.tool_call),
    raw,
  };
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

async function refreshModels(): Promise<void> {
  syncControlState();
  try {
    const result = await syncModelState({ autoActivate: true });
    state.tabLoaded.models = true;
    renderModelsPanel();
    setStatus(
      t(result.source === "catalog" ? "status.providersLoadedCatalog" : "status.providersLoadedLegacy", {
        count: result.providers.length,
      }),
      "info",
    );
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
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

  if (options.autoActivate) {
    const autoActivated = await maybeAutoActivateModel(result.providers, result.defaults, result.activeLLM);
    if (autoActivated) {
      state.activeLLM = autoActivated;
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

function renderModelsPanel(): void {
  renderActiveModelEditor();
  modelsProviderList.innerHTML = "";
  if (state.providers.length === 0) {
    appendEmptyItem(modelsProviderList, t("models.emptyProviders"));
  } else {
    for (const provider of state.providers) {
      const item = document.createElement("li");
      item.className = "detail-item";

      const title = document.createElement("p");
      title.className = "item-title";
      title.textContent = formatProviderLabel(provider);

      const enabledLine = document.createElement("p");
      enabledLine.className = "item-meta";
      enabledLine.textContent = t("models.enabledLine", {
        enabled: provider.enabled === false ? t("common.no") : t("common.yes"),
      });

      const keyStatus = document.createElement("p");
      keyStatus.className = "item-meta";
      keyStatus.textContent = provider.has_api_key
        ? t("models.apiKeyConfigured", {
            value: provider.current_api_key ?? t("models.apiKeyMasked"),
          })
        : t("models.apiKeyUnset");

      const defaultLine = document.createElement("p");
      defaultLine.className = "item-meta";
      defaultLine.textContent = t("models.defaultLine", {
        model: state.modelDefaults[provider.id] || t("common.none"),
      });

      const baseURLLine = document.createElement("p");
      baseURLLine.className = "item-meta";
      baseURLLine.textContent = t("models.baseURLLine", {
        baseURL: provider.current_base_url || t("common.none"),
      });

      const modelText = provider.models.map((model) => formatModelEntry(model)).join(", ") || t("models.noModels");
      const modelLine = document.createElement("p");
      modelLine.className = "item-meta";
      modelLine.textContent = t("models.modelLine", { models: modelText });

      const actions = document.createElement("div");
      actions.className = "actions-row";

      const editButton = document.createElement("button");
      editButton.type = "button";
      editButton.className = "secondary-btn";
      editButton.dataset.providerAction = "edit";
      editButton.dataset.providerId = provider.id;
      editButton.textContent = t("models.editProvider");

      const deleteButton = document.createElement("button");
      deleteButton.type = "button";
      deleteButton.className = "danger-btn";
      deleteButton.dataset.providerAction = "delete";
      deleteButton.dataset.providerId = provider.id;
      deleteButton.textContent = t("models.deleteProvider");

      actions.append(editButton, deleteButton);
      item.append(title, enabledLine, keyStatus, defaultLine, baseURLLine, modelLine, actions);
      modelsProviderList.appendChild(item);
    }
  }
}

function renderActiveModelEditor(): void {
  renderActiveProviderOptions();
  if (state.providers.length === 0) {
    modelsActiveModelSelect.innerHTML = "";
    modelsActiveModelManualInput.value = "";
    modelsActiveProviderSelect.disabled = true;
    modelsActiveModelSelect.disabled = true;
    modelsActiveModelManualInput.disabled = true;
    modelsSetActiveButton.disabled = true;
    modelsActiveSummary.textContent = t("models.activeSummary", {
      providerId: t("common.none"),
      model: t("common.none"),
    });
    syncAllCustomSelects();
    return;
  }

  const hasActiveProvider = state.providers.some((provider) => provider.id === state.activeLLM.provider_id);
  const selectedProviderID = hasActiveProvider ? state.activeLLM.provider_id : state.providers[0].id;
  modelsActiveProviderSelect.value = selectedProviderID;
  renderActiveModelOptions(selectedProviderID, state.activeLLM.model);

  const selectedProvider = state.providers.find((provider) => provider.id === selectedProviderID);
  const hasActiveModelInList =
    selectedProvider?.models.some((model) => model.id === state.activeLLM.model) ?? false;
  modelsActiveModelManualInput.value = hasActiveModelInList ? "" : state.activeLLM.model;

  modelsActiveProviderSelect.disabled = false;
  modelsActiveModelManualInput.disabled = false;
  modelsSetActiveButton.disabled = false;
  modelsActiveSummary.textContent = t("models.activeSummary", {
    providerId: state.activeLLM.provider_id || t("common.none"),
    model: state.activeLLM.model || t("common.none"),
  });
  syncAllCustomSelects();
}

function renderActiveProviderOptions(): void {
  modelsActiveProviderSelect.innerHTML = "";
  if (state.providers.length === 0) {
    const emptyOption = document.createElement("option");
    emptyOption.value = "";
    emptyOption.textContent = t("models.noProviderOption");
    modelsActiveProviderSelect.appendChild(emptyOption);
    syncCustomSelect(modelsActiveProviderSelect);
    return;
  }

  for (const provider of state.providers) {
    const option = document.createElement("option");
    option.value = provider.id;
    option.textContent = formatProviderLabel(provider);
    modelsActiveProviderSelect.appendChild(option);
  }
  syncCustomSelect(modelsActiveProviderSelect);
}

function renderActiveModelOptions(providerID: string, preferredModel = ""): void {
  modelsActiveModelSelect.innerHTML = "";
  const provider = state.providers.find((item) => item.id === providerID);
  const models = provider?.models ?? [];
  if (models.length === 0) {
    const emptyOption = document.createElement("option");
    emptyOption.value = "";
    emptyOption.textContent = t("models.noModelOption");
    modelsActiveModelSelect.appendChild(emptyOption);
    modelsActiveModelSelect.disabled = true;
    syncCustomSelect(modelsActiveModelSelect);
    return;
  }

  for (const model of models) {
    const option = document.createElement("option");
    option.value = model.id;
    option.textContent = formatModelEntry(model);
    modelsActiveModelSelect.appendChild(option);
  }

  const preferred = preferredModel.trim();
  const hasPreferred = preferred !== "" && models.some((model) => model.id === preferred);
  const fallback = state.modelDefaults[providerID];
  const hasFallback = typeof fallback === "string" && fallback !== "" && models.some((model) => model.id === fallback);
  const selected = hasPreferred ? preferred : hasFallback ? fallback : models[0].id;
  modelsActiveModelSelect.value = selected;
  modelsActiveModelSelect.disabled = false;
  syncCustomSelect(modelsActiveModelSelect);
}

async function setActiveModel(): Promise<void> {
  syncControlState();
  const providerID = modelsActiveProviderSelect.value.trim();
  const modelFromProvider = modelsActiveModelSelect.value.trim();
  const manualModel = modelsActiveModelManualInput.value.trim();
  const modelID = manualModel || modelFromProvider;

  if (providerID === "" || modelID === "") {
    setStatus(t("error.providerAndModelRequired"), "error");
    return;
  }

  try {
    const out = await requestJSON<ActiveModelsInfo>("/models/active", {
      method: "PUT",
      body: {
        provider_id: providerID,
        model: modelID,
      },
    });
    const active = normalizeModelSlot(out.active_llm);
    state.activeLLM = active;
    renderActiveModelEditor();
    setStatus(
      t("status.activeModelUpdated", {
        providerId: active.provider_id || providerID,
        model: active.model || modelID,
      }),
      "info",
    );
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
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
    return "openai";
  }
  const baseProviderID = slugifyProviderID(modelsProviderNameInput.value) || slugifyProviderID(selectedProviderType) || "provider";
  return ensureUniqueProviderID(baseProviderID);
}

function providerSupportsCustomModels(providerTypeID: string): boolean {
  const normalized = normalizeProviderTypeValue(providerTypeID);
  return normalized !== "" && !BUILTIN_PROVIDER_IDS.has(normalized);
}

function syncProviderCustomModelsField(providerTypeID: string): void {
  const enabled = providerSupportsCustomModels(providerTypeID);
  modelsProviderCustomModelsField.hidden = !enabled;
  modelsProviderCustomModelsAddButton.disabled = !enabled;
  for (const input of Array.from(modelsProviderCustomModelsRows.querySelectorAll<HTMLInputElement>("input[data-custom-model-input=\"true\"]"))) {
    input.disabled = !enabled;
  }
  if (!enabled) {
    resetProviderCustomModelsEditor();
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
}

function openProviderModal(mode: "create" | "edit", providerID = ""): void {
  state.providerModal.mode = mode;
  state.providerModal.open = true;
  state.providerModal.editingProviderID = providerID;

  if (mode === "create") {
    resetProviderModalForm();
    modelsProviderModalTitle.textContent = t("models.addProviderTitle");
    modelsProviderTypeSelect.focus();
  } else {
    modelsProviderModalTitle.textContent = t("models.editProviderTitle");
    populateProviderForm(providerID);
    modelsProviderNameInput.focus();
  }
  modelsProviderModal.classList.remove("is-hidden");
}

function closeProviderModal(): void {
  state.providerModal.open = false;
  state.providerModal.editingProviderID = "";
  modelsProviderModal.classList.add("is-hidden");
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
  modelsProviderTimeoutMSInput.value = "";
  resetProviderKVEditor(modelsProviderHeadersRows, "headers");
  populateProviderAliasRows(provider.models);
  populateProviderCustomModelsRows(provider);
  syncProviderCustomModelsField(resolveProviderType(provider));
  setStatus(t("status.providerLoadedForEdit", { providerId: provider.id }), "info");
}

async function upsertProvider(): Promise<void> {
  syncControlState();
  const selectedProviderType = normalizeProviderTypeValue(modelsProviderTypeSelect.value);
  const providerID = resolveProviderIDForUpsert(selectedProviderType);
  if (providerID === "") {
    setStatus(t("error.providerTypeRequired"), "error");
    return;
  }

  let timeoutMS: number | undefined;
  const timeoutRaw = modelsProviderTimeoutMSInput.value.trim();
  if (timeoutRaw !== "") {
    const parsed = Number.parseInt(timeoutRaw, 10);
    if (Number.isNaN(parsed) || parsed < 0) {
      setStatus(t("error.providerTimeoutInvalid"), "error");
      return;
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
    setStatus(asErrorMessage(error), "error");
    return;
  }

  let aliases: Record<string, string> | undefined;
  try {
    aliases = collectProviderKVMap(modelsProviderAliasesRows, {
      invalidKey: t("error.invalidProviderAliasesKey"),
      invalidValue: (key) => t("error.invalidProviderAliasesValue", { key }),
    });
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return;
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
      setStatus(asErrorMessage(error), "error");
      return;
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
  if (typeof timeoutMS === "number") {
    payload.timeout_ms = timeoutMS;
  }
  if (headers) {
    payload.headers = headers;
  }
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
  if (Object.keys(mergedAliases).length > 0) {
    payload.model_aliases = mergedAliases;
  }

  try {
    const out = await requestJSON<ProviderInfo>(`/models/${encodeURIComponent(providerID)}/config`, {
      method: "PUT",
      body: payload,
    });
    await refreshModels();
    closeProviderModal();
    modelsProviderAPIKeyInput.value = "";
    setStatus(
      t(state.providerModal.mode === "create" ? "status.providerCreated" : "status.providerUpdated", {
        providerId: out.id,
      }),
      "info",
    );
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
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

function populateProviderAliasRows(models: ModelInfo[]): void {
  const aliases = new Map<string, string>();
  for (const model of models) {
    const alias = model.id.trim();
    const target = (model.alias_of ?? "").trim();
    if (alias === "" || target === "") {
      continue;
    }
    aliases.set(alias, target);
  }
  modelsProviderAliasesRows.innerHTML = "";
  if (aliases.size === 0) {
    appendProviderKVRow(modelsProviderAliasesRows, "aliases");
    return;
  }
  for (const [alias, target] of aliases.entries()) {
    appendProviderKVRow(modelsProviderAliasesRows, "aliases", alias, target);
  }
}

function populateProviderCustomModelsRows(provider: ProviderInfo): void {
  resetProviderCustomModelsEditor();
  if (BUILTIN_PROVIDER_IDS.has(provider.id)) {
    return;
  }

  const customModelIDs = provider.models
    .filter((item) => (item.alias_of ?? "").trim() === "")
    .map((item) => item.id.trim())
    .filter((item) => item !== "");
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
    renderQQChannelConfig();
    if (!options.silent) {
      setStatus(t("status.qqChannelLoaded"), "info");
    }
  } catch (error) {
    state.qqChannelConfig = defaultQQChannelConfig();
    state.qqChannelAvailable = false;
    state.tabLoaded.channels = false;
    renderQQChannelConfig();
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
    renderQQChannelConfig();
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
    if (state.activeWorkspacePath !== "" && !files.some((file) => file.path === state.activeWorkspacePath)) {
      clearWorkspaceSelection();
    }
    renderWorkspaceFiles();
    renderWorkspaceEditor();
    state.tabLoaded.workspace = true;
    if (!options.silent) {
      setStatus(t("status.workspaceFilesLoaded", { count: files.length }), "info");
    }
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

function renderWorkspaceFiles(): void {
  workspaceFilesBody.innerHTML = "";
  if (state.workspaceFiles.length === 0) {
    const row = document.createElement("tr");
    const col = document.createElement("td");
    col.colSpan = 3;
    col.className = "empty-cell";
    col.textContent = t("workspace.empty");
    row.appendChild(col);
    workspaceFilesBody.appendChild(row);
    return;
  }

  state.workspaceFiles.forEach((file) => {
    const row = document.createElement("tr");

    const pathCol = document.createElement("td");
    pathCol.className = "mono";
    pathCol.textContent = file.path;

    const sizeCol = document.createElement("td");
    sizeCol.textContent = file.size === null ? t("common.none") : String(file.size);

    const actionCol = document.createElement("td");
    const openButton = document.createElement("button");
    openButton.type = "button";
    openButton.dataset.workspaceOpen = file.path;
    openButton.textContent = t("workspace.openFile");
    openButton.disabled = file.path === state.activeWorkspacePath;

    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    deleteButton.className = "secondary-btn";
    deleteButton.dataset.workspaceDelete = file.path;
    deleteButton.textContent = t("workspace.deleteFile");
    deleteButton.disabled = file.kind !== "skill";

    actionCol.append(openButton, document.createTextNode(" "), deleteButton);
    row.append(pathCol, sizeCol, actionCol);
    workspaceFilesBody.appendChild(row);
  });
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
    renderWorkspaceFiles();
    renderWorkspaceEditor();
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

  setCronModalMode("edit", jobID);

  cronIDInput.value = job.id;
  cronNameInput.value = job.name;
  cronIntervalInput.value = job.schedule.cron ?? "";
  cronSessionIDInput.value = job.dispatch.target.session_id ?? "";
  cronMaxConcurrencyInput.value = String(job.runtime.max_concurrency ?? 1);
  cronTimeoutInput.value = String(job.runtime.timeout_seconds ?? 30);
  cronMisfireInput.value = String(job.runtime.misfire_grace_seconds ?? 0);
  cronEnabledInput.checked = job.enabled;
  cronTextInput.value = job.text ?? "";

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
    setStatus(t("status.cronJobsLoaded", { count: jobs.length }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function renderCronJobs(): void {
  cronJobsBody.innerHTML = "";
  if (state.cronJobs.length === 0) {
    const row = document.createElement("tr");
    const col = document.createElement("td");
    col.colSpan = 6;
    col.className = "empty-cell";
    col.textContent = t("cron.empty");
    row.appendChild(col);
    cronJobsBody.appendChild(row);
    return;
  }

  state.cronJobs.forEach((job) => {
    const row = document.createElement("tr");
    const jobState = state.cronStates[job.id];
    const nextRun = jobState?.next_run_at;

    const idCol = document.createElement("td");
    idCol.className = "mono";
    idCol.textContent = job.id;

    const nameCol = document.createElement("td");
    nameCol.textContent = job.name;

    const enabledCol = document.createElement("td");
    enabledCol.textContent = job.enabled ? t("common.yes") : t("common.no");

    const nextCol = document.createElement("td");
    nextCol.textContent = nextRun ? compactTime(nextRun) : t("common.none");

    const statusCol = document.createElement("td");
    statusCol.textContent = formatCronStatus(jobState);
    if (jobState?.last_error) {
      statusCol.title = jobState.last_error;
    }

    const actionCol = document.createElement("td");
    const actions = document.createElement("div");
    actions.className = "actions-row";

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

    actions.append(runBtn, editBtn, deleteBtn);
    actionCol.appendChild(actions);

    row.append(idCol, nameCol, enabledCol, nextCol, statusCol, actionCol);
    cronJobsBody.appendChild(row);
  });
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

async function saveCronJob(): Promise<boolean> {
  syncControlState();

  const id = cronIDInput.value.trim();
  const name = cronNameInput.value.trim();
  const intervalText = cronIntervalInput.value.trim();
  const sessionID = cronSessionIDInput.value.trim();
  const text = cronTextInput.value.trim();

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
  if (text === "") {
    setStatus(t("error.cronTextRequired"), "error");
    return false;
  }

  const maxConcurrency = parseIntegerInput(cronMaxConcurrencyInput.value, 1, 1);
  const timeoutSeconds = parseIntegerInput(cronTimeoutInput.value, 30, 1);
  const misfireGraceSeconds = parseIntegerInput(cronMisfireInput.value, 0, 0);
  const editing = state.cronModal.mode === "edit";
  const existingJob = state.cronJobs.find((job) => job.id === state.cronModal.editingJobID);

  const payload: CronJobSpec = {
    id,
    name,
    enabled: cronEnabledInput.checked,
    schedule: {
      type: existingJob?.schedule.type ?? "interval",
      cron: intervalText,
      timezone: existingJob?.schedule.timezone ?? "",
    },
    task_type: existingJob?.task_type ?? "text",
    text,
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
    setStatus(t(editing ? "status.cronUpdated" : "status.cronCreated", { jobId: id }), "info");
    return true;
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return false;
  }
}

async function deleteCronJob(jobID: string): Promise<void> {
  syncControlState();
  if (!window.confirm(t("cron.deleteConfirm", { jobId: jobID }))) {
    return;
  }

  try {
    const result = await requestJSON<DeleteResult>(`/cron/jobs/${encodeURIComponent(jobID)}`, {
      method: "DELETE",
    });
    await refreshCronJobs();
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
    enabled: provider.enabled ?? true,
    current_api_key: provider.current_api_key ?? "",
    current_base_url: provider.current_base_url ?? "",
  }));
}

function formatProviderLabel(provider: ProviderInfo): string {
  const base = provider.display_name === provider.id ? provider.id : `${provider.display_name} (${provider.id})`;
  if (provider.openai_compatible) {
    return `${base} (${t("models.compatible")})`;
  }
  return base;
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
  const parts = [model.id];
  if (model.alias_of && model.alias_of.trim() !== "") {
    parts.push(`->${model.alias_of}`);
  }
  const caps = formatCapabilities(model.capabilities);
  if (caps !== "") {
    parts.push(`[${caps}]`);
  }
  return parts.join(" ");
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
  try {
    const payload = JSON.parse(normalized) as AgentStreamEvent;
    summary = formatToolCallSummary(payload.tool_call);
  } catch {
    // ignore invalid raw payload and keep fallback summary
  }
  return {
    summary,
    raw: normalized,
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
  return (
    value === "chat" ||
    value === "search" ||
    value === "models" ||
    value === "channels" ||
    value === "workspace" ||
    value === "cron"
  );
}

function newSessionID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return `session-${crypto.randomUUID()}`;
  }
  return `session-${Date.now()}`;
}

function mustElement<T extends HTMLElement>(id: string): T {
  const element = document.getElementById(id);
  if (!element) {
    throw new Error(t("error.missingElement", { id }));
  }
  return element as T;
}
