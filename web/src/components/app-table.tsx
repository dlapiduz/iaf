"use client";

import Link from "next/link";
import { Application } from "@/lib/api";
import { StatusBadge, BuildBadge } from "./status-badge";

interface AppTableProps {
  applications: Application[];
}

function sourceType(app: Application): string {
  if (app.image) return "Image";
  if (app.gitUrl) return "Git";
  if (app.blob) return "Code";
  return "Unknown";
}

export function AppTable({ applications }: AppTableProps) {
  if (applications.length === 0) {
    return (
      <div className="text-center py-12 text-gray-500">
        <p className="text-lg">No applications deployed</p>
        <p className="text-sm mt-1">
          Deploy an app via the API or MCP server to get started.
        </p>
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="min-w-full divide-y divide-gray-200">
        <thead className="bg-gray-50">
          <tr>
            <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
              Name
            </th>
            <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
              Status
            </th>
            <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
              Source
            </th>
            <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
              Build
            </th>
            <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
              Replicas
            </th>
            <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
              URL
            </th>
          </tr>
        </thead>
        <tbody className="bg-white divide-y divide-gray-200">
          {applications.map((app) => (
            <tr key={app.name} className="hover:bg-gray-50">
              <td className="px-6 py-4 whitespace-nowrap">
                <Link
                  href={`/applications/${app.name}`}
                  className="text-blue-600 hover:text-blue-800 font-medium"
                >
                  {app.name}
                </Link>
              </td>
              <td className="px-6 py-4 whitespace-nowrap">
                <StatusBadge phase={app.phase} />
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                {sourceType(app)}
              </td>
              <td className="px-6 py-4 whitespace-nowrap">
                <BuildBadge status={app.buildStatus} />
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                {app.availableReplicas}/{app.replicas}
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm">
                {app.url ? (
                  <a
                    href={app.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-blue-600 hover:text-blue-800"
                  >
                    {app.url}
                  </a>
                ) : (
                  <span className="text-gray-400">-</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
