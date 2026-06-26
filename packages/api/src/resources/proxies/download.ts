export type DownloadItem = {
  url: string;
  timeout?: number;
  headers?: Record<string, string>;
  "follow-redirect"?: boolean;
  rounds?: number;
  "max-bytes"?: number;
};

export type DownloadRequest = Omit<DownloadItem, "url"> & {
  urls?: DownloadItem[];
  concurrency?: number;
};

export type DownloadResult = {
  digest: string;
  "speed-min": number;
  "speed-max": number;
  "speed-avg": number;
  "speed-score": number;
  success: number;
  failed: number;
  total: number;
  error?: string;
};

export type DownloadsResult = {
  version: number;
  results: Omit<DownloadResult, "version">[];
};
