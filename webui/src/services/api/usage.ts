/**
 * 使用统计相关 API
 */

import { apiClient } from './client';
import { computeKeyStats, KeyStats } from '@/utils/usage';

const USAGE_TIMEOUT_MS = 60 * 1000;

interface QueueRecord {
  provider?: string;
  model?: string;
  alias?: string;
  endpoint?: string;
  auth_type?: string;
  api_key?: string;
  request_id?: string;
  reasoning_effort?: string;
  timestamp?: string;
  latency_ms?: number;
  source?: string;
  auth_index?: string;
  tokens?: {
    input_tokens?: number;
    output_tokens?: number;
    reasoning_tokens?: number;
    cached_tokens?: number;
    cache_read_tokens?: number;
    cache_creation_tokens?: number;
    total_tokens?: number;
  };
  failed?: boolean;
  fail?: { status_code?: number; body?: string };
  response_headers?: Record<string, string[]>;
}

const isQueueRecord = (value: unknown): value is QueueRecord =>
  value !== null && typeof value === 'object';

/**
 * 将 usage-queue 原始记录转换为前端期望的格式
 */
export function transformQueueRecords(records: unknown[]): Record<string, unknown> {
  const apis: Record<string, Record<string, unknown>> = {};

  for (const raw of records) {
    if (!isQueueRecord(raw)) continue;

    const provider = (raw.provider || 'unknown').toLowerCase();
    const model = raw.model || raw.alias || 'unknown';
    const rawEndpoint = raw.endpoint?.trim();
    const endpoint = rawEndpoint || `POST /v1/${provider}/chat/completions`;

    if (!apis[endpoint]) {
      apis[endpoint] = { models: {} };
    }
    const endpointData = apis[endpoint] as Record<string, Record<string, unknown>>;
    if (!endpointData.models[model]) {
      endpointData.models[model] = { details: [] };
    }
    const modelData = endpointData.models[model] as { details: unknown[] };

    const tokens = raw.tokens;
    modelData.details.push({
      timestamp: raw.timestamp || new Date().toISOString(),
      source: raw.source || '',
      auth_index: raw.auth_index ?? null,
      latency_ms: typeof raw.latency_ms === 'number' ? raw.latency_ms : undefined,
      tokens: {
        input_tokens: tokens?.input_tokens ?? 0,
        output_tokens: tokens?.output_tokens ?? 0,
        reasoning_tokens: tokens?.reasoning_tokens ?? 0,
        cached_tokens: tokens?.cached_tokens ?? 0,
        cache_tokens: tokens?.cache_read_tokens ?? 0,
        total_tokens: tokens?.total_tokens ?? 0,
      },
      failed: raw.failed === true,
    });
  }

  return { apis };
}

export interface UsageExportPayload {
  version?: number;
  exported_at?: string;
  usage?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface UsageImportResponse {
  added?: number;
  skipped?: number;
  total_requests?: number;
  failed_requests?: number;
  [key: string]: unknown;
}

export const usageApi = {
  /**
   * 获取使用统计原始数据
   * 从 usage-queue 获取记录并转换为前端期望的格式
   */
  getUsage: async (): Promise<Record<string, unknown>> => {
    const records = await apiClient.get<unknown[]>('/usage-queue?count=10000', { timeout: USAGE_TIMEOUT_MS });
    return transformQueueRecords(records);
  },

  /**
   * 导出使用统计快照
   */
  exportUsage: () => apiClient.get<UsageExportPayload>('/usage/export', { timeout: USAGE_TIMEOUT_MS }),

  /**
   * 导入使用统计快照
   */
  importUsage: (payload: unknown) =>
    apiClient.post<UsageImportResponse>('/usage/import', payload, { timeout: USAGE_TIMEOUT_MS }),

  /**
   * Get persistent usage history from JSONL storage
   */
  getUsageHistory: async (): Promise<QueueRecord[]> => {
    const response = await apiClient.get<{ records: unknown[] }>('/usage/history', { timeout: USAGE_TIMEOUT_MS });
    const raw = response?.records ?? [];
    const records: QueueRecord[] = [];
    for (const item of raw) {
      if (isQueueRecord(item)) {
        records.push(item as QueueRecord);
      }
    }
    return records;
  },

  /**
   * 计算密钥成功/失败统计，必要时会先获取 usage 数据
   */
  async getKeyStats(usageData?: unknown): Promise<KeyStats> {
    let payload = usageData;
    if (!payload) {
      payload = await this.getUsage();
    }
    return computeKeyStats(payload);
  }
};
