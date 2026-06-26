import axios from "axios";

export interface HttpOptions {
  baseURL?: string;
  timeout?: number;
}

export const createHttp = (options?: HttpOptions) => {
  const instance = axios.create({
    baseURL: options?.baseURL || "http://127.0.0.1:32198",
    timeout: options?.timeout
  });
  return instance;
};
