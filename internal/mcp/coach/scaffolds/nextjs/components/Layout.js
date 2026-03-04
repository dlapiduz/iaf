import Head from 'next/head';
import Link from 'next/link';

export default function Layout({ children, title = 'My App' }) {
  return (
    <>
      <Head>
        <title>{title}</title>
        <meta name="viewport" content="width=device-width, initial-scale=1" />
      </Head>
      <div className="min-h-screen flex flex-col bg-gray-50">
        <header className="bg-white border-b border-gray-200 shadow-sm">
          <div className="max-w-5xl mx-auto px-8 py-4 flex items-center justify-between">
            <Link href="/" className="text-lg font-semibold text-gray-900 hover:text-blue-600">
              My App
            </Link>
            <nav className="flex gap-6 text-sm">
              <Link href="/" className="text-gray-600 hover:text-gray-900">Home</Link>
            </nav>
          </div>
        </header>
        <main className="flex-1 max-w-5xl mx-auto w-full px-8 py-8">
          {children}
        </main>
        <footer className="bg-white border-t border-gray-200 text-center text-sm text-gray-500 py-4">
          Built with IAF
        </footer>
      </div>
    </>
  );
}
