import { create } from 'zustand';
import { usageApi } from '@/services/api';
import { useAuthStore } from '@/stores/useAuthStore';
import { collectUsageDetails, computeKeyStatsFromDetails, type KeyStats, type UsageDetail } from '@/utils/usage';
import { transformQueueRecords } from '@/services/api/usage';
import i18n from '@/i18n';

export const USAGE_STATS_STALE_TIME_MS = 240_000;

export type LoadUsageStatsOptions = {
  force?: boolean;
  staleTimeMs?: number;
};

type UsageStatsSnapshot = Record<string, unknown>;

type UsageStatsState = {
  usage: UsageStatsSnapshot | null;
  keyStats: KeyStats;
  usageDetails: UsageDetail[];
  loading: boolean;
  error: string | null;
  lastRefreshedAt: number | null;
  scopeKey: string;
  loadUsageStats: (options?: LoadUsageStatsOptions) => Promise<void>;
  clearUsageStats: () => void;
};

const createEmptyKeyStats = (): KeyStats => ({ bySource: {}, byAuthIndex: {} });

let usageRequestToken = 0;
let inFlightUsageRequest: { id: number; scopeKey: string; promise: Promise<void> } | null = null;

const getErrorMessage = (error: unknown) =>
  error instanceof Error
    ? error.message
    : typeof error === 'string'
      ? error
      : i18n.t('usage_stats.loading_error');

function mergeUsageSnapshots(
  live: UsageStatsSnapshot | null,
  history: UsageStatsSnapshot | null
): UsageStatsSnapshot | null {
  if (!live && !history) return null;
  if (!live) return history;
  if (!history) return live;

  const merged: UsageStatsSnapshot = { ...live };
  const liveApis = (live as Record<string, unknown>).apis as Record<string, Record<string, unknown>> | undefined;
  const histApis = (history as Record<string, unknown>).apis as Record<string, Record<string, unknown>> | undefined;

  if (!histApis) return merged;
  if (!liveApis) {
    merged.apis = histApis;
    return merged;
  }

  const resultApis: Record<string, Record<string, unknown>> = { ...liveApis };

  for (const [endpoint, histEndpoint] of Object.entries(histApis)) {
    if (!resultApis[endpoint]) {
      resultApis[endpoint] = histEndpoint;
      continue;
    }
    const liveEntry = resultApis[endpoint] as Record<string, unknown>;
    const liveModels = liveEntry?.models as Record<string, { details: unknown[] }> | undefined;
    const histModels = histEndpoint?.models as Record<string, { details: unknown[] }> | undefined;
    if (!liveModels || !histModels) continue;

    for (const [model, histModel] of Object.entries(histModels)) {
      if (!liveModels[model]) {
        liveModels[model] = histModel;
        continue;
      }
      // Merge details, deduplicate by (timestamp, source).
      const existingKeys = new Set(
        liveModels[model].details.map((d) => {
          const rec = d as Record<string, unknown>;
          return `${rec.timestamp}::${rec.source ?? ''}`;
        })
      );
      for (const detail of histModel.details) {
        const rec = detail as Record<string, unknown>;
        const key = `${rec.timestamp}::${rec.source ?? ''}`;
        if (!existingKeys.has(key)) {
          liveModels[model].details.push(detail);
          existingKeys.add(key);
        }
      }
    }
  }

  merged.apis = resultApis;
  return merged;
}

export const useUsageStatsStore = create<UsageStatsState>((set, get) => ({
  usage: null,
  keyStats: createEmptyKeyStats(),
  usageDetails: [],
  loading: false,
  error: null,
  lastRefreshedAt: null,
  scopeKey: '',

  loadUsageStats: async (options = {}) => {
    const force = options.force === true;
    const staleTimeMs = options.staleTimeMs ?? USAGE_STATS_STALE_TIME_MS;
    const { apiBase = '', managementKey = '' } = useAuthStore.getState();
    const scopeKey = `${apiBase}::${managementKey}`;
    const state = get();
    const scopeChanged = state.scopeKey !== scopeKey;

    // 先复用同源 in-flight 请求，避免多个页面同时发起重复 /usage。
    if (inFlightUsageRequest && inFlightUsageRequest.scopeKey === scopeKey) {
      await inFlightUsageRequest.promise;
      return;
    }

    // 连接目标变化时，旧请求结果必须失效。
    if (inFlightUsageRequest && inFlightUsageRequest.scopeKey !== scopeKey) {
      usageRequestToken += 1;
      inFlightUsageRequest = null;
    }

    const fresh =
      !scopeChanged &&
      state.lastRefreshedAt !== null &&
      Date.now() - state.lastRefreshedAt < staleTimeMs;

    if (!force && fresh) {
      return;
    }

    if (scopeChanged) {
      set({
        usage: null,
        keyStats: createEmptyKeyStats(),
        usageDetails: [],
        error: null,
        lastRefreshedAt: null,
        scopeKey
      });
    }

    const requestId = (usageRequestToken += 1);
    set({ loading: true, error: null, scopeKey });

    const requestPromise = (async () => {
      try {
        // Fetch live queue and history in parallel.
        const [usageResponse, historyRecords] = await Promise.all([
          usageApi.getUsage(),
          usageApi.getUsageHistory().catch(() => [] as Awaited<ReturnType<typeof usageApi.getUsageHistory>>)
        ]);

        const rawUsage = usageResponse?.usage ?? usageResponse;
        const liveUsage =
          rawUsage && typeof rawUsage === 'object' ? (rawUsage as UsageStatsSnapshot) : null;

        // Transform history records into the same nested format as live data.
        const historySnapshot = historyRecords.length > 0
          ? (transformQueueRecords(historyRecords) as UsageStatsSnapshot)
          : null;

        const merged = mergeUsageSnapshots(liveUsage, historySnapshot);

        if (requestId !== usageRequestToken) return;

        const usageDetails = collectUsageDetails(merged);
        set({
          usage: merged,
          keyStats: computeKeyStatsFromDetails(usageDetails),
          usageDetails,
          loading: false,
          error: null,
          lastRefreshedAt: Date.now(),
          scopeKey
        });
      } catch (error: unknown) {
        if (requestId !== usageRequestToken) return;
        const message = getErrorMessage(error);
        set({
          loading: false,
          error: message,
          scopeKey
        });
        throw new Error(message);
      } finally {
        if (inFlightUsageRequest?.id === requestId) {
          inFlightUsageRequest = null;
        }
      }
    })();

    inFlightUsageRequest = { id: requestId, scopeKey, promise: requestPromise };
    await requestPromise;
  },

  clearUsageStats: () => {
    usageRequestToken += 1;
    inFlightUsageRequest = null;
    set({
      usage: null,
      keyStats: createEmptyKeyStats(),
      usageDetails: [],
      loading: false,
      error: null,
      lastRefreshedAt: null,
      scopeKey: ''
    });
  }
}));
