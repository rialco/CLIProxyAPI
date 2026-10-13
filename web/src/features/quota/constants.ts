import type { QuotaProviderType } from './providers/types';

/** Tab order also determines provider grouping on the All tab. */
export const QUOTA_TAB_ORDER: readonly QuotaProviderType[] = [
  'claude',
  'antigravity',
  'codex',
  'xai',
  'kimi',
  'devin',
  'meta',
];

export type QuotaTabId = 'all' | QuotaProviderType;

/** Use 20 entries per page, also limiting refresh-all upstream concurrency to 20. */
export const QUOTA_PAGE_SIZE = 20;

/** Default sorting groups providers; soonest sorting prioritizes recovery time. */
export const QUOTA_SORT_MODES = ['default', 'soonest'] as const;

export type QuotaSortMode = (typeof QUOTA_SORT_MODES)[number];

/** Match the reveal hook's 360 ms total animation budget. */
export const CARD_ENTRANCE_BUDGET_MS = 360;
