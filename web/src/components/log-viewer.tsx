"use client";

import { useEffect, useRef } from "react";

interface LogViewerProps {
  logs: string;
  title?: string;
  loading?: boolean;
}

export function LogViewer({ logs, title, loading }: LogViewerProps) {
  const containerRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logs]);

  return (
    <div className="rounded-lg border border-gray-200 overflow-hidden">
      {title && (
        <div className="bg-gray-50 px-4 py-2 border-b border-gray-200 flex items-center justify-between">
          <h3 className="text-sm font-medium text-gray-700">{title}</h3>
          {loading && (
            <span className="text-xs text-blue-600 animate-pulse">
              Loading...
            </span>
          )}
        </div>
      )}
      <pre
        ref={containerRef}
        className="bg-gray-900 text-gray-100 p-4 text-xs font-mono overflow-auto max-h-96 whitespace-pre-wrap"
      >
        {logs || (loading ? "Loading logs..." : "No logs available.")}
      </pre>
    </div>
  );
}
