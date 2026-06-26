import type { AxiosInstance } from "axios";

export type VersionInfo = {
  name: string;
  version: string;
};

export const createVersionApi = (instance: AxiosInstance) => {
  return {
    get: () => {
      return instance.get<VersionInfo>("/version");
    }
  };
};
