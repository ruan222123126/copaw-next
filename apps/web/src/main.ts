import { parseEnvMap, parseErrorMessage } from "./api-utils.js";
import { DEFAULT_LOCALE, getLocale, isWebMessageKey, setLocale, t } from "./i18n.js";

type Tone = "neutral" | "info" | "error";
type TabKey = "chat" | "models" | "envs" | "workspace" | "cron";
type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";
type ProviderKVKind = "headers" | "aliases";

interface ChatSpec {
  id: string;
  name: string;
  session_id: string;
  user_id: string;
  channel: string;
  updated_at: string;
}

interface RuntimeContent {
  type?: string;
  text?: string;
}

interface RuntimeMessage {
  id?: string;
  role?: string;
  content?: RuntimeContent[];
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

interface EnvVar {
  key: string;
  value: string;
}

interface WorkspaceFileInfo {
  path: string;
  kind: "config" | "skill";
  size: number | null;
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

interface ViewMessage {
  id: string;
  role: "user" | "assistant";
  text: string;
}

interface JSONRequestOptions {
  method?: HttpMethod;
  body?: unknown;
  headers?: Record<string, string>;
}

const DEFAULT_API_BASE = "http://127.0.0.1:8088";
const DEFAULT_API_KEY = "";
const DEFAULT_USER_ID = "demo-user";
const DEFAULT_CHANNEL = "console";
const SETTINGS_KEY = "copaw-next.web.chat.settings";
const LOCALE_KEY = "copaw-next.web.locale";
const BUILTIN_PROVIDER_IDS = new Set(["openai"]);
const TABS: TabKey[] = ["chat", "models", "envs", "workspace", "cron"];

const apiBaseInput = mustElement<HTMLInputElement>("api-base");
const apiKeyInput = mustElement<HTMLInputElement>("api-key");
const userIdInput = mustElement<HTMLInputElement>("user-id");
const channelInput = mustElement<HTMLInputElement>("channel");
const localeSelect = mustElement<HTMLSelectElement>("locale-select");
const reloadChatsButton = mustElement<HTMLButtonElement>("reload-chats");
const settingsToggleButton = mustElement<HTMLButtonElement>("settings-toggle");
const settingsPopover = mustElement<HTMLElement>("settings-popover");
const statusLine = mustElement<HTMLElement>("status-line");

const tabButtons = Array.from(document.querySelectorAll<HTMLButtonElement>(".tab-btn"));

const panelChat = mustElement<HTMLElement>("panel-chat");
const panelModels = mustElement<HTMLElement>("panel-models");
const panelEnvs = mustElement<HTMLElement>("panel-envs");
const panelWorkspace = mustElement<HTMLElement>("panel-workspace");
const panelCron = mustElement<HTMLElement>("panel-cron");

const newChatButton = mustElement<HTMLButtonElement>("new-chat");
const chatList = mustElement<HTMLUListElement>("chat-list");
const chatTitle = mustElement<HTMLElement>("chat-title");
const chatSession = mustElement<HTMLElement>("chat-session");
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

const refreshEnvsButton = mustElement<HTMLButtonElement>("refresh-envs");
const envsTableBody = mustElement<HTMLTableSectionElement>("envs-table-body");
const envsForm = mustElement<HTMLFormElement>("envs-form");
const envsJSONInput = mustElement<HTMLTextAreaElement>("envs-json");

const refreshWorkspaceButton = mustElement<HTMLButtonElement>("refresh-workspace");
const workspaceFilesBody = mustElement<HTMLTableSectionElement>("workspace-files-body");
const workspaceEditorModal = mustElement<HTMLElement>("workspace-editor-modal");
const workspaceEditorModalCloseButton = mustElement<HTMLButtonElement>("workspace-editor-modal-close-btn");
const workspaceEditorForm = mustElement<HTMLFormElement>("workspace-editor-form");
const workspaceFilePathInput = mustElement<HTMLInputElement>("workspace-file-path");
const workspaceFileContentInput = mustElement<HTMLTextAreaElement>("workspace-file-content");
const workspaceSaveFileButton = mustElement<HTMLButtonElement>("workspace-save-file-btn");
const workspaceDeleteFileButton = mustElement<HTMLButtonElement>("workspace-delete-file-btn");

const refreshCronButton = mustElement<HTMLButtonElement>("refresh-cron");
const cronJobsBody = mustElement<HTMLTableSectionElement>("cron-jobs-body");
const cronCreateForm = mustElement<HTMLFormElement>("cron-create-form");
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

const panelByTab: Record<TabKey, HTMLElement> = {
  chat: panelChat,
  models: panelModels,
  envs: panelEnvs,
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
    models: false,
    envs: false,
    workspace: false,
    cron: false,
  },

