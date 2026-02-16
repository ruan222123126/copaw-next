import { parseEnvMap, parseErrorMessage } from "./api-utils.js";
import { DEFAULT_LOCALE, getLocale, isWebMessageKey, setLocale, t } from "./i18n.js";

type Tone = "neutral" | "info" | "error";
type TabKey = "chat" | "models" | "envs" | "skills" | "workspace" | "cron";
type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";

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

interface ModelSlotConfig {
  provider_id: string;
  model: string;
}

interface ActiveModelsInfo {
  active_llm: ModelSlotConfig;
}

interface ModelCatalogInfo {
  providers: ProviderInfo[];
  defaults: Record<string, string>;
  active_llm: ModelSlotConfig;
}

interface EnvVar {
  key: string;
  value: string;
}

interface SkillSpec {
  name: string;
  source?: string;
  enabled: boolean;
  path?: string;
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
const DEFAULT_USER_ID = "demo-user";
const DEFAULT_CHANNEL = "console";
const SETTINGS_KEY = "copaw-next.web.chat.settings";
const LOCALE_KEY = "copaw-next.web.locale";
const TABS: TabKey[] = ["chat", "models", "envs", "skills", "workspace", "cron"];

const apiBaseInput = mustElement<HTMLInputElement>("api-base");
const userIdInput = mustElement<HTMLInputElement>("user-id");
const channelInput = mustElement<HTMLInputElement>("channel");
const localeSelect = mustElement<HTMLSelectElement>("locale-select");
const reloadChatsButton = mustElement<HTMLButtonElement>("reload-chats");
const statusLine = mustElement<HTMLElement>("status-line");

const tabButtons = Array.from(document.querySelectorAll<HTMLButtonElement>(".tab-btn"));

const panelChat = mustElement<HTMLElement>("panel-chat");
const panelModels = mustElement<HTMLElement>("panel-models");
const panelEnvs = mustElement<HTMLElement>("panel-envs");
const panelSkills = mustElement<HTMLElement>("panel-skills");
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
const modelsProviderList = mustElement<HTMLUListElement>("models-provider-list");
const modelsActiveText = mustElement<HTMLElement>("models-active-text");
const modelsActiveForm = mustElement<HTMLFormElement>("models-active-form");
const modelsProviderSelect = mustElement<HTMLSelectElement>("models-provider-select");
const modelsModelSelect = mustElement<HTMLSelectElement>("models-model-select");
const modelsModelInput = mustElement<HTMLInputElement>("models-model-input");
const modelsConfigForm = mustElement<HTMLFormElement>("models-config-form");
const modelsProviderIDSelect = mustElement<HTMLSelectElement>("models-provider-id-select");
const modelsProviderNameInput = mustElement<HTMLInputElement>("models-provider-name-input");
const modelsProviderAPIKeyInput = mustElement<HTMLInputElement>("models-provider-api-key-input");
const modelsProviderBaseURLInput = mustElement<HTMLInputElement>("models-provider-base-url-input");
const modelsProviderTimeoutMSInput = mustElement<HTMLInputElement>("models-provider-timeout-ms-input");
const modelsProviderEnabledInput = mustElement<HTMLInputElement>("models-provider-enabled-input");
const modelsProviderHeadersInput = mustElement<HTMLTextAreaElement>("models-provider-headers-input");
const modelsProviderAliasesInput = mustElement<HTMLTextAreaElement>("models-provider-aliases-input");
const modelsProviderResetButton = mustElement<HTMLButtonElement>("models-provider-reset-btn");

const refreshEnvsButton = mustElement<HTMLButtonElement>("refresh-envs");
const envsTableBody = mustElement<HTMLTableSectionElement>("envs-table-body");
const envsForm = mustElement<HTMLFormElement>("envs-form");
const envsJSONInput = mustElement<HTMLTextAreaElement>("envs-json");

const refreshSkillsButton = mustElement<HTMLButtonElement>("refresh-skills");
const skillsAllList = mustElement<HTMLUListElement>("skills-all-list");
const skillsEnabledList = mustElement<HTMLUListElement>("skills-enabled-list");

const refreshWorkspaceButton = mustElement<HTMLButtonElement>("refresh-workspace");
const workspaceDownloadLink = mustElement<HTMLAnchorElement>("workspace-download-link");
const workspaceUploadForm = mustElement<HTMLFormElement>("workspace-upload-form");
const workspaceUploadFileInput = mustElement<HTMLInputElement>("workspace-upload-file");

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
  skills: panelSkills,
  workspace: panelWorkspace,
  cron: panelCron,
};

