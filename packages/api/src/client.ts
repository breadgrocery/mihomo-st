import { type HttpOptions, createHttp } from "./http";
import { createConfigApi } from "./resources/config";
import { createDigestApi } from "./resources/digest";
import { createProxiesApi } from "./resources/proxies";
import { createVersionApi } from "./resources/version";

export const createApiClient = (options?: HttpOptions) => {
  const http = createHttp(options);
  return {
    config: createConfigApi(http),
    digest: createDigestApi(http),
    proxies: createProxiesApi(http),
    version: createVersionApi(http)
  };
};