  chats: [] as ChatSpec[],
  activeChatId: null as string | null,
  activeSessionId: newSessionID(),
  messages: [] as ViewMessage[],
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
  envs: [] as EnvVar[],
  workspaceFiles: [] as WorkspaceFileInfo[],
  activeWorkspacePath: "",
  activeWorkspaceContent: "",
  cronJobs: [] as CronJobSpec[],
  cronStates: {} as Record<string, CronJobState>,
};

void bootstrap();

async function bootstrap(): Promise<void> {
  initLocale();
  restoreSettings();
  bindEvents();
  setSettingsPopoverOpen(false);
  applyLocaleToDocument();
  renderTabPanels();
  renderChatHeader();
  renderChatList();
  renderMessages();
  renderWorkspaceFiles();
  renderWorkspaceEditor();
  syncCronDispatchHint();
  ensureCronSessionID();
  resetProviderModalForm();
  await syncModelStateOnBoot();

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
    event.stopPropagation();
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
    if (event.key !== "Escape") {
      return;
    }
    if (isWorkspaceEditorModalOpen()) {
      closeWorkspaceEditorModal();
      return;
    }
    if (isSettingsPopoverOpen()) {
      setSettingsPopoverOpen(false);
    }
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

  apiBaseInput.addEventListener("change", async () => {
    await handleControlChange(false);
  });
  apiKeyInput.addEventListener("change", async () => {
    await handleControlChange(false);
  });

  userIdInput.addEventListener("change", async () => {
    await handleControlChange(true);
  });

  channelInput.addEventListener("change", async () => {
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

  refreshEnvsButton.addEventListener("click", async () => {
    await refreshEnvs();
  });
  envsForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await putEnvs();
  });
  envsTableBody.addEventListener("click", async (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest<HTMLButtonElement>("button[data-env-key]");
    if (!button) {
      return;
    }
    const key = button.dataset.envKey ?? "";
    if (key === "") {
      return;
    }
    await deleteEnv(key);
  });

  refreshWorkspaceButton.addEventListener("click", async () => {
    await refreshWorkspace();
  });
  workspaceEditorModalCloseButton.addEventListener("click", () => {
    closeWorkspaceEditorModal();
  });
  workspaceEditorModal.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    if (target.closest("[data-workspace-modal-close=\"true\"]")) {
      closeWorkspaceEditorModal();
    }
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

  refreshCronButton.addEventListener("click", async () => {
    await refreshCronJobs();
  });
  cronNewSessionButton.addEventListener("click", () => {
    cronSessionIDInput.value = newSessionID();
  });
  cronCreateForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await createCronJob();
  });
  cronJobsBody.addEventListener("click", async (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest<HTMLButtonElement>("button[data-cron-run]");
    if (!button) {
      return;
    }
    const jobID = button.dataset.cronRun ?? "";
    if (jobID === "") {
      return;
    }
    await runCronJob(jobID);
  });
}

function isSettingsPopoverOpen(): boolean {
  return !settingsPopover.classList.contains("is-hidden");
}

function setSettingsPopoverOpen(open: boolean): void {
  settingsPopover.classList.toggle("is-hidden", !open);
  settingsPopover.setAttribute("aria-hidden", String(!open));
  settingsToggleButton.setAttribute("aria-expanded", String(open));
}

function isWorkspaceEditorModalOpen(): boolean {
  return !workspaceEditorModal.classList.contains("is-hidden");
}

function openWorkspaceEditorModal(): void {
  workspaceEditorModal.classList.remove("is-hidden");
}

function closeWorkspaceEditorModal(): void {
  workspaceEditorModal.classList.add("is-hidden");
}

