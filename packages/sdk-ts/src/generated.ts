/* eslint-disable */
// This file is auto-generated from packages/contracts/openapi/openapi.yaml.
// Do not edit manually.

export const OPENAPI_VERSION = "3.0.3" as const;

export type APIPath = "/agent/process" | "/channels/qq/inbound" | "/channels/qq/state" | "/chats" | "/chats/{chat_id}" | "/chats/batch-delete" | "/config/channels" | "/config/channels/{channel_name}" | "/config/channels/types" | "/cron/jobs" | "/cron/jobs/{job_id}" | "/cron/jobs/{job_id}/pause" | "/cron/jobs/{job_id}/resume" | "/cron/jobs/{job_id}/run" | "/cron/jobs/{job_id}/state" | "/envs" | "/envs/{key}" | "/healthz" | "/models" | "/models/{provider_id}" | "/models/{provider_id}/config" | "/models/active" | "/models/catalog" | "/skills" | "/skills/{skill_name}" | "/skills/{skill_name}/disable" | "/skills/{skill_name}/enable" | "/skills/{skill_name}/files/{source}/{file_path}" | "/skills/available" | "/skills/batch-disable" | "/skills/batch-enable" | "/version" | "/workspace/export" | "/workspace/files" | "/workspace/files/{file_path}" | "/workspace/import";

export type APIMethodByPath = {
  "/agent/process": "post";
  "/channels/qq/inbound": "post";
  "/channels/qq/state": "get";
  "/chats": "get" | "post";
  "/chats/{chat_id}": "delete" | "get" | "put";
  "/chats/batch-delete": "post";
  "/config/channels": "get" | "put";
  "/config/channels/{channel_name}": "get" | "put";
  "/config/channels/types": "get";
  "/cron/jobs": "get" | "post";
  "/cron/jobs/{job_id}": "delete" | "get" | "put";
  "/cron/jobs/{job_id}/pause": "post";
  "/cron/jobs/{job_id}/resume": "post";
  "/cron/jobs/{job_id}/run": "post";
  "/cron/jobs/{job_id}/state": "get";
  "/envs": "get" | "put";
  "/envs/{key}": "delete";
  "/healthz": "get";
  "/models": "get";
  "/models/{provider_id}": "delete";
  "/models/{provider_id}/config": "put";
  "/models/active": "get" | "put";
  "/models/catalog": "get";
  "/skills": "get" | "post";
  "/skills/{skill_name}": "delete";
  "/skills/{skill_name}/disable": "post";
  "/skills/{skill_name}/enable": "post";
  "/skills/{skill_name}/files/{source}/{file_path}": "get";
  "/skills/available": "get";
  "/skills/batch-disable": "post";
  "/skills/batch-enable": "post";
  "/version": "get";
  "/workspace/export": "get";
  "/workspace/files": "get";
  "/workspace/files/{file_path}": "delete" | "get" | "put";
  "/workspace/import": "post";
};

export interface APIErrorEnvelope {
  error: {
    code: string;
    message: string;
    details?: unknown;
  };
}