const state = {
  apiBase: DEFAULT_API_BASE,
  userId: DEFAULT_USER_ID,
  channel: DEFAULT_CHANNEL,
  activeTab: "chat" as TabKey,
  tabLoaded: {
    chat: true,
    models: false,
    envs: false,
    skills: false,
    workspace: false,
    cron: false,
  },

  chats: [] as ChatSpec[],
  activeChatId: null as string | null,
  activeSessionId: newSessionID(),
  messages: [] as ViewMessage[],
  sending: false,

  providers: [] as ProviderInfo[],
  modelDefaults: {} as Record<string, string>,
  activeModels: null as ActiveModelsInfo | null,
  envs: [] as EnvVar[],
  skillsAll: [] as SkillSpec[],
  skillsAvailable: [] as SkillSpec[],
  cronJobs: [] as CronJobSpec[],
  cronStates: {} as Record<string, CronJobState>,
};

void bootstrap();

async function bootstrap(): Promise<void> {
  initLocale();
  restoreSettings();
  bindEvents();
  applyLocaleToDocument();
  renderTabPanels();
  renderChatHeader();
  renderChatList();
  renderMessages();
  setWorkspaceDownloadLink();
  syncCronDispatchHint();
  ensureCronSessionID();
  resetProviderForm();

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
  modelsProviderSelect.addEventListener("change", () => {
    renderModelOptionsForProvider(modelsProviderSelect.value, modelsModelSelect.value);
  });
  modelsConfigForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await upsertProvider();
  });
  modelsProviderIDSelect.addEventListener("change", () => {
    populateProviderForm(modelsProviderIDSelect.value, true);
  });
  modelsProviderResetButton.addEventListener("click", () => {
    resetProviderForm();
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
      populateProviderForm(providerID);
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

  refreshSkillsButton.addEventListener("click", async () => {
    await refreshSkills();
  });
  skillsAllList.addEventListener("click", async (event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest<HTMLButtonElement>("button[data-skill-name]");
    if (!button) {
      return;
    }
    const skillName = button.dataset.skillName ?? "";
    const action = button.dataset.skillAction;
    if (skillName === "" || (action !== "enable" && action !== "disable")) {
      return;
    }
    await toggleSkill(skillName, action === "enable");
  });

  refreshWorkspaceButton.addEventListener("click", () => {
    refreshWorkspace();
  });
  workspaceDownloadLink.addEventListener("click", () => {
    syncControlState();
    setWorkspaceDownloadLink();
  });
  workspaceUploadForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await uploadWorkspace();
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

  renderChatHeader();
  renderChatList();
  renderMessages();
  if (state.tabLoaded.models) {
    renderModelsPanel();
  }
  if (state.tabLoaded.envs) {
    renderEnvsPanel();
  }
  if (state.tabLoaded.skills) {
    renderSkillsPanel();
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
  setWorkspaceDownloadLink();
  invalidateResourceTabs();

  await reloadChats();
  if (state.activeTab !== "chat") {
    await loadTabData(state.activeTab, true);
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
      case "skills":
        await refreshSkills();
        break;
      case "workspace":
        refreshWorkspace();
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
  userIdInput.value = state.userId;
  channelInput.value = state.channel;
}

function syncControlState(): void {
  state.apiBase = apiBaseInput.value.trim() || DEFAULT_API_BASE;
  state.userId = userIdInput.value.trim() || DEFAULT_USER_ID;
  state.channel = channelInput.value.trim() || DEFAULT_CHANNEL;
  localStorage.setItem(
    SETTINGS_KEY,
    JSON.stringify({
      apiBase: state.apiBase,
      userId: state.userId,
      channel: state.channel,
    }),
  );
}

function invalidateResourceTabs(): void {
  state.tabLoaded.models = false;
  state.tabLoaded.envs = false;
  state.tabLoaded.skills = false;
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
    await streamReply(inputText, (delta) => {
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

async function streamReply(userText: string, onDelta: (delta: string) => void): Promise<void> {
  const payload = {
    input: [{ role: "user", type: "message", content: [{ type: "text", text: userText }] }],
    session_id: state.activeSessionId,
    user_id: state.userId,
    channel: state.channel,
    stream: true,
  };

  const response = await fetch(toAbsoluteURL("/agent/process"), {
    method: "POST",
    headers: {
      "content-type": "application/json",
      accept: "text/event-stream,application/json",
    },
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
    const result = await loadModelCatalog();
    state.providers = result.providers;
    state.modelDefaults = result.defaults;
    state.activeModels = { active_llm: result.active };
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

async function loadModelCatalog(): Promise<{
  providers: ProviderInfo[];
  defaults: Record<string, string>;
  active: ModelSlotConfig;
  source: "catalog" | "legacy";
}> {
  try {
    const catalog = await requestJSON<ModelCatalogInfo>("/models/catalog");
    const providers = normalizeProviders(catalog.providers);
    return {
      providers,
      defaults: normalizeDefaults(catalog.defaults, providers),
      active: normalizeActiveModel(catalog.active_llm),
      source: "catalog",
    };
  } catch {
    const [providersRaw, activeRaw] = await Promise.all([
      requestJSON<ProviderInfo[]>("/models"),
      requestJSON<ActiveModelsInfo>("/models/active"),
    ]);
    const providers = normalizeProviders(providersRaw);
    return {
      providers,
      defaults: buildDefaultMapFromProviders(providers),
      active: normalizeActiveModel(activeRaw.active_llm),
      source: "legacy",
    };
  }
}

function renderModelsPanel(): void {
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

      actions.append(editButton);
      item.append(title, enabledLine, keyStatus, defaultLine, baseURLLine, modelLine, actions);
      modelsProviderList.appendChild(item);
    }
  }

  const activeLLM = state.activeModels?.active_llm;
  modelsActiveText.textContent = activeLLM
    ? t("models.activeSummary", {
        providerId: activeLLM.provider_id,
        model: activeLLM.model,
      })
    : t("common.none");

  const preferredProvider = activeLLM?.provider_id || modelsProviderSelect.value || state.providers[0]?.id || "";
  const preferredModel = activeLLM?.model || modelsModelSelect.value || state.modelDefaults[preferredProvider] || "";

  modelsProviderSelect.innerHTML = "";
  if (state.providers.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = t("models.noProviderOption");
    modelsProviderSelect.appendChild(option);
  } else {
    for (const provider of state.providers) {
      const option = document.createElement("option");
      option.value = provider.id;
      option.textContent = formatProviderLabel(provider);
      modelsProviderSelect.appendChild(option);
    }
  }

  if (preferredProvider !== "") {
    modelsProviderSelect.value = preferredProvider;
  }
  renderModelOptionsForProvider(modelsProviderSelect.value, preferredModel);
  renderProviderConfigProviderOptions(modelsProviderIDSelect.value || preferredProvider);
  populateProviderForm(modelsProviderIDSelect.value, true);
}

function renderProviderConfigProviderOptions(preferredProviderID = ""): void {
  const fallbackProviderID = preferredProviderID || state.providers[0]?.id || "";
  modelsProviderIDSelect.innerHTML = "";
  if (state.providers.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = t("models.noProviderOption");
    modelsProviderIDSelect.appendChild(option);
    modelsProviderIDSelect.value = "";
    return;
  }
  for (const provider of state.providers) {
    const option = document.createElement("option");
    option.value = provider.id;
    option.textContent = formatProviderLabel(provider);
    modelsProviderIDSelect.appendChild(option);
  }
  modelsProviderIDSelect.value = state.providers.some((item) => item.id === fallbackProviderID)
    ? fallbackProviderID
    : state.providers[0].id;
}

function renderModelOptionsForProvider(providerID: string, preferredModel = ""): void {
  modelsModelSelect.innerHTML = "";
  const provider = state.providers.find((item) => item.id === providerID);
  const modelIDs = dedupeModelIDs(provider?.models ?? [], state.modelDefaults[providerID]);

  if (modelIDs.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = t("models.noModelOption");
    modelsModelSelect.appendChild(option);
    modelsModelSelect.value = "";
    return;
  }

  for (const modelID of modelIDs) {
    const option = document.createElement("option");
    option.value = modelID;
    option.textContent = modelID;
    modelsModelSelect.appendChild(option);
  }

  if (preferredModel !== "" && modelIDs.includes(preferredModel)) {
    modelsModelSelect.value = preferredModel;
  } else if (state.modelDefaults[providerID] && modelIDs.includes(state.modelDefaults[providerID])) {
    modelsModelSelect.value = state.modelDefaults[providerID];
  } else {
    modelsModelSelect.value = modelIDs[0];
  }
}

async function setActiveModel(): Promise<void> {
  syncControlState();
  const providerID = modelsProviderSelect.value.trim();
  const model = modelsModelInput.value.trim() || modelsModelSelect.value.trim();

  if (providerID === "" || model === "") {
    setStatus(t("error.providerAndModelRequired"), "error");
    return;
  }

  try {
    await requestJSON<ActiveModelsInfo>("/models/active", {
      method: "PUT",
      body: {
        provider_id: providerID,
        model,
      },
    });
    modelsModelInput.value = "";
    await refreshModels();
    setStatus(t("status.activeModelUpdated", { providerId: providerID, model }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function resetProviderForm(): void {
  if (state.providers.length > 0) {
    renderProviderConfigProviderOptions(state.providers[0].id);
    populateProviderForm(modelsProviderIDSelect.value, true);
    return;
  }
  modelsProviderIDSelect.value = "";
  modelsProviderNameInput.value = "";
  modelsProviderAPIKeyInput.value = "";
  modelsProviderBaseURLInput.value = "";
  modelsProviderTimeoutMSInput.value = "";
  modelsProviderEnabledInput.checked = true;
  modelsProviderHeadersInput.value = "{}";
  modelsProviderAliasesInput.value = "{}";
}

function populateProviderForm(providerID: string, silent = false): void {
  const provider = state.providers.find((item) => item.id === providerID);
  if (!provider) {
    if (!silent) {
      setStatus(t("status.providerNotFound", { providerId: providerID }), "error");
    }
    return;
  }
  modelsProviderIDSelect.value = provider.id;
  modelsProviderNameInput.value = provider.display_name ?? provider.name ?? provider.id;
  modelsProviderAPIKeyInput.value = "";
  modelsProviderBaseURLInput.value = provider.current_base_url ?? "";
  modelsProviderEnabledInput.checked = provider.enabled !== false;
  modelsProviderTimeoutMSInput.value = "";
  modelsProviderHeadersInput.value = "{}";
  modelsProviderAliasesInput.value = "{}";
  if (!silent) {
    setStatus(t("status.providerLoadedForEdit", { providerId: provider.id }), "info");
  }
}

async function upsertProvider(): Promise<void> {
  syncControlState();
  const providerID = modelsProviderIDSelect.value.trim();
  if (providerID === "") {
    setStatus(t("error.providerIDRequired"), "error");
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
    headers = parseOptionalStringMap(modelsProviderHeadersInput.value, {
      invalidJSON: t("error.invalidProviderHeadersJSON"),
      invalidMap: t("error.invalidProviderHeadersMap"),
      invalidKey: t("error.invalidProviderHeadersKey"),
      invalidValue: (key) => t("error.invalidProviderHeadersValue", { key }),
    });
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return;
  }

  let aliases: Record<string, string> | undefined;
  try {
    aliases = parseOptionalStringMap(modelsProviderAliasesInput.value, {
      invalidJSON: t("error.invalidProviderAliasesJSON"),
      invalidMap: t("error.invalidProviderAliasesMap"),
      invalidKey: t("error.invalidProviderAliasesKey"),
      invalidValue: (key) => t("error.invalidProviderAliasesValue", { key }),
    });
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return;
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
  if (aliases) {
    payload.model_aliases = aliases;
  }

  try {
    const out = await requestJSON<ProviderInfo>(`/models/${encodeURIComponent(providerID)}/config`, {
      method: "PUT",
      body: payload,
    });
    await refreshModels();
    renderProviderConfigProviderOptions(out.id);
    populateProviderForm(out.id, true);
    modelsProviderAPIKeyInput.value = "";
    setStatus(t("status.providerConfigSaved", { providerId: out.id }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function parseOptionalStringMap(
  raw: string,
  messages: {
    invalidJSON: string;
    invalidMap: string;
    invalidKey: string;
    invalidValue: (key: string) => string;
  },
): Record<string, string> | undefined {
  if (raw.trim() === "") {
    return undefined;
  }
  return parseEnvMap(raw, messages);
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

async function refreshSkills(): Promise<void> {
  syncControlState();
  try {
    const [allSkills, availableSkills] = await Promise.all([
      requestJSON<SkillSpec[]>("/skills"),
      requestJSON<SkillSpec[]>("/skills/available"),
    ]);
    state.skillsAll = allSkills;
    state.skillsAvailable = availableSkills;
    state.tabLoaded.skills = true;
    renderSkillsPanel();
    setStatus(t("status.skillsLoaded", { count: allSkills.length }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function renderSkillsPanel(): void {
  skillsAllList.innerHTML = "";
  skillsEnabledList.innerHTML = "";

  if (state.skillsAll.length === 0) {
    appendEmptyItem(skillsAllList, t("skills.empty"));
  } else {
    state.skillsAll.forEach((skill) => {
      const item = document.createElement("li");
      item.className = "detail-item";

      const title = document.createElement("p");
      title.className = "item-title";
      title.textContent = skill.name;

      const meta = document.createElement("p");
      meta.className = "item-meta";
      const source = skill.source ?? t("skills.sourceUnknown");
      meta.textContent = t("skills.meta", {
        source,
        enabled: skill.enabled ? t("common.yes") : t("common.no"),
      });

      const action = document.createElement("button");
      action.type = "button";
      action.className = "secondary-btn";
      action.dataset.skillName = skill.name;
      action.dataset.skillAction = skill.enabled ? "disable" : "enable";
      action.textContent = skill.enabled ? t("skills.disable") : t("skills.enable");

      item.append(title, meta, action);
      skillsAllList.appendChild(item);
    });
  }

  if (state.skillsAvailable.length === 0) {
    appendEmptyItem(skillsEnabledList, t("skills.enabledEmpty"));
  } else {
    state.skillsAvailable.forEach((skill) => {
      const item = document.createElement("li");
      item.textContent = skill.name;
      skillsEnabledList.appendChild(item);
    });
  }
}

async function toggleSkill(name: string, enable: boolean): Promise<void> {
  syncControlState();
  const action = enable ? "enable" : "disable";
  try {
    await requestJSON<Record<string, boolean>>(`/skills/${encodeURIComponent(name)}/${action}`, {
      method: "POST",
    });
    await refreshSkills();
    setStatus(
      t("status.skillToggled", {
        action: enable ? t("skills.enable") : t("skills.disable"),
        name,
      }),
      "info",
    );
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function refreshWorkspace(): void {
  syncControlState();
  setWorkspaceDownloadLink();
  state.tabLoaded.workspace = true;
  setStatus(t("status.workspaceLinkRefreshed"), "info");
}

function setWorkspaceDownloadLink(): void {
  workspaceDownloadLink.href = toAbsoluteURL("/workspace/download");
}

async function uploadWorkspace(): Promise<void> {
  syncControlState();
  const file = workspaceUploadFileInput.files?.[0];
  if (!file) {
    setStatus(t("status.zipRequired"), "error");
    return;
  }

  const formData = new FormData();
  formData.append("file", file);

  try {
    const result = await requestJSON<Record<string, boolean>>("/workspace/upload", {
      method: "POST",
      body: formData,
    });
    workspaceUploadFileInput.value = "";
    const success = result.success === true;
    setStatus(success ? t("status.workspaceUploadSuccess") : t("status.workspaceUploadDone"), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
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

function normalizeActiveModel(active: ModelSlotConfig | undefined): ModelSlotConfig {
  return {
    provider_id: active?.provider_id ?? "",
    model: active?.model ?? "",
  };
}

function dedupeModelIDs(models: ModelInfo[], defaultModelID?: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  if (defaultModelID && defaultModelID.trim() !== "") {
    const normalized = defaultModelID.trim();
    seen.add(normalized);
    out.push(normalized);
  }
  for (const model of models) {
    const modelID = model.id.trim();
    if (modelID === "" || seen.has(modelID)) {
      continue;
    }
    seen.add(modelID);
    out.push(modelID);
  }
  return out;
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

function compactTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString(getLocale(), { hour12: false });
}

function isTabKey(value: string | undefined): value is TabKey {
  return value === "chat" || value === "models" || value === "envs" || value === "skills" || value === "workspace" || value === "cron";
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