function initLocale(): void {
  const savedLocale = localStorage.getItem(LOCALE_KEY);
  const locale = setLocale(savedLocale ?? navigator.language ?? DEFAULT_LOCALE);
  localeSelect.value = locale;
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
  renderMessages();
  if (state.tabLoaded.models) {
    renderModelsPanel();
  }
  if (state.tabLoaded.envs) {
    renderEnvsPanel();
  }
  if (state.tabLoaded.workspace) {
    renderWorkspaceFiles();
    renderWorkspaceEditor();
  }
  if (state.tabLoaded.cron) {
    renderCronJobs();
  }
  syncCronDispatchHint();
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
    if (tab === "chat") {
      return;
    }
    if (!force && state.tabLoaded[tab]) {
      return;
    }

    switch (tab) {
      case "models":
        await refreshModels();
        break;
      case "envs":
        await refreshEnvs();
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
      if (typeof parsed.channel === "string" && parsed.channel.trim() !== "") {
        state.channel = parsed.channel.trim();
      }
    } catch {
      localStorage.removeItem(SETTINGS_KEY);
    }
  }
  apiBaseInput.value = state.apiBase;
  apiKeyInput.value = state.apiKey;
  userIdInput.value = state.userId;
  channelInput.value = state.channel;
}

function syncControlState(): void {
  state.apiBase = apiBaseInput.value.trim() || DEFAULT_API_BASE;
  state.apiKey = apiKeyInput.value.trim();
  state.userId = userIdInput.value.trim() || DEFAULT_USER_ID;
  state.channel = channelInput.value.trim() || DEFAULT_CHANNEL;
  localStorage.setItem(
    SETTINGS_KEY,
    JSON.stringify({
      apiBase: state.apiBase,
      apiKey: state.apiKey,
      userId: state.userId,
      channel: state.channel,
    }),
  );
}

function invalidateResourceTabs(): void {
  state.tabLoaded.models = false;
  state.tabLoaded.envs = false;
  state.tabLoaded.workspace = false;
  state.tabLoaded.cron = false;
}

