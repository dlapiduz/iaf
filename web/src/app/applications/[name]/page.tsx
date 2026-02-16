"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import {
  getApplication,
  getApplicationLogs,
  getApplicationBuild,
  deleteApplication,
} from "@/lib/api";
import { StatusBadge, BuildBadge } from "@/components/status-badge";
import { LogViewer } from "@/components/log-viewer";
import { useState } from "react";

export default function ApplicationDetailPage() {
  const params = useParams();
  const router = useRouter();
  const queryClient = useQueryClient();
  const name = params.name as string;
  const [activeTab, setActiveTab] = useState<"logs" | "build">("logs");

  const { data: app, isLoading } = useQuery({
    queryKey: ["application", name],
    queryFn: () => getApplication(name),
  });

  const { data: logsData, isLoading: logsLoading } = useQuery({
    queryKey: ["application-logs", name],
    queryFn: () => getApplicationLogs(name),
    enabled: activeTab === "logs",
  });

  const { data: buildData, isLoading: buildLoading } = useQuery({
    queryKey: ["application-build", name],
    queryFn: () => getApplicationBuild(name),
    enabled: activeTab === "build",
  });

  const deleteMutation = useMutation({
    mutationFn: () => deleteApplication(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["applications"] });
      router.push("/applications");
    },
  });

  if (isLoading) {
    return (
      <div className="p-8 text-gray-500">Loading application details...</div>
    );
  }

  if (!app) {
    return <div className="p-8 text-red-500">Application not found.</div>;
  }

  return (
    <div className="p-8">
      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">{app.name}</h1>
          <div className="flex items-center gap-3 mt-2">
            <StatusBadge phase={app.phase} />
            {app.buildStatus && <BuildBadge status={app.buildStatus} />}
          </div>
        </div>
        <button
          onClick={() => {
            if (confirm(`Delete application "${app.name}"?`)) {
              deleteMutation.mutate();
            }
          }}
          className="px-4 py-2 bg-red-600 text-white rounded-md text-sm hover:bg-red-700"
          disabled={deleteMutation.isPending}
        >
          {deleteMutation.isPending ? "Deleting..." : "Delete"}
        </button>
      </div>

      {/* Info Grid */}
      <div className="bg-white rounded-lg shadow mb-8">
        <div className="px-6 py-4 border-b border-gray-200">
          <h2 className="text-lg font-medium text-gray-900">Details</h2>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 p-6">
          <InfoRow label="URL" value={app.url || "-"} link={app.url} />
          <InfoRow label="Port" value={String(app.port)} />
          <InfoRow
            label="Replicas"
            value={`${app.availableReplicas}/${app.replicas}`}
          />
          <InfoRow label="Latest Image" value={app.latestImage || "-"} />
          {app.gitUrl && <InfoRow label="Git URL" value={app.gitUrl} />}
          {app.gitRevision && (
            <InfoRow label="Git Revision" value={app.gitRevision} />
          )}
          {app.image && <InfoRow label="Image" value={app.image} />}
          <InfoRow label="Created" value={app.createdAt} />
        </div>

        {/* Environment variables */}
        {app.env && app.env.length > 0 && (
          <div className="px-6 pb-6">
            <h3 className="text-sm font-medium text-gray-700 mb-2">
              Environment Variables
            </h3>
            <div className="bg-gray-50 rounded-md p-3">
              {app.env.map((e) => (
                <div key={e.name} className="text-sm font-mono">
                  <span className="text-gray-600">{e.name}</span>=
                  <span className="text-gray-900">{e.value}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Logs Tabs */}
      <div className="bg-white rounded-lg shadow">
        <div className="border-b border-gray-200">
          <nav className="flex -mb-px">
            <button
              onClick={() => setActiveTab("logs")}
              className={`px-6 py-3 text-sm font-medium border-b-2 ${
                activeTab === "logs"
                  ? "border-blue-500 text-blue-600"
                  : "border-transparent text-gray-500 hover:text-gray-700"
              }`}
            >
              Application Logs
            </button>
            <button
              onClick={() => setActiveTab("build")}
              className={`px-6 py-3 text-sm font-medium border-b-2 ${
                activeTab === "build"
                  ? "border-blue-500 text-blue-600"
                  : "border-transparent text-gray-500 hover:text-gray-700"
              }`}
            >
              Build Logs
            </button>
          </nav>
        </div>
        <div className="p-6">
          {activeTab === "logs" && (
            <LogViewer
              logs={logsData?.logs || ""}
              title={
                logsData?.podName ? `Pod: ${logsData.podName}` : undefined
              }
              loading={logsLoading}
            />
          )}
          {activeTab === "build" && (
            <LogViewer
              logs={buildData?.buildLogs || ""}
              title={`Build Status: ${buildData?.buildStatus || app.buildStatus || "N/A"}`}
              loading={buildLoading}
            />
          )}
        </div>
      </div>
    </div>
  );
}

function InfoRow({
  label,
  value,
  link,
}: {
  label: string;
  value: string;
  link?: string;
}) {
  return (
    <div>
      <dt className="text-sm font-medium text-gray-500">{label}</dt>
      <dd className="mt-1 text-sm text-gray-900">
        {link ? (
          <a
            href={link}
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 hover:text-blue-800"
          >
            {value}
          </a>
        ) : (
          value
        )}
      </dd>
    </div>
  );
}
