export type DelayItem = {
  url: string;
  timeout?: number;
  headers?: Record<string, string>;
  "follow-redirect"?: boolean;
  expected?: string;
  rounds?: number;
  unified?: boolean;
};

export type DelayRequest = Omit<DelayItem, "url"> & {
  urls?: DelayItem[];
  concurrency?: number;
};

export type DelayResult = {
  version: number;
  digest: string;
  "delay-min": number;
  "delay-max": number;
  "delay-avg": number;
  "delay-cost": number;
  success: number;
  failed: number;
  total: number;
  error?: string;
};

export type DelaysResult = {
  version: number;
  results: Omit<DelayResult, "version">[];
};
