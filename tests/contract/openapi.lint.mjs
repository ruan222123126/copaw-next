import { readFile } from "node:fs/promises";
import process from "node:process";
import { parse } from "yaml";

const specPath = new URL("../../packages/contracts/openapi/openapi.yaml", import.meta.url);

const raw = await readFile(specPath, "utf8");
const spec = parse(raw);
const failures = [];

function expect(condition, message) {
  if (!condition) {
    failures.push(message);
  }
}

function hasRequired(schema, key) {
  return Array.isArray(schema?.required) && schema.required.includes(key);
}

const schemas = spec?.components?.schemas ?? {};
const chatSpec = schemas.ChatSpec;
const runtimeContent = schemas.RuntimeContent;
const agentInputMessage = schemas.AgentInputMessage;
const agentProcessRequest = schemas.AgentProcessRequest;
const cronScheduleSpec = schemas.CronScheduleSpec;
const cronDispatchTarget = schemas.CronDispatchTarget;
const cronRuntimeSpec = schemas.CronRuntimeSpec;
const cronJobSpec = schemas.CronJobSpec;
const cronJobState = schemas.CronJobState;
const cronJobView = schemas.CronJobView;
const modelSlotConfig = schemas.ModelSlotConfig;
const activeModelsInfo = schemas.ActiveModelsInfo;
const modelInfo = schemas.ModelInfo;
const providerInfo = schemas.ProviderInfo;
const providerTypeInfo = schemas.ProviderTypeInfo;
const providerConfigPatch = schemas.ProviderConfigPatch;
const deleteResult = schemas.DeleteResult;
const modelCatalogInfo = schemas.ModelCatalogInfo;
const apiKeyAuth = spec?.components?.securitySchemes?.ApiKeyAuth;

expect(spec?.openapi === "3.0.3", "openapi 版本必须是 3.0.3");
expect(typeof spec?.paths === "object", "paths 必须存在");

expect(chatSpec?.properties?.created_at?.format === "date-time", "ChatSpec.created_at 必须声明 date-time format");
expect(chatSpec?.properties?.updated_at?.format === "date-time", "ChatSpec.updated_at 必须声明 date-time format");
expect(hasRequired(chatSpec, "session_id"), "ChatSpec.required 必须包含 session_id");
expect(hasRequired(chatSpec, "user_id"), "ChatSpec.required 必须包含 user_id");
expect(hasRequired(chatSpec, "channel"), "ChatSpec.required 必须包含 channel");

expect(Array.isArray(runtimeContent?.properties?.type?.enum), "RuntimeContent.type 必须声明 enum");
expect(runtimeContent?.properties?.type?.enum?.includes("text"), "RuntimeContent.type enum 必须包含 text");
expect(hasRequired(runtimeContent, "type"), "RuntimeContent.required 必须包含 type");

expect(Array.isArray(agentInputMessage?.properties?.role?.enum), "AgentInputMessage.role 必须声明 enum");
expect(agentInputMessage?.properties?.role?.enum?.includes("user"), "AgentInputMessage.role enum 必须包含 user");
expect(agentInputMessage?.properties?.role?.enum?.includes("assistant"), "AgentInputMessage.role enum 必须包含 assistant");
expect(agentInputMessage?.properties?.type?.enum?.includes("message"), "AgentInputMessage.type enum 必须包含 message");
expect(agentInputMessage?.properties?.content?.minItems === 1, "AgentInputMessage.content 必须设置 minItems=1");
expect(hasRequired(agentInputMessage, "role"), "AgentInputMessage.required 必须包含 role");
expect(hasRequired(agentInputMessage, "type"), "AgentInputMessage.required 必须包含 type");
expect(hasRequired(agentInputMessage, "content"), "AgentInputMessage.required 必须包含 content");

expect(agentProcessRequest?.properties?.input?.minItems === 1, "AgentProcessRequest.input 必须设置 minItems=1");
expect(hasRequired(agentProcessRequest, "input"), "AgentProcessRequest.required 必须包含 input");
expect(hasRequired(agentProcessRequest, "session_id"), "AgentProcessRequest.required 必须包含 session_id");
expect(hasRequired(agentProcessRequest, "user_id"), "AgentProcessRequest.required 必须包含 user_id");
expect(agentProcessRequest?.properties?.channel?.minLength === 1, "AgentProcessRequest.channel 必须设置 minLength=1");
expect(hasRequired(agentProcessRequest, "stream"), "AgentProcessRequest.required 必须包含 stream");

expect(Array.isArray(cronScheduleSpec?.properties?.type?.enum), "CronScheduleSpec.type 必须声明 enum");
expect(cronScheduleSpec?.properties?.type?.enum?.includes("interval"), "CronScheduleSpec.type enum 必须包含 interval");
expect(cronScheduleSpec?.properties?.type?.enum?.includes("cron"), "CronScheduleSpec.type enum 必须包含 cron");
expect(hasRequired(cronScheduleSpec, "cron"), "CronScheduleSpec.required 必须包含 cron");

