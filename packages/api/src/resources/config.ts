import type { AxiosInstance } from "axios";
import type { PartialDeep, RequiredDeep } from "type-fest";
import type { DelayRequest } from "./proxies/delay";
import type { DownloadRequest } from "./proxies/download";

export type ProxyServer = {
  expand?: boolean;
  nameservers?: string[];
  timeout?: number;
};

export type RuntimeConfig = {
  "default-timeout": number;
  "skip-cert-verify": boolean;
  "proxy-server": RequiredDeep<ProxyServer>;
  delay: RequiredDeep<DelayRequest>;
  download: RequiredDeep<DownloadRequest>;
};

export const createConfigApi = (instance: AxiosInstance) => {
  return {
    get: () => {
      return instance.get<RuntimeConfig>("/config");
    },
    update: (config: PartialDeep<RuntimeConfig>) => {
      return instance.patch<RuntimeConfig>("/config", config);
    }
  };
};
