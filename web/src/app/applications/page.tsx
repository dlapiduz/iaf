"use client";

import { useQuery } from "@tanstack/react-query";
import { listApplications } from "@/lib/api";
import { AppTable } from "@/components/app-table";

export default function ApplicationsPage() {
  const {
    data: apps,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["applications"],
    queryFn: listApplications,
  });

  return (
    <div className="p-8">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-gray-900">Applications</h1>
      </div>

      <div className="bg-white rounded-lg shadow">
        {isLoading ? (
          <div className="p-8 text-center text-gray-500">
            Loading applications...
          </div>
        ) : error ? (
          <div className="p-8 text-center text-red-500">
            Error: {(error as Error).message}
          </div>
        ) : (
          <AppTable applications={apps || []} />
        )}
      </div>
    </div>
  );
}
