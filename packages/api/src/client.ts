import { type HttpOptions, createHttp } from "./http";
import { createConfigApi } from "./resources/config";
import { createDigestApi } from "./resources/digest";
import { createProxiesApi } from "./resources/proxies";
import { createVersionApi } from "./resources/version";

export const createApiClient = (options: HttpOptions) => {
  const http = createHttp(options);
  return {
    version: createVersionApi(http),
    digest: createDigestApi(http),
    config: createConfigApi(http),
    proxies: createProxiesApi(http)
  };
};
