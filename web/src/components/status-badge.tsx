"use client";

import { cn } from "@/lib/utils";

const phaseColors: Record<string, string> = {
  Running: "bg-green-100 text-green-800",
  Building: "bg-blue-100 text-blue-800",
  Deploying: "bg-yellow-100 text-yellow-800",
  Pending: "bg-gray-100 text-gray-800",
  Failed: "bg-red-100 text-red-800",
};

export function StatusBadge({ phase }: { phase: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium",
        phaseColors[phase] || "bg-gray-100 text-gray-800"
      )}
    >
      {phase || "Unknown"}
    </span>
  );
}

const buildColors: Record<string, string> = {
  Succeeded: "bg-green-100 text-green-800",
  Building: "bg-blue-100 text-blue-800",
  Failed: "bg-red-100 text-red-800",
};

export function BuildBadge({ status }: { status?: string }) {
  if (!status) return null;
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium",
        buildColors[status] || "bg-gray-100 text-gray-800"
      )}
    >
      {status}
    </span>
  );
}