expect(hasRequired(cronDispatchTarget, "user_id"), "CronDispatchTarget.required 必须包含 user_id");
expect(hasRequired(cronDispatchTarget, "session_id"), "CronDispatchTarget.required 必须包含 session_id");

expect(cronRuntimeSpec?.properties?.max_concurrency?.minimum === 1, "CronRuntimeSpec.max_concurrency 必须设置 minimum=1");
expect(cronRuntimeSpec?.properties?.timeout_seconds?.minimum === 1, "CronRuntimeSpec.timeout_seconds 必须设置 minimum=1");
expect(cronRuntimeSpec?.properties?.misfire_grace_seconds?.minimum === 0, "CronRuntimeSpec.misfire_grace_seconds 必须设置 minimum=0");

expect(hasRequired(cronJobSpec, "schedule"), "CronJobSpec.required 必须包含 schedule");
expect(hasRequired(cronJobSpec, "task_type"), "CronJobSpec.required 必须包含 task_type");
expect(hasRequired(cronJobSpec, "dispatch"), "CronJobSpec.required 必须包含 dispatch");
expect(hasRequired(cronJobSpec, "runtime"), "CronJobSpec.required 必须包含 runtime");

expect(Array.isArray(cronJobState?.properties?.last_status?.enum), "CronJobState.last_status 必须声明 enum");
expect(cronJobState?.properties?.last_status?.enum?.includes("succeeded"), "CronJobState.last_status enum 必须包含 succeeded");
expect(cronJobState?.properties?.paused?.type === "boolean", "CronJobState.paused 必须是 boolean");
expect(hasRequired(cronJobView, "spec"), "CronJobView.required 必须包含 spec");
expect(hasRequired(cronJobView, "state"), "CronJobView.required 必须包含 state");

expect(modelSlotConfig?.properties?.provider_id?.minLength === 1, "ModelSlotConfig.provider_id 必须设置 minLength=1");
expect(modelSlotConfig?.properties?.model?.minLength === 1, "ModelSlotConfig.model 必须设置 minLength=1");
expect(hasRequired(modelSlotConfig, "provider_id"), "ModelSlotConfig.required 必须包含 provider_id");
expect(hasRequired(modelSlotConfig, "model"), "ModelSlotConfig.required 必须包含 model");
expect(hasRequired(activeModelsInfo, "active_llm"), "ActiveModelsInfo.required 必须包含 active_llm");
expect(hasRequired(modelInfo, "id"), "ModelInfo.required 必须包含 id");
expect(hasRequired(modelInfo, "name"), "ModelInfo.required 必须包含 name");
expect(providerInfo?.properties?.display_name?.minLength === 1, "ProviderInfo.display_name 必须设置 minLength=1");
expect(hasRequired(providerInfo, "display_name"), "ProviderInfo.required 必须包含 display_name");
expect(providerInfo?.properties?.openai_compatible?.type === "boolean", "ProviderInfo.openai_compatible 必须是 boolean");
expect(hasRequired(providerInfo, "openai_compatible"), "ProviderInfo.required 必须包含 openai_compatible");
expect(providerInfo?.properties?.enabled?.type === "boolean", "ProviderInfo.enabled 必须是 boolean");
expect(hasRequired(providerInfo, "models"), "ProviderInfo.required 必须包含 models");
expect(hasRequired(providerTypeInfo, "id"), "ProviderTypeInfo.required 必须包含 id");
expect(hasRequired(providerTypeInfo, "display_name"), "ProviderTypeInfo.required 必须包含 display_name");
expect(providerConfigPatch?.properties?.timeout_ms?.minimum === 0, "ProviderConfigPatch.timeout_ms 必须设置 minimum=0");
expect(deleteResult?.properties?.deleted?.type === "boolean", "DeleteResult.deleted 必须是 boolean");
expect(hasRequired(deleteResult, "deleted"), "DeleteResult.required 必须包含 deleted");
expect(hasRequired(modelCatalogInfo, "providers"), "ModelCatalogInfo.required 必须包含 providers");
expect(hasRequired(modelCatalogInfo, "provider_types"), "ModelCatalogInfo.required 必须包含 provider_types");
expect(hasRequired(modelCatalogInfo, "defaults"), "ModelCatalogInfo.required 必须包含 defaults");
expect(hasRequired(modelCatalogInfo, "active_llm"), "ModelCatalogInfo.required 必须包含 active_llm");

expect(apiKeyAuth?.type === "apiKey", "必须声明 ApiKeyAuth 安全方案");
expect(apiKeyAuth?.in === "header", "ApiKeyAuth 必须位于 header");
expect(apiKeyAuth?.name === "X-API-Key", "ApiKeyAuth header 必须为 X-API-Key");

if (failures.length > 0) {
  console.error("OpenAPI lint failed:");
  for (const failure of failures) {
    console.error(`- ${failure}`);
  }
  process.exit(1);
}

console.log("OpenAPI lint passed");
