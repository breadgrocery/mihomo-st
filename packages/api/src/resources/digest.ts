import type { AxiosInstance } from "axios";

export type DigestRequest = Record<string, unknown>;

export type DigestResponse = {
  digest: string;
};

export const createDigestApi = (instance: AxiosInstance) => {
  return {
    create: (request: DigestRequest) => {
      return instance.post<DigestResponse>("/digest", request);
    }
  };
};
