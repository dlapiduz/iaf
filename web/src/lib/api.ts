const API_BASE = "/api/v1";

export interface Application {
  name: string;
  phase: string;
  url: string;
  image?: string;
  gitUrl?: string;
  gitRevision?: string;
  blob?: string;
  port: number;
  replicas: number;
  availableReplicas: number;
  latestImage?: string;
  buildStatus?: string;
  env?: { name: string; value: string }[];
  host?: string;
  conditions?: {
    type: string;
    status: string;
    reason: string;
    message: string;
  }[];
  createdAt: string;
}

export interface LogsResponse {
  logs: string;
  pods: number;
  podName?: string;
}

export interface BuildResponse {
  buildLogs: string;
  buildStatus: string;
  podName?: string;
}

async function fetchWithAuth(url: string, options?: RequestInit) {
  const apiKey =
    typeof window !== "undefined"
      ? localStorage.getItem("iaf_api_key") || "iaf-dev-key"
      : "iaf-dev-key";

  const res = await fetch(url, {
    ...options,
    headers: {
      ...options?.headers,
      Authorization: `Bearer ${apiKey}`,
      "Content-Type": "application/json",
    },
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `HTTP ${res.status}`);
  }

  return res.json();
}

export async function listApplications(): Promise<Application[]> {
  return fetchWithAuth(`${API_BASE}/applications`);
}

export async function getApplication(name: string): Promise<Application> {
  return fetchWithAuth(`${API_BASE}/applications/${name}`);
}

export async function deleteApplication(name: string): Promise<void> {
  await fetchWithAuth(`${API_BASE}/applications/${name}`, {
    method: "DELETE",
  });
}

export async function getApplicationLogs(
  name: string,
  lines = 100
): Promise<LogsResponse> {
  return fetchWithAuth(`${API_BASE}/applications/${name}/logs?lines=${lines}`);
}

export async function getApplicationBuild(
  name: string
): Promise<BuildResponse> {
  return fetchWithAuth(`${API_BASE}/applications/${name}/build`);
}
