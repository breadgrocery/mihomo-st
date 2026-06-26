import type { AxiosInstance } from "axios";
import type { ProxyServer } from "../config";
import type { DelayRequest, DelayResult, DelaysResult } from "./delay";
import type { DownloadRequest, DownloadResult, DownloadsResult } from "./download";

export type ProxyInfo = {
  type: string;
  name: string;
  server: string;
  port: number;
  digest: string;
};

export type ProxyListResult = {
  version: number;
  proxies: ProxyInfo[];
};

export type ProxyImportRequest = {
  type: "text" | "local" | "remote";
  payload: string;
  mode?: "replace" | "append";
  headers?: Record<string, string>;
  timeout?: number;
  "follow-redirect"?: boolean;
  "proxy-server"?: ProxyServer;
};

export type ProxyImportResult = ProxyListResult & {
  warnings?: Record<string, unknown>[];
};

export type ProxyExportResult = {
  proxies: Record<string, unknown>[];
};

export type ProxyRequest = {
  url: string;
  method?: "GET" | "DELETE" | "HEAD" | "OPTIONS" | "POST" | "PUT" | "PATCH";
  headers?: Record<string, string>;
  timeout?: number;
  "follow-redirect"?: boolean;
  body?: string;
};

export const createProxiesApi = (instance: AxiosInstance) => {
  return {
    list: () => {
      return instance.get<ProxyListResult>("/proxies");
    },
    import: (request: ProxyImportRequest) => {
      return instance.post<ProxyImportResult>("/proxies/import", request);
    },
    export: () => {
      return instance.get<ProxyExportResult>("/proxies/export");
    },
    delay: (digest: string, request?: DelayRequest) => {
      const path = `/proxies/${digest}/delay`;
      return instance.post<DelayResult>(path, request);
    },
    download: (digest: string, request?: DownloadRequest) => {
      const path = `/proxies/${digest}/download`;
      return instance.post<DownloadResult>(path, request);
    },
    delays: (request?: DelayRequest) => {
      return instance.post<DelaysResult>("/proxies/delay", request);
    },
    downloads: (request?: DownloadRequest) => {
      return instance.post<DownloadsResult>("/proxies/download", request);
    },
    proxy: (digest: string, request: ProxyRequest) => {
      const path = `/proxies/${digest}/proxy`;
      return instance.post(path, request);
    }
  };
};
