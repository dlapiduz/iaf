import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";
import { Providers } from "./providers";

export const metadata: Metadata = {
  title: "IAF Dashboard",
  description: "Intelligent Application Fabric - Application Dashboard",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className="bg-gray-50 min-h-screen">
        <Providers>
          <div className="flex min-h-screen">
            {/* Sidebar */}
            <aside className="w-64 bg-gray-900 text-white flex flex-col">
              <div className="p-6 border-b border-gray-800">
                <h1 className="text-xl font-bold">IAF</h1>
                <p className="text-xs text-gray-400 mt-1">
                  Intelligent Application Fabric
                </p>
              </div>
              <nav className="flex-1 p-4">
                <ul className="space-y-1">
                  <li>
                    <Link
                      href="/"
                      className="block px-4 py-2 rounded-md text-sm text-gray-300 hover:bg-gray-800 hover:text-white"
                    >
                      Dashboard
                    </Link>
                  </li>
                  <li>
                    <Link
                      href="/applications"
                      className="block px-4 py-2 rounded-md text-sm text-gray-300 hover:bg-gray-800 hover:text-white"
                    >
                      Applications
                    </Link>
                  </li>
                </ul>
              </nav>
              <div className="p-4 border-t border-gray-800 text-xs text-gray-500">
                v0.1.0
              </div>
            </aside>

            {/* Main content */}
            <main className="flex-1 overflow-auto">{children}</main>
          </div>
        </Providers>
      </body>
    </html>
  );
}
