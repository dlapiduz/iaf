"use client";

import { useQuery } from "@tanstack/react-query";
import { listApplications } from "@/lib/api";
import { StatusBadge } from "@/components/status-badge";
import Link from "next/link";

export default function DashboardPage() {
  const { data: apps, isLoading } = useQuery({
    queryKey: ["applications"],
    queryFn: listApplications,
  });

  const running = apps?.filter((a) => a.phase === "Running").length ?? 0;
  const building = apps?.filter((a) => a.phase === "Building").length ?? 0;
  const failed = apps?.filter((a) => a.phase === "Failed").length ?? 0;
  const total = apps?.length ?? 0;

  return (
    <div className="p-8">
      <h1 className="text-2xl font-bold text-gray-900 mb-8">Dashboard</h1>

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
        <StatCard label="Total Apps" value={total} color="gray" />
        <StatCard label="Running" value={running} color="green" />
        <StatCard label="Building" value={building} color="blue" />
        <StatCard label="Failed" value={failed} color="red" />
      </div>

      {/* Recent Applications */}
      <div className="bg-white rounded-lg shadow">
        <div className="px-6 py-4 border-b border-gray-200 flex items-center justify-between">
          <h2 className="text-lg font-medium text-gray-900">
            Recent Applications
          </h2>
          <Link
            href="/applications"
            className="text-sm text-blue-600 hover:text-blue-800"
          >
            View all
          </Link>
        </div>
        <div className="divide-y divide-gray-200">
          {isLoading ? (
            <div className="px-6 py-4 text-gray-500 text-sm">Loading...</div>
          ) : apps && apps.length > 0 ? (
            apps.slice(0, 5).map((app) => (
              <Link
                key={app.name}
                href={`/applications/${app.name}`}
                className="block px-6 py-4 hover:bg-gray-50"
              >
                <div className="flex items-center justify-between">
                  <div>
                    <span className="font-medium text-gray-900">
                      {app.name}
                    </span>
                    {app.url && (
                      <span className="ml-3 text-sm text-gray-500">
                        {app.url}
                      </span>
                    )}
                  </div>
                  <StatusBadge phase={app.phase} />
                </div>
              </Link>
            ))
          ) : (
            <div className="px-6 py-8 text-center text-gray-500">
              <p>No applications deployed yet.</p>
              <p className="text-sm mt-1">
                Use the MCP server or API to deploy your first app.
              </p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  color,
}: {
  label: string;
  value: number;
  color: string;
}) {
  const colorMap: Record<string, string> = {
    gray: "bg-gray-50 text-gray-900",
    green: "bg-green-50 text-green-900",
    blue: "bg-blue-50 text-blue-900",
    red: "bg-red-50 text-red-900",
  };
  return (
    <div className={`rounded-lg p-6 ${colorMap[color] || colorMap.gray}`}>
      <p className="text-sm font-medium opacity-75">{label}</p>
      <p className="text-3xl font-bold mt-1">{value}</p>
    </div>
  );
}