async function reloadChats(): Promise<void> {
  try {
    const query = new URLSearchParams({
      user_id: state.userId,
      channel: state.channel,
    });
    const chats = await requestJSON<ChatSpec[]>(`/chats?${query.toString()}`);
    state.chats = chats;
    if (state.activeChatId && !state.chats.some((chat) => chat.id === state.activeChatId)) {
      state.activeChatId = null;
    }
    renderChatList();
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

  try {
    const history = await requestJSON<ChatHistoryResponse>(`/chats/${encodeURIComponent(chat.id)}`);
    state.messages = history.messages.map(toViewMessage);
    renderMessages();
    setStatus(t("status.loadedMessages", { count: history.messages.length }), "info");
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
  renderMessages();
}

async function sendMessage(): Promise<void> {
  syncControlState();
  if (state.sending) {
    return;
  }

  const inputText = messageInput.value.trim();
  if (inputText === "") {
    setStatus(t("status.inputRequired"), "error");
    return;
  }

  if (state.apiBase === "" || state.userId === "" || state.channel === "") {
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
    },
    {
      id: assistantID,
      role: "assistant",
      text: "",
    },
  );
  renderMessages();
  messageInput.value = "";
  setStatus(t("status.streamingReply"), "info");

  try {
    await streamReply(inputText, bizParams, (delta) => {
      const target = state.messages.find((item) => item.id === assistantID);
      if (!target) {
        return;
      }
      target.text += delta;
      renderMessages();
    });
    setStatus(t("status.replyCompleted"), "info");

    await reloadChats();
    const matched = state.chats.find(
      (chat) =>
        chat.session_id === state.activeSessionId &&
        chat.user_id === state.userId &&
        chat.channel === state.channel,
    );
    if (matched) {
      await openChat(matched.id);
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
): Promise<void> {
  const payload: Record<string, unknown> = {
    input: [{ role: "user", type: "message", content: [{ type: "text", text: userText }] }],
    session_id: state.activeSessionId,
    user_id: state.userId,
    channel: state.channel,
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
    const result = consumeSSEBuffer(buffer, onDelta);
    buffer = result.rest;
    doneReceived = result.done;
  }

  buffer += decoder.decode().replaceAll("\r", "");
  if (!doneReceived && buffer.trim() !== "") {
    const result = consumeSSEBuffer(`${buffer}\n\n`, onDelta);
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

function consumeSSEBuffer(raw: string, onDelta: (delta: string) => void): { done: boolean; rest: string } {
  let buffer = raw;
  let done = false;
  while (!done) {
    const boundary = buffer.indexOf("\n\n");
    if (boundary < 0) {
      break;
    }
    const block = buffer.slice(0, boundary);
    buffer = buffer.slice(boundary + 2);
    done = consumeSSEBlock(block, onDelta) || done;
  }
  return { done, rest: buffer };
}

function consumeSSEBlock(block: string, onDelta: (delta: string) => void): boolean {
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
  try {
    const payload = JSON.parse(data) as { delta?: unknown };
    if (typeof payload.delta === "string") {
      onDelta(payload.delta);
      return false;
    }
  } catch {
    onDelta(data);
    return false;
  }
  throw new Error(t("error.invalidSSEPayload"));
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
    li.appendChild(button);
    chatList.appendChild(li);
  });
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
    item.textContent = message.text || (message.role === "assistant" ? t("common.ellipsis") : "");
    messageList.appendChild(item);
  }
  messageList.scrollTop = messageList.scrollHeight;
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
}

function renderActiveProviderOptions(): void {
  modelsActiveProviderSelect.innerHTML = "";
  if (state.providers.length === 0) {
    const emptyOption = document.createElement("option");
    emptyOption.value = "";
    emptyOption.textContent = t("models.noProviderOption");
    modelsActiveProviderSelect.appendChild(emptyOption);
    return;
  }

  for (const provider of state.providers) {
    const option = document.createElement("option");
    option.value = provider.id;
    option.textContent = formatProviderLabel(provider);
    modelsActiveProviderSelect.appendChild(option);
  }
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
  const providerID =
    state.providerModal.mode === "edit" && state.providerModal.editingProviderID !== ""
      ? state.providerModal.editingProviderID
      : selectedProviderType;
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

  let customModels: string[] | undefined;
  if (providerSupportsCustomModels(providerID)) {
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

async function refreshEnvs(): Promise<void> {
  syncControlState();
  try {
    state.envs = await requestJSON<EnvVar[]>("/envs");
    state.tabLoaded.envs = true;
    renderEnvsPanel();
    setStatus(t("status.envsLoaded", { count: state.envs.length }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function renderEnvsPanel(): void {
  envsTableBody.innerHTML = "";
  if (state.envs.length === 0) {
    const row = document.createElement("tr");
    const col = document.createElement("td");
    col.colSpan = 3;
    col.className = "empty-cell";
    col.textContent = t("env.empty");
    row.appendChild(col);
    envsTableBody.appendChild(row);
  } else {
    state.envs.forEach((env) => {
      const row = document.createElement("tr");

      const key = document.createElement("td");
      key.className = "mono";
      key.textContent = env.key;

      const value = document.createElement("td");
      value.className = "mono";
      value.textContent = env.value;

      const action = document.createElement("td");
      const button = document.createElement("button");
      button.type = "button";
      button.className = "secondary-btn";
      button.dataset.envKey = env.key;
      button.textContent = t("common.delete");
      action.appendChild(button);

      row.append(key, value, action);
      envsTableBody.appendChild(row);
    });
  }

  const envMap: Record<string, string> = {};
  for (const env of state.envs) {
    envMap[env.key] = env.value;
  }
  envsJSONInput.value = JSON.stringify(envMap, null, 2);
}

async function putEnvs(): Promise<void> {
  syncControlState();
  let body: Record<string, string>;
  try {
    body = parseEnvMap(envsJSONInput.value, {
      invalidJSON: t("error.invalidEnvJSON"),
      invalidMap: t("error.invalidEnvMap"),
      invalidKey: t("error.invalidEnvKey"),
      invalidValue: (key) => t("error.invalidEnvValue", { key }),
    });
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return;
  }

  try {
    state.envs = await requestJSON<EnvVar[]>("/envs", {
      method: "PUT",
      body,
    });
    renderEnvsPanel();
    setStatus(t("status.envMapUpdated", { count: state.envs.length }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

async function deleteEnv(key: string): Promise<void> {
  syncControlState();
  try {
    state.envs = await requestJSON<EnvVar[]>(`/envs/${encodeURIComponent(key)}`, {
      method: "DELETE",
    });
    renderEnvsPanel();
    setStatus(t("status.envDeleted", { key }), "info");
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
    actionCol.appendChild(openButton);
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

async function openWorkspaceFile(path: string): Promise<void> {
  syncControlState();
  try {
    const payload = await getWorkspaceFile(path);
    state.activeWorkspacePath = path;
    state.activeWorkspaceContent = JSON.stringify(payload, null, 2);
    renderWorkspaceEditor();
    openWorkspaceEditorModal();
    setStatus(t("status.workspaceFileLoaded", { path }), "info");
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
  try {
    payload = JSON.parse(workspaceFileContentInput.value);
  } catch {
    setStatus(t("error.workspaceInvalidJSON"), "error");
    return;
  }
  try {
    await putWorkspaceFile(path, payload);
    state.activeWorkspacePath = path;
    state.activeWorkspaceContent = JSON.stringify(payload, null, 2);
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
  renderWorkspaceEditor();
  closeWorkspaceEditorModal();
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
    col.colSpan = 5;
    col.className = "empty-cell";
    col.textContent = t("cron.empty");
    row.appendChild(col);
    cronJobsBody.appendChild(row);
    return;
  }

  state.cronJobs.forEach((job) => {
    const row = document.createElement("tr");
    const nextRun = state.cronStates[job.id]?.next_run_at;

    const idCol = document.createElement("td");
    idCol.className = "mono";
    idCol.textContent = job.id;

    const nameCol = document.createElement("td");
    nameCol.textContent = job.name;

    const enabledCol = document.createElement("td");
    enabledCol.textContent = job.enabled ? t("common.yes") : t("common.no");

    const nextCol = document.createElement("td");
    nextCol.textContent = nextRun ? compactTime(nextRun) : t("common.none");

    const actionCol = document.createElement("td");
    const runBtn = document.createElement("button");
    runBtn.type = "button";
    runBtn.className = "secondary-btn";
    runBtn.dataset.cronRun = job.id;
    runBtn.textContent = t("cron.run");
    actionCol.appendChild(runBtn);

    row.append(idCol, nameCol, enabledCol, nextCol, actionCol);
    cronJobsBody.appendChild(row);
  });
}

async function createCronJob(): Promise<void> {
  syncControlState();

  const id = cronIDInput.value.trim();
  const name = cronNameInput.value.trim();
  const intervalText = cronIntervalInput.value.trim();
  const sessionID = cronSessionIDInput.value.trim();
  const text = cronTextInput.value.trim();

  if (id === "" || name === "") {
    setStatus(t("error.cronIdNameRequired"), "error");
    return;
  }
  if (intervalText === "") {
    setStatus(t("error.cronScheduleRequired"), "error");
    return;
  }
  if (sessionID === "") {
    setStatus(t("error.cronSessionRequired"), "error");
    return;
  }
  if (text === "") {
    setStatus(t("error.cronTextRequired"), "error");
    return;
  }

  const maxConcurrency = parseIntegerInput(cronMaxConcurrencyInput.value, 1, 1);
  const timeoutSeconds = parseIntegerInput(cronTimeoutInput.value, 30, 1);
  const misfireGraceSeconds = parseIntegerInput(cronMisfireInput.value, 0, 0);

  const payload: CronJobSpec = {
    id,
    name,
    enabled: cronEnabledInput.checked,
    schedule: {
      type: "interval",
      cron: intervalText,
      timezone: "",
    },
    task_type: "text",
    text,
    dispatch: {
      type: "channel",
      channel: state.channel,
      target: {
        user_id: state.userId,
        session_id: sessionID,
      },
      mode: "",
      meta: {},
    },
    runtime: {
      max_concurrency: maxConcurrency,
      timeout_seconds: timeoutSeconds,
      misfire_grace_seconds: misfireGraceSeconds,
    },
    meta: {},
  };

  try {
    await requestJSON<CronJobSpec>("/cron/jobs", {
      method: "POST",
      body: payload,
    });
    await refreshCronJobs();
    setStatus(t("status.cronCreated", { jobId: id }), "info");
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

function toViewMessage(message: RuntimeMessage): ViewMessage {
  const joined = (message.content ?? [])
    .map((item) => item.text ?? "")
    .join("")
    .trim();
  return {
    id: message.id || `msg-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    role: message.role === "user" ? "user" : "assistant",
    text: joined,
  };
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
  return value === "chat" || value === "models" || value === "envs" || value === "workspace" || value === "cron";
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
